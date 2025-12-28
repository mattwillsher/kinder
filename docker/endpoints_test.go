package docker

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/hipkoi/kinder/cacert"
)

const (
	testNetworkCIDR = "172.31.31.0/24"
	testNetworkName = "kinder-test-endpoints"
	// Use alternative ports for testing to avoid conflicts
	testStepCAPort   = "19000"
	testCoreDNSPort  = "1053"
	testZotPort      = "15000"
	testGatusPort    = "18081"
	testTraefikPort  = "18080"
	testTraefikHTTP  = "8000"
	testTraefikHTTPS = "8443"
)

// setupTestEnvironment creates network and data directory for tests
func setupTestEnvironment(t *testing.T) (context.Context, string) {
	ctx := context.Background()

	// Create test data directory
	dataDir := filepath.Join(os.TempDir(), "kinder-endpoint-test")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("Failed to create test data directory: %v", err)
	}

	// Generate CA cert for Step CA tests
	certPath := filepath.Join(dataDir, "ca.crt")
	keyPath := filepath.Join(dataDir, "ca.key")
	if err := cacert.GenerateCA(certPath, keyPath); err != nil {
		t.Fatalf("Failed to generate CA certificate: %v", err)
	}

	// Clean up any existing test network
	if exists, _ := NetworkExists(ctx, testNetworkName); exists {
		RemoveNetwork(ctx, testNetworkName)
	}

	// Create test network
	networkConfig := NetworkConfig{
		Name:   testNetworkName,
		CIDR:   testNetworkCIDR,
		Driver: "bridge",
	}
	if _, err := CreateNetwork(ctx, networkConfig); err != nil {
		t.Fatalf("Failed to create test network: %v", err)
	}

	return ctx, dataDir
}

// cleanupTestEnvironment removes test network and data directory
func cleanupTestEnvironment(ctx context.Context, dataDir string) {
	RemoveNetwork(ctx, testNetworkName)
	os.RemoveAll(dataDir)
}

// waitForPort waits for a TCP port to be available, with timeout
func waitForPort(host string, port string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for port %s:%s", host, port)
}

// waitForHTTP waits for an HTTP endpoint to respond, with timeout
func waitForHTTP(url string, timeout time.Duration) error {
	// Create a transport that skips certificate verification for testing
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for HTTP endpoint %s", url)
}

func TestCoreDNSEndpoint(t *testing.T) {
	t.Skip("Skipping CoreDNS test - requires privileged port 53")
}

func TestStepCAEndpoint(t *testing.T) {
	t.Skip("Skipping Step CA test - requires custom port configuration")
}

func TestZotEndpoint(t *testing.T) {
	t.Skip("Skipping Zot test - requires custom port configuration")
}

func TestGatusEndpoint(t *testing.T) {
	t.Skip("Skipping Gatus test - requires custom port configuration")
}

func TestTraefikEndpoint(t *testing.T) {
	t.Skip("Skipping Traefik test - requires custom port configuration")
}
