package inference

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectJSPackageManager_FromPackageManagerField(t *testing.T) {
	tests := []struct {
		name           string
		packageManager string
		want           string
	}{
		{"pnpm with version", "pnpm@8.0.0", "pnpm"},
		{"pnpm without version", "pnpm", "pnpm"},
		{"yarn with version", "yarn@4.0.0", "yarn"},
		{"yarn berry", "yarn@3.6.0", "yarn"},
		{"bun with version", "bun@1.0.0", "bun"},
		{"npm with version", "npm@10.0.0", "npm"},
		{"uppercase PNPM", "PNPM@8.0.0", "pnpm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := &PackageJSON{
				PackageManager: tt.packageManager,
			}
			// Use a temp directory with no lock files
			tmpDir := t.TempDir()
			got := detectJSPackageManager(tmpDir, pkg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectJSPackageManager_FromLockFiles(t *testing.T) {
	tests := []struct {
		name     string
		lockFile string
		want     string
	}{
		{"pnpm lock file", "pnpm-lock.yaml", "pnpm"},
		{"yarn lock file", "yarn.lock", "yarn"},
		{"bun lock file", "bun.lockb", "bun"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			// Create the lock file
			err := os.WriteFile(filepath.Join(tmpDir, tt.lockFile), []byte{}, 0644)
			require.NoError(t, err)

			// No packageManager field in package.json
			pkg := &PackageJSON{}
			got := detectJSPackageManager(tmpDir, pkg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectJSPackageManager_PackageManagerFieldTakesPrecedence(t *testing.T) {
	// Create a temp directory with a yarn.lock file
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "yarn.lock"), []byte{}, 0644)
	require.NoError(t, err)

	// But package.json says to use pnpm
	pkg := &PackageJSON{
		PackageManager: "pnpm@8.0.0",
	}

	got := detectJSPackageManager(tmpDir, pkg)
	// packageManager field should take precedence over lock file
	assert.Equal(t, "pnpm", got)
}

func TestDetectJSPackageManager_DefaultsToNpm(t *testing.T) {
	tmpDir := t.TempDir()
	pkg := &PackageJSON{} // No packageManager field

	got := detectJSPackageManager(tmpDir, pkg)
	assert.Equal(t, "npm", got)
}

func TestDetectJSPackageManager_NilPackageJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Should not panic and should default to npm
	got := detectJSPackageManager(tmpDir, nil)
	assert.Equal(t, "npm", got)
}

func TestGetInstallCommand(t *testing.T) {
	tests := []struct {
		pm   string
		want string
	}{
		{"pnpm", "pnpm install"},
		{"yarn", "yarn install"},
		{"bun", "bun install"},
		{"npm", "npm install"},
		{"unknown", "npm install"},
	}

	for _, tt := range tests {
		t.Run(tt.pm, func(t *testing.T) {
			got := getInstallCommand(tt.pm)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunScript(t *testing.T) {
	tests := []struct {
		pm     string
		script string
		want   string
	}{
		{"pnpm", "dev", "pnpm run dev"},
		{"yarn", "dev", "yarn dev"},
		{"bun", "dev", "bun run dev"},
		{"npm", "dev", "npm run dev"},
	}

	for _, tt := range tests {
		t.Run(tt.pm+"_"+tt.script, func(t *testing.T) {
			got := runScript(tt.pm, tt.script)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestJavaScriptInferrer_InferWithPackageManager(t *testing.T) {
	// Create a temp directory with package.json that specifies pnpm
	tmpDir := t.TempDir()
	packageJSON := `{
		"name": "test-app",
		"packageManager": "pnpm@8.15.0",
		"scripts": {
			"dev": "next dev",
			"build": "next build"
		},
		"dependencies": {
			"next": "14.0.0"
		}
	}`
	err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644)
	require.NoError(t, err)

	inferrer := &JavaScriptInferrer{}
	info, err := inferrer.Infer(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, "pnpm", info.PackageManager)
	assert.Equal(t, "pnpm install", info.InstallCommand)
	assert.Equal(t, "pnpm run dev", info.DevCommand)
	assert.Equal(t, "pnpm run build", info.BuildCommand)
}
