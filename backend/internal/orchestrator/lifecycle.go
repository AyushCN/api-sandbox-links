package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Resource limits applied to every sandbox container. Kept modest so several
// sandboxes can run on a dev machine, and generous enough for typical APIs.
const (
	defaultMemoryLimit = 512 * 1024 * 1024 // 512 MB
	defaultCPUQuota    = 0.75              // 75% of one core
	defaultPIDsLimit   = 256
	// imageUser stays empty: the image picks its own runtime user (often root).
	// Forcing a UID here would break repos whose app writes to root-owned paths,
	// and the containment that matters for untrusted code is the capability /
	// no-new-privileges policy below, not the uid. Enforcing a non-root user
	// per image is tracked as separate, unproven hardening.
	imageUser = ""
)

var defaultPIDs = int64(defaultPIDsLimit)

// Sandbox app-container isolation policy (applied by sandboxHostConfig):
//
//   - CapDrop ALL removes every Linux capability the container would inherit
//     as root (NET_RAW, SYS_ADMIN, DAC_OVERRIDE, ...), shrinking the kernel
//     attack surface available to untrusted repository code.
//   - CapAdd NET_BIND_SERVICE is the single capability kept back: nixpacks'
//     `static` provider defaults to port 80, and binding a port below 1024
//     needs this capability even for uid 0.
//   - no-new-privileges then stops any setuid binary or file capability inside
//     the image from re-elevating at exec time, which is what makes the drops
//     above stick.
//
// These are slices rather than literals so the policy is asserted by
// isolation_test.go without a daemon and verified as actually applied by
// isolation_runtime_test.go against a live one.
var (
	sandboxCapDrop     = []string{"ALL"}
	sandboxCapAdd      = []string{"NET_BIND_SERVICE"}
	sandboxSecurityOpt = []string{"no-new-privileges:true"}

	// Sidecar databases run the official postgres/mysql entrypoints, which
	// start as root and drop to the database user with gosu. They need these
	// five capabilities for first boot; everything else is dropped.
	sidecarCapDrop     = []string{"ALL"}
	sidecarCapAdd      = []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID"}
	sidecarSecurityOpt = []string{"no-new-privileges:true"}
)

// sandboxLogConfig bounds container log growth. Without it the Docker default
// json-file driver grows without limit and a noisy sandbox can fill the host.
func sandboxLogConfig() container.LogConfig {
	return container.LogConfig{
		Type:   "json-file",
		Config: map[string]string{"max-size": "10m", "max-file": "3"},
	}
}

// sandboxRestartPolicy restarts a crashed process a few times but never
// resurrects a container the platform intentionally stopped.
func sandboxRestartPolicy() container.RestartPolicy {
	return container.RestartPolicy{Name: "on-failure", MaximumRetryCount: 3}
}

// sandboxHostConfig builds the HostConfig every sandbox app container runs
// with: resource limits (memory/CPU/PIDs), the isolation policy documented
// above, and a restart policy that lets the platform recover a sandbox after
// a daemon reboot. It is a pure function so the policy is unit-testable with
// no Docker daemon in reach.
func sandboxHostConfig() *container.HostConfig {
	pids := defaultPIDs // fresh pointer per call; the SDK only reads it
	return &container.HostConfig{
		Resources: container.Resources{
			// Promoted fields (embedded struct) must be set via the Resources
			// key inside a composite literal. MemorySwap == Memory disables
			// swap for the container, so it cannot exceed its memory budget.
			Memory:     defaultMemoryLimit,
			MemorySwap: defaultMemoryLimit,
			NanoCPUs:   int64(defaultCPUQuota * 1e9),
			PidsLimit:  &pids,
		},
		CapDrop:       append([]string(nil), sandboxCapDrop...),
		CapAdd:        append([]string(nil), sandboxCapAdd...),
		SecurityOpt:   append([]string(nil), sandboxSecurityOpt...),
		RestartPolicy: sandboxRestartPolicy(),
		LogConfig:     sandboxLogConfig(),
	}
}

// sidecarHostConfig builds the HostConfig every sidecar DB container runs
// with resource limits and the isolation policy documented above.
func sidecarHostConfig(bind string, engine string) *container.HostConfig {
	pids := defaultPIDs // fresh pointer per call; the SDK only reads it
	switch engine {
	case "postgres":
		return &container.HostConfig{
			Resources: container.Resources{
				Memory:     defaultMemoryLimit,
				MemorySwap: defaultMemoryLimit, // MemorySwap == Memory disables swap
				NanoCPUs:   int64(defaultCPUQuota * 1e9),
				PidsLimit:  &pids,
			},
			CapDrop:       append([]string(nil), sidecarCapDrop...),
			CapAdd:        append([]string(nil), sidecarCapAdd...),
			SecurityOpt:   append([]string(nil), sidecarSecurityOpt...),
			RestartPolicy: sandboxRestartPolicy(),
			LogConfig:     sandboxLogConfig(),
			Binds:         []string{bind},
		}
	case "mysql":
		return &container.HostConfig{
			Resources: container.Resources{
				Memory:     defaultMemoryLimit,
				MemorySwap: defaultMemoryLimit, // MemorySwap == Memory disables swap
				NanoCPUs:   int64(defaultCPUQuota * 1e9),
				PidsLimit:  &pids,
			},
			CapDrop:       append([]string(nil), sidecarCapDrop...),
			CapAdd:        append([]string(nil), sidecarCapAdd...),
			SecurityOpt:   append([]string(nil), sidecarSecurityOpt...),
			RestartPolicy: sandboxRestartPolicy(),
			LogConfig:     sandboxLogConfig(),
			Binds:         []string{fmt.Sprintf("%s:/var/lib/mysql", bind)},
		}
	default:
		return &container.HostConfig{}
	}
}

// sandboxNetworkOptions is the CreateOptions for a sandbox's private bridge.
//
// DECISION (network): deliberately an ordinary bridge, NOT Internal: true.
// An internal network blocks all outbound traffic, and sandboxes need egress —
// apps fetch packages on first boot, call external APIs, and expect working
// DNS. The protection Internal: true would buy (sandboxes cannot reach
// host-published ports) is obtained instead by binding platform services to
// 127.0.0.1 in docker-compose.dev.yml: a loopback-bound socket is never
// reachable via the bridge gateway IP, while an 0.0.0.0 one is — and that
// keeps egress intact. No host firewall rules are deployed for this platform,
// so this binding policy is the only thing standing between sandbox code and
// the platform's Postgres/Redis. Do not flip Internal without replacing it.
func sandboxNetworkOptions() network.CreateOptions {
	return network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{"api-sandbox-links.purpose": "private-sandbox"},
	}
}

// RunOptions describes a container the orchestrator should start for a sandbox.
type RunOptions struct {
	SandboxID    string
	ImageTag     string
	Subdomain    string
	InternalPort int
	Env          []string          // KEY=VALUE pairs, already decrypted
	NetworkName  string            // private per-sandbox network
	Labels       map[string]string // extra labels (traefik, provenance)
	User         string
}

// RunResult reports where the container landed so callers can store it.
type RunResult struct {
	ContainerID string
	IPAddress   string
	Port        int
}

// netName returns the private Docker network name for a sandbox. It is
// predictable from the sandbox ID so tear-down and forking can reuse it.
func netName(sandboxID string) string { return "sandbox_" + sandboxID[:8] }

// volumeName returns a Docker volume name for a sandbox (used by sidecar DBs).
func volumeName(sandboxID string) string { return "sandboxdata_" + sandboxID[:8] }

// ensureNetwork creates the sandbox's private network if it is not present
// and always returns the network NAME (never the daemon's ID), so callers can
// use it directly as a NetworkingConfig endpoint key and as the lookup key in
// ContainerInspect's NetworkSettings.Networks map. One private network per
// sandbox is what gives the platform its isolation story: the app container
// and its sidecar DB can talk to each other but no other tenant's containers
// can.
func (o *Orchestrator) ensureNetwork(ctx context.Context, sandboxID string) (string, error) {
	name := netName(sandboxID)
	_, err := o.docker.NetworkInspect(ctx, name, network.InspectOptions{})
	if err == nil {
		return name, nil
	}
	if !client.IsErrNotFound(err) {
		return "", fmt.Errorf("orchestrator: inspecting network %s: %w", name, err)
	}
	if _, err := o.docker.NetworkCreate(ctx, name, sandboxNetworkOptions()); err != nil {
		return "", fmt.Errorf("orchestrator: creating network %s: %w", name, err)
	}
	return name, nil
}

// RunSandbox provisions the private network and starts the app container with
// resource limits and Traefik labels. It is a single synchronous step used by
// the deploy pipeline and the wake middleware.
//
// DECISION (routing): sandbox traffic is edge-routed by a Traefik wildcard
// rule into the backend's gateway (see internal/proxy/wake_middleware.go) so
// hibernated sandboxes can be woken mid-request. Each container still carries
// complete per-subdomain Traefik labels at low priority so direct router
// metadata exists (and would take over if wake-on-request were ever removed);
// the wildcard router wins in normal operation.
func (o *Orchestrator) RunSandbox(ctx context.Context, opts RunOptions) (*RunResult, error) {
	netID, err := o.ensureNetwork(ctx, opts.SandboxID)
	if err != nil {
		return nil, err
	}
	_ = netID

	labels := map[string]string{
		"api-sandbox-links.sandbox-id": opts.SandboxID,
		"api-sandbox-links.subdomain":  opts.Subdomain,
		// Traefik direct-route fallback (priority 1 loses to the wildcard
		// gateway router — see DECISION above).
		"traefik.enable": "true",
		"traefik.http.routers." + opts.Subdomain + ".rule":                      "Host(`" + opts.Subdomain + "." + o.cfg.SandboxBaseDomain + "`)",
		"traefik.http.routers." + opts.Subdomain + ".entrypoints":               "web",
		"traefik.http.routers." + opts.Subdomain + ".priority":                  "1",
		"traefik.http.routers." + opts.Subdomain + ".service":                   opts.Subdomain,
		"traefik.http.services." + opts.Subdomain + ".loadbalancer.server.port": itoa(opts.InternalPort),
	}
	for k, v := range opts.Labels {
		labels[k] = v
	}

	cc := &container.Config{
		Image:  opts.ImageTag,
		Env:    opts.Env,
		Labels: labels,
	}
	if opts.User != "" {
		cc.User = opts.User
	}
	hc := sandboxHostConfig()
	nc := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			netName(opts.SandboxID): {NetworkID: ""},
		},
	}

	resp, err := o.docker.ContainerCreate(ctx, cc, hc, nc, nil, "sandbox_"+opts.SandboxID[:8]+"_"+opts.Subdomain)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: creating sandbox container: %w", err)
	}
	if err := o.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("orchestrator: starting sandbox container: %w", err)
	}

	ip, err := o.ContainerIP(ctx, resp.ID, netName(opts.SandboxID))
	if err != nil {
		return nil, err
	}
	return &RunResult{ContainerID: resp.ID, IPAddress: ip, Port: opts.InternalPort}, nil
}

// ContainerIP resolves a container's address on the given network.
func (o *Orchestrator) ContainerIP(ctx context.Context, containerID, networkName string) (string, error) {
	info, err := o.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("orchestrator: inspecting container %s: %w", containerID, err)
	}
	ip := info.NetworkSettings.Networks[networkName].IPAddress
	if ip == "" {
		return "", fmt.Errorf("orchestrator: container %s has no address on network %s", containerID, networkName)
	}
	return ip, nil
}

// StartContainer starts an existing (hibernated) container.
func (o *Orchestrator) StartContainer(ctx context.Context, containerID string) error {
	if err := o.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("orchestrator: starting container %s: %w", containerID, err)
	}
	return nil
}

// StopContainer stops a container without removing it (hibernate path).
func (o *Orchestrator) StopContainer(ctx context.Context, containerID string) error {
	timeout := 10
	if err := o.docker.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("orchestrator: stopping container %s: %w", containerID, err)
	}
	return nil
}

// RemoveContainer force-removes a container and its volumes (destroy path).
func (o *Orchestrator) RemoveContainer(ctx context.Context, containerID string) error {
	if err := o.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
		return fmt.Errorf("orchestrator: removing container %s: %w", containerID, err)
	}
	return nil
}

// RemoveVolume deletes a named Docker volume (destroy path — irreversible).
func (o *Orchestrator) RemoveVolume(ctx context.Context, name string) error {
	if err := o.docker.VolumeRemove(ctx, name, true); err != nil {
		return fmt.Errorf("orchestrator: removing volume %s: %w", name, err)
	}
	return nil
}

// RemoveNetwork deletes a private sandbox network (destroy path).
func (o *Orchestrator) RemoveNetwork(ctx context.Context, sandboxID string) error {
	if err := o.docker.NetworkRemove(ctx, netName(sandboxID)); err != nil {
		return fmt.Errorf("orchestrator: removing network %s: %w", netName(sandboxID), err)
	}
	return nil
}

// WaitHealthy polls the container's port until it accepts a TCP connection or
// the timeout elapses. Used by the wake middleware before forwarding a request
// and by the deploy pipeline before marking a sandbox running.
func (o *Orchestrator) WaitHealthy(ctx context.Context, containerID, ip string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("orchestrator: waiting for container %s health: %w", containerID, ctx.Err())
		case <-ticker.C:
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, itoa(port)), 2*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("orchestrator: container %s not healthy after %s (last err: %v)", containerID, timeout, err)
		}
	}
}

// discoverPortCandidates are probed (in order) when the nixpacks-detected port
// doesn't match what the app actually binds. Some apps hardcode a port instead
// of honoring PORT, so the planned port can be wrong; the gateway routes to
// whatever the deploy records as the container's internal port.
var discoverPortCandidates = []int{8080, 8000, 3000, 5000, 5001, 4000, 9000, 8888, 80}

// DiscoverPort finds which port the container's app actually listens on. The
// planned (nixpacks-detected) port is tried first, then a set of well-known
// app ports, returning the first one that accepts a TCP connection. App
// startup can be slow (waiting on a database), so probing runs until timeout.
func (o *Orchestrator) DiscoverPort(ctx context.Context, ip string, planned int, timeout time.Duration) (int, error) {
	seen := map[int]bool{}
	candidates := make([]int, 0, len(discoverPortCandidates)+1)
	for _, p := range append([]int{planned}, discoverPortCandidates...) {
		if !seen[p] {
			seen[p] = true
			candidates = append(candidates, p)
		}
	}
	deadline := time.Now().Add(timeout)
	for i := 0; ; i++ {
		if ctx.Err() != nil {
			return 0, fmt.Errorf("orchestrator: discovering app port: %w", ctx.Err())
		}
		port := candidates[i%len(candidates)]
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, itoa(port)), 2*time.Second)
		if err == nil {
			conn.Close()
			o.logger.Info("deploy: discovered app listening port", slog.Int("port", port))
			return port, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("orchestrator: no listening port found on %s within %s (last err: %v)", ip, timeout, err)
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("orchestrator: discovering app port: %w", ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// SandboxIDLabel is the Docker label every container belonging to a sandbox
// carries. It is the only thing tying a running container back to its row, so it
// is declared once here rather than spelled out at each use.
const SandboxIDLabel = "api-sandbox-links.sandbox-id"

// FindContainersBySandbox lists every container tagged with the given sandbox
// ID (the app container plus any sidecar DB). Used for full cleanup.
func (o *Orchestrator) FindContainersBySandbox(ctx context.Context, sandboxID string) ([]container.Summary, error) {
	f := filters.NewArgs()
	f.Add("label", SandboxIDLabel+"="+sandboxID)
	all, err := o.docker.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return nil, fmt.Errorf("orchestrator: listing sandbox containers: %w", err)
	}
	return all, nil
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
