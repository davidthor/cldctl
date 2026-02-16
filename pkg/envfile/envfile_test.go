package envfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvFile_BasicKeyValue(t *testing.T) {
	content := []byte(`
KEY1=value1
KEY2=value2
`)
	vars := make(map[string]string)
	err := parseEnvFile(content, vars)
	require.NoError(t, err)
	assert.Equal(t, "value1", vars["KEY1"])
	assert.Equal(t, "value2", vars["KEY2"])
}

func TestParseEnvFile_CommentsAndEmptyLines(t *testing.T) {
	content := []byte(`
# This is a comment
KEY1=value1

# Another comment

KEY2=value2
`)
	vars := make(map[string]string)
	err := parseEnvFile(content, vars)
	require.NoError(t, err)
	assert.Equal(t, "value1", vars["KEY1"])
	assert.Equal(t, "value2", vars["KEY2"])
	assert.Len(t, vars, 2)
}

func TestParseEnvFile_QuotedValues(t *testing.T) {
	content := []byte(`
DOUBLE="hello world"
SINGLE='hello world'
UNQUOTED=hello world
`)
	vars := make(map[string]string)
	err := parseEnvFile(content, vars)
	require.NoError(t, err)
	assert.Equal(t, "hello world", vars["DOUBLE"])
	assert.Equal(t, "hello world", vars["SINGLE"])
	assert.Equal(t, "hello world", vars["UNQUOTED"])
}

func TestParseEnvFile_ExportPrefix(t *testing.T) {
	content := []byte(`
export KEY1=value1
export KEY2="value2"
`)
	vars := make(map[string]string)
	err := parseEnvFile(content, vars)
	require.NoError(t, err)
	assert.Equal(t, "value1", vars["KEY1"])
	assert.Equal(t, "value2", vars["KEY2"])
}

func TestParseEnvFile_EmptyValue(t *testing.T) {
	content := []byte(`KEY=`)
	vars := make(map[string]string)
	err := parseEnvFile(content, vars)
	require.NoError(t, err)
	assert.Equal(t, "", vars["KEY"])
}

func TestParseEnvFile_ValueWithEquals(t *testing.T) {
	content := []byte(`DATABASE_URL=postgresql://user:pass@host:5432/db?sslmode=require`)
	vars := make(map[string]string)
	err := parseEnvFile(content, vars)
	require.NoError(t, err)
	assert.Equal(t, "postgresql://user:pass@host:5432/db?sslmode=require", vars["DATABASE_URL"])
}

func TestLoad_BasicChain(t *testing.T) {
	dir := t.TempDir()

	// Write .env
	err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY1=base\nKEY2=base\n"), 0644)
	require.NoError(t, err)

	// Write .env.local (overrides KEY2)
	err = os.WriteFile(filepath.Join(dir, ".env.local"), []byte("KEY2=local\nKEY3=local\n"), 0644)
	require.NoError(t, err)

	vars, err := Load(dir, "")
	require.NoError(t, err)
	assert.Equal(t, "base", vars["KEY1"])
	assert.Equal(t, "local", vars["KEY2"]) // overridden
	assert.Equal(t, "local", vars["KEY3"])
}

func TestLoad_EnvironmentSpecificFiles(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY1=base\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, ".env.staging"), []byte("KEY1=staging\nKEY2=staging\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, ".env.staging.local"), []byte("KEY2=staging-local\n"), 0644)
	require.NoError(t, err)

	vars, err := Load(dir, "staging")
	require.NoError(t, err)
	assert.Equal(t, "staging", vars["KEY1"])       // overridden by .env.staging
	assert.Equal(t, "staging-local", vars["KEY2"]) // overridden by .env.staging.local
}

func TestLoad_MissingFiles(t *testing.T) {
	dir := t.TempDir()

	// No .env files at all -- should return empty map, no error
	vars, err := Load(dir, "production")
	require.NoError(t, err)
	assert.Empty(t, vars)
}

func TestLoad_OnlyBaseEnv(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=abc123\n"), 0644)
	require.NoError(t, err)

	vars, err := Load(dir, "production")
	require.NoError(t, err)
	assert.Equal(t, "abc123", vars["SECRET"])
}
