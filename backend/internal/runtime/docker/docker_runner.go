package docker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/api-sandbox-links/backend/internal/runtime"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Compile-time assertion that DockerRunner implements runtime.ContainerRunner (R-01)
var _ runtime.ContainerRunner = (*DockerRunner)(nil)

// DockerRunner adapts the Docker Engine API client to the platform runtime.ContainerRunner interface.
type DockerRunner struct {
	cli *client.Client
}

// NewDockerRunner constructs a container runner backed by the provided Docker client.
func NewDockerRunner(cli *client.Client) *DockerRunner {
	return &DockerRunner{cli: cli}
}

// Create provisions a container from spec without starting it.
func (d *DockerRunner) Create(ctx context.Context, spec runtime.ContainerSpec) (runtime.ContainerInfo, error) {
	if d == nil || d.cli == nil {
		return runtime.ContainerInfo{}, fmt.Errorf("docker_runner: client not initialized")
	}

	cc := &container.Config{
		Image:        spec.Image,
		Cmd:          spec.Command,
		Env:          spec.Env,
		Labels:       spec.Labels,
		NetworkDisabled: spec.NetworkDisabled,
	}

	var mounts []mount.Mount
	for _, m := range spec.Mounts {
		mt := mount.TypeVolume
		if m.Type == "bind" {
			mt = mount.TypeBind
		}
		mounts = append(mounts, mount.Mount{
			Type:     mt,
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	initVal := spec.Init
	hc := &container.HostConfig{
		Init:            &initVal,
		ReadonlyRootfs:  spec.ReadonlyRootfs,
		Runtime:         spec.Runtime,
		Mounts:          mounts,
		Tmpfs:           spec.Tmpfs,
		CapDrop:         spec.CapDrop,
		CapAdd:          spec.CapAdd,
		SecurityOpt:     []string{"no-new-privileges:true"},
		Resources: container.Resources{
			NanoCPUs:   int64(spec.CPUs * 1e9),
			Memory:     spec.MemoryBytes,
			MemorySwap: spec.MemorySwapBytes,
			PidsLimit:  &spec.PidsLimit,
		},
	}

	nc := &network.NetworkingConfig{}
	if spec.NetworkName != "" {
		nc.EndpointsConfig = map[string]*network.EndpointSettings{
			spec.NetworkName: {
				Aliases: spec.Aliases,
			},
		}
	}

	resp, err := d.cli.ContainerCreate(ctx, cc, hc, nc, nil, spec.Name)
	if err != nil {
		return runtime.ContainerInfo{}, fmt.Errorf("docker_runner: create container %s: %w", spec.Name, err)
	}

	return runtime.ContainerInfo{
		ID:     resp.ID,
		Name:   spec.Name,
		Image:  spec.Image,
		State:  runtime.StateStopped,
		Labels: spec.Labels,
	}, nil
}

// Start boots a created or stopped container.
func (d *DockerRunner) Start(ctx context.Context, containerID string) error {
	if d == nil || d.cli == nil {
		return fmt.Errorf("docker_runner: client not initialized")
	}
	if err := d.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("docker_runner: start container %s: %w", containerID, err)
	}
	return nil
}

// Stop sends termination signal to a container.
func (d *DockerRunner) Stop(ctx context.Context, containerID string, timeoutSeconds int) error {
	if d == nil || d.cli == nil {
		return fmt.Errorf("docker_runner: client not initialized")
	}
	opts := container.StopOptions{}
	if timeoutSeconds > 0 {
		opts.Timeout = &timeoutSeconds
	}
	if err := d.cli.ContainerStop(ctx, containerID, opts); err != nil {
		return fmt.Errorf("docker_runner: stop container %s: %w", containerID, err)
	}
	return nil
}

// Remove deletes a stopped container.
func (d *DockerRunner) Remove(ctx context.Context, containerID string) error {
	if d == nil || d.cli == nil {
		return fmt.Errorf("docker_runner: client not initialized")
	}
	if err := d.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
		return fmt.Errorf("docker_runner: remove container %s: %w", containerID, err)
	}
	return nil
}

// Inspect fetches metadata and IP connectivity for a container.
func (d *DockerRunner) Inspect(ctx context.Context, containerID string) (runtime.ContainerInfo, error) {
	if d == nil || d.cli == nil {
		return runtime.ContainerInfo{}, fmt.Errorf("docker_runner: client not initialized")
	}
	info, err := d.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return runtime.ContainerInfo{}, fmt.Errorf("docker_runner: inspect container %s: %w", containerID, err)
	}

	state := runtime.StateStopped
	if info.State != nil && info.State.Running {
		state = runtime.StateRunning
	}

	ip := ""
	if info.NetworkSettings != nil {
		ip = info.NetworkSettings.IPAddress
		if ip == "" {
			for _, ep := range info.NetworkSettings.Networks {
				if ep.IPAddress != "" {
					ip = ep.IPAddress
					break
				}
			}
		}
	}

	startedAt := time.Time{}
	if info.State != nil && info.State.StartedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339Nano, info.State.StartedAt); parseErr == nil {
			startedAt = t
		}
	}

	labels := map[string]string{}
	if info.Config != nil && info.Config.Labels != nil {
		labels = info.Config.Labels
	}

	name := info.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	image := ""
	if info.Config != nil {
		image = info.Config.Image
	}

	return runtime.ContainerInfo{
		ID:        info.ID,
		Name:      name,
		Image:     image,
		State:     state,
		IPAddress: ip,
		Labels:    labels,
		StartedAt: startedAt,
	}, nil
}

// Wait blocks until the container exits and returns its status code.
func (d *DockerRunner) Wait(ctx context.Context, containerID string) (int64, error) {
	if d == nil || d.cli == nil {
		return -1, fmt.Errorf("docker_runner: client not initialized")
	}
	statusC, errC := d.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errC:
		if err != nil {
			return -1, fmt.Errorf("docker_runner: wait container %s: %w", containerID, err)
		}
	case status := <-statusC:
		return status.StatusCode, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
	return 0, nil
}

// ListByLabel queries containers bearing a given label key and value.
func (d *DockerRunner) ListByLabel(ctx context.Context, key, val string) ([]runtime.ContainerInfo, error) {
	if d == nil || d.cli == nil {
		return nil, fmt.Errorf("docker_runner: client not initialized")
	}
	filterArg := key
	if val != "" {
		filterArg = key + "=" + val
	}
	list, err := d.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", filterArg)),
	})
	if err != nil {
		return nil, fmt.Errorf("docker_runner: list containers by label %s: %w", filterArg, err)
	}

	var out []runtime.ContainerInfo
	for _, c := range list {
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}
		st := runtime.StateStopped
		if c.State == "running" {
			st = runtime.StateRunning
		}
		out = append(out, runtime.ContainerInfo{
			ID:     c.ID,
			Name:   name,
			Image:  c.Image,
			State:  st,
			Labels: c.Labels,
		})
	}
	return out, nil
}

// Logs streams demultiplexed stdout and stderr from a container.
func (d *DockerRunner) Logs(ctx context.Context, containerID string, opts runtime.LogOptions) (io.ReadCloser, io.ReadCloser, error) {
	if d == nil || d.cli == nil {
		return nil, nil, fmt.Errorf("docker_runner: client not initialized")
	}

	getStream := func(isStderr bool) (io.ReadCloser, error) {
		return d.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
			ShowStdout: !isStderr,
			ShowStderr: isStderr,
			Follow:     opts.Follow,
			Tail:       opts.Tail,
			Timestamps: opts.Timestamps,
		})
	}

	stdout, err := getStream(false)
	if err != nil {
		return nil, nil, fmt.Errorf("docker_runner: stdout logs: %w", err)
	}
	stderr, err := getStream(true)
	if err != nil {
		_ = stdout.Close()
		return nil, nil, fmt.Errorf("docker_runner: stderr logs: %w", err)
	}

	return stdout, stderr, nil
}
