package analyze

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseEnvExample(t *testing.T) {
	content := `
# Signing key used for login tokens
JWT_SECRET=
# Optional log level
LOG_LEVEL=info
export DATABASE_URL="postgres://user:pass@localhost/db" # local only
QUOTED='a # b'
EMPTY_WITH_COMMENT= # trailing comment
# OPTIONAL_KEY=default-value
# just a comment
PLACEHOLDER=your_api_key_here
ANGLE=<token>
TODO_VALUE=TODO
`
	vars := ParseEnvExample(content)
	by := map[string]EnvVar{}
	for _, v := range vars {
		by[v.Key] = v
	}
	if got := by["JWT_SECRET"]; !got.Required || !got.Secret || got.Comment != "Signing key used for login tokens" {
		t.Fatalf("JWT_SECRET = %+v", got)
	}
	if got := by["LOG_LEVEL"]; got.Required || got.Default != "info" {
		t.Fatalf("LOG_LEVEL = %+v", got)
	}
	if got := by["DATABASE_URL"]; got.Default != "postgres://user:pass@localhost/db" || got.Required {
		t.Fatalf("DATABASE_URL = %+v", got)
	}
	if got := by["QUOTED"]; got.Default != "a # b" {
		t.Fatalf("QUOTED = %+v", got)
	}
	if got := by["EMPTY_WITH_COMMENT"]; !got.Required {
		t.Fatalf("EMPTY_WITH_COMMENT = %+v", got)
	}
	if got := by["OPTIONAL_KEY"]; got.Required || got.Default != "default-value" {
		t.Fatalf("OPTIONAL_KEY = %+v", got)
	}
	if got := by["PLACEHOLDER"]; !got.Required || got.Secret {
		t.Fatalf("PLACEHOLDER = %+v", got)
	}
	if got := by["ANGLE"]; !got.Required {
		t.Fatalf("ANGLE = %+v", got)
	}
	if got := by["TODO_VALUE"]; !got.Required {
		t.Fatalf("TODO_VALUE = %+v", got)
	}
}

func TestManagedClassification(t *testing.T) {
	svc := Services{Postgres: true, Redis: true}
	vars := []EnvVar{
		{Key: "PORT", Required: true},
		{Key: "DATABASE_URL", Required: true},
		{Key: "REDIS_URL", Required: true},
		{Key: "MYSQL_URL", Required: true},
		{Key: "JWT_SECRET", Required: true},
	}
	ApplyManaged(vars, svc)
	if !vars[0].Managed || vars[0].ManagedBy != "platform" || vars[0].Required {
		t.Fatalf("PORT = %+v", vars[0])
	}
	if !vars[1].Managed || vars[1].ManagedBy != "postgres" {
		t.Fatalf("DATABASE_URL = %+v", vars[1])
	}
	if !vars[2].Managed || vars[2].ManagedBy != "redis" {
		t.Fatalf("REDIS_URL = %+v", vars[2])
	}
	if vars[3].Managed {
		t.Fatalf("MYSQL_URL should not be managed without MySQL: %+v", vars[3])
	}
	if vars[4].Managed || !vars[4].Required {
		t.Fatalf("JWT_SECRET = %+v", vars[4])
	}
}

func TestDetectServices(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"dependencies":{"pg":"^8","ioredis":"^5"}}`)
	svc := DetectServices(dir)
	if !svc.Postgres || !svc.Redis || svc.MySQL {
		t.Fatalf("services = %+v", svc)
	}
	if len(svc.Reasons) == 0 {
		t.Fatalf("expected reasons")
	}
}

func TestDetectServicesFromEnvFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env.example", "DATABASE_URL=mysql://user:pass@db/app\nREDIS_URL=redis://cache:6379\n")
	svc := DetectServices(dir)
	if !svc.MySQL || !svc.Redis || svc.Postgres {
		t.Fatalf("services = %+v", svc)
	}
}

func TestStartCommandProcfileWins(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Procfile", "web: gunicorn app:app --bind 0.0.0.0:$PORT\n")
	write(t, dir, "requirements.txt", "gunicorn\nflask\n")
	info := Detect(dir)
	cmd, source, _ := InferStartCommand(dir, info, readPackageJSON(dir))
	if cmd != "gunicorn app:app --bind 0.0.0.0:$PORT" || source != "procfile" {
		t.Fatalf("cmd=%q source=%q", cmd, source)
	}
}

func TestStartCommandDjango(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "manage.py", "#!/usr/bin/env python\n")
	write(t, dir, "mysite/settings.py", "INSTALLED_APPS=[]\n")
	write(t, dir, "requirements.txt", "django\ngunicorn\n")
	info := Detect(dir)
	cmd, source, _ := InferStartCommand(dir, info, nil)
	if !strings.Contains(cmd, "gunicorn mysite.wsgi") || source != "detected" {
		t.Fatalf("cmd=%q source=%q", cmd, source)
	}
}

func TestStartCommandMissingPythonWarns(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "requirements.txt", "requests\n")
	info := Detect(dir)
	cmd, source, warnings := InferStartCommand(dir, info, nil)
	if cmd != "" || source != "none" {
		t.Fatalf("cmd=%q source=%q", cmd, source)
	}
	if len(warnings) != 0 {
		// Missing python start is reported by Analyze, not InferStartCommand.
		t.Fatalf("unexpected warnings %+v", warnings)
	}
}

func TestValidateEnvValues(t *testing.T) {
	svc := Services{Postgres: true}
	bad := map[string]string{
		"PORT":        "1",
		"DOCKER_HOST": "x",
		"HOME":        "/",
		"1BAD":        "x",
		"DATABASE_URL": "x",
	}
	problems := ValidateEnvValues(bad, svc)
	for _, want := range []string{"PORT", "DOCKER_HOST", "HOME", "1BAD", "DATABASE_URL"} {
		found := false
		for _, p := range problems {
			if strings.Contains(p, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected problem for %s in %v", want, problems)
		}
	}
	if got := ValidateEnvValues(map[string]string{"JWT_SECRET": "x"}, svc); len(got) != 0 {
		t.Fatalf("valid env rejected: %v", got)
	}
	if got := ValidateEnvValues(map[string]string{"BIG": strings.Repeat("x", 8193)}, svc); len(got) == 0 {
		t.Fatalf("expected size rejection")
	}
}

func TestMissingRequired(t *testing.T) {
	vars := []EnvVar{
		{Key: "A", Required: true},
		{Key: "B", Required: true, Managed: true},
		{Key: "C", Required: false},
	}
	got := MissingRequired(vars, map[string]string{"C": "x"})
	if !reflect.DeepEqual(got, []string{"A"}) {
		t.Fatalf("missing = %v", got)
	}
}

func TestGoVersionBlocker(t *testing.T) {
	res := &Result{Language: "go", LanguageVersion: "1.27"}
	applyGoToolchain(res)
	if len(res.Blockers) != 1 || res.Blockers[0].Code != "unsupported_go_version" {
		t.Fatalf("blockers = %+v", res.Blockers)
	}
	if res.LanguageVersion != MaxSupportedGo {
		t.Fatalf("version = %q", res.LanguageVersion)
	}
	if !strings.Contains(res.Blockers[0].Fix, "go.mod") {
		t.Fatalf("fix = %q", res.Blockers[0].Fix)
	}
}

func TestNativeAnalyzeDockerfilePriority(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Dockerfile", "FROM alpine\nCMD [\"node\", \"index.js\"]\n")
	write(t, dir, "package.json", `{"scripts":{"start":"node index.js"}}`)
	a := New("definitely-not-nixpacks")
	res := a.analyzeDir(context.Background(), dir)
	if res.Builder != "dockerfile" {
		t.Fatalf("builder = %q", res.Builder)
	}
	if res.Language != "node" {
		t.Fatalf("language = %q", res.Language)
	}
	if res.StartCommand != "node index.js" {
		t.Fatalf("start = %q", res.StartCommand)
	}
}

func TestNativeAnalyzeNoStartWarns(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "requirements.txt", "requests\n")
	a := New("definitely-not-nixpacks")
	res := a.analyzeDir(context.Background(), dir)
	var found bool
	for _, w := range res.Warnings {
		if w.Code == "no_start_command" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

func TestAnalyzeCacheBySHA(t *testing.T) {
	a := New("nixpacks")
	a.store("abc", &Result{CommitSHA: "abc", Language: "node"})
	got := a.lookup("abc")
	if got == nil || got.Language != "node" {
		t.Fatalf("cache miss: %+v", got)
	}
	a.cache["abc"] = cacheEntry{result: got, at: a.now().Add(-CacheTTL - 1)}
	if a.lookup("abc") != nil {
		t.Fatalf("expected expired entry to miss")
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
