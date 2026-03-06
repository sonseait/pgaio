package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"pgaio/model"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Client wraps MinIO client for S3-compatible storage operations.
type S3Client struct {
	client *minio.Client
	bucket string
}

// NewS3Client creates a new S3-compatible client using minio-go.
func NewS3Client(endpoint, accessKey, secretKey, bucket, region string, useSSL bool) (*S3Client, error) {
	if bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required")
	}
	if region == "" {
		region = "us-east-1"
	}

	// Parse endpoint — strip protocol prefix
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	return &S3Client{client: client, bucket: bucket}, nil
}

// ListObjects lists objects in the S3 bucket with optional prefix.
func (s *S3Client) ListObjects(ctx context.Context, prefix string, delimiter string) (*model.S3ListResponse, error) {
	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: delimiter == "",
	}

	var objects []model.S3Object
	var totalSize int64
	seenDirs := map[string]bool{}

	for obj := range s.client.ListObjects(ctx, s.bucket, opts) {
		if obj.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", obj.Err)
		}

		key := obj.Key

		// When using non-recursive (delimiter) mode, detect directories
		if delimiter != "" && prefix != "" {
			relKey := strings.TrimPrefix(key, prefix)
			if idx := strings.Index(relKey, "/"); idx >= 0 {
				dirKey := prefix + relKey[:idx+1]
				if !seenDirs[dirKey] {
					seenDirs[dirKey] = true
					objects = append(objects, model.S3Object{
						Key:   dirKey,
						IsDir: true,
					})
				}
				continue
			}
		} else if delimiter != "" && prefix == "" {
			if idx := strings.Index(key, "/"); idx >= 0 {
				dirKey := key[:idx+1]
				if !seenDirs[dirKey] {
					seenDirs[dirKey] = true
					objects = append(objects, model.S3Object{
						Key:   dirKey,
						IsDir: true,
					})
				}
				continue
			}
		}

		// Skip the prefix itself
		if key == prefix {
			continue
		}

		totalSize += obj.Size
		objects = append(objects, model.S3Object{
			Key:          key,
			Size:         obj.Size,
			LastModified: obj.LastModified,
			IsDir:        false,
			SizeHuman:    humanBytes(obj.Size),
		})
	}

	if objects == nil {
		objects = []model.S3Object{}
	}

	return &model.S3ListResponse{
		Objects:    objects,
		Prefix:     prefix,
		Bucket:     s.bucket,
		TotalSize:  humanBytes(totalSize),
		TotalCount: len(objects),
	}, nil
}

// DeleteObject deletes an object from S3.
func (s *S3Client) DeleteObject(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// DeletePrefix deletes all objects with a given prefix.
func (s *S3Client) DeletePrefix(ctx context.Context, prefix string) error {
	opts := minio.ListObjectsOptions{Prefix: prefix, Recursive: true}
	for obj := range s.client.ListObjects(ctx, s.bucket, opts) {
		if obj.Err != nil {
			return obj.Err
		}
		if err := s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// GetPresignedURL generates a pre-signed URL for downloading.
func (s *S3Client) GetPresignedURL(ctx context.Context, key string) (string, error) {
	reqParams := make(url.Values)
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, 3600*1e9, reqParams) // 1 hour
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// ParseWalgS3Prefix extracts bucket and path from WAL-G S3 prefix.
func ParseWalgS3Prefix(prefix string) (bucket, path string) {
	// Format: s3://bucket-name/path/to/backups
	prefix = strings.TrimPrefix(prefix, "s3://")
	parts := strings.SplitN(prefix, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		path = parts[1]
	}
	return
}
