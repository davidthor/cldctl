package gcs

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidthor/cldctl/pkg/state/backend"
)

// mockGCSServer simulates Google Cloud Storage API for testing.
type mockGCSServer struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

func newMockGCSServer() *mockGCSServer {
	return &mockGCSServer{
		objects: make(map[string][]byte),
	}
}

func (m *mockGCSServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// GCS API uses paths like: /storage/v1/b/bucket/o/object or /b/bucket/o/object
	// For downloads: /storage/v1/b/bucket/o/object?alt=media
	path := r.URL.Path

	// Handle upload endpoint
	if strings.HasPrefix(path, "/upload/storage/v1/b/") {
		m.handleUpload(w, r)
		return
	}

	// Parse bucket and object from path
	// Format: /storage/v1/b/{bucket}/o/{object} or /b/{bucket}/o/{object}
	var bucket, object string
	if strings.HasPrefix(path, "/storage/v1/b/") {
		path = strings.TrimPrefix(path, "/storage/v1/b/")
	} else if strings.HasPrefix(path, "/b/") {
		path = strings.TrimPrefix(path, "/b/")
	}

	// Split on /o/ or /o (for list operations)
	if strings.Contains(path, "/o/") {
		parts := strings.SplitN(path, "/o/", 2)
		bucket = parts[0]
		if len(parts) >= 2 {
			object = parts[1]
		}
	} else if strings.HasSuffix(path, "/o") {
		// List operation: /bucket/o
		bucket = strings.TrimSuffix(path, "/o")
		object = ""
	} else {
		// Just bucket name or malformed
		bucket = path
	}

	// Handle list objects
	if object == "" && r.Method == http.MethodGet {
		m.handleListObjects(w, r, bucket)
		return
	}

	key := bucket + "/" + object

	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("alt") == "media" {
			m.handleDownload(w, key)
		} else {
			m.handleGetMetadata(w, key)
		}
	case http.MethodDelete:
		m.handleDelete(w, key)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *mockGCSServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Parse path: /upload/storage/v1/b/{bucket}/o?name={object}
	path := strings.TrimPrefix(r.URL.Path, "/upload/storage/v1/b/")
	parts := strings.SplitN(path, "/o", 2)
	if len(parts) < 1 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	bucket := parts[0]
	// URL-decode the object name from query parameter
	object := r.URL.Query().Get("name")
	if object == "" {
		// Try uploadType=media path
		object = r.URL.Query().Get("uploadType")
	}

	key := bucket + "/" + object

	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m.objects[key] = data

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"name":"` + object + `"}`))
}

func (m *mockGCSServer) handleDownload(w http.ResponseWriter, key string) {
	data, ok := m.objects[key]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": {"code": 404, "message": "No such object"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (m *mockGCSServer) handleGetMetadata(w http.ResponseWriter, key string) {
	if _, ok := m.objects[key]; !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": {"code": 404, "message": "No such object"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	parts := strings.SplitN(key, "/", 2)
	name := ""
	if len(parts) > 1 {
		name = parts[1]
	}
	_, _ = w.Write([]byte(`{"name":"` + name + `"}`))
}

func (m *mockGCSServer) handleDelete(w http.ResponseWriter, key string) {
	if _, ok := m.objects[key]; !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": {"code": 404, "message": "No such object"}}`))
		return
	}
	delete(m.objects, key)
	w.WriteHeader(http.StatusNoContent)
}

func (m *mockGCSServer) handleListObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	prefix := r.URL.Query().Get("prefix")

	var items []string
	for key := range m.objects {
		if strings.HasPrefix(key, bucket+"/") {
			objectName := strings.TrimPrefix(key, bucket+"/")
			if prefix == "" || strings.HasPrefix(objectName, prefix) {
				items = append(items, `{"name":"`+objectName+`"}`)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"items":[` + strings.Join(items, ",") + `]}`))
}

func TestNewBackend_MissingBucket(t *testing.T) {
	_, err := NewBackend(map[string]string{})
	if err == nil {
		t.Error("expected error for missing bucket")
	}
	if !strings.Contains(err.Error(), "bucket") {
		t.Errorf("expected error message to mention bucket, got: %v", err)
	}
}

func TestNewBackend_EmptyBucket(t *testing.T) {
	_, err := NewBackend(map[string]string{
		"bucket": "",
	})
	if err == nil {
		t.Error("expected error for empty bucket")
	}
}

func TestBackend_Type(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	b, err := NewBackend(map[string]string{
		"bucket":   "test-bucket",
		"endpoint": server.URL + "/storage/v1/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.Type() != "gcs" {
		t.Errorf("expected type 'gcs', got %q", b.Type())
	}
}

func TestBackend_fullPath(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		path     string
		expected string
	}{
		{
			name:     "no prefix",
			prefix:   "",
			path:     "state.json",
			expected: "state.json",
		},
		{
			name:     "with prefix",
			prefix:   "env/staging",
			path:     "state.json",
			expected: "env/staging/state.json",
		},
		{
			name:     "nested path with prefix",
			prefix:   "cldctl",
			path:     "environments/prod/state.json",
			expected: "cldctl/environments/prod/state.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Backend{prefix: tt.prefix}
			result := b.fullPath(tt.path)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestLockInfo_Marshal(t *testing.T) {
	info := backend.LockInfo{
		ID:        "test-id",
		Path:      "test/path",
		Who:       "test-user",
		Operation: "apply",
		Created:   time.Now(),
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded backend.LockInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != info.ID {
		t.Errorf("ID mismatch: expected %q, got %q", info.ID, decoded.ID)
	}
	if decoded.Path != info.Path {
		t.Errorf("Path mismatch: expected %q, got %q", info.Path, decoded.Path)
	}
	if decoded.Who != info.Who {
		t.Errorf("Who mismatch: expected %q, got %q", info.Who, decoded.Who)
	}
	if decoded.Operation != info.Operation {
		t.Errorf("Operation mismatch: expected %q, got %q", info.Operation, decoded.Operation)
	}
}

func TestGCSLock_ID(t *testing.T) {
	info := backend.LockInfo{
		ID:        "test-lock-id",
		Path:      "test/state",
		Who:       "test-user",
		Operation: "plan",
	}

	lock := &gcsLock{
		info: info,
	}

	if lock.ID() != "test-lock-id" {
		t.Errorf("expected ID 'test-lock-id', got %q", lock.ID())
	}
}

func TestGCSLock_Info(t *testing.T) {
	info := backend.LockInfo{
		ID:        "test-lock-id",
		Path:      "test/state",
		Who:       "test-user",
		Operation: "apply",
		Created:   time.Now(),
	}

	lock := &gcsLock{
		info: info,
	}

	returnedInfo := lock.Info()
	if returnedInfo.ID != info.ID {
		t.Errorf("expected ID %q, got %q", info.ID, returnedInfo.ID)
	}
	if returnedInfo.Who != info.Who {
		t.Errorf("expected Who %q, got %q", info.Who, returnedInfo.Who)
	}
}

func TestBackend_InterfaceCompliance(t *testing.T) {
	// Verify that Backend implements the backend.Backend interface
	var _ backend.Backend = (*Backend)(nil)
}

func TestMockServer_DownloadNotFound(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	resp, err := http.Get(server.URL + "/storage/v1/b/bucket/o/nonexistent?alt=media")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestMockServer_UploadAndDownload(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Upload
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/upload/storage/v1/b/bucket/o?name=test.json", bytes.NewReader(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Download
	resp, err = http.Get(server.URL + "/storage/v1/b/bucket/o/test.json?alt=media")
	if err != nil {
		t.Fatalf("download request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, data) {
		t.Errorf("expected %s, got %s", data, body)
	}
}

func TestMockServer_Delete(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Upload first
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/upload/storage/v1/b/bucket/o?name=test.json", bytes.NewReader(data))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Delete
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/storage/v1/b/bucket/o/test.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp, err = http.Get(server.URL + "/storage/v1/b/bucket/o/test.json?alt=media")
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestMockServer_GetMetadata(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Upload first
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/upload/storage/v1/b/bucket/o?name=test.json", bytes.NewReader(data))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Get metadata (without alt=media)
	resp, err := http.Get(server.URL + "/storage/v1/b/bucket/o/test.json")
	if err != nil {
		t.Fatalf("metadata request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Get metadata non-existent
	resp, err = http.Get(server.URL + "/storage/v1/b/bucket/o/nonexistent.json")
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestMockServer_ListObjects(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Upload some objects with URL-encoded names
	objects := []string{"state%2Fenv1.json", "state%2Fenv2.json", "other%2Ffile.txt"}
	expectedNames := []string{"state/env1.json", "state/env2.json", "other/file.txt"}
	for i, obj := range objects {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/upload/storage/v1/b/bucket/o?name="+obj, bytes.NewReader([]byte("{}")))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("upload %d failed: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("upload %d returned %d: %s", i, resp.StatusCode, string(body))
		}
		resp.Body.Close()
	}

	// List all
	resp, err := http.Get(server.URL + "/storage/v1/b/bucket/o")
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	for _, obj := range expectedNames {
		if !strings.Contains(string(body), obj) {
			t.Errorf("expected response to contain %q", obj)
		}
	}

	// List with prefix
	resp, err = http.Get(server.URL + "/storage/v1/b/bucket/o?prefix=state/")
	if err != nil {
		t.Fatalf("list with prefix request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "state/env1.json") {
		t.Error("expected response to contain 'state/env1.json'")
	}
	if strings.Contains(string(body), "other/file.txt") {
		t.Error("expected response to NOT contain 'other/file.txt'")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty config",
			config:      map[string]string{},
			expectError: true,
			errorMsg:    "bucket",
		},
		{
			name: "empty bucket",
			config: map[string]string{
				"bucket": "",
			},
			expectError: true,
			errorMsg:    "bucket",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewBackend(tt.config)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestLockError(t *testing.T) {
	info := backend.LockInfo{
		ID:        "existing-lock",
		Path:      "test/path",
		Who:       "other-user",
		Operation: "apply",
	}

	lockErr := &backend.LockError{
		Info: info,
		Err:  backend.ErrLocked,
	}

	if lockErr.Error() != backend.ErrLocked.Error() {
		t.Errorf("expected error message %q, got %q", backend.ErrLocked.Error(), lockErr.Error())
	}

	if lockErr.Unwrap() != backend.ErrLocked {
		t.Error("expected Unwrap to return ErrLocked")
	}
}

func TestBackendConfig_WithPrefix(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	b, err := NewBackend(map[string]string{
		"bucket":   "test-bucket",
		"prefix":   "cldctl/state",
		"endpoint": server.URL + "/storage/v1/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gcsb := b.(*Backend)
	if gcsb.prefix != "cldctl/state" {
		t.Errorf("expected prefix 'cldctl/state', got %q", gcsb.prefix)
	}
	if gcsb.bucket != "test-bucket" {
		t.Errorf("expected bucket 'test-bucket', got %q", gcsb.bucket)
	}
}

func TestLockInfo_WithExpiration(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	info := backend.LockInfo{
		ID:        "test-id",
		Path:      "test/path",
		Who:       "test-user",
		Operation: "apply",
		Created:   time.Now(),
		Expires:   expires,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded backend.LockInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Expires.IsZero() {
		t.Error("expected Expires to be set")
	}
}

func TestReadAll_Helper(t *testing.T) {
	data := []byte(`{"test": "data"}`)
	reader := bytes.NewReader(data)

	buf, err := readAll(reader)
	if err != nil {
		t.Fatalf("readAll failed: %v", err)
	}

	if !bytes.Equal(buf.Bytes(), data) {
		t.Errorf("expected %s, got %s", data, buf.Bytes())
	}
}

func TestBackend_Close(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	b, err := NewBackend(map[string]string{
		"bucket":   "test-bucket",
		"endpoint": server.URL + "/storage/v1/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gcsb := b.(*Backend)
	err = gcsb.Close()
	if err != nil {
		t.Errorf("unexpected error closing backend: %v", err)
	}
}

func TestMockServer_DeleteNotFound(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Delete non-existent object
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/storage/v1/b/bucket/o/nonexistent.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestMockServer_MethodNotAllowed(t *testing.T) {
	mock := newMockGCSServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPatch, server.URL+"/storage/v1/b/bucket/o/test.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}
