package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/tags"
	"github.com/soroushalinia/backupd/internal/config"
)

type S3Client struct {
	client *minio.Client
	bucket string
	prefix string
}

type S3Config struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
	Prefix    string
	Secure    bool
}

func NewS3(cfg S3Config) (*S3Client, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("creating s3 client: %w", err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("checking bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("creating bucket: %w", err)
		}
	}

	return &S3Client{
		client: client,
		bucket: cfg.Bucket,
		prefix: strings.TrimSuffix(cfg.Prefix, "/"),
	}, nil
}

func (s *S3Client) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return s.prefix + "/" + k
}

func (s *S3Client) Upload(ctx context.Context, key string, r io.Reader) error {
	size := int64(-1)
	if seeker, ok := r.(io.Seeker); ok {
		if end, err := seeker.Seek(0, io.SeekEnd); err == nil {
			if _, err := seeker.Seek(0, io.SeekStart); err == nil {
				size = end
			}
		}
	}

	_, err := s.client.PutObject(ctx, s.bucket, s.key(key), r, size,
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return fmt.Errorf("uploading %q: %w", key, err)
	}
	return nil
}

func (s *S3Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	fullKey := s.key(key)
	_, err := s.client.StatObject(ctx, s.bucket, fullKey, minio.StatObjectOptions{})
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %q: %w", key, err)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, fullKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("downloading %q: %w", key, err)
	}
	return obj, nil
}

func (s *S3Client) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, s.key(key), minio.RemoveObjectOptions{})
}

func (s *S3Client) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	opts := minio.ListObjectsOptions{Prefix: s.key(prefix)}
	var objects []ObjectInfo
	for obj := range s.client.ListObjects(ctx, s.bucket, opts) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		objects = append(objects, ObjectInfo{
			Key:          strings.TrimPrefix(obj.Key, s.prefix+"/"),
			Size:         obj.Size,
			LastModified: obj.LastModified.Format("2006-01-02T15:04:05Z"),
		})
	}
	return objects, nil
}

func (s *S3Client) SetTags(ctx context.Context, key string, tagMap map[string]string) error {
	if len(tagMap) == 0 {
		return nil
	}
	otags, err := tags.NewTags(tagMap, false)
	if err != nil {
		return err
	}
	return s.client.PutObjectTagging(ctx, s.bucket, s.key(key), otags, minio.PutObjectTaggingOptions{})
}

func NewFromDest(dest config.Destination) (*S3Client, error) {
	secure := true
	if dest.Secure != nil {
		secure = *dest.Secure
	}
	return NewS3(S3Config{
		Endpoint:  dest.Endpoint,
		Region:    dest.Region,
		AccessKey: dest.AccessKey,
		SecretKey: dest.SecretKey,
		Bucket:    dest.Bucket,
		Prefix:    dest.Prefix,
		Secure:    secure,
	})
}

func (s *S3Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, s.key(key), minio.StatObjectOptions{})
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// multipartPartSize is the S3 part size for streaming uploads. S3 requires
// every part except the last to be at least 5 MiB.
const multipartPartSize = 8 * 1024 * 1024

// UploadMultipart streams an unknown-size reader to S3 as a multipart
// upload with fixed-size parts, keeping memory bounded by one part. The
// upload is aborted (not left dangling) on any failure.
func (s *S3Client) UploadMultipart(ctx context.Context, key string, r io.Reader) error {
	core := minio.Core{Client: s.client}
	objKey := s.key(key)

	uploadID, err := core.NewMultipartUpload(ctx, s.bucket, objKey, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return fmt.Errorf("creating multipart upload %q: %w", key, err)
	}

	abort := func() {
		if err := core.AbortMultipartUpload(ctx, s.bucket, objKey, uploadID); err != nil {
			log.Printf("warning: aborting multipart upload %s: %v", objKey, err)
		}
	}

	var parts []minio.CompletePart
	partID := 1
	buf := make([]byte, multipartPartSize)
	for {
		n, err := io.ReadFull(r, buf)
		if n == 0 && err == io.EOF {
			break
		}
		if n == 0 && err != nil {
			abort()
			return fmt.Errorf("reading for multipart upload %q: %w", key, err)
		}

		part, perr := core.PutObjectPart(ctx, s.bucket, objKey, uploadID, partID, bytes.NewReader(buf[:n]), int64(n), minio.PutObjectPartOptions{})
		if perr != nil {
			abort()
			return fmt.Errorf("uploading part %d of %q: %w", partID, key, perr)
		}
		parts = append(parts, minio.CompletePart{PartNumber: partID, ETag: part.ETag})
		partID++

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
	}

	if _, err := core.CompleteMultipartUpload(ctx, s.bucket, objKey, uploadID, parts, minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
		abort()
		return fmt.Errorf("completing multipart upload %q: %w", key, err)
	}
	return nil
}
