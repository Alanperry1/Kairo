package research

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const multipartChunkSize = 16 << 20

type StreamingObjectStorage interface {
	PutStream(context.Context, string, string, io.Reader) error
}

func (l LocalStorage) PutStream(ctx context.Context, key, kind string, reader io.Reader) error {
	name, err := l.filename(key)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(name), ".upload-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = io.Copy(file, &contextReader{ctx: ctx, reader: reader})
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), name)
}
func (r *R2Storage) PutStream(ctx context.Context, key, kind string, reader io.Reader) error {
	if !validObjectKey(key) {
		return errors.New("invalid object key")
	}
	created, err := r.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(r.bucket), Key: aws.String(key), ContentType: aws.String(kind)})
	if err != nil {
		return storageError("start multipart upload", err)
	}
	complete := false
	defer func() {
		if !complete {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, _ = r.client.AbortMultipartUpload(cleanup, &s3.AbortMultipartUploadInput{Bucket: aws.String(r.bucket), Key: aws.String(key), UploadId: created.UploadId})
		}
	}()
	buffer := make([]byte, multipartChunkSize)
	parts := []types.CompletedPart{}
	for index := int32(1); ; index++ {
		n, readErr := io.ReadFull(&contextReader{ctx: ctx, reader: reader}, buffer)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		if n > 0 {
			part, err := r.client.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(r.bucket), Key: aws.String(key), UploadId: created.UploadId, PartNumber: aws.Int32(index), ContentLength: aws.Int64(int64(n)), Body: bytes.NewReader(buffer[:n])})
			if err != nil {
				return storageError("multipart part upload", err)
			}
			parts = append(parts, types.CompletedPart{ETag: part.ETag, PartNumber: aws.Int32(index)})
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	if len(parts) == 0 {
		return errors.New("empty object stream")
	}
	_, err = r.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String(r.bucket), Key: aws.String(key), UploadId: created.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: parts}})
	if err != nil {
		return storageError("complete multipart upload", err)
	}
	complete = true
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
