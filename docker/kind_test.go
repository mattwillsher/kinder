package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRegistryName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"registry-1.docker.io", "docker.io"},
		{"docker.io", "docker.io"},
		{"ghcr.io", "ghcr.io"},
		{"quay.io", "quay.io"},
		{"registry.k8s.io", "registry.k8s.io"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeRegistryName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeRegistryName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetUpstreamServer(t *testing.T) {
	tests := []struct {
		registry string
		expected string
	}{
		{"docker.io", "https://registry-1.docker.io"},
		{"registry-1.docker.io", "https://registry-1.docker.io"},
		{"ghcr.io", "https://ghcr.io"},
		{"quay.io", "https://quay.io"},
		{"registry.k8s.io", "https://registry.k8s.io"},
	}

	for _, tt := range tests {
		t.Run(tt.registry, func(t *testing.T) {
			result := getUpstreamServer(tt.registry)
			if result != tt.expected {
				t.Errorf("getUpstreamServer(%q) = %q, want %q", tt.registry, result, tt.expected)
			}
		})
	}
}

func TestBuildContainerdPatches(t *testing.T) {
	t.Run("with mirrors", func(t *testing.T) {
		cfg := KindConfig{
			RegistryMirrors: map[string]string{
				"docker.io": "http://zot:5000",
			},
		}

		patches := buildContainerdPatches(cfg)
		if len(patches) != 1 {
			t.Fatalf("expected 1 patch, got %d", len(patches))
		}

		// Should only set config_path, not the old-style mirrors
		if !strings.Contains(patches[0], `config_path = "/etc/containerd/certs.d"`) {
			t.Error("patch should contain config_path setting")
		}

		// Should NOT contain the old deprecated format
		if strings.Contains(patches[0], "registry.mirrors") {
			t.Error("patch should not contain deprecated registry.mirrors format")
		}
	})

	t.Run("without mirrors", func(t *testing.T) {
		cfg := KindConfig{}

		patches := buildContainerdPatches(cfg)
		if patches != nil {
			t.Errorf("expected nil patches for empty config, got %v", patches)
		}
	})

	t.Run("with ZotHostname only", func(t *testing.T) {
		cfg := KindConfig{
			ZotHostname: "zot",
		}

		patches := buildContainerdPatches(cfg)
		if len(patches) != 1 {
			t.Fatalf("expected 1 patch when ZotHostname is set, got %d", len(patches))
		}
	})
}

func TestCreateCertsDirStructure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kind-certs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a fake CA cert
	caCertPath := filepath.Join(tmpDir, "ca.crt")
	caCertContent := []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----")
	if err := os.WriteFile(caCertPath, caCertContent, 0644); err != nil {
		t.Fatalf("failed to create fake CA cert: %v", err)
	}

	mirrors := map[string]string{
		"registry-1.docker.io": "http://zot:5000",
		"ghcr.io":              "http://zot:5000",
	}

	err = createCertsDirStructure(tmpDir, caCertPath, mirrors, "zot")
	if err != nil {
		t.Fatalf("createCertsDirStructure failed: %v", err)
	}

	// Verify zot:5000 directory was created for direct registry access
	zotDir := filepath.Join(tmpDir, "certs.d", "zot:5000")
	if _, err := os.Stat(zotDir); os.IsNotExist(err) {
		t.Error("zot:5000 directory was not created")
	}

	// Verify docker.io directory was created (normalized from registry-1.docker.io)
	dockerDir := filepath.Join(tmpDir, "certs.d", "docker.io")
	if _, err := os.Stat(dockerDir); os.IsNotExist(err) {
		t.Error("docker.io directory was not created")
	}

	// Verify ghcr.io directory was created
	ghcrDir := filepath.Join(tmpDir, "certs.d", "ghcr.io")
	if _, err := os.Stat(ghcrDir); os.IsNotExist(err) {
		t.Error("ghcr.io directory was not created")
	}

	// Verify hosts.toml content for docker.io
	dockerHostsToml := filepath.Join(dockerDir, "hosts.toml")
	content, err := os.ReadFile(dockerHostsToml)
	if err != nil {
		t.Fatalf("failed to read docker.io hosts.toml: %v", err)
	}

	contentStr := string(content)

	// Should use the canonical docker server
	if !strings.Contains(contentStr, `server = "https://registry-1.docker.io"`) {
		t.Error("hosts.toml should contain registry-1.docker.io as upstream server")
	}

	// Should use the path prefix matching the original registry name
	if !strings.Contains(contentStr, `[host."http://zot:5000/registry-1.docker.io"]`) {
		t.Errorf("hosts.toml should contain mirror URL with path prefix, got:\n%s", contentStr)
	}

	// Verify CA cert was copied
	dockerCaCert := filepath.Join(dockerDir, "ca.crt")
	if _, err := os.Stat(dockerCaCert); os.IsNotExist(err) {
		t.Error("CA cert was not copied to docker.io directory")
	}

	// Verify ghcr.io hosts.toml
	ghcrHostsToml := filepath.Join(ghcrDir, "hosts.toml")
	ghcrContent, err := os.ReadFile(ghcrHostsToml)
	if err != nil {
		t.Fatalf("failed to read ghcr.io hosts.toml: %v", err)
	}

	ghcrStr := string(ghcrContent)
	if !strings.Contains(ghcrStr, `server = "https://ghcr.io"`) {
		t.Error("ghcr.io hosts.toml should contain ghcr.io as upstream server")
	}

	if !strings.Contains(ghcrStr, `[host."http://zot:5000/ghcr.io"]`) {
		t.Error("ghcr.io hosts.toml should contain mirror URL with path prefix")
	}
}

func TestCreateCertsDirStructure_NoCACert(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kind-certs-noca-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mirrors := map[string]string{
		"ghcr.io": "http://zot:5000",
	}

	// Pass empty CA cert path and no zot hostname
	err = createCertsDirStructure(tmpDir, "", mirrors, "")
	if err != nil {
		t.Fatalf("createCertsDirStructure failed: %v", err)
	}

	// Verify hosts.toml was still created
	hostsToml := filepath.Join(tmpDir, "certs.d", "ghcr.io", "hosts.toml")
	if _, err := os.Stat(hostsToml); os.IsNotExist(err) {
		t.Error("hosts.toml was not created")
	}

	// CA cert should NOT exist in the directory
	caCert := filepath.Join(tmpDir, "certs.d", "ghcr.io", "ca.crt")
	if _, err := os.Stat(caCert); !os.IsNotExist(err) {
		t.Error("CA cert should not exist when no CA cert path provided")
	}
}

func TestCreateCertsDirStructure_CleansExisting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kind-certs-clean-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create an existing certs.d directory with stale content
	staleDir := filepath.Join(tmpDir, "certs.d", "old-registry.io")
	if err := os.MkdirAll(staleDir, 0755); err != nil {
		t.Fatalf("failed to create stale directory: %v", err)
	}
	staleFile := filepath.Join(staleDir, "hosts.toml")
	if err := os.WriteFile(staleFile, []byte("stale"), 0644); err != nil {
		t.Fatalf("failed to create stale file: %v", err)
	}

	mirrors := map[string]string{
		"ghcr.io": "http://zot:5000",
	}

	err = createCertsDirStructure(tmpDir, "", mirrors, "")
	if err != nil {
		t.Fatalf("createCertsDirStructure failed: %v", err)
	}

	// Old directory should be removed
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("stale directory should have been removed")
	}

	// New directory should exist
	newDir := filepath.Join(tmpDir, "certs.d", "ghcr.io")
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Error("new ghcr.io directory should exist")
	}
}

func TestKindConstants(t *testing.T) {
	if KindClusterName != "kinder" {
		t.Errorf("expected KindClusterName 'kinder', got '%s'", KindClusterName)
	}

	if KindNodeImage != "kindest/node:v1.32.2" {
		t.Errorf("expected KindNodeImage 'kindest/node:v1.32.2', got '%s'", KindNodeImage)
	}
}

func TestKindConfig(t *testing.T) {
	cfg := KindConfig{
		ClusterName: "test-cluster",
		NodeImage:   "kindest/node:v1.30.0",
		CACertPath:  "/path/to/ca.crt",
		NetworkName: "test-network",
		RegistryMirrors: map[string]string{
			"docker.io": "http://zot:5000",
		},
		ZotHostname: "zot",
		WorkerNodes: 2,
	}

	if cfg.ClusterName != "test-cluster" {
		t.Errorf("expected ClusterName 'test-cluster', got '%s'", cfg.ClusterName)
	}

	if cfg.WorkerNodes != 2 {
		t.Errorf("expected WorkerNodes 2, got %d", cfg.WorkerNodes)
	}

	if len(cfg.RegistryMirrors) != 1 {
		t.Errorf("expected 1 registry mirror, got %d", len(cfg.RegistryMirrors))
	}
}

func TestBuildKindConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kind-build-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a fake CA cert
	caCertPath := filepath.Join(tmpDir, "ca.crt")
	caCertContent := []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----")
	if err := os.WriteFile(caCertPath, caCertContent, 0644); err != nil {
		t.Fatalf("failed to create fake CA cert: %v", err)
	}

	cfg := KindConfig{
		ClusterName: "test-cluster",
		NodeImage:   "kindest/node:v1.30.0",
		CACertPath:  caCertPath,
		NetworkName: "test-network",
		RegistryMirrors: map[string]string{
			"ghcr.io":              "http://zot:5000",
			"registry-1.docker.io": "http://zot:5000",
		},
		ZotHostname: "zot",
		WorkerNodes: 2,
	}

	kindCfg, err := buildKindConfig(cfg)
	if err != nil {
		t.Fatalf("buildKindConfig failed: %v", err)
	}

	// Verify cluster name
	if kindCfg.Name != "test-cluster" {
		t.Errorf("expected cluster name 'test-cluster', got '%s'", kindCfg.Name)
	}

	// Verify nodes: 1 control-plane + 2 workers = 3 nodes
	if len(kindCfg.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(kindCfg.Nodes))
	}

	// Verify containerd patches
	if len(kindCfg.ContainerdConfigPatches) != 1 {
		t.Errorf("expected 1 containerd patch, got %d", len(kindCfg.ContainerdConfigPatches))
	}

	// Verify certs.d directory was created
	certsDir := filepath.Join(tmpDir, "certs.d")
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		t.Error("certs.d directory was not created")
	}

	// Verify control plane has extra mounts
	controlPlane := kindCfg.Nodes[0]
	if len(controlPlane.ExtraMounts) < 2 {
		t.Errorf("expected at least 2 extra mounts for control plane, got %d", len(controlPlane.ExtraMounts))
	}

	// Verify CA cert mount
	foundCACert := false
	foundCertsD := false
	for _, mount := range controlPlane.ExtraMounts {
		if mount.ContainerPath == "/etc/ssl/certs/kinder-ca.crt" {
			foundCACert = true
		}
		if mount.ContainerPath == "/etc/containerd/certs.d" {
			foundCertsD = true
		}
	}

	if !foundCACert {
		t.Error("CA cert mount not found in control plane mounts")
	}
	if !foundCertsD {
		t.Error("certs.d mount not found in control plane mounts")
	}
}

func TestBuildKindConfig_NoMirrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kind-nomirror-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a fake CA cert
	caCertPath := filepath.Join(tmpDir, "ca.crt")
	caCertContent := []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----")
	if err := os.WriteFile(caCertPath, caCertContent, 0644); err != nil {
		t.Fatalf("failed to create fake CA cert: %v", err)
	}

	cfg := KindConfig{
		ClusterName:     "test-cluster",
		CACertPath:      caCertPath,
		RegistryMirrors: nil, // No mirrors
		WorkerNodes:     0,
	}

	kindCfg, err := buildKindConfig(cfg)
	if err != nil {
		t.Fatalf("buildKindConfig failed: %v", err)
	}

	// Should have no containerd patches without mirrors
	if len(kindCfg.ContainerdConfigPatches) != 0 {
		t.Errorf("expected 0 containerd patches without mirrors, got %d", len(kindCfg.ContainerdConfigPatches))
	}

	// Should still have CA cert mount
	controlPlane := kindCfg.Nodes[0]
	if len(controlPlane.ExtraMounts) < 1 {
		t.Error("expected at least CA cert mount")
	}
}
