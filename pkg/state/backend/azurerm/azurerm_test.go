package azurerm

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

// mockAzureBlobServer simulates Azure Blob Storage API for testing.
type mockAzureBlobServer struct {
	mu       sync.RWMutex
	blobs    map[string][]byte
	metadata map[string]map[string]string
}

func newMockAzureBlobServer() *mockAzureBlobServer {
	return &mockAzureBlobServer{
		blobs:    make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}
}

func (m *mockAzureBlobServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Parse container and blob from path: /container/blob
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		// Handle container-level operations
		if r.URL.Query().Get("restype") == "container" && r.URL.Query().Get("comp") == "list" {
			// List blobs operation
			m.handleListBlobs(w, r, parts[0])
			return
		}
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	container := parts[0]
	blob := parts[1]
	key := container + "/" + blob

	switch r.Method {
	case http.MethodGet:
		m.handleGet(w, key)
	case http.MethodPut:
		m.handlePut(w, r, key)
	case http.MethodDelete:
		m.handleDelete(w, key)
	case http.MethodHead:
		m.handleHead(w, key)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *mockAzureBlobServer) handleGet(w http.ResponseWriter, key string) {
	data, ok := m.blobs[key]
	if !ok {
		w.Header().Set("x-ms-error-code", "BlobNotFound")
		http.Error(w, "BlobNotFound", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (m *mockAzureBlobServer) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m.blobs[key] = data
	w.WriteHeader(http.StatusCreated)
}

func (m *mockAzureBlobServer) handleDelete(w http.ResponseWriter, key string) {
	if _, ok := m.blobs[key]; !ok {
		w.Header().Set("x-ms-error-code", "BlobNotFound")
		http.Error(w, "BlobNotFound", http.StatusNotFound)
		return
	}
	delete(m.blobs, key)
	w.WriteHeader(http.StatusAccepted)
}

func (m *mockAzureBlobServer) handleHead(w http.ResponseWriter, key string) {
	if _, ok := m.blobs[key]; !ok {
		w.Header().Set("x-ms-error-code", "BlobNotFound")
		http.Error(w, "BlobNotFound", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (m *mockAzureBlobServer) handleListBlobs(w http.ResponseWriter, r *http.Request, container string) {
	prefix := r.URL.Query().Get("prefix")
	
	var blobs []string
	for key := range m.blobs {
		if strings.HasPrefix(key, container+"/") {
			blobName := strings.TrimPrefix(key, container+"/")
			if prefix == "" || strings.HasPrefix(blobName, prefix) {
				blobs = append(blobs, blobName)
			}
		}
	}

	// Return XML response similar to Azure
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	
	response := `<?xml version="1.0" encoding="utf-8"?><EnumerationResults><Blobs>`
	for _, blob := range blobs {
		response += `<Blob><Name>` + blob + `</Name></Blob>`
	}
	response += `</Blobs></EnumerationResults>`
	_, _ = w.Write([]byte(response))
}

func TestNewBackend_MissingStorageAccount(t *testing.T) {
	_, err := NewBackend(map[string]string{
		"container_name": "test-container",
	})
	if err == nil {
		t.Error("expected error for missing storage account")
	}
	if !strings.Contains(err.Error(), "storage_account_name") {
		t.Errorf("expected error message to mention storage_account_name, got: %v", err)
	}
}

func TestNewBackend_MissingContainer(t *testing.T) {
	_, err := NewBackend(map[string]string{
		"storage_account_name": "test-account",
	})
	if err == nil {
		t.Error("expected error for missing container")
	}
	if !strings.Contains(err.Error(), "container_name") {
		t.Errorf("expected error message to mention container_name, got: %v", err)
	}
}

func TestBackend_Type(t *testing.T) {
	// Create a mock server
	mock := newMockAzureBlobServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	b, err := NewBackend(map[string]string{
		"storage_account_name": "testaccount",
		"container_name":       "testcontainer",
		"endpoint":             server.URL + "/",
		"connection_string":    "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=" + server.URL + "/;",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.Type() != "azurerm" {
		t.Errorf("expected type 'azurerm', got %q", b.Type())
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

func TestAzureLock_ID(t *testing.T) {
	info := backend.LockInfo{
		ID:        "test-lock-id",
		Path:      "test/state",
		Who:       "test-user",
		Operation: "plan",
	}

	lock := &azureLock{
		info: info,
	}

	if lock.ID() != "test-lock-id" {
		t.Errorf("expected ID 'test-lock-id', got %q", lock.ID())
	}
}

func TestAzureLock_Info(t *testing.T) {
	info := backend.LockInfo{
		ID:        "test-lock-id",
		Path:      "test/state",
		Who:       "test-user",
		Operation: "apply",
		Created:   time.Now(),
	}

	lock := &azureLock{
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

func TestToPtr(t *testing.T) {
	str := "test"
	ptr := toPtr(str)
	if ptr == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *ptr != str {
		t.Errorf("expected %q, got %q", str, *ptr)
	}

	num := 42
	numPtr := toPtr(num)
	if *numPtr != num {
		t.Errorf("expected %d, got %d", num, *numPtr)
	}
}

func TestBackend_InterfaceCompliance(t *testing.T) {
	// Verify that Backend implements the backend.Backend interface
	var _ backend.Backend = (*Backend)(nil)
}

func TestMockServer_GetNotFound(t *testing.T) {
	mock := newMockAzureBlobServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	resp, err := http.Get(server.URL + "/container/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestMockServer_PutAndGet(t *testing.T) {
	mock := newMockAzureBlobServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Put
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/container/test.json", bytes.NewReader(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	// Get
	resp, err = http.Get(server.URL + "/container/test.json")
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
	mock := newMockAzureBlobServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Put first
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/container/test.json", bytes.NewReader(data))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Delete
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/container/test.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp, err = http.Get(server.URL + "/container/test.json")
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestMockServer_Head(t *testing.T) {
	mock := newMockAzureBlobServer()
	server := httptest.NewServer(mock)
	defer server.Close()

	// Put first
	data := []byte(`{"test": "data"}`)
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/container/test.json", bytes.NewReader(data))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Head
	req, _ = http.NewRequest(http.MethodHead, server.URL+"/container/test.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("head request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Head non-existent
	req, _ = http.NewRequest(http.MethodHead, server.URL+"/container/nonexistent.json", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("head request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
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
			errorMsg:    "storage_account_name",
		},
		{
			name: "missing container",
			config: map[string]string{
				"storage_account_name": "test",
			},
			expectError: true,
			errorMsg:    "container_name",
		},
		{
			name: "empty storage account",
			config: map[string]string{
				"storage_account_name": "",
				"container_name":       "test",
			},
			expectError: true,
			errorMsg:    "storage_account_name",
		},
		{
			name: "empty container",
			config: map[string]string{
				"storage_account_name": "test",
				"container_name":       "",
			},
			expectError: true,
			errorMsg:    "container_name",
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
