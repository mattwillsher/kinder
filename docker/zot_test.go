package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZotConfig(t *testing.T) {
	config := ZotConfig{
		ContainerName: "test-zot",
		Hostname:      "registry.test.internal",
		NetworkName:   "test-network",
		DataDir:       "/tmp/data",
		Image:         "ghcr.io/project-zot/zot-linux-amd64:test",
	}

	if config.ContainerName != "test-zot" {
		t.Errorf("expected ContainerName 'test-zot', got '%s'", config.ContainerName)
	}

	if config.Hostname != "registry.test.internal" {
		t.Errorf("expected Hostname 'registry.test.internal', got '%s'", config.Hostname)
	}

	if config.Image != "ghcr.io/project-zot/zot-linux-amd64:test" {
		t.Errorf("expected Image 'ghcr.io/project-zot/zot-linux-amd64:test', got '%s'", config.Image)
	}
}

func TestZotConstants(t *testing.T) {
	if ZotImage != "ghcr.io/project-zot/zot-linux-amd64:latest" {
		t.Errorf("expected ZotImage 'ghcr.io/project-zot/zot-linux-amd64:latest', got '%s'", ZotImage)
	}

	if ZotContainerName != "kinder-zot" {
		t.Errorf("expected ZotContainerName 'kinder-zot', got '%s'", ZotContainerName)
	}

	if ZotHostname != "zot" {
		t.Errorf("expected ZotHostname 'zot', got '%s'", ZotHostname)
	}
}

func TestGenerateZotConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")

	mirrors := []string{"ghcr.io", "registry-1.docker.io", "quay.io", "registry.k8s.io"}
	err = generateZotConfig(configPath, mirrors)
	if err != nil {
		t.Fatalf("generateZotConfig failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config.json was not created")
	}

	// Read and verify content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.json: %v", err)
	}

	contentStr := string(content)

	// Check for expected content
	expectedStrings := []string{
		"distSpecVersion",
		"storage",
		"rootDirectory",
		"/var/lib/registry",
		"http",
		"address",
		"0.0.0.0",
		"port",
		"5000",
		"compat",
		"docker2s2", // Docker manifest compatibility mode
		"log",
		"level",
		"info",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(contentStr, expected) {
			t.Errorf("config.json does not contain expected string: %s", expected)
		}
	}
}

func TestGenerateZotConfig_InvalidPath(t *testing.T) {
	mirrors := []string{"ghcr.io"}
	err := generateZotConfig("/nonexistent/path/config.json", mirrors)
	if err == nil {
		t.Error("expected error when writing to invalid path")
	}
}

func TestMirrorPath(t *testing.T) {
	tests := []struct {
		registry string
		expected string
	}{
		{"docker.io", "/docker.io"},
		{"ghcr.io", "/ghcr.io"},
		{"registry-1.docker.io", "/registry-1.docker.io"},
		{"quay.io", "/quay.io"},
		{"registry.k8s.io", "/registry.k8s.io"},
	}

	for _, tt := range tests {
		t.Run(tt.registry, func(t *testing.T) {
			result := MirrorPath(tt.registry)
			if result != tt.expected {
				t.Errorf("MirrorPath(%q) = %q, want %q", tt.registry, result, tt.expected)
			}
		})
	}
}

func TestGenerateZotConfig_ContentPrefixes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zot-content-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	mirrors := []string{"ghcr.io", "registry-1.docker.io"}

	err = generateZotConfig(configPath, mirrors)
	if err != nil {
		t.Fatalf("generateZotConfig failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.json: %v", err)
	}

	contentStr := string(content)

	// Verify prefix pattern is present (no destination since all images cache at root)
	if !strings.Contains(contentStr, `"prefix": "**"`) {
		t.Errorf("config.json does not contain expected prefix pattern")
	}

	// Verify upstream registry URLs are configured
	expectedUpstreams := []string{
		"https://ghcr.io",
		"https://registry-1.docker.io",
	}

	for _, expected := range expectedUpstreams {
		if !strings.Contains(contentStr, expected) {
			t.Errorf("config.json does not contain expected upstream URL: %s", expected)
		}
	}
}
