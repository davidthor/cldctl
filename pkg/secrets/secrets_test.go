package secrets

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.providers == nil {
		t.Error("providers map is nil")
	}
	if m.priority == nil {
		t.Error("priority slice is nil")
	}
	if m.cache == nil {
		t.Error("cache is nil")
	}
}

func TestDefaultManager(t *testing.T) {
	m := DefaultManager()
	if m == nil {
		t.Fatal("DefaultManager returned nil")
	}

	// Should have env provider registered
	if len(m.providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(m.providers))
	}
	if _, ok := m.providers["env"]; !ok {
		t.Error("env provider not registered")
	}
}

func TestManager_RegisterProvider(t *testing.T) {
	m := NewManager()

	provider := NewFileProvider(map[string]string{"key": "value"})
	m.RegisterProvider(provider)

	if len(m.providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(m.providers))
	}
	if _, ok := m.providers["file"]; !ok {
		t.Error("file provider not registered")
	}
	if len(m.priority) != 1 || m.priority[0] != "file" {
		t.Error("priority not set correctly")
	}
}

func TestManager_SetPriority(t *testing.T) {
	m := NewManager()
	m.RegisterProvider(NewEnvProvider())
	m.RegisterProvider(NewFileProvider(map[string]string{}))

	m.SetPriority([]string{"file", "env"})

	if len(m.priority) != 2 {
		t.Errorf("Expected 2 priorities, got %d", len(m.priority))
	}
	if m.priority[0] != "file" {
		t.Errorf("First priority should be 'file', got %q", m.priority[0])
	}
	if m.priority[1] != "env" {
		t.Errorf("Second priority should be 'env', got %q", m.priority[1])
	}
}

func TestManager_Get(t *testing.T) {
	m := NewManager()
	m.RegisterProvider(NewFileProvider(map[string]string{
		"db-password": "secret123",
		"api-key":     "apikey456",
	}))

	ctx := context.Background()

	t.Run("existing secret", func(t *testing.T) {
		value, err := m.Get(ctx, "db-password")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "secret123" {
			t.Errorf("Value: got %q, want %q", value, "secret123")
		}
	})

	t.Run("nonexistent secret", func(t *testing.T) {
		_, err := m.Get(ctx, "nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent secret")
		}
	})

	t.Run("caching", func(t *testing.T) {
		// First call populates cache
		value1, _ := m.Get(ctx, "api-key")
		// Second call should use cache
		value2, _ := m.Get(ctx, "api-key")

		if value1 != value2 {
			t.Error("Cached value should match")
		}
	})
}

func TestManager_GetFromProvider(t *testing.T) {
	m := NewManager()
	m.RegisterProvider(NewFileProvider(map[string]string{
		"secret1": "value1",
	}))

	ctx := context.Background()

	t.Run("existing provider and secret", func(t *testing.T) {
		value, err := m.GetFromProvider(ctx, "file", "secret1")
		if err != nil {
			t.Fatalf("GetFromProvider failed: %v", err)
		}
		if value != "value1" {
			t.Errorf("Value: got %q, want %q", value, "value1")
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		_, err := m.GetFromProvider(ctx, "unknown", "secret1")
		if err == nil {
			t.Error("Expected error for unknown provider")
		}
	})
}

func TestManager_GetBatch(t *testing.T) {
	m := NewManager()
	m.RegisterProvider(NewFileProvider(map[string]string{
		"secret1": "value1",
		"secret2": "value2",
		"secret3": "value3",
	}))

	ctx := context.Background()
	keys := []string{"secret1", "secret2", "nonexistent"}

	results, err := m.GetBatch(ctx, keys)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
	if results["secret1"] != "value1" {
		t.Errorf("secret1: got %q, want %q", results["secret1"], "value1")
	}
	if results["secret2"] != "value2" {
		t.Errorf("secret2: got %q, want %q", results["secret2"], "value2")
	}
}

func TestManager_ResolveSecrets(t *testing.T) {
	m := NewManager()
	m.RegisterProvider(NewFileProvider(map[string]string{
		"db-password": "supersecret",
		"api-key":     "myapikey",
	}))

	ctx := context.Background()

	t.Run("simple string resolution", func(t *testing.T) {
		data := map[string]interface{}{
			"password": "${secret:db-password}",
			"normal":   "regular value",
		}

		result, err := m.ResolveSecrets(ctx, data)
		if err != nil {
			t.Fatalf("ResolveSecrets failed: %v", err)
		}

		if result["password"] != "supersecret" {
			t.Errorf("password: got %q, want %q", result["password"], "supersecret")
		}
		if result["normal"] != "regular value" {
			t.Errorf("normal: got %q, want %q", result["normal"], "regular value")
		}
	})

	t.Run("nested map resolution", func(t *testing.T) {
		data := map[string]interface{}{
			"database": map[string]interface{}{
				"password": "${secret:db-password}",
				"host":     "localhost",
			},
		}

		result, err := m.ResolveSecrets(ctx, data)
		if err != nil {
			t.Fatalf("ResolveSecrets failed: %v", err)
		}

		dbConfig := result["database"].(map[string]interface{})
		if dbConfig["password"] != "supersecret" {
			t.Errorf("nested password: got %q", dbConfig["password"])
		}
	})

	t.Run("array resolution", func(t *testing.T) {
		data := map[string]interface{}{
			"secrets": []interface{}{
				"${secret:db-password}",
				"${secret:api-key}",
				"plain",
			},
		}

		result, err := m.ResolveSecrets(ctx, data)
		if err != nil {
			t.Fatalf("ResolveSecrets failed: %v", err)
		}

		secrets := result["secrets"].([]interface{})
		if len(secrets) != 3 {
			t.Fatalf("Expected 3 items, got %d", len(secrets))
		}
		if secrets[0] != "supersecret" {
			t.Errorf("secrets[0]: got %q", secrets[0])
		}
		if secrets[1] != "myapikey" {
			t.Errorf("secrets[1]: got %q", secrets[1])
		}
	})

	t.Run("with provider prefix", func(t *testing.T) {
		data := map[string]interface{}{
			"password": "${secret:file:db-password}",
		}

		result, err := m.ResolveSecrets(ctx, data)
		if err != nil {
			t.Fatalf("ResolveSecrets failed: %v", err)
		}

		if result["password"] != "supersecret" {
			t.Errorf("password: got %q", result["password"])
		}
	})

	t.Run("inline secret in string", func(t *testing.T) {
		data := map[string]interface{}{
			"url": "postgresql://user:${secret:db-password}@localhost/db",
		}

		result, err := m.ResolveSecrets(ctx, data)
		if err != nil {
			t.Fatalf("ResolveSecrets failed: %v", err)
		}

		expected := "postgresql://user:supersecret@localhost/db"
		if result["url"] != expected {
			t.Errorf("url: got %q, want %q", result["url"], expected)
		}
	})

	t.Run("unclosed reference", func(t *testing.T) {
		data := map[string]interface{}{
			"bad": "${secret:unclosed",
		}

		_, err := m.ResolveSecrets(ctx, data)
		if err == nil {
			t.Error("Expected error for unclosed reference")
		}
	})
}

func TestManager_ClearCache(t *testing.T) {
	m := NewManager()
	m.RegisterProvider(NewFileProvider(map[string]string{
		"key": "value",
	}))

	ctx := context.Background()

	// Populate cache
	_, _ = m.Get(ctx, "key")

	// Verify cache has item
	if _, ok := m.cache.get("key"); !ok {
		t.Error("Cache should have 'key'")
	}

	// Clear cache
	m.ClearCache()

	// Verify cache is empty
	if _, ok := m.cache.get("key"); ok {
		t.Error("Cache should be empty after clear")
	}
}

func TestManager_PriorityOrder(t *testing.T) {
	m := NewManager()

	// File provider with one value
	m.RegisterProvider(NewFileProvider(map[string]string{
		"shared-key": "file-value",
	}))

	// Create a custom provider for env with different value
	env := &mockProvider{
		name: "mock-env",
		secrets: map[string]string{
			"shared-key": "env-value",
		},
	}
	m.RegisterProvider(env)

	ctx := context.Background()

	t.Run("first provider wins", func(t *testing.T) {
		m.SetPriority([]string{"file", "mock-env"})

		value, err := m.Get(ctx, "shared-key")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "file-value" {
			t.Errorf("Value should be from file provider: got %q", value)
		}
	})

	t.Run("second provider wins with different priority", func(t *testing.T) {
		m.ClearCache() // Clear to test fresh
		m.SetPriority([]string{"mock-env", "file"})

		value, err := m.Get(ctx, "shared-key")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "env-value" {
			t.Errorf("Value should be from mock-env provider: got %q", value)
		}
	})
}

// mockProvider for testing
type mockProvider struct {
	name    string
	secrets map[string]string
}

func (p *mockProvider) Name() string { return p.name }

func (p *mockProvider) Get(ctx context.Context, key string) (string, error) {
	if v, ok := p.secrets[key]; ok {
		return v, nil
	}
	return "", ErrSecretNotFound
}

func (p *mockProvider) GetBatch(ctx context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, k := range keys {
		if v, ok := p.secrets[k]; ok {
			result[k] = v
		}
	}
	return result, nil
}

func (p *mockProvider) List(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}

func (p *mockProvider) Set(ctx context.Context, key, value string) error {
	p.secrets[key] = value
	return nil
}

func (p *mockProvider) Delete(ctx context.Context, key string) error {
	delete(p.secrets, key)
	return nil
}

// EnvProvider tests

func TestEnvProvider(t *testing.T) {
	provider := NewEnvProvider()

	if provider.Name() != "env" {
		t.Errorf("Name: got %q, want %q", provider.Name(), "env")
	}
}

func TestEnvProviderWithPrefix(t *testing.T) {
	provider := NewEnvProviderWithPrefix("MYAPP_")

	if provider.prefix != "MYAPP_" {
		t.Errorf("prefix: got %q, want %q", provider.prefix, "MYAPP_")
	}
}

func TestEnvProvider_Get(t *testing.T) {
	provider := NewEnvProvider()
	ctx := context.Background()

	// Set test env var
	os.Setenv("CLDCTL_SECRET_TEST_KEY", "test-value")
	defer os.Unsetenv("CLDCTL_SECRET_TEST_KEY")

	t.Run("with prefix", func(t *testing.T) {
		value, err := provider.Get(ctx, "test-key")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "test-value" {
			t.Errorf("Value: got %q, want %q", value, "test-value")
		}
	})

	t.Run("without prefix - direct name", func(t *testing.T) {
		os.Setenv("DIRECT_KEY", "direct-value")
		defer os.Unsetenv("DIRECT_KEY")

		value, err := provider.Get(ctx, "DIRECT_KEY")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "direct-value" {
			t.Errorf("Value: got %q, want %q", value, "direct-value")
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := provider.Get(ctx, "nonexistent-key")
		if err != ErrSecretNotFound {
			t.Errorf("Expected ErrSecretNotFound, got %v", err)
		}
	})
}

func TestEnvProvider_GetBatch(t *testing.T) {
	provider := NewEnvProvider()
	ctx := context.Background()

	os.Setenv("CLDCTL_SECRET_KEY1", "value1")
	os.Setenv("CLDCTL_SECRET_KEY2", "value2")
	defer os.Unsetenv("CLDCTL_SECRET_KEY1")
	defer os.Unsetenv("CLDCTL_SECRET_KEY2")

	results, err := provider.GetBatch(ctx, []string{"key1", "key2", "nonexistent"})
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

func TestEnvProvider_List(t *testing.T) {
	provider := NewEnvProvider()
	ctx := context.Background()

	os.Setenv("CLDCTL_SECRET_DB_PASSWORD", "pass")
	os.Setenv("CLDCTL_SECRET_DB_USER", "user")
	os.Setenv("CLDCTL_SECRET_API_KEY", "key")
	defer os.Unsetenv("CLDCTL_SECRET_DB_PASSWORD")
	defer os.Unsetenv("CLDCTL_SECRET_DB_USER")
	defer os.Unsetenv("CLDCTL_SECRET_API_KEY")

	keys, err := provider.List(ctx, "db")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Should find db-password and db-user
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d: %v", len(keys), keys)
	}

	// Verify keys contain expected values
	found := 0
	for _, k := range keys {
		if strings.HasPrefix(k, "db-") {
			found++
		}
	}
	if found != 2 {
		t.Errorf("Expected 2 'db-' prefixed keys, got %d", found)
	}
}

func TestEnvProvider_Set(t *testing.T) {
	provider := NewEnvProvider()
	ctx := context.Background()

	err := provider.Set(ctx, "new-key", "new-value")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	defer os.Unsetenv("CLDCTL_SECRET_NEW_KEY")

	// Verify it was set
	value := os.Getenv("CLDCTL_SECRET_NEW_KEY")
	if value != "new-value" {
		t.Errorf("Env var not set correctly: got %q", value)
	}
}

func TestEnvProvider_Delete(t *testing.T) {
	provider := NewEnvProvider()
	ctx := context.Background()

	os.Setenv("CLDCTL_SECRET_TO_DELETE", "value")

	err := provider.Delete(ctx, "to-delete")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	value := os.Getenv("CLDCTL_SECRET_TO_DELETE")
	if value != "" {
		t.Error("Env var should be deleted")
	}
}

// FileProvider tests

func TestFileProvider(t *testing.T) {
	secrets := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}
	provider := NewFileProvider(secrets)

	if provider.Name() != "file" {
		t.Errorf("Name: got %q, want %q", provider.Name(), "file")
	}
}

func TestFileProvider_Get(t *testing.T) {
	provider := NewFileProvider(map[string]string{
		"secret": "mysecret",
	})
	ctx := context.Background()

	t.Run("existing key", func(t *testing.T) {
		value, err := provider.Get(ctx, "secret")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "mysecret" {
			t.Errorf("Value: got %q, want %q", value, "mysecret")
		}
	})

	t.Run("nonexistent key", func(t *testing.T) {
		_, err := provider.Get(ctx, "nonexistent")
		if err != ErrSecretNotFound {
			t.Errorf("Expected ErrSecretNotFound, got %v", err)
		}
	})
}

func TestFileProvider_GetBatch(t *testing.T) {
	provider := NewFileProvider(map[string]string{
		"key1": "value1",
		"key2": "value2",
	})
	ctx := context.Background()

	results, err := provider.GetBatch(ctx, []string{"key1", "key2", "key3"})
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

func TestFileProvider_List(t *testing.T) {
	provider := NewFileProvider(map[string]string{
		"db-password": "pass",
		"db-user":     "user",
		"api-key":     "key",
	})
	ctx := context.Background()

	keys, err := provider.List(ctx, "db-")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}
}

func TestFileProvider_Set(t *testing.T) {
	provider := NewFileProvider(map[string]string{})
	ctx := context.Background()

	err := provider.Set(ctx, "new-key", "new-value")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	value, _ := provider.Get(ctx, "new-key")
	if value != "new-value" {
		t.Errorf("Value not set: got %q", value)
	}
}

func TestFileProvider_Delete(t *testing.T) {
	provider := NewFileProvider(map[string]string{
		"to-delete": "value",
	})
	ctx := context.Background()

	err := provider.Delete(ctx, "to-delete")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = provider.Get(ctx, "to-delete")
	if err != ErrSecretNotFound {
		t.Error("Key should be deleted")
	}
}

// Cache tests

func TestSecretCache(t *testing.T) {
	cache := newSecretCache()

	t.Run("set and get", func(t *testing.T) {
		cache.set("key", "value")

		value, ok := cache.get("key")
		if !ok {
			t.Error("Key should exist in cache")
		}
		if value != "value" {
			t.Errorf("Value: got %q, want %q", value, "value")
		}
	})

	t.Run("get nonexistent", func(t *testing.T) {
		_, ok := cache.get("nonexistent")
		if ok {
			t.Error("Nonexistent key should not be found")
		}
	})

	t.Run("clear", func(t *testing.T) {
		cache.set("key1", "value1")
		cache.set("key2", "value2")

		cache.clear()

		if _, ok := cache.get("key1"); ok {
			t.Error("Cache should be empty after clear")
		}
	})
}

func TestSecretCache_Concurrent(t *testing.T) {
	cache := newSecretCache()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key"
			cache.set(key, "value")
			cache.get(key)
		}(i)
	}
	wg.Wait()
}

func TestManager_Concurrent(t *testing.T) {
	m := NewManager()
	m.RegisterProvider(NewFileProvider(map[string]string{
		"key": "value",
	}))

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Get(ctx, "key")
		}()
	}
	wg.Wait()
}
