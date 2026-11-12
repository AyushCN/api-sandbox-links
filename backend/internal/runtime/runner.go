package runtime

import (
	"context"
	"io"
)

// ContainerRunner abstracts container execution and inspection operations,
// isolating the orchestration layer from direct engine/SDK coupling.
type ContainerRunner interface {
	// Create provisions a container from spec without starting it.
	Create(ctx context.Context, spec ContainerSpec) (ContainerInfo, error)

	// Start executes a created or stopped container.
	Start(ctx context.Context, containerID string) error

	// Stop signals and terminates a running container.
	Stop(ctx context.Context, containerID string, timeoutSeconds int) error

	// Remove tears down and deletes a container.
	Remove(ctx context.Context, containerID string) error

	// Inspect retrieves live metadata and network connectivity for a container.
	Inspect(ctx context.Context, containerID string) (ContainerInfo, error)

	// Wait blocks until the container exits and returns its exit status code.
	Wait(ctx context.Context, containerID string) (int64, error)

	// ListByLabel queries containers bearing a given label key and value.
	ListByLabel(ctx context.Context, key, val string) ([]ContainerInfo, error)

	// Logs streams demultiplexed stdout and stderr from a container.
	Logs(ctx context.Context, containerID string, opts LogOptions) (stdout io.ReadCloser, stderr io.ReadCloser, err error)
}
