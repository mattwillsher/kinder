//go:build integration

package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/hipkoi/kinder/config"
)

// TestKinderIntegration tests the full kinder stack
func TestKinderIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Build kinder binary
	t.Log("Building kinder binary...")
	buildCmd := exec.Command("go", "build", "-o", "kinder-test")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build kinder: %v", err)
	}
	defer os.Remove("kinder-test")

	// Generate CA certificate
	t.Log("Generating CA certificate...")
	genCmd := exec.Command("./kinder-test", "ca", "generate")
	if output, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to generate CA: %v\n%s", err, output)
	}

	// Create network
	t.Log("Creating kinder network...")
	netCmd := exec.Command("./kinder-test", "network", "create")
	netCmd.Run() // Ignore error if network already exists

	// Start all services
	t.Log("Starting all kinder services...")
	startCmd := exec.Command("./kinder-test", "start")
	if output, err := startCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to start kinder services: %v\n%s", err, output)
	}

	// Defer cleanup
	defer func() {
		t.Log("Stopping all kinder services...")
		stopCmd := exec.Command("./kinder-test", "stop")
		stopCmd.Run()
	}()

	// Wait for services to be fully ready (Traefik needs time to get ACME certs)
	t.Log("Waiting for services to be ready...")
	time.Sleep(10 * time.Second)

	// Test endpoints
	tests := []struct {
		name string
		test func(*testing.T)
	}{
		{"Zot Registry", testZotReachability}, // Test Zot first (simpler, no TLS via Traefik)
		{"Step CA", testStepCAReachability},   // Then test Step CA via Traefik
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

// TestKindClusterIntegration tests the full Kind cluster creation with registry config
func TestKindClusterIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if os.Getenv("KINDER_SKIP_KIND_TEST") != "" {
		t.Skip("Skipping Kind cluster test (KINDER_SKIP_KIND_TEST set)")
	}

	// Build kinder binary
	t.Log("Building kinder binary...")
	buildCmd := exec.Command("go", "build", "-o", "kinder-test")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build kinder: %v", err)
	}
	defer os.Remove("kinder-test")

	// Start kinder services first (Kind needs the network and CA)
	t.Log("Starting kinder services...")
	startCmd := exec.Command("./kinder-test", "start")
	if output, err := startCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to start kinder services: %v\n%s", err, output)
	}

	// Defer full cleanup
	defer func() {
		t.Log("Stopping Kind cluster...")
		kindStopCmd := exec.Command("./kinder-test", "kind", "stop")
		kindStopCmd.Run()

		t.Log("Stopping kinder services...")
		stopCmd := exec.Command("./kinder-test", "stop")
		stopCmd.Run()
	}()

	// Wait for services to be ready
	t.Log("Waiting for services to be ready...")
	time.Sleep(5 * time.Second)

	// Start Kind cluster
	t.Log("Starting Kind cluster...")
	kindStartCmd := exec.Command("./kinder-test", "kind", "start")
	if output, err := kindStartCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to start Kind cluster: %v\n%s", err, output)
	}

	// Check Kind status
	t.Log("Checking Kind status...")
	kindStatusCmd := exec.Command("./kinder-test", "kind", "status")
	output, err := kindStatusCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to get Kind status: %v\n%s", err, output)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "running") {
		t.Errorf("Kind cluster should be running, got: %s", outputStr)
	}

	// Get kubeconfig
	t.Log("Getting kubeconfig...")
	kubeconfigCmd := exec.Command("./kinder-test", "kind", "kubeconfig")
	kubeconfigOutput, err := kubeconfigCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to get kubeconfig: %v\n%s", err, kubeconfigOutput)
	}

	if !strings.Contains(string(kubeconfigOutput), "apiVersion") {
		t.Error("kubeconfig should contain apiVersion")
	}

	// Verify certs.d directory was created using the config system
	t.Log("Verifying certs.d directory structure...")

	// Initialize config to get the correct data directory
	if err := config.Initialize(""); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	dataDir, err := config.GetDataDir()
	if err != nil {
		t.Fatalf("Failed to get data directory: %v", err)
	}
	certsDir := filepath.Join(dataDir, "certs.d")

	expectedDirs := []string{"docker.io", "ghcr.io", "quay.io", "registry.k8s.io"}
	for _, dir := range expectedDirs {
		hostsPath := filepath.Join(certsDir, dir, "hosts.toml")
		if _, err := os.Stat(hostsPath); os.IsNotExist(err) {
			t.Errorf("hosts.toml not found for %s", dir)
			continue
		}

		content, _ := os.ReadFile(hostsPath)
		contentStr := string(content)

		// Verify hosts.toml has correct format
		if !strings.Contains(contentStr, "server = ") {
			t.Errorf("%s/hosts.toml missing server declaration", dir)
		}
		if !strings.Contains(contentStr, "[host.") {
			t.Errorf("%s/hosts.toml missing host section", dir)
		}
		if !strings.Contains(contentStr, "http://zot:5000/") {
			t.Errorf("%s/hosts.toml missing zot mirror with path prefix", dir)
		}
	}

	t.Log("✓ Kind cluster created successfully with registry configuration")
}

func testStepCAReachability(t *testing.T) {
	// Test Step CA HTTPS endpoint via Traefik
	// Step CA is not exposed directly to localhost, it's accessed via Traefik reverse proxy
	// Access Step CA via Traefik at the default domain and port
	stepCAURL := "https://ca.c0000201.sslip.io:8443/health"

	// Wait for endpoint with retries (Traefik needs time to get ACME certs)
	if err := waitForHTTPEndpoint(stepCAURL, 30*time.Second, true); err != nil {
		t.Errorf("Step CA endpoint not reachable via Traefik: %v", err)
		return
	}
	t.Log("✓ Step CA endpoint (via Traefik) is reachable")
}

func testZotReachability(t *testing.T) {
	// Test Zot HTTP endpoint with retries
	zotURL := "http://localhost:5000/v2/"

	if err := waitForHTTPEndpoint(zotURL, 30*time.Second, false); err != nil {
		t.Errorf("Zot endpoint not reachable: %v", err)
		return
	}
	t.Log("✓ Zot registry endpoint (http://localhost:5000) is reachable")
}

// Helper function to wait for HTTP endpoint with retries
func waitForHTTPEndpoint(url string, timeout time.Duration, skipVerify bool) error {
	tr := &http.Transport{}
	if skipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: tr,
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout waiting for endpoint: %s", url)
}
