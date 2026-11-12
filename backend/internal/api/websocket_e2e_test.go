package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/api-sandbox-links/backend/internal/secret"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/go-chi/chi/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// github_id values here are in the 300s to stay clear of the other packages'
// fixtures: `go test ./...` runs packages in parallel against one database, and
// users.github_id carries a unique constraint.
//
// The end-to-end test for the live log channel: a real HTTP server, a real
// WebSocket handshake with a real client, and a real container's real log
// output. Everything below it is a unit test, so a regression in routing, the
// upgrade, the frame shape, or the auth check would not be caught by any of them.
//
// It needs Postgres (for the sandbox row and the ownership check) and a Docker
// daemon (for a container to read logs from). Both are skipped when absent:
//
//	docker run -d --rm -p 55432:5432 -e POSTGRES_PASSWORD=pw postgres:16-alpine
//	ASL_TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/postgres?sslmode=disable' \
//	  go test -run TestLiveLogsWS ./internal/api/
func e2eServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("ASL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping: set ASL_TEST_DATABASE_URL to run the websocket end-to-end test")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("skipping: cannot reach the test database (%v)", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	store := db.NewStore(gdb)

	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	orch, err := orchestrator.New(context.Background(), &config.Config{DockerHost: host},
		store, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Skipf("skipping: no usable Docker daemon (%v)", err)
	}
	t.Cleanup(func() { _ = orch.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(&config.Config{}, store, nil, orch, nil, nil, nil, logger), gdb
}

// e2eSandbox creates a user, repository and running sandbox whose ContainerID is
// a real container labelled with the sandbox id, which is what
// FindContainersBySandbox and the log stream both key off.
func e2eSandbox(t *testing.T, s *Server, gdb *gorm.DB, label string, tag int, script string) *db.Sandbox {
	t.Helper()
	image := os.Getenv("ASL_TEST_IMAGE")
	if image == "" {
		image = "node:20-alpine"
	}

	// The label is how a container is tied back to a sandbox, so it has to be set
	// at create time.
	created, err := e2eDocker(t).ContainerCreate(context.Background(),
		&container.Config{
			Image: image,
			Cmd:   []string{"sh", "-c", script},
			// The id is fixed in the test, so the label can be correct from the
			// start rather than being patched afterwards.
			Labels: map[string]string{orchestrator.SandboxIDLabel: e2eID(label, 3)},
		}, &container.HostConfig{}, nil, nil, "")
	if err != nil {
		t.Skipf("skipping: cannot create a container from %s (%v)", image, err)
	}
	cid := created.ID
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = e2eDocker(t).ContainerRemove(c, cid, container.RemoveOptions{Force: true})
	})

	uid := func(seed uint32) string { return e2eID(label, seed) }
	userID, repoID, sandboxID := uid(1), uid(2), uid(3)

	// Registered before anything is inserted. A t.Fatalf between two inserts
	// would otherwise leave the earlier row behind, and the next run of the same
	// test would trip over users_pkey. Deletes are idempotent, so cleaning up
	// rows that were never created is a no-op.
	t.Cleanup(func() {
		gdb.Exec("DELETE FROM resource_snapshots WHERE sandbox_id = ?", sandboxID)
		gdb.Exec("DELETE FROM deployments WHERE sandbox_id = ?", sandboxID)
		gdb.Exec("DELETE FROM sandboxes WHERE id = ?", sandboxID)
		gdb.Exec("DELETE FROM repositories WHERE id = ?", repoID)
		gdb.Exec("DELETE FROM users WHERE id = ?", userID)
	})

	user := &db.User{ID: userID, GithubID: int64(tag), Username: "u-" + label,
		Email: label + "@example.test", GithubToken: []byte("x")}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("creating user: %v", err)
	}
	repo := &db.Repository{ID: repoID, UserID: user.ID, GithubURL: "https://github.com/example/" + label}
	if err := gdb.Create(repo).Error; err != nil {
		t.Fatalf("creating repository: %v", err)
	}
	now := time.Now().UTC()
	sb := &db.Sandbox{
		ID: sandboxID, RepositoryID: repo.ID, BranchName: "main", Subdomain: label,
		Status: db.StatusRunning, ImageTag: "latest", ContainerID: cid,
		LifetimeSeconds: 86400, ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := gdb.Create(sb).Error; err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}

	// Started only once the row exists, so the daemon never has a labelled
	// container for a sandbox that is not in the database yet. Starting it even
	// where the stream is expected to be refused means the refusal is proven
	// against a container that really is producing data.
	startContainer(t, cid)
	return sb
}

// startContainer runs the container's script and reports whether it got going.
// The unavailable and non-owner cases never need it, since those paths are
// decided before any log is read.
func startContainer(t *testing.T, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := e2eDocker(t).ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		t.Skipf("skipping: cannot start the container (%v)", err)
	}
	// Give the script a moment to emit, so the tail has something to backfill.
	time.Sleep(500 * time.Millisecond)
}

// e2eDocker is a Docker client for the test's own container bookkeeping, kept
// separate from the orchestrator's so the test needs no test-only accessor on
// production code.
func e2eDocker(t *testing.T) *client.Client {
	t.Helper()
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	cli, err := client.NewClientWithOpts(client.FromEnv,
		client.WithHost(host), client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	return cli
}

// wsServer stands up the real route pattern behind a real chi router, with only
// the session middleware replaced — it always resolves to the given user, which
// is what the production middleware does once it has read the cookie.
//
// The router matters: the handler reads the sandbox id out of chi's URL params,
// so a test using a plain ServeMux would be rejected for the wrong reason and
// could pass without ever reaching the stream.
func wsServer(t *testing.T, s *Server, user *db.User) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), ctxUserKey, user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/sandboxes/{id}/logs/ws", http.HandlerFunc(s.handleLogsWS))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// dial opens the log socket and returns it with a cleanup that closes it.
func dial(t *testing.T, srv *httptest.Server, sandboxID string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/sandboxes/" + sandboxID + "/logs/ws"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dialling %s: %v", url, err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

// readEvents collects frames until it has seen want of them, the terminal frame,
// or the deadline passes.
func readEvents(t *testing.T, conn *websocket.Conn, want int, timeout time.Duration) []map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var out []map[string]any
	for len(out) < want {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var ev map[string]any
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("frame is not JSON (%q): %v", data, err)
		}
		out = append(out, ev)
		if ev["type"] == "closed" || ev["type"] == "unavailable" {
			break
		}
	}
	return out
}

func linesOf(evs []map[string]any) (stdout, stderr []string) {
	for _, ev := range evs {
		line, _ := ev["line"].(string)
		switch ev["stream"] {
		case "stderr":
			stderr = append(stderr, line)
		case "stdout":
			stdout = append(stdout, line)
		}
	}
	return stdout, stderr
}

// The headline path: a browser opens the log panel and gets the container's
// output, split by stream, with parseable timestamps.
func TestLiveLogsWSStreamsContainerOutput(t *testing.T) {
	// Log lines are stamped by the daemon, so a frame older than the test cannot
	// be a live one — this is what makes the assertion below reject a zero time,
	// which is still valid RFC3339 and would otherwise slip through.
	notBefore := time.Now().Add(-30 * time.Second)

	s, gdb := e2eServer(t)
	sb := e2eSandbox(t, s, gdb, "wsout", 301,
		`echo hello-stdout; echo hello-stderr >&2; sleep 30`)

	var user db.User
	if err := gdb.First(&user, "id = ?", mustUserID(t, gdb, sb)).Error; err != nil {
		t.Fatalf("loading user: %v", err)
	}
	conn := dial(t, wsServer(t, s, &user), sb.ID)

	evs := readEvents(t, conn, 2, 30*time.Second)
	stdout, stderr := linesOf(evs)
	if len(stdout) == 0 {
		t.Fatalf("no stdout frames in %v", evs)
	}
	if len(stderr) == 0 {
		t.Fatalf("no stderr frames in %v", evs)
	}
	if !strings.Contains(strings.Join(stdout, "\n"), "hello-stdout") {
		t.Errorf("stdout = %v, want hello-stdout", stdout)
	}
	if !strings.Contains(strings.Join(stderr, "\n"), "hello-stderr") {
		t.Errorf("stderr = %v, want hello-stderr", stderr)
	}

	// Every frame carries a parseable, freshly-generated timestamp, which is what
	// makes the tail backfill honest: the browser has to be able to trust that
	// the time is when the line was produced, not when it arrived.
	for _, ev := range evs {
		if ev["type"] != "log" {
			t.Errorf("frame %v is not a log frame", ev)
			continue
		}
		ts, _ := ev["timestamp"].(string)
		parsed, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			t.Errorf("frame %v has unparseable timestamp %q: %v", ev, ts, err)
			continue
		}
		if parsed.Before(notBefore) || parsed.After(time.Now().Add(time.Minute)) {
			t.Errorf("frame %v has timestamp %s, want one from this run", ev, parsed)
		}
	}
}

// Authorization is enforced on the socket itself. A caller who does not own the
// sandbox must be refused with a policy violation rather than served data.
func TestLiveLogsWSRejectsNonOwner(t *testing.T) {
	s, gdb := e2eServer(t)
	sb := e2eSandbox(t, s, gdb, "wsdeny", 302, `echo secret; sleep 30`)

	// A different user, with no relationship to this sandbox.
	intruder := &db.User{ID: "99999999-9999-4999-8999-999999999999", GithubID: 9999,
		Username: "intruder", Email: "intruder@example.test", GithubToken: []byte("x")}
	if err := gdb.Create(intruder).Error; err != nil {
		t.Fatalf("creating intruder: %v", err)
	}
	t.Cleanup(func() { gdb.Delete(&db.User{ID: intruder.ID}) })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(wsServer(t, s, intruder).URL, "http") +
		"/api/sandboxes/" + sb.ID + "/logs/ws"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		// Refused before the upgrade, which is the correct shape.
		return
	}
	defer conn.CloseNow()
	_, _, rerr := conn.Read(ctx)
	if rerr == nil {
		t.Fatal("a non-owner was served the stream")
	}
	var se websocket.CloseError
	if !errors.As(rerr, &se) || se.Code != websocket.StatusPolicyViolation {
		t.Errorf("closed with %v, want a policy violation", rerr)
	}
}

// A sandbox that is not running must not open a socket at all; the client is
// told why in a terminal frame instead of being left on a silent connection.
func TestLiveLogsWSReportsUnavailableSandbox(t *testing.T) {
	s, gdb := e2eServer(t)
	sb := e2eSandbox(t, s, gdb, "wshib", 303, `echo never; sleep 30`)

	if err := gdb.Model(&db.Sandbox{}).Where("id = ?", sb.ID).
		Update("status", db.StatusHibernated).Error; err != nil {
		t.Fatalf("hibernating: %v", err)
	}
	var user db.User
	if err := gdb.First(&user, "id = ?", mustUserID(t, gdb, sb)).Error; err != nil {
		t.Fatalf("loading user: %v", err)
	}
	conn := dial(t, wsServer(t, s, &user), sb.ID)

	evs := readEvents(t, conn, 1, 15*time.Second)
	if len(evs) == 0 {
		t.Fatal("no frame at all; the client is left guessing")
	}
	if evs[0]["type"] != "unavailable" {
		t.Errorf("first frame = %v, want a terminal `unavailable`", evs[0])
	}
	if reason, _ := evs[0]["reason"].(string); !strings.Contains(reason, "hibernated") {
		t.Errorf("reason = %q, want it to name the sandbox state", reason)
	}
}

// e2eID derives a stable uuid-shaped id so the container can be labelled with
// the sandbox id at create time, before the sandbox row exists.
func e2eID(label string, seed uint32) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(label))
	hi := h.Sum32()
	h2 := fnv.New32a()
	_, _ = h2.Write([]byte(label + "/salt"))
	lo := h2.Sum32()
	return fmt.Sprintf("%08x-%04x-4%03x-8%03x-%04x%08x",
		0x10000000+seed, hi&0xffff, lo&0x0fff, hi&0x0fff, lo&0xffff, lo)
}

func mustUserID(t *testing.T, gdb *gorm.DB, sb *db.Sandbox) string {
	t.Helper()
	var id string
	if err := gdb.Model(&db.Repository{}).Where("id = ?", sb.RepositoryID).
		Pluck("user_id", &id).Error; err != nil {
		t.Fatalf("resolving user: %v", err)
	}
	return id
}

func ctx30() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	_ = cancel // the deadline is the point; the client outlives this call
	return ctx
}

// TestLiveLogsWSStaysOpenWhileTheContainerRuns is the regression test for a
// panel that connects and then immediately reports "connection lost" over and
// over. Every other test here closes deliberately, so nothing caught the server
// hanging up on a stream that was working perfectly well.
//
// The container emits a line every 2s and the socket is held open across several
// of them, which spans the handler's 5s status poll. If the server closes the
// stream on its own the read surfaces a CloseError and the test reports the code
// and reason, because "it disconnected" is useless on its own.
func TestLiveLogsWSStaysOpenWhileTheContainerRuns(t *testing.T) {
	s, gdb := e2eServer(t)
	sb := e2eSandbox(t, s, gdb, "wssoak", 304, `for i in $(seq 1 30); do echo tick-$i; sleep 2; done`)

	var user db.User
	if err := gdb.First(&user, "id = ?", mustUserID(t, gdb, sb)).Error; err != nil {
		t.Fatalf("loading user: %v", err)
	}
	srv := wsServer(t, s, &user)

	// A long deadline: the test is expected to run out of frames, not time.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/sandboxes/" + sb.ID + "/logs/ws"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dialling: %v", err)
	}
	defer conn.CloseNow()

	frames := 0
	firstFrame := time.Now()
	for time.Since(firstFrame) < 12*time.Second {
		_, data, err := conn.Read(ctx)
		if err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				t.Fatalf("server closed the stream after %d frames in %s: code %d, reason %q",
					frames, time.Since(firstFrame).Round(time.Millisecond), ce.Code, ce.Reason)
			}
			t.Fatalf("read failed after %d frames in %s (not a clean close): %v",
				frames, time.Since(firstFrame).Round(time.Millisecond), err)
		}
		var ev map[string]any
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("frame is not JSON (%q): %v", data, err)
		}
		if ev["type"] == "unavailable" || ev["type"] == "closed" {
			t.Fatalf("server ended the stream with %v after %d frames, but the container is still running", ev, frames)
		}
		frames++
	}

	// One line every 2s for 12s: a stream that delivered fewer than three has
	// silently stopped following.
	if frames < 3 {
		t.Errorf("only %d frames in 12s from a container printing every 2s; the stream is not following", frames)
	}
	t.Logf("%d frames over %s without a close", frames, time.Since(firstFrame).Round(time.Millisecond))
}

// TestLiveLogsWSSurvivesASlowConsumer is the regression test for the panel that
// reconnects forever. pumpLogLines gives up the moment ws.send fails, both pumps
// then finish, sendTerminal fails for the same reason, so shutdown closes with
// 1011 — which the client reports as "connection lost". The only thing that has
// to go wrong is a client that stops draining for wsWriteTimeout, and a browser
// rendering a chatty container into xterm.js does that routinely.
//
// The client here never reads, so the server's write is guaranteed to outrun
// wsWriteTimeout. The test asserts the stream is NOT closed, which fails against
// the current code.
func TestLiveLogsWSSurvivesASlowConsumer(t *testing.T) {
	s, gdb := e2eServer(t)
	sb := e2eSandbox(t, s, gdb, "wsslow", 305, `for i in $(seq 1 20000); do echo "line $i aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; done`)

	var user db.User
	if err := gdb.First(&user, "id = ?", mustUserID(t, gdb, sb)).Error; err != nil {
		t.Fatalf("loading user: %v", err)
	}
	srv := wsServer(t, s, &user)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/sandboxes/" + sb.ID + "/logs/ws"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dialling: %v", err)
	}
	defer conn.CloseNow()

	// Deliberately read nothing for longer than wsWriteTimeout. A server that
	// tears the stream down on a stalled write is indistinguishable, to the
	// browser, from the network failing.
	time.Sleep(8 * time.Second)

	conn.SetReadLimit(-1)
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	_, data, err := conn.Read(readCtx)
	if err != nil {
		var ce websocket.CloseError
		if errors.As(err, &ce) {
			t.Fatalf("server closed the stream on a slow client: code %d, reason %q", ce.Code, ce.Reason)
		}
		t.Fatalf("stream is not readable after 8s of not reading (not a clean close): %v", err)
	}
	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("frame is not JSON: %v", err)
	}
	t.Logf("stream still delivering after 8s stalled consumer; first frame type %v", ev["type"])
}

// TestLiveLogsWSOverTheRealRouterWithACookie is the test that reproduces the
// browser's exact path: the real chi router, the real requireUser middleware,
// and a real sealed session cookie.
//
// Every other socket test here dials the handler through wsServer, which
// injects the user into the context and therefore skips requireUser entirely.
// That gap is why a socket which never opens at all looked identical to one that
// opens and then drops: a failed handshake is reported by the browser as 1006,
// the same code as a vanished TCP connection, and both were retried forever.
//
// Asserts the panel actually receives log frames, not merely that it connected.
func TestLiveLogsWSOverTheRealRouterWithACookie(t *testing.T) {
	s, gdb := e2eServer(t)

	box, err := secret.NewBox(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	s.box = box
	// DevAuth off, so a missing or bad cookie is a real 401 rather than being
	// papered over with a fabricated identity.
	s.cfg = &config.Config{FrontendURL: "http://localhost:3000", DevAuth: false}

	sb := e2eSandbox(t, s, gdb, "wsroute", 306, `for i in $(seq 1 20); do echo routed-$i; sleep 1; done`)
	userID := mustUserID(t, gdb, sb)

	// The session payload is the GitHub ID in decimal, not the user UUID: that
	// is what sessionUser parses and looks the account up by.
	var githubID int64
	if err := gdb.Table("users").Where("id = ?", userID).
		Pluck("github_id", &githubID).Error; err != nil {
		t.Fatalf("reading github_id: %v", err)
	}
	cookie, err := box.Encrypt([]byte(strconv.FormatInt(githubID, 10)))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	srv := httptest.NewServer(s.Router())
	t.Cleanup(srv.Close)

	dial := func(cookieValue string) (*websocket.Conn, *http.Response, error) {
		h := http.Header{}
		if cookieValue != "" {
			h.Set("Cookie", "asl_session="+cookieValue)
		}
		return websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+
			"/api/sandboxes/"+sb.ID+"/logs/ws", &websocket.DialOptions{HTTPHeader: h})
	}

	// With no cookie the handshake must be refused. nhooyr surfaces this as an
	// error, and a browser surfaces it as a 1006 close — which is precisely the
	// code that made this look like a network problem.
	if _, resp, err := dial(""); err == nil {
		t.Fatal("handshake succeeded with no session cookie; requireUser is not guarding the socket")
	} else {
		t.Logf("no cookie: %v (status %d)", err, respStatus(resp))
	}

	// With a real cookie the socket must open and deliver frames.
	conn, _, err := dial(string(cookie))
	if err != nil {
		t.Fatalf("handshake failed with a valid session cookie over the real router: %v", err)
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for i := 0; i < 8; i++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				t.Fatalf("closed after %d frames: code %d, reason %q", i, ce.Code, ce.Reason)
			}
			t.Fatalf("read failed after %d frames: %v", i, err)
		}
		var ev map[string]any
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("frame is not JSON: %v", err)
		}
		if ev["type"] == "log" {
			t.Logf("frame %d: log line %v", i, ev["line"])
			return
		}
		if ev["type"] == "unavailable" || ev["type"] == "closed" {
			t.Fatalf("server ended the stream immediately: %v", ev)
		}
	}
	t.Fatal("no log frame arrived; the socket is open but the panel would render nothing")
}

func respStatus(r *http.Response) int {
	if r == nil {
		return 0
	}
	return r.StatusCode
}
