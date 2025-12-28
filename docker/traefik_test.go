package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraefikConfig(t *testing.T) {
	config := TraefikConfig{
		ContainerName: "test-traefik",
		Hostname:      "traefik.test.internal",
		NetworkName:   "test-network",
		DataDir:       "/tmp/data",
		Image:         "traefik:test",
	}

	if config.ContainerName != "test-traefik" {
		t.Errorf("expected ContainerName 'test-traefik', got '%s'", config.ContainerName)
	}

	if config.Hostname != "traefik.test.internal" {
		t.Errorf("expected Hostname 'traefik.test.internal', got '%s'", config.Hostname)
	}

	if config.Image != "traefik:test" {
		t.Errorf("expected Image 'traefik:test', got '%s'", config.Image)
	}
}

func TestTraefikConstants(t *testing.T) {
	if TraefikImage != "traefik:latest" {
		t.Errorf("expected TraefikImage 'traefik:latest', got '%s'", TraefikImage)
	}

	if TraefikContainerName != "kinder-traefik" {
		t.Errorf("expected TraefikContainerName 'kinder-traefik', got '%s'", TraefikContainerName)
	}

	if TraefikHostname != "traefik" {
		t.Errorf("expected TraefikHostname 'traefik', got '%s'", TraefikHostname)
	}
}

func TestGenerateTraefikStaticConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "traefik-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "traefik.yaml")

	err = generateTraefikStaticConfig(configPath)
	if err != nil {
		t.Fatalf("generateTraefikStaticConfig failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("traefik.yaml was not created")
	}

	// Read and verify content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read traefik.yaml: %v", err)
	}

	contentStr := string(content)

	// Check for expected content
	expectedStrings := []string{
		"api:",
		"dashboard: true",
		"entryPoints:",
		"web:",
		"address:",
		":80",
		"websecure:",
		":443",
		"providers:",
		"file:",
		"filename:",
		"log:",
		"level:",
		"certificatesResolvers:",
		"stepca:",
		"httpChallenge:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(contentStr, expected) {
			t.Errorf("traefik.yaml does not contain expected string: %s", expected)
		}
	}
}

func TestGenerateTraefikDynamicConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "traefik-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "dynamic.yaml")
	testDomain := "c0000201.sslip.io"

	err = generateTraefikDynamicConfig(configPath, testDomain)
	if err != nil {
		t.Fatalf("generateTraefikDynamicConfig failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("dynamic.yaml was not created")
	}

	// Read and verify content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read dynamic.yaml: %v", err)
	}

	contentStr := string(content)

	// Check for expected content
	expectedStrings := []string{
		"http:",
		"routers:",
		"zot-router:",
		"registry." + testDomain,
		"gatus-router:",
		"gatus." + testDomain,
		"stepca-router:",
		"ca." + testDomain,
		"services:",
		"zot-service:",
		"loadBalancer:",
		"servers:",
		"http://zot:5000",
		"http://gatus:8080",
		"https://stepca:9000",
		"serversTransports:",
		"stepca-transport:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(contentStr, expected) {
			t.Errorf("dynamic.yaml does not contain expected string: %s", expected)
		}
	}
}

func TestGenerateTraefikStaticConfig_InvalidPath(t *testing.T) {
	err := generateTraefikStaticConfig("/nonexistent/path/traefik.yaml")
	if err == nil {
		t.Error("expected error when writing to invalid path")
	}
}

func TestGenerateTraefikDynamicConfig_InvalidPath(t *testing.T) {
	err := generateTraefikDynamicConfig("/nonexistent/path/dynamic.yaml", "test.example.com")
	if err == nil {
		t.Error("expected error when writing to invalid path")
	}
}
