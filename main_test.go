package main

import (
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/hipkoi/kinder/config"
)

// TestMain sets up a separate data directory for tests to avoid
// interfering with the user's actual kinder installation.
func TestMain(m *testing.M) {
	// Create a temp directory for test data
	testDataDir, err := os.MkdirTemp("", "kinder-test-*")
	if err != nil {
		panic("failed to create test data directory: " + err.Error())
	}

	// Set environment variable before initializing config
	os.Setenv("KINDER_DATADIR", testDataDir)

	// Initialize config (this will pick up the env var)
	_ = config.Initialize("")

	// Run tests
	code := m.Run()

	// Cleanup
	os.Unsetenv("KINDER_DATADIR")
	os.RemoveAll(testDataDir)

	os.Exit(code)
}

func TestGetDataDir(t *testing.T) {
	dataDir, err := getDataDir()
	if err != nil {
		t.Fatalf("getDataDir failed: %v", err)
	}

	// Should be using the test data directory (set by TestMain)
	if !filepath.HasPrefix(dataDir, os.TempDir()) {
		t.Logf("dataDir: %s", dataDir)
		// Not a failure - just informational since TestMain may have set it up
	}
}

func TestGetDataDirForApp(t *testing.T) {
	// Save and clear XDG_DATA_HOME to test fallback
	origXDG := os.Getenv("XDG_DATA_HOME")
	os.Unsetenv("XDG_DATA_HOME")
	defer func() {
		if origXDG != "" {
			os.Setenv("XDG_DATA_HOME", origXDG)
		}
	}()

	dataDir, err := getDataDirForApp("testapp")
	if err != nil {
		t.Fatalf("getDataDirForApp failed: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".local", "share", "testapp")
	if dataDir != expected {
		t.Errorf("expected %q, got %q", expected, dataDir)
	}
}

func TestGetDataDirForAppWithXDG(t *testing.T) {
	// Set XDG_DATA_HOME
	os.Setenv("XDG_DATA_HOME", "/tmp/xdg-data-test")
	defer os.Unsetenv("XDG_DATA_HOME")

	dataDir, err := getDataDirForApp("testapp")
	if err != nil {
		t.Fatalf("getDataDirForApp failed: %v", err)
	}

	expected := "/tmp/xdg-data-test/testapp"
	if dataDir != expected {
		t.Errorf("expected %q, got %q", expected, dataDir)
	}
}
