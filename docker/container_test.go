package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

func TestContainerConfig(t *testing.T) {
	config := ContainerConfig{
		Name:           "test-container",
		Image:          "nginx:latest",
		Hostname:       "test.internal",
		NetworkName:    "test-network",
		NetworkAliases: []string{"test.internal", "www.test.internal"},
		Env: []string{
			"KEY1=value1",
			"KEY2=value2",
		},
		ExposedPorts: nat.PortSet{
			"80/tcp": struct{}{},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: "/tmp/data",
				Target: "/data",
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
		WorkingDir: "/app",
		User:       "nobody",
	}

	if config.Name != "test-container" {
		t.Errorf("expected Name 'test-container', got '%s'", config.Name)
	}

	if config.Image != "nginx:latest" {
		t.Errorf("expected Image 'nginx:latest', got '%s'", config.Image)
	}

	if config.Hostname != "test.internal" {
		t.Errorf("expected Hostname 'test.internal', got '%s'", config.Hostname)
	}

	if len(config.NetworkAliases) != 2 {
		t.Errorf("expected 2 network aliases, got %d", len(config.NetworkAliases))
	}

	if len(config.Env) != 2 {
		t.Errorf("expected 2 environment variables, got %d", len(config.Env))
	}

	if len(config.Mounts) != 1 {
		t.Errorf("expected 1 mount, got %d", len(config.Mounts))
	}

	if config.RestartPolicy.Name != "always" {
		t.Errorf("expected RestartPolicy 'always', got '%s'", config.RestartPolicy.Name)
	}

	if config.WorkingDir != "/app" {
		t.Errorf("expected WorkingDir '/app', got '%s'", config.WorkingDir)
	}

	if config.User != "nobody" {
		t.Errorf("expected User 'nobody', got '%s'", config.User)
	}
}

func TestContainerExists_Generic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Test with a container that definitely doesn't exist
	exists, err := ContainerExists(ctx, "nonexistent-generic-container-12345")
	if err != nil {
		t.Fatalf("failed to check container existence: %v", err)
	}

	if exists {
		t.Error("nonexistent container should not exist")
	}
}

func TestCopyFile_Generic(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "container-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source file
	srcPath := filepath.Join(tmpDir, "source.txt")
	srcContent := []byte("generic test content")
	if err := os.WriteFile(srcPath, srcContent, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Copy file
	dstPath := filepath.Join(tmpDir, "dest.txt")
	if err := CopyFile(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	// Verify destination file
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}

	if string(dstContent) != string(srcContent) {
		t.Errorf("expected content '%s', got '%s'", srcContent, dstContent)
	}
}

func TestCopyFile_SourceNotExist_Generic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "container-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "nonexistent.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")

	err = CopyFile(srcPath, dstPath)
	if err == nil {
		t.Error("expected error when source file doesn't exist")
	}
}
