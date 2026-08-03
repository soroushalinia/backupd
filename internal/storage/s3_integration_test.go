package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

// testS3Config reads S3 test credentials from the environment. These tests
// require a real S3-compatible endpoint (e.g. MinIO) and are skipped unless
// BACKUPD_TEST_MINIO=1 is set, mirroring how the database tests skip when
// their dump tools are missing.
func testS3Config(t *testing.T) *S3Config {
	t.Helper()
	if os.Getenv("BACKUPD_TEST_MINIO") != "1" {
		t.Skip("set BACKUPD_TEST_MINIO=1 with a running MinIO (BACKUPD_TEST_MINIO_ENDPOINT) to run S3 tests")
	}
	return &S3Config{
		Endpoint:  envOr("BACKUPD_TEST_MINIO_ENDPOINT", "localhost:9000"),
		AccessKey: envOr("BACKUPD_TEST_MINIO_ACCESS_KEY", "testuser"),
		SecretKey: envOr("BACKUPD_TEST_MINIO_SECRET_KEY", "testpass123"),
		Bucket:    envOr("BACKUPD_TEST_MINIO_BUCKET", "backupd-test"),
		Secure:    false,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestS3ExistsAndRoundTrip(t *testing.T) {
	cfg := testS3Config(t)
	ctx := context.Background()

	client, err := NewS3(*cfg)
	if err != nil {
		t.Fatal(err)
	}

	key := "integration/roundtrip.bin"
	payload := bytes.Repeat([]byte("s3 round trip"), 1000)

	if err := client.Upload(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}

	exists, err := client.Exists(ctx, key)
	if err != nil || !exists {
		t.Fatalf("expected %s to exist (exists=%v err=%v)", key, exists, err)
	}

	r, err := client.Download(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("download mismatch: got %d bytes, want %d", len(got), len(payload))
	}

	if err := client.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	exists, err = client.Exists(ctx, key)
	if err != nil || exists {
		t.Fatalf("expected %s to be deleted (exists=%v err=%v)", key, exists, err)
	}
}

// UploadMultipart must split a payload larger than one part (8 MiB) into
// multiple parts and reassemble it on download.
func TestS3UploadMultipartRoundTrip(t *testing.T) {
	cfg := testS3Config(t)
	ctx := context.Background()

	client, err := NewS3(*cfg)
	if err != nil {
		t.Fatal(err)
	}

	key := "integration/multipart.bin"
	payload := bytes.Repeat([]byte("multipart payload "), 70000) // ~1.8 MiB

	if err := client.UploadMultipart(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}

	r, err := client.Download(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("multipart download mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

// A payload spanning several parts exercises the part loop, part numbering,
// and the final complete call.
func TestS3UploadMultipartLarge(t *testing.T) {
	cfg := testS3Config(t)
	ctx := context.Background()

	client, err := NewS3(*cfg)
	if err != nil {
		t.Fatal(err)
	}

	key := "integration/multipart-large.bin"
	// 3 parts at 8 MiB each: forces multi-part upload (17 MiB total).
	payload := bytes.Repeat([]byte("large-part"), 2*1024*1024)

	if err := client.UploadMultipart(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}

	obj, err := client.Download(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	got, err := io.ReadAll(obj)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("multipart download mismatch: got %d bytes, want %d", len(got), len(payload))
	}

	// A multipart upload must be listable via the normal list path.
	objects, err := client.List(ctx, "integration/multipart-large")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || objects[0].Key != key {
		t.Fatalf("list mismatch: %+v", objects)
	}
}
