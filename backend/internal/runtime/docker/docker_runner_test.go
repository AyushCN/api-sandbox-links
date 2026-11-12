package docker

import (
	"testing"

	"github.com/api-sandbox-links/backend/internal/runtime"
)

// TestDockerRunnerInterfaceAssertion verifies R-01: compile-time and runtime interface assertion
func TestDockerRunnerInterfaceAssertion(t *testing.T) {
	var runner runtime.ContainerRunner = NewDockerRunner(nil)
	if runner == nil {
		t.Fatal("expected non-nil runner interface reference")
	}
}
