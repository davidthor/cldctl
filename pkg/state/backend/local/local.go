// Package local implements a local filesystem state backend.
package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/davidthor/cldctl/pkg/state/backend"
	"github.com/google/uuid"
)

func init() {
	backend.Register("local", NewBackend)
}

// Backend implements the state backend interface for local filesystem storage.
type Backend struct {
	basePath string
	mu       sync.RWMutex
	locks    map[string]*localLock
}

// NewBackend creates a new local backend.
func NewBackend(config map[string]string) (backend.Backend, error) {
	path := config["path"]
	if path == "" {
		// Default to ~/.cldctl/state
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(homeDir, ".cldctl", "state")
	}

	// Ensure base path exists
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	return &Backend{
		basePath: path,
		locks:    make(map[string]*localLock),
	}, nil
}

func (b *Backend) Type() string {
	return "local"
}

func (b *Backend) Read(ctx context.Context, path string) (io.ReadCloser, error) {
	fullPath := b.fullPath(path)

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, backend.ErrNotFound
		}
		return nil, fmt.Errorf("failed to read %s: %w", fullPath, err)
	}

	return file, nil
}

func (b *Backend) Write(ctx context.Context, path string, data io.Reader) error {
	fullPath := b.fullPath(path)

	// Ensure parent directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write to temp file first, then rename for atomicity
	tempFile, err := os.CreateTemp(dir, ".cldctl-state-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	_, err = io.Copy(tempFile, data)
	if closeErr := tempFile.Close(); closeErr != nil && err == nil {
		err = closeErr
	}

	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to write state: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, fullPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

func (b *Backend) Delete(ctx context.Context, path string) error {
	fullPath := b.fullPath(path)

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Idempotent
		}
		return fmt.Errorf("failed to delete %s: %w", fullPath, err)
	}

	return nil
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := b.fullPath(prefix)

	var paths []string
	err := filepath.Walk(fullPrefix, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			// Return path relative to base
			relPath, _ := filepath.Rel(b.basePath, path)
			paths = append(paths, relPath)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", fullPrefix, err)
	}

	return paths, nil
}

func (b *Backend) Exists(ctx context.Context, path string) (bool, error) {
	fullPath := b.fullPath(path)

	_, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check %s: %w", fullPath, err)
	}

	return true, nil
}

func (b *Backend) Lock(ctx context.Context, path string, info backend.LockInfo) (backend.Lock, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	lockPath := path + ".lock"

	// Check if already locked
	if existing, ok := b.locks[lockPath]; ok {
		return nil, &backend.LockError{
			Info: existing.info,
			Err:  backend.ErrLocked,
		}
	}

	// Check lock file on disk
	lockFilePath := b.fullPath(lockPath)
	if data, err := os.ReadFile(lockFilePath); err == nil {
		var existingInfo backend.LockInfo
		if err := json.Unmarshal(data, &existingInfo); err == nil {
			// Check if lock is stale (older than 1 hour)
			if time.Since(existingInfo.Created) < time.Hour {
				return nil, &backend.LockError{
					Info: existingInfo,
					Err:  backend.ErrLocked,
				}
			}
		}
	}

	// Create lock
	info.ID = uuid.New().String()
	info.Path = path
	info.Created = time.Now()

	// Write lock file
	lockData, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal lock info: %w", err)
	}

	dir := filepath.Dir(lockFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	if err := os.WriteFile(lockFilePath, lockData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write lock file: %w", err)
	}

	lock := &localLock{
		backend:  b,
		path:     lockPath,
		filePath: lockFilePath,
		info:     info,
	}
	b.locks[lockPath] = lock

	return lock, nil
}

func (b *Backend) fullPath(path string) string {
	return filepath.Join(b.basePath, path)
}

// localLock implements the Lock interface for local filesystem.
type localLock struct {
	backend  *Backend
	path     string
	filePath string
	info     backend.LockInfo
}

func (l *localLock) ID() string {
	return l.info.ID
}

func (l *localLock) Unlock(ctx context.Context) error {
	l.backend.mu.Lock()
	defer l.backend.mu.Unlock()

	delete(l.backend.locks, l.path)

	if err := os.Remove(l.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}

	return nil
}

func (l *localLock) Info() backend.LockInfo {
	return l.info
}
