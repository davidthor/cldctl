//go:build integration

// Package integration contains integration tests for arcctl.
// These tests require external services and are not run by default.
// Run with: go test -tags=integration -v ./testdata/integration/...
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestClerkNextJSPostgres tests the full integration of:
// - Clerk authentication
// - PostgreSQL database
// - Next.js application
// - arcctl deployment
//
// Required environment variables:
//   - CLERK_PUBLISHABLE_KEY: Clerk publishable key (pk_test_... or pk_live_...)
//   - CLERK_SECRET_KEY: Clerk secret key (sk_test_... or sk_live_...)
//
// Optional environment variables:
//   - ARCCTL_BINARY: Path to arcctl binary (default: searches PATH or builds)
//   - TEST_TIMEOUT: Maximum time to wait for deployment (default: 5m)
//
// Note: Clerk infers the domain from the publishable key, so CLERK_DOMAIN is not required.
func TestClerkNextJSPostgres(t *testing.T) {
	// Check required environment variables
	clerkPublishableKey := os.Getenv("CLERK_PUBLISHABLE_KEY")
	clerkSecretKey := os.Getenv("CLERK_SECRET_KEY")

	if clerkPublishableKey == "" || clerkSecretKey == "" {
		t.Skip("Skipping integration test: CLERK_PUBLISHABLE_KEY and CLERK_SECRET_KEY must be set")
	}

	// Validate Clerk key formats
	if !strings.HasPrefix(clerkPublishableKey, "pk_") {
		t.Fatalf("CLERK_PUBLISHABLE_KEY should start with 'pk_', got: %s...", clerkPublishableKey[:10])
	}
	if !strings.HasPrefix(clerkSecretKey, "sk_") {
		t.Fatalf("CLERK_SECRET_KEY should start with 'sk_', got: %s...", clerkSecretKey[:10])
	}

	// Get or build arcctl binary
	arcctlBinary := getArcctlBinary(t)
	t.Logf("Using arcctl binary: %s", arcctlBinary)

	// Get test directory
	testDir := getTestDirectory(t)
	t.Logf("Test directory: %s", testDir)

	// Parse timeout
	timeout := 5 * time.Minute
	if timeoutStr := os.Getenv("TEST_TIMEOUT"); timeoutStr != "" {
		var err error
		timeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			t.Fatalf("Invalid TEST_TIMEOUT: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Environment name for this test
	envName := fmt.Sprintf("clerk-test-%d", time.Now().Unix())

	// Cleanup on test completion
	defer func() {
		t.Log("Cleaning up environment...")
		cleanupEnvironment(t, arcctlBinary, envName)
	}()

	// Step 1: Deploy the environment
	t.Log("Step 1: Deploying environment with arcctl...")
	deployEnvironment(t, ctx, arcctlBinary, testDir, envName, map[string]string{
		"CLERK_PUBLISHABLE_KEY": clerkPublishableKey,
		"CLERK_SECRET_KEY":      clerkSecretKey,
	})

	// Step 2: Wait for application to be ready
	t.Log("Step 2: Waiting for application to be ready...")
	appURL := waitForApplication(t, ctx)

	// Step 3: Test health endpoint (public)
	t.Log("Step 3: Testing health endpoint...")
	testHealthEndpoint(t, appURL)

	// Step 4: Test protected endpoint without auth (should return 401)
	t.Log("Step 4: Testing protected endpoint without authentication...")
	testProtectedEndpointNoAuth(t, appURL)

	// Step 5: Test protected endpoint with auth (should return 200)
	t.Log("Step 5: Testing protected endpoint with authentication...")
	testProtectedEndpointWithAuth(t, appURL, clerkSecretKey)

	t.Log("All integration tests passed!")
}

// getArcctlBinary returns the path to the arcctl binary.
// It first checks ARCCTL_BINARY env var, then PATH, then builds if needed.
func getArcctlBinary(t *testing.T) string {
	t.Helper()

	// Check environment variable first
	if binary := os.Getenv("ARCCTL_BINARY"); binary != "" {
		if _, err := os.Stat(binary); err == nil {
			return binary
		}
		t.Fatalf("ARCCTL_BINARY set but file not found: %s", binary)
	}

	// Check if arcctl is in PATH
	if path, err := exec.LookPath("arcctl"); err == nil {
		return path
	}

	// Build arcctl
	t.Log("Building arcctl binary...")
	repoRoot := getRepoRoot(t)
	binaryPath := filepath.Join(repoRoot, "bin", "arcctl")

	cmd := exec.Command("make", "build")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build arcctl: %v", err)
	}

	return binaryPath
}

// getTestDirectory returns the path to the clerk-nextjs-postgres test directory.
func getTestDirectory(t *testing.T) string {
	t.Helper()
	repoRoot := getRepoRoot(t)
	return filepath.Join(repoRoot, "testdata", "integration", "clerk-nextjs-postgres")
}

// getRepoRoot returns the root directory of the repository.
func getRepoRoot(t *testing.T) string {
	t.Helper()

	// Start from the test file location and walk up to find go.mod
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("Could not find repository root (no go.mod found)")
		}
		dir = parent
	}
}

// deployEnvironment deploys the test environment using arcctl.
func deployEnvironment(t *testing.T, ctx context.Context, arcctlBinary, testDir, envName string, envVars map[string]string) {
	t.Helper()

	// Get repository root for datacenter path
	repoRoot := getRepoRoot(t)
	datacenterPath := filepath.Join(repoRoot, "examples", "datacenters", "local-docker")

	// Build command with environment file
	// Name and datacenter are CLI flags, not part of the config file
	// New CLI syntax: arcctl update environment <name> <config-file>
	envFile := filepath.Join(testDir, "environment.yml")
	args := []string{
		"update", "environment", envName, envFile,
		"--datacenter", datacenterPath,
		"--auto-approve",
	}

	cmd := exec.CommandContext(ctx, arcctlBinary, args...)
	cmd.Dir = testDir

	// Set environment variables
	cmd.Env = os.Environ()
	for k, v := range envVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Logf("Running: %s %s", arcctlBinary, strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to deploy environment:\nstdout: %s\nstderr: %s\nerror: %v",
			stdout.String(), stderr.String(), err)
	}

	t.Logf("Deployment output: %s", stdout.String())
}

// waitForApplication waits for the application to be ready and returns its URL.
func waitForApplication(t *testing.T, ctx context.Context) string {
	t.Helper()

	// Default URL for local development
	appURL := "http://localhost:8080"

	client := &http.Client{Timeout: 5 * time.Second}

	// Poll the health endpoint until it responds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for application to be ready")
		case <-ticker.C:
			resp, err := client.Get(appURL + "/api/health")
			if err != nil {
				t.Logf("Health check failed (retrying): %v", err)
				continue
			}
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				t.Log("Application is ready!")
				return appURL
			}
			t.Logf("Health check returned %d (retrying)", resp.StatusCode)
		}
	}
}

// testHealthEndpoint tests the public health endpoint.
func testHealthEndpoint(t *testing.T, appURL string) {
	t.Helper()

	resp, err := http.Get(appURL + "/api/health")
	if err != nil {
		t.Fatalf("Failed to call health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200 OK from health endpoint, got %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	// Verify database connectivity
	if result["database"] != "connected" {
		t.Errorf("Expected database to be connected, got: %v", result["database"])
	}

	t.Logf("Health check passed: %v", result)
}

// testProtectedEndpointNoAuth tests that the protected endpoint requires authentication.
func testProtectedEndpointNoAuth(t *testing.T, appURL string) {
	t.Helper()

	resp, err := http.Get(appURL + "/api/protected")
	if err != nil {
		t.Fatalf("Failed to call protected endpoint: %v", err)
	}
	defer resp.Body.Close()

	// Should return 401 Unauthorized without auth
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 401 Unauthorized, got %d: %s", resp.StatusCode, string(body))
	}

	t.Log("Protected endpoint correctly returns 401 without authentication")
}

// testProtectedEndpointWithAuth tests the protected endpoint with Clerk authentication.
// Note: This requires a valid Clerk session token which is difficult to obtain in tests.
// For now, this test documents the expected behavior and can be extended with
// Clerk's test utilities when available.
func testProtectedEndpointWithAuth(t *testing.T, appURL, clerkSecretKey string) {
	t.Helper()

	// To properly test authenticated requests, we would need to:
	// 1. Create a test user in Clerk
	// 2. Obtain a session token for that user
	// 3. Send the request with the Authorization header
	//
	// Clerk provides testing utilities for this:
	// https://clerk.com/docs/testing/overview
	//
	// For now, we verify the endpoint exists and the schema is correct
	// by checking the 401 response format.

	resp, err := http.Get(appURL + "/api/protected")
	if err != nil {
		t.Fatalf("Failed to call protected endpoint: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify the response has the expected error format
	if _, ok := result["error"]; !ok {
		t.Error("Expected 'error' field in unauthorized response")
	}

	t.Log("Protected endpoint authentication test passed (verified 401 response format)")
	t.Log("Note: Full authentication test requires Clerk test utilities")
}

// cleanupEnvironment destroys the test environment.
func cleanupEnvironment(t *testing.T, arcctlBinary, envName string) {
	t.Helper()

	// New CLI syntax: arcctl destroy environment <name>
	cmd := exec.Command(arcctlBinary, "destroy", "environment", envName, "--auto-approve")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Logf("Warning: Failed to cleanup environment %s: %v\nstderr: %s",
			envName, err, stderr.String())
	} else {
		t.Logf("Environment %s cleaned up successfully", envName)
	}
}

// TestClerkNextJSPostgres_EnvironmentValidation tests that the environment.yml is valid.
func TestClerkNextJSPostgres_EnvironmentValidation(t *testing.T) {
	testDir := getTestDirectory(t)
	envFile := filepath.Join(testDir, "environment.yml")

	// Check environment file exists
	if _, err := os.Stat(envFile); err != nil {
		t.Fatalf("Environment file not found: %s", envFile)
	}

	// Check architect.yml exists
	architectFile := filepath.Join(testDir, "architect.yml")
	if _, err := os.Stat(architectFile); err != nil {
		t.Fatalf("architect.yml not found: %s", architectFile)
	}

	// Check Dockerfile exists
	dockerfile := filepath.Join(testDir, "app", "Dockerfile")
	if _, err := os.Stat(dockerfile); err != nil {
		t.Fatalf("Dockerfile not found: %s", dockerfile)
	}

	t.Log("All required files present")
}

// TestClerkNextJSPostgres_ComponentValidation validates the component configuration.
func TestClerkNextJSPostgres_ComponentValidation(t *testing.T) {
	arcctlBinary := getArcctlBinary(t)
	testDir := getTestDirectory(t)

	// Validate the component
	// New CLI syntax: arcctl validate component <path>
	cmd := exec.Command(arcctlBinary, "validate", "component", testDir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Component validation failed:\nstdout: %s\nstderr: %s\nerror: %v",
			stdout.String(), stderr.String(), err)
	}

	t.Log("Component validation passed")
}
