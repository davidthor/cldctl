// Package s3 implements an S3-compatible state backend.
package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/davidthor/cldctl/pkg/state/backend"
	"github.com/google/uuid"
)

func init() {
	backend.Register("s3", NewBackend)
}

// Backend implements the state backend interface for S3-compatible storage.
type Backend struct {
	client *s3.Client
	bucket string
	prefix string
	region string
}

// NewBackend creates a new S3 backend.
func NewBackend(cfg map[string]string) (backend.Backend, error) {
	bucket, ok := cfg["bucket"]
	if !ok || bucket == "" {
		return nil, fmt.Errorf("s3 backend requires 'bucket' configuration")
	}

	region := cfg["region"]
	if region == "" {
		region = "us-east-1"
	}

	// Build AWS config options
	var opts []func(*config.LoadOptions) error
	opts = append(opts, config.WithRegion(region))

	// Custom endpoint is handled via S3 client options below

	// Support explicit credentials
	if accessKey := cfg["access_key"]; accessKey != "" {
		secretKey := cfg["secret_key"]
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with path-style addressing for compatibility
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg["force_path_style"] == "true"
		// Support custom endpoint (for MinIO, R2, etc.)
		if endpoint := cfg["endpoint"]; endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &Backend{
		client: client,
		bucket: bucket,
		prefix: cfg["key"],
		region: region,
	}, nil
}

func (b *Backend) Type() string {
	return "s3"
}

func (b *Backend) Read(ctx context.Context, statePath string) (io.ReadCloser, error) {
	key := b.fullPath(statePath)

	output, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &b.bucket,
		Key:    &key,
	})
	if err != nil {
		// Check for not found
		var nsk *types.NoSuchKey
		if ok := errors.As(err, &nsk); ok {
			return nil, backend.ErrNotFound
		}
		return nil, fmt.Errorf("failed to read state from s3://%s/%s: %w", b.bucket, key, err)
	}

	return output.Body, nil
}

func (b *Backend) Write(ctx context.Context, statePath string, data io.Reader) error {
	key := b.fullPath(statePath)

	// Read all data to get content length
	content, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("failed to read data: %w", err)
	}

	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &b.bucket,
		Key:         &key,
		Body:        bytes.NewReader(content),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to write state to s3://%s/%s: %w", b.bucket, key, err)
	}

	return nil
}

func (b *Backend) Delete(ctx context.Context, statePath string) error {
	key := b.fullPath(statePath)

	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &b.bucket,
		Key:    &key,
	})
	if err != nil {
		// Ignore not found errors for idempotency
		var nsk *types.NoSuchKey
		if ok := errors.As(err, &nsk); ok {
			return nil
		}
		return fmt.Errorf("failed to delete state from s3://%s/%s: %w", b.bucket, key, err)
	}

	return nil
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := b.fullPath(prefix)

	var paths []string
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: &b.bucket,
		Prefix: &fullPrefix,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			// Return path relative to backend prefix
			relPath := strings.TrimPrefix(*obj.Key, b.prefix+"/")
			paths = append(paths, relPath)
		}
	}

	return paths, nil
}

func (b *Backend) Exists(ctx context.Context, statePath string) (bool, error) {
	key := b.fullPath(statePath)

	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &b.bucket,
		Key:    &key,
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if ok := errors.As(err, &nsk); ok {
			return false, nil
		}
		// Also check for 404 status
		var notFound *types.NotFound
		if ok := errors.As(err, &notFound); ok {
			return false, nil
		}
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return true, nil
}

func (b *Backend) Lock(ctx context.Context, statePath string, info backend.LockInfo) (backend.Lock, error) {
	lockKey := b.fullPath(statePath + ".lock")

	// Check for existing lock
	existingLock, err := b.readLock(ctx, lockKey)
	if err == nil {
		// Check if lock is stale (older than 1 hour)
		if time.Since(existingLock.Created) < time.Hour {
			return nil, &backend.LockError{
				Info: existingLock,
				Err:  backend.ErrLocked,
			}
		}
	}

	// Create lock
	info.ID = uuid.New().String()
	info.Path = statePath
	info.Created = time.Now()

	lockData, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal lock info: %w", err)
	}

	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &b.bucket,
		Key:         &lockKey,
		Body:        bytes.NewReader(lockData),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create lock: %w", err)
	}

	return &s3Lock{
		backend: b,
		key:     lockKey,
		info:    info,
	}, nil
}

func (b *Backend) readLock(ctx context.Context, key string) (backend.LockInfo, error) {
	output, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &b.bucket,
		Key:    &key,
	})
	if err != nil {
		return backend.LockInfo{}, err
	}
	defer output.Body.Close()

	var info backend.LockInfo
	if err := json.NewDecoder(output.Body).Decode(&info); err != nil {
		return backend.LockInfo{}, err
	}

	return info, nil
}

func (b *Backend) fullPath(statePath string) string {
	if b.prefix == "" {
		return statePath
	}
	return path.Join(b.prefix, statePath)
}

// s3Lock implements the Lock interface for S3.
type s3Lock struct {
	backend *Backend
	key     string
	info    backend.LockInfo
}

func (l *s3Lock) ID() string {
	return l.info.ID
}

func (l *s3Lock) Unlock(ctx context.Context) error {
	_, err := l.backend.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &l.backend.bucket,
		Key:    &l.key,
	})
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	return nil
}

func (l *s3Lock) Info() backend.LockInfo {
	return l.info
}
