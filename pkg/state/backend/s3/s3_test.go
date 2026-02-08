package s3

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

// mockS3Server simulates AWS S3 API for testing.
type mockS3Server struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

func newMockS3Server() *mockS3Server {
	return &mockS3Server{
		objects: make(map[string][]byte),
	}
}

func (m *mockS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Parse bucket and key from path: /bucket/key
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	bucket := parts[0]
	key := ""
	if len(parts) > 1 {
		key = parts[1]
	}

	// Handle list objects
	if key == "" && r.URL.Query().Get("list-type") == "2" {
		m.handleListObjects(w, r, bucket)
		return
	}

	fullKey := bucket + "/" + key

	switch r.Method {
	case http.MethodGet:
		m.handleGet(w, fullKey)
	case http.MethodPut:
		m.handlePut(w, r, fullKey)
	case http.MethodDelete:
		m.handleDelete(w, fullKey)
	case http.MethodHead:
		m.handleHead(w, fullKey)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *mockS3Server) handleGet(w http.ResponseWriter, key string) {
	data, ok := m.objects[key]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code></Error>`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (m *mockS3Server) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m.objects[key] = data
	w.WriteHeader(http.StatusOK)
}

func (m *mockS3Server) handleDelete(w http.ResponseWriter, key string) {
	delete(m.objects, key)
	w.WriteHeader(http.StatusNoContent)
}

func (m *mockS3Server) handleHead(w http.ResponseWriter, key string) {
	if _, ok := m.objects[key]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (m *mockS3Server) handleListObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	prefix := r.URL.Query().Get("prefix")

	var keys []string
	for key := range m.objects {
		if strings.HasPrefix(key, bucket+"/") {
			objectKey := strings.TrimPrefix(key, bucket+"/")
			if prefix == "" || strings.HasPrefix(objectKey, prefix) {
				keys = append(keys, objectKey)
			}
		}
	}

	// Return XML response similar to S3
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)

	response := `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><Name>` + bucket + `</Name>`
	for _, key := range keys {
		response += `<Contents><Key>` + key + `</Key></Contents>`
	}
	response += `</ListBucketResult>`
	_, _ = w.Write([]byte(response))
}

func TestNewBackend_MissingBucket(t *testing.T) {
	_, err := NewBackend(map[string]string{
		"region": "us-east-1",
	})
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
		"region": "us-east-1",
	})
	if err == nil {
		t.Error("expected error for empty bucket")
	}
}

func TestNewBackend_DefaultRegion(t *testing.T) {
	// Create a mock server
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	b, err := NewBackend(map[string]string{
		"bucket":           "test-bucket",
		"endpoint":         server.URL,
		"access_key":       "test-key",
		"secret_key":       "test-secret",
		"force_path_style": "true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s3b := b.(*Backend)
	if s3b.region != "us-east-1" {
		t.Errorf("expected default region 'us-east-1', got %q", s3b.region)
	}
}

func TestBackend_Type(t *testing.T) {
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	b, err := NewBackend(map[string]string{
		"bucket":           "test-bucket",
		"endpoint":         server.URL,
		"access_key":       "test-key",
		"secret_key":       "test-secret",
		"force_path_style": "true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.Type() != "s3" {
		t.Errorf("expected type 's3', got %q", b.Type())
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

func TestS3Lock_ID(t *testing.T) {
	info := backend.LockInfo{
		ID:        "test-lock-id",
		Path:      "test/state",
		Who:       "test-user",
		Operation: "plan",
	}

	lock := &s3Lock{
		info: info,
	}

	if lock.ID() != "test-lock-id" {
		t.Errorf("expected ID 'test-lock-id', got %q", lock.ID())
	}
}

func TestS3Lock_Info(t *testing.T) {
	info := backend.LockInfo{
		ID:        "test-lock-id",
		Path:      "test/state",
		Who:       "test-user",
		Operation: "apply",
		Created:   time.Now(),
	}

	lock := &s3Lock{
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

func TestMockServer_GetNotFound(t *testing.T) {
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	resp, err := http.Get(server.URL + "/bucket/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestMockServer_PutAndGet(t *testing.T) {
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Put
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/bucket/test.json", bytes.NewReader(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Get
	resp, err = http.Get(server.URL + "/bucket/test.json")
	if err != nil {
		t.Fatalf("get request failed: %v", err)
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
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Put first
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/bucket/test.json", bytes.NewReader(data))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Delete
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/bucket/test.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp, err = http.Get(server.URL + "/bucket/test.json")
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestMockServer_Head(t *testing.T) {
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Put first
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/bucket/test.json", bytes.NewReader(data))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Head
	req, _ = http.NewRequest(http.MethodHead, server.URL+"/bucket/test.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("head request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Head non-existent
	req, _ = http.NewRequest(http.MethodHead, server.URL+"/bucket/nonexistent.json", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("head request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestMockServer_ListObjects(t *testing.T) {
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Put some objects
	objects := []string{"state/env1.json", "state/env2.json", "other/file.txt"}
	for _, obj := range objects {
		req, _ := http.NewRequest(http.MethodPut, server.URL+"/bucket/"+obj, bytes.NewReader([]byte("{}")))
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}

	// List all
	resp, err := http.Get(server.URL + "/bucket/?list-type=2")
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	for _, obj := range objects {
		if !strings.Contains(string(body), obj) {
			t.Errorf("expected response to contain %q", obj)
		}
	}

	// List with prefix
	resp, err = http.Get(server.URL + "/bucket/?list-type=2&prefix=state/")
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

func TestBackendConfig_WithCustomEndpoint(t *testing.T) {
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	b, err := NewBackend(map[string]string{
		"bucket":           "test-bucket",
		"region":           "us-west-2",
		"endpoint":         server.URL,
		"access_key":       "test-key",
		"secret_key":       "test-secret",
		"force_path_style": "true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s3b := b.(*Backend)
	if s3b.bucket != "test-bucket" {
		t.Errorf("expected bucket 'test-bucket', got %q", s3b.bucket)
	}
	if s3b.region != "us-west-2" {
		t.Errorf("expected region 'us-west-2', got %q", s3b.region)
	}
}

func TestBackendConfig_WithKeyPrefix(t *testing.T) {
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	defer server.Close()

	b, err := NewBackend(map[string]string{
		"bucket":           "test-bucket",
		"key":              "cldctl/state",
		"endpoint":         server.URL,
		"access_key":       "test-key",
		"secret_key":       "test-secret",
		"force_path_style": "true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s3b := b.(*Backend)
	if s3b.prefix != "cldctl/state" {
		t.Errorf("expected prefix 'cldctl/state', got %q", s3b.prefix)
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
