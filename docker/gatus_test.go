package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatusConfig(t *testing.T) {
	config := GatusConfig{
		ContainerName: "test-gatus",
		Hostname:      "gatus.test.internal",
		NetworkName:   "test-network",
		DataDir:       "/tmp/data",
		Image:         "twinproduction/gatus:test",
	}

	if config.ContainerName != "test-gatus" {
		t.Errorf("expected ContainerName 'test-gatus', got '%s'", config.ContainerName)
	}

	if config.Hostname != "gatus.test.internal" {
		t.Errorf("expected Hostname 'gatus.test.internal', got '%s'", config.Hostname)
	}

	if config.Image != "twinproduction/gatus:test" {
		t.Errorf("expected Image 'twinproduction/gatus:test', got '%s'", config.Image)
	}
}

func TestGatusConstants(t *testing.T) {
	if GatusImage != "twinproduction/gatus:latest" {
		t.Errorf("expected GatusImage 'twinproduction/gatus:latest', got '%s'", GatusImage)
	}

	if GatusContainerName != "kinder-gatus" {
		t.Errorf("expected GatusContainerName 'kinder-gatus', got '%s'", GatusContainerName)
	}

	if GatusHostname != "gatus" {
		t.Errorf("expected GatusHostname 'gatus', got '%s'", GatusHostname)
	}
}

func TestGenerateGatusConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gatus-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")

	err = generateGatusConfig(configPath)
	if err != nil {
		t.Fatalf("generateGatusConfig failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config.yaml was not created")
	}

	// Read and verify content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.yaml: %v", err)
	}

	contentStr := string(content)

	// Check for expected content
	expectedStrings := []string{
		"endpoints:",
		"Step CA",
		"https://stepca:9000",
		"Zot Registry",
		"http://zot:5000",
		"Kubernetes API",
		"https://kinder-control-plane:6443/livez",
		"insecure: true",
		"interval: 30s",
		"conditions:",
		"web:",
		"port: 8080",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(contentStr, expected) {
			t.Errorf("config.yaml does not contain expected string: %s", expected)
		}
	}
}

func TestGenerateGatusConfig_InvalidPath(t *testing.T) {
	err := generateGatusConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error when writing to invalid path")
	}
}
