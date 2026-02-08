// Package gcs implements a Google Cloud Storage state backend.
package gcs

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

	"cloud.google.com/go/storage"
	"github.com/davidthor/cldctl/pkg/state/backend"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

func init() {
	backend.Register("gcs", NewBackend)
}

// Backend implements the state backend interface for Google Cloud Storage.
type Backend struct {
	client *storage.Client
	bucket string
	prefix string
}

// NewBackend creates a new GCS backend.
func NewBackend(cfg map[string]string) (backend.Backend, error) {
	bucketName, ok := cfg["bucket"]
	if !ok || bucketName == "" {
		return nil, fmt.Errorf("gcs backend requires 'bucket' configuration")
	}

	ctx := context.Background()
	var opts []option.ClientOption

	// Support explicit credentials file
	if credentialsFile := cfg["credentials"]; credentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(credentialsFile))
	}

	// Support credentials JSON
	if credentialsJSON := cfg["credentials_json"]; credentialsJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(credentialsJSON)))
	}

	// Support custom endpoint (for emulator)
	if endpoint := cfg["endpoint"]; endpoint != "" {
		opts = append(opts, option.WithEndpoint(endpoint))
		opts = append(opts, option.WithoutAuthentication())
	}

	// Create GCS client
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	return &Backend{
		client: client,
		bucket: bucketName,
		prefix: cfg["prefix"],
	}, nil
}

func (b *Backend) Type() string {
	return "gcs"
}

func (b *Backend) Read(ctx context.Context, statePath string) (io.ReadCloser, error) {
	objectPath := b.fullPath(statePath)

	reader, err := b.client.Bucket(b.bucket).Object(objectPath).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, backend.ErrNotFound
		}
		return nil, fmt.Errorf("failed to read state from gs://%s/%s: %w", b.bucket, objectPath, err)
	}

	return reader, nil
}

func (b *Backend) Write(ctx context.Context, statePath string, data io.Reader) error {
	objectPath := b.fullPath(statePath)

	// Read all data first
	content, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("failed to read data: %w", err)
	}

	writer := b.client.Bucket(b.bucket).Object(objectPath).NewWriter(ctx)
	writer.ContentType = "application/json"

	if _, err := writer.Write(content); err != nil {
		writer.Close()
		return fmt.Errorf("failed to write state to gs://%s/%s: %w", b.bucket, objectPath, err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	return nil
}

func (b *Backend) Delete(ctx context.Context, statePath string) error {
	objectPath := b.fullPath(statePath)

	err := b.client.Bucket(b.bucket).Object(objectPath).Delete(ctx)
	if err != nil {
		// Ignore not found errors for idempotency
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil
		}
		return fmt.Errorf("failed to delete state from gs://%s/%s: %w", b.bucket, objectPath, err)
	}

	return nil
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := b.fullPath(prefix)

	var paths []string
	it := b.client.Bucket(b.bucket).Objects(ctx, &storage.Query{
		Prefix: fullPrefix,
	})

	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		// Return path relative to backend prefix
		relPath := strings.TrimPrefix(attrs.Name, b.prefix+"/")
		if b.prefix == "" {
			relPath = attrs.Name
		}
		paths = append(paths, relPath)
	}

	return paths, nil
}

func (b *Backend) Exists(ctx context.Context, statePath string) (bool, error) {
	objectPath := b.fullPath(statePath)

	_, err := b.client.Bucket(b.bucket).Object(objectPath).Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return true, nil
}

func (b *Backend) Lock(ctx context.Context, statePath string, info backend.LockInfo) (backend.Lock, error) {
	lockPath := b.fullPath(statePath + ".lock")

	// Check for existing lock
	existingLock, err := b.readLock(ctx, lockPath)
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

	writer := b.client.Bucket(b.bucket).Object(lockPath).NewWriter(ctx)
	writer.ContentType = "application/json"

	if _, err := writer.Write(lockData); err != nil {
		writer.Close()
		return nil, fmt.Errorf("failed to create lock: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close lock writer: %w", err)
	}

	return &gcsLock{
		backend: b,
		path:    lockPath,
		info:    info,
	}, nil
}

func (b *Backend) readLock(ctx context.Context, lockPath string) (backend.LockInfo, error) {
	reader, err := b.client.Bucket(b.bucket).Object(lockPath).NewReader(ctx)
	if err != nil {
		return backend.LockInfo{}, err
	}
	defer reader.Close()

	var info backend.LockInfo
	if err := json.NewDecoder(reader).Decode(&info); err != nil {
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

// Close closes the GCS client.
func (b *Backend) Close() error {
	return b.client.Close()
}

// gcsLock implements the Lock interface for GCS.
type gcsLock struct {
	backend *Backend
	path    string
	info    backend.LockInfo
}

func (l *gcsLock) ID() string {
	return l.info.ID
}

func (l *gcsLock) Unlock(ctx context.Context) error {
	err := l.backend.client.Bucket(l.backend.bucket).Object(l.path).Delete(ctx)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	return nil
}

func (l *gcsLock) Info() backend.LockInfo {
	return l.info
}

// Ensure we implement the Backend interface
var _ backend.Backend = (*Backend)(nil)

// Helper to read full content into memory
func readAll(reader io.Reader) (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}
	_, err := io.Copy(buf, reader)
	return buf, err
}
