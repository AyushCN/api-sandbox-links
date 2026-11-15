package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/docker/docker/api/types/container"
	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
)

// dbImage is the sidecar image used per engine. SQLite needs nothing (it lives
// inside the app container) so it is omitted here.
var dbImage = map[string]string{
	"postgres": "postgres:16-alpine",
	"mysql":    "mysql:8",
}

// dbContainerName returns the predictable container name for a DB sidecar.
func dbContainerName(sandboxID string) string { return "db_" + sandboxID[:8] }

// DetectDatabaseNeed decides whether a repo expects a database and which
// engine, by reading the files nixpacks already scans. Heuristics are cheap
// and intentionally simple: presence of a Prisma/ORM config, an example
// DATABASE_URL, or well-known driver dependencies.
func DetectDatabaseNeed(dir string) (engine string, needed bool) {
	checks := []struct {
		name  string
		probe func() bool
	}{
		{"prisma", func() bool { return fileExists(dir, "schema.prisma") }},
		{"sequelize-config", func() bool { return fileExists(dir, "config", "config.json") }},
	}
	for _, c := range checks {
		if c.probe() {
			return "postgres", true
		}
	}

	if env := readEnvExample(dir); env != "" {
		low := strings.ToLower(env)
		switch {
		case strings.Contains(low, "mysql://"), strings.Contains(low, "mysql"), strings.Contains(low, "3306"):
			return "mysql", true
		case strings.Contains(low, "postgres://"), strings.Contains(low, "postgresql://"), strings.Contains(low, "5432"):
			return "postgres", true
		}
	}

	if data := readFiles(dir, "package.json", "go.mod", "requirements.txt", "Pipfile", "pyproject.toml", "docker-compose.yml", "docker-compose.yaml", "compose.yaml", "compose.yml"); data != "" {
		low := strings.ToLower(data)
		switch {
		case strings.Contains(low, "mysql2"), strings.Contains(low, "github.com/go-sql-driver/mysql"),
			strings.Contains(low, "mysql-connector"), strings.Contains(low, "mysqlclient"),
			strings.Contains(low, "pymysql"), strings.Contains(low, "aiomysql"),
			strings.Contains(low, "image: mysql"), strings.Contains(low, "image: \"mysql\""):
			return "mysql", true
		case strings.Contains(low, "\"pg\""), strings.Contains(low, "psycopg"), strings.Contains(low, "gorm.io/driver/postgres"),
			strings.Contains(low, "lib/pq"), strings.Contains(low, "jackc/pgx"), strings.Contains(low, "asyncpg"),
			strings.Contains(low, "image: postgres"), strings.Contains(low, "image: \"postgres\""):
			return "postgres", true
		}
	}
	return "", false
}

// ProvisionDatabase starts a sidecar DB container for the sandbox on its
// private network, creates a data volume, and stores the (encrypted)
// connection string. The app container receives DATABASE_URL at deploy time.
func (o *Orchestrator) ProvisionDatabase(ctx context.Context, sandbox *db.Sandbox, engine string) (*db.SandboxDatabase, string, error) {
	image, ok := dbImage[engine]
	if !ok {
		return nil, "", fmt.Errorf("orchestrator: unsupported sidecar engine %q", engine)
	}

	// The private per-sandbox network is created lazily by RunSandbox, which
	// runs later in the deploy pipeline — provision the sidecar on it now so
	// the app container and its DB can talk on the same isolated network.
	if _, err := o.ensureNetwork(ctx, sandbox.ID); err != nil {
		return nil, "", err
	}

	// Ensure the database image is present locally before attempting container creation.
	if _, _, err := o.docker.ImageInspectWithRaw(ctx, image); err != nil {
		rc, pullErr := o.docker.ImagePull(ctx, image, imageTypes.PullOptions{})
		if pullErr != nil {
			return nil, "", fmt.Errorf("orchestrator: pulling sidecar db image %s: %w", image, pullErr)
		}
		_, _ = io.Copy(io.Discard, rc)
		_ = rc.Close()
	}

	pass, err := randomSecret(16)
	if err != nil {
		return nil, "", fmt.Errorf("orchestrator: generating db password: %w", err)
	}
	user, dbName := "sandbox", "sandboxdb"

	// Tie the volume name to the database row ID: teardown and forks derive
	// this name from the row, so generating an unrelated ID here leaks data.
	databaseID := newID()
	volName := volumeName(databaseID)
	if _, err := o.docker.VolumeCreate(ctx, volumeCreate(volName, sandbox.ID)); err != nil {
		return nil, "", fmt.Errorf("orchestrator: creating db volume: %w", err)
	}

	var env []string
	var mount string
	var hc *container.HostConfig

	switch engine {
	case "postgres":
		mount = fmt.Sprintf("%s:/var/lib/postgresql/data", volName)
		hc = sidecarHostConfig(mount, "postgres")
		env = []string{
			"POSTGRES_USER=" + user,
			"POSTGRES_PASSWORD=" + pass,
			"POSTGRES_DB=" + dbName,
		}
	case "mysql":
		mount = fmt.Sprintf("%s:/var/lib/mysql", volName)
		hc = sidecarHostConfig(mount, "mysql")
		hc.Binds = []string{mount}
		env = []string{
			"MYSQL_USER=" + user,
			"MYSQL_PASSWORD=" + pass,
			"MYSQL_DATABASE=" + dbName,
			"MYSQL_ROOT_PASSWORD=" + pass,
		}
	default:
		mount = fmt.Sprintf("%s:/var/lib/postgresql/data", volName)
		hc = sidecarHostConfig(mount, engine)
	}

	cc := &container.Config{Image: image, Env: env}

	nc := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
		netName(sandbox.ID): {NetworkID: ""},
	}}
	labels := map[string]string{
		"api-sandbox-links.sandbox-id": sandbox.ID,
		"api-sandbox-links.service":    "sidecar-db",
		"api-sandbox-links.db-engine":  engine,
	}
	cc.Labels = labels

	resp, err := o.docker.ContainerCreate(ctx, cc, hc, nc, nil, dbContainerName(sandbox.ID))
	if err != nil {
		_ = o.RemoveVolume(ctx, volName)
		return nil, "", fmt.Errorf("orchestrator: creating sidecar db container: %w", err)
	}
	if err := o.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, "", fmt.Errorf("orchestrator: starting sidecar db container: %w", err)
	}

	// The container name is the DNS hostname on the private network.
	host := dbContainerName(sandbox.ID)
	connURL := fmt.Sprintf("postgres://%s:%s@%s:5432/%s", user, pass, host, dbName)
	if engine == "mysql" {
		connURL = fmt.Sprintf("mysql://%s:%s@%s:3306/%s", user, pass, host, dbName)
	}

	enc, err := o.secret.Encrypt([]byte(connURL))
	if err != nil {
		return nil, "", fmt.Errorf("orchestrator: encrypting db connection url: %w", err)
	}
	row := &db.SandboxDatabase{
		ID:            databaseID,
		SandboxID:     sandbox.ID,
		Engine:        engine,
		ContainerID:   resp.ID,
		ConnectionURL: enc,
	}
	if err := o.store.CreateDatabase(row); err != nil {
		return nil, "", err
	}
	return row, connURL, nil
}

// WaitForDatabase polls the DB's TCP port (5432/3306) until it accepts
// connections, giving Postgres's first-boot init time to finish.
func (o *Orchestrator) WaitForDatabase(ctx context.Context, sandboxID, containerID, engine string, timeout time.Duration) error {
	port := 5432
	if engine == "mysql" {
		port = 3306
	}
	ip, err := o.ContainerIP(ctx, containerID, netName(sandboxID))
	if err != nil {
		return err
	}
	return o.WaitHealthy(ctx, containerID, ip, port, timeout)
}

func fileExists(dir string, elems ...string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()
	target := filepath.Join(elems...)
	_, err = root.Stat(target)
	return err == nil
}

func readEnvExample(dir string) string {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return ""
	}
	defer root.Close()
	b, err := root.ReadFile(".env.example")
	if err != nil {
		return ""
	}
	return string(b)
}

func readFiles(dir string, names ...string) string {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return ""
	}
	defer root.Close()
	var sb strings.Builder
	for _, n := range names {
		b, err := root.ReadFile(n)
		if err == nil {
			sb.Write(b)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func randomSecret(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
