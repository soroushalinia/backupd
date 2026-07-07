package storage

import (
	"context"
	"fmt"
	"io"
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
	_, err := s.client.PutObject(ctx, s.bucket, s.key(key), r, -1,
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
