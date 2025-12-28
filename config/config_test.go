package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	// Initialize with no config file
	if err := Initialize(""); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	cfg, err := Get()
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	if cfg.AppName != DefaultAppName {
		t.Errorf("expected AppName %q, got %q", DefaultAppName, cfg.AppName)
	}

	if cfg.Domain != DefaultDomain {
		t.Errorf("expected Domain %q, got %q", DefaultDomain, cfg.Domain)
	}

	if cfg.Network.Name != DefaultNetworkName {
		t.Errorf("expected Network.Name %q, got %q", DefaultNetworkName, cfg.Network.Name)
	}

	if cfg.Network.CIDR != DefaultNetworkCIDR {
		t.Errorf("expected Network.CIDR %q, got %q", DefaultNetworkCIDR, cfg.Network.CIDR)
	}

	if cfg.Traefik.Port != DefaultTraefikPort {
		t.Errorf("expected Traefik.Port %q, got %q", DefaultTraefikPort, cfg.Traefik.Port)
	}

	if cfg.Images.StepCA != DefaultStepCAImage {
		t.Errorf("expected Images.StepCA %q, got %q", DefaultStepCAImage, cfg.Images.StepCA)
	}

	if len(cfg.RegistryMirrors) != len(DefaultRegistryMirrors) {
		t.Errorf("expected %d registry mirrors, got %d", len(DefaultRegistryMirrors), len(cfg.RegistryMirrors))
	}
}

func TestGetString(t *testing.T) {
	if err := Initialize(""); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	domain := GetString(KeyDomain)
	if domain != DefaultDomain {
		t.Errorf("expected %q, got %q", DefaultDomain, domain)
	}
}

func TestSet(t *testing.T) {
	if err := Initialize(""); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	Set(KeyDomain, "custom.example.com")
	domain := GetString(KeyDomain)
	if domain != "custom.example.com" {
		t.Errorf("expected %q, got %q", "custom.example.com", domain)
	}

	// Cleanup: reset to default
	Set(KeyDomain, DefaultDomain)
}

func TestEnvironmentVariable(t *testing.T) {
	// Set environment variable before initializing
	os.Setenv("KINDER_TRAEFIK_PORT", "9443")
	defer os.Unsetenv("KINDER_TRAEFIK_PORT")

	if err := Initialize(""); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	port := GetString(KeyTraefikPort)
	if port != "9443" {
		t.Errorf("expected env override %q, got %q", "9443", port)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "kinder-config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `appName: myapp
domain: myapp.local
network:
  name: mynetwork
  cidr: 10.0.0.0/16
traefik:
  port: "443"
images:
  stepca: custom/step-ca:v1
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	if err := Initialize(configPath); err != nil {
		t.Fatalf("failed to initialize with config file: %v", err)
	}

	cfg, err := Get()
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	if cfg.AppName != "myapp" {
		t.Errorf("expected AppName %q, got %q", "myapp", cfg.AppName)
	}

	if cfg.Domain != "myapp.local" {
		t.Errorf("expected Domain %q, got %q", "myapp.local", cfg.Domain)
	}

	if cfg.Network.Name != "mynetwork" {
		t.Errorf("expected Network.Name %q, got %q", "mynetwork", cfg.Network.Name)
	}

	if cfg.Network.CIDR != "10.0.0.0/16" {
		t.Errorf("expected Network.CIDR %q, got %q", "10.0.0.0/16", cfg.Network.CIDR)
	}

	if cfg.Traefik.Port != "443" {
		t.Errorf("expected Traefik.Port %q, got %q", "443", cfg.Traefik.Port)
	}

	if cfg.Images.StepCA != "custom/step-ca:v1" {
		t.Errorf("expected Images.StepCA %q, got %q", "custom/step-ca:v1", cfg.Images.StepCA)
	}

	// Verify ConfigFile() returns the path
	if ConfigFile() != configPath {
		t.Errorf("expected ConfigFile() %q, got %q", configPath, ConfigFile())
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &FileConfig{}
	cfg.ApplyDefaults()

	if cfg.AppName != DefaultAppName {
		t.Errorf("expected AppName %q, got %q", DefaultAppName, cfg.AppName)
	}

	if cfg.Network.Name != DefaultAppName {
		t.Errorf("expected Network.Name to match AppName %q, got %q", DefaultAppName, cfg.Network.Name)
	}

	if cfg.Network.Bridge != DefaultAppName+"br0" {
		t.Errorf("expected Network.Bridge %q, got %q", DefaultAppName+"br0", cfg.Network.Bridge)
	}
}

func TestContainerName(t *testing.T) {
	cfg := &FileConfig{AppName: "testapp"}

	name := cfg.ContainerName("stepca")
	if name != "testapp-stepca" {
		t.Errorf("expected %q, got %q", "testapp-stepca", name)
	}
}

func TestGetConfigDir(t *testing.T) {
	// Save original env var
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", origXDG)

	// Test with XDG_CONFIG_HOME set
	os.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	dir, err := GetConfigDir("testapp")
	if err != nil {
		t.Fatalf("failed to get config dir: %v", err)
	}
	expected := "/tmp/xdg-config/testapp"
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}

	// Test without XDG_CONFIG_HOME
	os.Unsetenv("XDG_CONFIG_HOME")
	dir, err = GetConfigDir("testapp")
	if err != nil {
		t.Fatalf("failed to get config dir: %v", err)
	}
	homeDir, _ := os.UserHomeDir()
	expected = filepath.Join(homeDir, ".config", "testapp")
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}
}

func TestGetConfigPath(t *testing.T) {
	os.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	defer os.Unsetenv("XDG_CONFIG_HOME")

	path, err := GetConfigPath("testapp")
	if err != nil {
		t.Fatalf("failed to get config path: %v", err)
	}
	expected := "/tmp/xdg-config/testapp/config.yaml"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestGetDataDir(t *testing.T) {
	// Test with KINDER_DATADIR set
	os.Setenv("KINDER_DATADIR", "/tmp/custom-data-dir")
	defer os.Unsetenv("KINDER_DATADIR")

	if err := Initialize(""); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	dataDir, err := GetDataDir()
	if err != nil {
		t.Fatalf("GetDataDir failed: %v", err)
	}

	if dataDir != "/tmp/custom-data-dir" {
		t.Errorf("expected %q, got %q", "/tmp/custom-data-dir", dataDir)
	}
}

func TestGetDataDirFallback(t *testing.T) {
	// Clear any dataDir config
	os.Unsetenv("KINDER_DATADIR")

	if err := Initialize(""); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	// Clear the dataDir setting in Viper
	Set(KeyDataDir, "")

	dataDir, err := GetDataDir()
	if err != nil {
		t.Fatalf("GetDataDir failed: %v", err)
	}

	// Should fall back to XDG standard
	homeDir, _ := os.UserHomeDir()
	expected := homeDir + "/.local/share/" + DefaultAppName
	if dataDir != expected {
		t.Errorf("expected %q, got %q", expected, dataDir)
	}
}
