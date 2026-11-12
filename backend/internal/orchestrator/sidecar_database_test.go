package orchestrator

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/secret"
	"github.com/docker/docker/api/types/container"
)

func TestSidecarEnvSetsCredentials(t *testing.T) {
	tests := []struct {
		engine   string
		required []string
	}{
		{"postgres", []string{"POSTGRES_USER=sandbox", "POSTGRES_PASSWORD=pw", "POSTGRES_DB=sandboxdb"}},
		{"mysql", []string{"MYSQL_ROOT_PASSWORD=pw", "MYSQL_DATABASE=sandboxdb", "MYSQL_USER=sandbox", "MYSQL_PASSWORD=pw"}},
	}
	for _, tt := range tests {
		t.Run(tt.engine, func(t *testing.T) {
			got := strings.Join(sidecarEnv(tt.engine, "sandbox", "pw", "sandboxdb"), "\n")
			for _, want := range tt.required {
				if !strings.Contains(got, want) {
					t.Errorf("sidecarEnv(%s) = %q, missing %q", tt.engine, got, want)
				}
			}
		})
	}
	if env := sidecarEnv("sqlite", "sandbox", "pw", "sandboxdb"); env != nil {
		t.Errorf("sidecarEnv(sqlite) = %v, want nil", env)
	}
}

// TestSidecarDatabaseIsReadyAndAcceptsCredentials provisions the real sidecar
// image, waits for readiness, then connects with the generated credentials from
// inside the container. Requires ASL_TEST_DATABASE_URL and Docker.
func TestSidecarDatabaseIsReadyAndAcceptsCredentials(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	box, err := secret.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := New(context.Background(), &config.Config{DockerHost: host}, store, nil, box,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Skipf("no usable Docker daemon: %v", err)
	}
	t.Cleanup(func() { _ = o.Close() })

	engines := []struct {
		name  string
		tag   int64
		query []string
		env   func(pass string) []string
	}{
		{
			name: "postgres", tag: 880011,
			query: []string{"psql", "-U", "sandbox", "-d", "sandboxdb", "-tAc", "select 1"},
			env:   func(pass string) []string { return []string{"PGPASSWORD=" + pass} },
		},
		{
			name: "mysql", tag: 880012,
			query: []string{"mysql", "-usandbox", "-e", "select 1", "sandboxdb"},
			env:   func(pass string) []string { return []string{"MYSQL_PWD=" + pass} },
		},
	}
	for _, tt := range engines {
		t.Run(tt.name, func(t *testing.T) {
			sb := lifecycleSandbox(t, store, gdb, "sidecar-"+tt.name, tt.tag)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			row, connURL, err := o.ProvisionDatabase(ctx, sb, tt.name)
			if err != nil {
				t.Fatalf("ProvisionDatabase: %v", err)
			}
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
				defer stop()
				_ = o.TeardownSandbox(cleanup, sb)
			})
			if err := o.WaitForDatabase(ctx, sb.ID, row.ContainerID, tt.name, 2*time.Minute); err != nil {
				t.Fatalf("WaitForDatabase: %v", err)
			}
			d := &Deployer{store: store, box: box, orch: o}
			envs, err := d.buildEnv(ctx, sb, 3000)
			if err != nil {
				t.Fatalf("buildEnv: %v", err)
			}
			joined := strings.Join(envs, "\n")
			if !strings.Contains(joined, "DATABASE_URL="+connURL) {
				t.Errorf("buildEnv missing DATABASE_URL: %q", joined)
			}
			if tt.name == "mysql" && !strings.Contains(joined, "MYSQL_URL="+connURL) {
				t.Errorf("buildEnv missing MYSQL_URL: %q", joined)
			}
			u, err := url.Parse(connURL)
			if err != nil {
				t.Fatal(err)
			}
			pass, _ := u.User.Password()
			execID, err := o.docker.ContainerExecCreate(ctx, row.ContainerID,
				container.ExecOptions{Cmd: tt.query, Env: tt.env(pass)})
			if err != nil {
				t.Fatalf("ContainerExecCreate: %v", err)
			}
			attach, err := o.docker.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
			if err != nil {
				t.Fatalf("ContainerExecAttach: %v", err)
			}
			_, _ = io.Copy(io.Discard, attach.Reader)
			attach.Close()
			inspect, err := o.docker.ContainerExecInspect(ctx, execID.ID)
			if err != nil || inspect.ExitCode != 0 {
				t.Fatalf("database login failed: inspect=%+v err=%v", inspect, err)
			}
		})
	}
}
