package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// CopyVolume duplicates the contents of src volume into dst volume using a
// throwaway helper container. This is the documented simplification of
// block-level Copy-on-Write storage (see docs/ARCHITECTURE.md §Known
// simplifications): the fork just gets its own files, not a CoW dependency on
// the parent.
func (o *Orchestrator) CopyVolume(ctx context.Context, src, dst, sandboxID string) error {
	if _, err := o.docker.VolumeCreate(ctx, volumeCreate(dst, sandboxID)); err != nil {
		return fmt.Errorf("orchestrator: creating fork volume: %w", err)
	}

	cc := &container.Config{Image: "alpine:3", Cmd: []string{"sh", "-c", "cp -a /src/. /dst/"}}
	hc := &container.HostConfig{Binds: []string{src + ":/src", dst + ":/dst"}}
	resp, err := o.docker.ContainerCreate(ctx, cc, hc, &network.NetworkingConfig{},
		nil, "volcopy_"+dst[len(dst)-6:])
	if err != nil {
		return fmt.Errorf("orchestrator: starting volume copy: %w", err)
	}
	defer func() {
		if err := o.RemoveContainer(context.Background(), resp.ID); err != nil {
			o.logger.Warn("volume-copy: cleanup helper container", slog.String("err", err.Error()))
		}
	}()

	wc, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := o.docker.ContainerStart(wc, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("orchestrator: starting volume copy container: %w", err)
	}
	// Wait for the helper to finish (exit code 0).
	statusC, errC := o.docker.ContainerWait(wc, resp.ID, container.WaitConditionNotRunning)
	select {
	case <-wc.Done():
		return fmt.Errorf("orchestrator: volume copy timed out: %w", wc.Err())
	case err := <-errC:
		if err != nil {
			return fmt.Errorf("orchestrator: volume copy wait error: %w", err)
		}
	case r := <-statusC:
		if r.StatusCode != 0 {
			return fmt.Errorf("orchestrator: volume copy exited with code %d", r.StatusCode)
		}
	}
	return nil
}

// ForkDatabase provisions a new sidecar DB for a child sandbox whose data is
// a byte-for-byte copy of the parent's database volume (fork is a deep copy,
// so changes to either sandbox never affect the other). The child row's
// forked_from pointer records the lineage for the ForkTree view.
func (o *Orchestrator) ForkDatabase(ctx context.Context, parent *db.SandboxDatabase, childSandboxID string) (*db.SandboxDatabase, string, error) {
	image, ok := dbImage[parent.Engine]
	if !ok {
		return nil, "", fmt.Errorf("orchestrator: cannot fork engine %q", parent.Engine)
	}

	// Ensure the child's private network exists before attaching its DB sidecar.
	if _, err := o.ensureNetwork(ctx, childSandboxID); err != nil {
		return nil, "", err
	}

	// The child uses the same credentials as the parent (stored encrypted).
	plainURL, err := o.secret.Decrypt(parent.ConnectionURL)
	if err != nil {
		return nil, "", fmt.Errorf("orchestrator: decrypting parent connection url: %w", err)
	}

	databaseID := newID()
	newVol := volumeName(databaseID)
	if err := o.CopyVolume(ctx, volumeName(parent.ID), newVol, childSandboxID); err != nil {
		_ = o.RemoveVolume(ctx, newVol)
		return nil, "", fmt.Errorf("orchestrator: copying database volume: %w", err)
	}

	user, pass, dbName, err := parseConnURL(string(plainURL))
	if err != nil {
		return nil, "", err
	}

	binds := []string{fmt.Sprintf("%s:/var/lib/postgresql/data", newVol)}
	cc := &container.Config{
		Image:  image,
		Labels: map[string]string{"api-sandbox-links.sandbox-id": childSandboxID, "api-sandbox-links.service": "sidecar-db"},
	}
	hc := &container.HostConfig{Binds: binds}
	switch parent.Engine {
	case "postgres":
		cc.Env = []string{"POSTGRES_USER=" + user, "POSTGRES_PASSWORD=" + pass, "POSTGRES_DB=" + dbName}
	case "mysql":
		cc.Env = []string{"MYSQL_USER=" + user, "MYSQL_PASSWORD=" + pass, "MYSQL_DATABASE=" + dbName, "MYSQL_ROOT_PASSWORD=" + pass}
		hc.Binds = []string{fmt.Sprintf("%s:/var/lib/mysql", newVol)}
	}

	nc := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
		netName(childSandboxID): {NetworkID: ""},
	}}
	resp, err := o.docker.ContainerCreate(ctx, cc, hc, nc, nil, dbContainerName(childSandboxID))
	if err != nil {
		_ = o.RemoveVolume(ctx, newVol)
		return nil, "", fmt.Errorf("orchestrator: creating fork db container: %w", err)
	}
	if err := o.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, "", err
	}

	host := dbContainerName(childSandboxID)
	connURL := fmt.Sprintf("%s://%s:%s@%s:%s/%s", parent.Engine, user, pass, host, dbPort(parent.Engine), dbName)
	enc, err := o.secret.Encrypt([]byte(connURL))
	if err != nil {
		return nil, "", err
	}
	row := &db.SandboxDatabase{
		ID:            databaseID,
		SandboxID:     childSandboxID,
		Engine:        parent.Engine,
		ContainerID:   resp.ID,
		ConnectionURL: enc,
		ForkedFrom:    &parent.ID,
	}
	if err := o.store.CreateDatabase(row); err != nil {
		return nil, "", err
	}
	return row, connURL, nil
}

func dbPort(engine string) string {
	if engine == "mysql" {
		return "3306"
	}
	return "5432"
}

// parseConnURL splits a connection URL back into user/password/database so a
// fork container can be started with matching credentials over copied data.
func parseConnURL(u string) (user, pass, dbName string, err error) {
	rest := u
	if i := strings.Index(rest, "://"); i != -1 {
		rest = rest[i+3:]
	}
	at := strings.LastIndex(rest, "@")
	if at == -1 {
		return "", "", "", fmt.Errorf("orchestrator: connection url has no credentials: %q", u)
	}
	creds := rest[:at]
	hostPart := rest[at+1:]
	if i := strings.Index(creds, ":"); i != -1 {
		user, pass = creds[:i], creds[i+1:]
	} else {
		user = creds
	}
	// db name is after the first "/" following the host[:port].
	if i := strings.Index(hostPart, "/"); i != -1 {
		dbName = hostPart[i+1:]
	}
	if strings.Contains(dbName, "?") {
		dbName = strings.Split(dbName, "?")[0]
	}
	if user == "" || dbName == "" {
		return "", "", "", fmt.Errorf("orchestrator: cannot parse connection url %q", u)
	}
	return user, pass, dbName, nil
}
