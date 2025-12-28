package docker

import (
	"testing"
)

func TestStepCAConfig(t *testing.T) {
	config := StepCAConfig{
		ContainerName: "test-step-ca",
		Hostname:      "ca.test.internal",
		NetworkName:   "test-network",
		CACertPath:    "/tmp/ca.crt",
		CAKeyPath:     "/tmp/ca.key",
		DataDir:       "/tmp/data",
		Image:         "smallstep/step-ca:test",
	}

	if config.ContainerName != "test-step-ca" {
		t.Errorf("expected ContainerName 'test-step-ca', got '%s'", config.ContainerName)
	}

	if config.Hostname != "ca.test.internal" {
		t.Errorf("expected Hostname 'ca.test.internal', got '%s'", config.Hostname)
	}

	if config.Image != "smallstep/step-ca:test" {
		t.Errorf("expected Image 'smallstep/step-ca:test', got '%s'", config.Image)
	}
}

func TestStepCAConstants(t *testing.T) {
	if StepCAImage != "smallstep/step-ca:latest" {
		t.Errorf("expected StepCAImage 'smallstep/step-ca:latest', got '%s'", StepCAImage)
	}

	if StepCAContainerName != "kinder-step-ca" {
		t.Errorf("expected StepCAContainerName 'kinder-step-ca', got '%s'", StepCAContainerName)
	}

	if StepCAHostname != "stepca" {
		t.Errorf("expected StepCAHostname 'stepca', got '%s'", StepCAHostname)
	}
}

// Note: Full integration tests for CreateStepCAContainer and RemoveStepCAContainer
// are intentionally not included as they require:
// - Docker daemon running
// - Network setup
// - Image pulling
// - CA certificate/key files
// These should be tested manually or in a dedicated integration test environment.
