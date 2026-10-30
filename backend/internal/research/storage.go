package research

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type ObjectStorage interface {
	Put(context.Context, string, string, []byte) error
	Get(context.Context, string) (io.ReadCloser, int64, error)
	Delete(context.Context, string) error
	DeletePrefix(context.Context, string) error
}
type LocalStorage struct{ Root string }

func validObjectKey(key string) bool {
	return key != "" && !strings.HasPrefix(key, "/") && !strings.ContainsAny(key, "\\\x00") && path.Clean(key) == key && key != "." && key != ".." && !strings.HasPrefix(key, "../")
}
func (l LocalStorage) filename(key string) (string, error) {
	if !validObjectKey(key) {
		return "", errors.New("invalid object key")
	}
	return filepath.Join(l.Root, filepath.FromSlash(key)), nil
}
func (l LocalStorage) Put(ctx context.Context, key, contentType string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := l.filename(key)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".upload-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}
func (l LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	name, err := l.filename(key)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}
func (l LocalStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := l.filename(key)
	if err != nil {
		return err
	}
	err = os.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (l LocalStorage) DeletePrefix(ctx context.Context, prefix string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !strings.HasSuffix(prefix, "/") {
		return errors.New("object prefix must end in /")
	}
	name, err := l.filename(strings.TrimSuffix(prefix, "/"))
	if err != nil {
		return err
	}
	return os.RemoveAll(name)
}

type R2Storage struct {
	client *s3.Client
	bucket string
}

func validateR2Config(c Config) error {
	u, err := url.Parse(c.R2Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("R2_ENDPOINT must be an HTTPS origin without the bucket path")
	}
	if c.R2Bucket == "" || strings.ContainsAny(c.R2Bucket, "/\\ \t\n") {
		return errors.New("R2_BUCKET must be the bucket name")
	}
	if c.R2AccessKey == "" || c.R2SecretKey == "" {
		return errors.New("R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY are required; a bucket URL cannot authorize uploads")
	}
	return nil
}
func NewObjectStorage(c Config) (ObjectStorage, error) {
	if c.StorageBackend == "" || c.StorageBackend == "local" {
		return LocalStorage{Root: c.DataDir}, nil
	}
	if c.StorageBackend != "r2" {
		return nil, errors.New("STORAGE_BACKEND must be local or r2")
	}
	if err := validateR2Config(c); err != nil {
		return nil, err
	}
	return newR2Storage(c.R2Endpoint, c.R2Bucket, c.R2AccessKey, c.R2SecretKey), nil
}
func newR2Storage(endpoint, bucket, key, secret string) *R2Storage {
	return &R2Storage{bucket: bucket, client: s3.New(s3.Options{Region: "auto", BaseEndpoint: aws.String(endpoint), UsePathStyle: true, Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(key, secret, "")), HTTPClient: &http.Client{Timeout: 60 * time.Second}, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})}
}
func storageError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return ErrNotFound
		case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
			return fmt.Errorf("R2 %s authorization failed; check bucket credentials and permissions", operation)
		}
	}
	return fmt.Errorf("R2 %s failed; check endpoint, bucket, credentials, and network", operation)
}
func (r *R2Storage) Put(ctx context.Context, key, contentType string, data []byte) error {
	if !validObjectKey(key) {
		return errors.New("invalid object key")
	}
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key), ContentType: aws.String(contentType), ContentLength: aws.Int64(int64(len(data))), Body: bytes.NewReader(data)})
	if err != nil {
		return storageError("upload", err)
	}
	return nil
}
func (r *R2Storage) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if !validObjectKey(key) {
		return nil, 0, errors.New("invalid object key")
	}
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, 0, storageError("download", err)
	}
	size := int64(-1)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, size, nil
}
func (r *R2Storage) Delete(ctx context.Context, key string) error {
	if !validObjectKey(key) {
		return errors.New("invalid object key")
	}
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)})
	if err != nil {
		return storageError("delete", err)
	}
	return nil
}
func (r *R2Storage) DeletePrefix(ctx context.Context, prefix string) error {
	if !strings.HasSuffix(prefix, "/") || !validObjectKey(strings.TrimSuffix(prefix, "/")) {
		return errors.New("invalid object prefix")
	}
	pages := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{Bucket: aws.String(r.bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return storageError("list", err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if !strings.HasPrefix(key, prefix) {
				return errors.New("object prefix mismatch")
			}
			if err = r.Delete(ctx, key); err != nil {
				return err
			}
		}
	}
	return nil
}
func readObject(ctx context.Context, storage ObjectStorage, key string, limit int64) ([]byte, error) {
	body, _, err := storage.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err == nil && int64(len(raw)) > limit {
		return nil, errors.New("stored object exceeds size limit")
	}
	return raw, err
}
func (s *Server) objectStorage() ObjectStorage {
	if s.Objects != nil {
		return s.Objects
	}
	return LocalStorage{Root: s.Config.DataDir}
}
func (j *Jobs) objectStorage() ObjectStorage {
	if j.Objects != nil {
		return j.Objects
	}
	return LocalStorage{Root: j.root}
}
func (s *Server) serveObject(w http.ResponseWriter, r *http.Request, key string) bool {
	body, size, err := s.objectStorage().Get(r.Context(), key)
	if err != nil {
		handleError(w, err)
		return false
	}
	defer body.Close()
	if size >= 0 {
		w.Header().Set("Content-Length", fmt.Sprint(size))
	}
	w.Header().Set("Cache-Control", "private, no-store")
	_, err = io.Copy(w, body)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return true
}

// CheckObjectStorage proves write, read, integrity, and delete permissions without
// making the bucket public or using any research data.
func CheckObjectStorage(ctx context.Context, objects ObjectStorage) error {
	key := "checks/" + newID("storage") + ".txt"
	raw := []byte("Kairo object storage verification\n")
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = objects.Delete(cleanup, key)
	}()
	if err := objects.Put(ctx, key, "text/plain", raw); err != nil {
		return err
	}
	if err := verifyStoredObject(ctx, objects, key, raw); err != nil {
		return err
	}
	return objects.Delete(ctx, key)
}
