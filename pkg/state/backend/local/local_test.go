package local

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidthor/cldctl/pkg/state/backend"
)

func TestNewBackend(t *testing.T) {
	tmpDir := t.TempDir()

	b, err := NewBackend(map[string]string{
		"path": tmpDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.Type() != "local" {
		t.Errorf("expected type 'local', got %q", b.Type())
	}
}

func TestBackend_ReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	b, _ := NewBackend(map[string]string{"path": tmpDir})

	ctx := context.Background()
	testPath := "test/state.json"
	testData := []byte(`{"name": "test"}`)

	// Write
	err := b.Write(ctx, testPath, bytes.NewReader(testData))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Read
	reader, err := b.Read(ctx, testPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read all failed: %v", err)
	}

	if !bytes.Equal(data, testData) {
		t.Errorf("expected %s, got %s", testData, data)
	}
}

func TestBackend_ReadNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	b, _ := NewBackend(map[string]string{"path": tmpDir})

	ctx := context.Background()

	_, err := b.Read(ctx, "nonexistent")
	if err != backend.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBackend_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	b, _ := NewBackend(map[string]string{"path": tmpDir})

	ctx := context.Background()
	testPath := "test/state.json"
	testData := []byte(`{"name": "test"}`)

	// Write
	_ = b.Write(ctx, testPath, bytes.NewReader(testData))

	// Verify exists
	exists, _ := b.Exists(ctx, testPath)
	if !exists {
		t.Fatal("expected file to exist")
	}

	// Delete
	err := b.Delete(ctx, testPath)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Verify gone
	exists, _ = b.Exists(ctx, testPath)
	if exists {
		t.Error("expected file to not exist after delete")
	}
}

func TestBackend_List(t *testing.T) {
	tmpDir := t.TempDir()
	b, _ := NewBackend(map[string]string{"path": tmpDir})

	ctx := context.Background()

	// Create some files
	_ = b.Write(ctx, "environments/env1/state.json", bytes.NewReader([]byte("{}")))
	_ = b.Write(ctx, "environments/env2/state.json", bytes.NewReader([]byte("{}")))
	_ = b.Write(ctx, "other/file.txt", bytes.NewReader([]byte("{}")))

	// List all
	paths, err := b.List(ctx, "")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if len(paths) != 3 {
		t.Errorf("expected 3 paths, got %d: %v", len(paths), paths)
	}

	// List with directory prefix
	paths, err = b.List(ctx, "environments")
	if err != nil {
		t.Fatalf("list with prefix failed: %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d: %v", len(paths), paths)
	}
}

func TestBackend_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	b, _ := NewBackend(map[string]string{"path": tmpDir})

	ctx := context.Background()
	testPath := "test/state.json"

	// Check non-existent
	exists, err := b.Exists(ctx, testPath)
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if exists {
		t.Error("expected file to not exist")
	}

	// Create file
	_ = b.Write(ctx, testPath, bytes.NewReader([]byte("{}")))

	// Check exists
	exists, err = b.Exists(ctx, testPath)
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if !exists {
		t.Error("expected file to exist")
	}
}

func TestBackend_Lock(t *testing.T) {
	tmpDir := t.TempDir()
	b, _ := NewBackend(map[string]string{"path": tmpDir})

	ctx := context.Background()
	testPath := "test/state"

	lockInfo := backend.LockInfo{
		Who:       "test-user",
		Operation: "apply",
	}

	// Acquire lock
	lock, err := b.Lock(ctx, testPath, lockInfo)
	if err != nil {
		t.Fatalf("lock failed: %v", err)
	}

	if lock == nil {
		t.Fatal("expected lock to be returned")
	}

	// Verify lock file exists
	lockPath := filepath.Join(tmpDir, testPath+".lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("expected lock file to exist")
	}

	// Unlock
	err = lock.Unlock(ctx)
	if err != nil {
		t.Fatalf("unlock failed: %v", err)
	}

	// Verify lock file removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed after unlock")
	}
}

func TestBackend_LockConflict(t *testing.T) {
	tmpDir := t.TempDir()
	b, _ := NewBackend(map[string]string{"path": tmpDir})

	ctx := context.Background()
	testPath := "test/state"

	lockInfo := backend.LockInfo{
		Who:       "test-user",
		Operation: "apply",
	}

	// Acquire first lock
	lock1, err := b.Lock(ctx, testPath, lockInfo)
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer func() { _ = lock1.Unlock(ctx) }()

	// Try to acquire second lock (should fail)
	_, err = b.Lock(ctx, testPath, lockInfo)
	if err == nil {
		t.Error("expected error for conflicting lock")
	}
}

func TestBackend_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	b, _ := NewBackend(map[string]string{"path": tmpDir})

	ctx := context.Background()
	testPath := "test/state.json"

	// Write initial data
	_ = b.Write(ctx, testPath, bytes.NewReader([]byte(`{"version": 1}`)))

	// Write updated data
	err := b.Write(ctx, testPath, bytes.NewReader([]byte(`{"version": 2}`)))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Read and verify
	reader, _ := b.Read(ctx, testPath)
	data, _ := io.ReadAll(reader)
	reader.Close()

	expected := `{"version": 2}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, data)
	}
}
