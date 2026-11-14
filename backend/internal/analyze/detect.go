package analyze

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Services records which managed backing services the project appears to need.
type Services struct {
	Postgres bool     `json:"postgres"`
	MySQL    bool     `json:"mysql"`
	Redis    bool     `json:"redis"`
	Reasons  []string `json:"reasons"`
}

// Warning is a non-fatal finding with a concrete suggested fix.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

// Blocker is a finding that should stop a deploy until the user changes the
// repository. It always carries an exact fix.
type Blocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

// Result is the complete preflight report for one commit.
type Result struct {
	CommitSHA          string    `json:"commit_sha"`
	Branch             string    `json:"branch"`
	Builder            string    `json:"builder"`
	Language           string    `json:"language"`
	Framework          string    `json:"framework"`
	LanguageVersion    string    `json:"language_version"`
	StartCommand       string    `json:"start_command"`
	StartCommandSource string    `json:"start_command_source"`
	Port               int       `json:"port"`
	PortSource         string    `json:"port_source"`
	Services           Services  `json:"services"`
	EnvFile            string    `json:"env_file"`
	EnvVars            []EnvVar  `json:"env_vars"`
	Warnings           []Warning `json:"warnings"`
	Blockers           []Blocker `json:"blockers"`
}

type packageJSON struct {
	Name    string            `json:"name"`
	Main    string            `json:"main"`
	Scripts map[string]string `json:"scripts"`
	Engines struct {
		Node string `json:"node"`
	} `json:"engines"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

type projectInfo struct {
	language  string
	framework string
	version   string
	start     string
	startFrom string
	port      int
}

// Detect inspects the cloned directory and returns language/framework/version
// and dependency-based service hints. It intentionally never returns an empty
// language when any recognised manifest exists.
func Detect(dir string) projectInfo {
	info := projectInfo{language: "unknown"}
	pkg := readPackageJSON(dir)

	switch {
	case fileExists(dir, "go.mod"):
		info.language = "go"
		info.framework = detectGoFramework(dir)
		info.version = goDirective(dir)
	case pkg != nil:
		info.language = "node"
		info.framework = detectNodeFramework(pkg)
		info.version = cleanNodeVersion(pkg.Engines.Node)
		if info.version == "" {
			if v := readTrimmed(dir, ".nvmrc"); v != "" {
				info.version = cleanNodeVersion(v)
			}
		}
	case fileExists(dir, "requirements.txt") || fileExists(dir, "pyproject.toml") || fileExists(dir, "Pipfile"):
		info.language = "python"
		info.framework = detectPythonFramework(dir)
		info.version = detectPythonVersion(dir)
	case fileExists(dir, "pom.xml") || fileExists(dir, "build.gradle") || fileExists(dir, "build.gradle.kts"):
		info.language = "java"
		info.framework = detectJavaFramework(dir)
		info.version = detectJavaVersion(dir)
	case fileExists(dir, "Gemfile"):
		info.language = "ruby"
		info.framework = detectRubyFramework(dir)
	case fileExists(dir, "composer.json"):
		info.language = "php"
		if data := readFile(dir, "composer.json"); strings.Contains(strings.ToLower(data), "laravel") {
			info.framework = "laravel"
		}
	case fileExists(dir, "Cargo.toml"):
		info.language = "rust"
	}

	if info.language == "unknown" {
		if fileExists(dir, "index.html") {
			info.language = "static"
		}
	}
	return info
}

func DetectServices(dir string) Services {
	var svc Services
	add := func(reason string) {
		svc.Reasons = append(svc.Reasons, reason)
	}
	low := strings.ToLower(readFiles(dir,
		"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"go.mod", "go.sum", "requirements.txt", "pyproject.toml", "Pipfile",
		"pom.xml", "build.gradle", "build.gradle.kts"))

	pgTokens := []string{`"pg"`, "'pg'", `"postgres"`, `"pg-pool"`, "psycopg2", "psycopg[binary]", "asyncpg",
		"lib/pq", "jackc/pgx", "pgx/v5", "gorm.io/driver/postgres", "jdbc:postgresql", "spring-boot-starter-data-jpa"}
	mysqlTokens := []string{`"mysql"`, `"mysql2"`, "go-sql-driver/mysql", "pymysql", "mysqlclient",
		"jdbc:mysql", "gorm.io/driver/mysql"}
	redisTokens := []string{"ioredis", "redis", "go-redis", "redis-py", "spring-data-redis", "redis://"}

	for _, t := range pgTokens {
		if strings.Contains(low, t) {
			svc.Postgres = true
			add("postgres driver referenced by the project (" + t + ")")
			break
		}
	}
	for _, t := range mysqlTokens {
		if strings.Contains(low, t) {
			svc.MySQL = true
			add("mysql driver referenced by the project (" + t + ")")
			break
		}
	}
	for _, t := range redisTokens {
		if strings.Contains(low, t) {
			svc.Redis = true
			add("redis client referenced by the project (" + t + ")")
			break
		}
	}

	// The env example often names the engine even when dependencies are
	// abstracted behind an ORM, so use it as a second signal.
	if env := FindEnvFile(dir); env != "" {
		if b, err := os.ReadFile(env); err == nil {
			e := strings.ToLower(string(b))
			if !svc.MySQL && (strings.Contains(e, "mysql://") || strings.Contains(e, "jdbc:mysql")) {
				svc.MySQL = true
				add("MySQL URL in " + filepath.Base(env))
			}
			if !svc.Postgres && (strings.Contains(e, "postgres://") || strings.Contains(e, "postgresql://") || strings.Contains(e, "jdbc:postgresql")) {
				svc.Postgres = true
				add("Postgres URL in " + filepath.Base(env))
			}
			if !svc.Redis && (strings.Contains(e, "redis://") || strings.Contains(e, "redis_host")) {
				svc.Redis = true
				add("Redis settings in " + filepath.Base(env))
			}
		}
	}
	if svc.Postgres && svc.MySQL {
		// Both is possible but ambiguous; prefer the explicit Postgres signal.
		svc.MySQL = false
		add("both database drivers found; using Postgres")
	}
	sort.Strings(svc.Reasons)
	return svc
}

func detectNodeFramework(pkg *packageJSON) string {
	deps := map[string]bool{}
	for k := range pkg.Dependencies {
		deps[strings.ToLower(k)] = true
	}
	for k := range pkg.DevDependencies {
		deps[strings.ToLower(k)] = true
	}
	cases := []struct{ dep, name string }{
		{"next", "next"}, {"nestjs", "nestjs"}, {"@nestjs/core", "nestjs"},
		{"express", "express"}, {"fastify", "fastify"}, {"koa", "koa"},
		{"hapi", "hapi"}, {"@hapi/hapi", "hapi"}, {"nuxt", "nuxt"},
		{"react", "react"}, {"vue", "vue"},
	}
	for _, c := range cases {
		if deps[c.dep] {
			return c.name
		}
	}
	return ""
}

func detectGoFramework(dir string) string {
	data := strings.ToLower(readFile(dir, "go.mod"))
	switch {
	case strings.Contains(data, "gin-gonic/gin"):
		return "gin"
	case strings.Contains(data, "labstack/echo"):
		return "echo"
	case strings.Contains(data, "gofiber/fiber"):
		return "fiber"
	case strings.Contains(data, "gorilla/mux"):
		return "mux"
	case strings.Contains(data, "go-chi/chi"):
		return "chi"
	case strings.Contains(data, "net/http"):
		return "net/http"
	default:
		return ""
	}
}

func detectPythonFramework(dir string) string {
	low := strings.ToLower(readFiles(dir, "requirements.txt", "pyproject.toml", "Pipfile"))
	switch {
	case strings.Contains(low, "django"):
		return "django"
	case strings.Contains(low, "fastapi"):
		return "fastapi"
	case strings.Contains(low, "flask"):
		return "flask"
	case strings.Contains(low, "tornado"):
		return "tornado"
	case strings.Contains(low, "starlette"):
		return "starlette"
	default:
		return ""
	}
}

func detectJavaFramework(dir string) string {
	low := strings.ToLower(readFiles(dir, "pom.xml", "build.gradle", "build.gradle.kts"))
	if strings.Contains(low, "spring-boot") {
		return "spring"
	}
	if strings.Contains(low, "quarkus") {
		return "quarkus"
	}
	if strings.Contains(low, "micronaut") {
		return "micronaut"
	}
	return ""
}

func detectRubyFramework(dir string) string {
	low := strings.ToLower(readFile(dir, "Gemfile"))
	if strings.Contains(low, "rails") {
		return "rails"
	}
	if strings.Contains(low, "sinatra") {
		return "sinatra"
	}
	return ""
}

var goDirectiveRE = regexp.MustCompile(`(?m)^\s*go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)
var goToolchainRE = regexp.MustCompile(`(?m)^\s*toolchain\s+go(\d+\.\d+(?:\.\d+)?)\s*$`)

func goDirective(dir string) string {
	data := readFile(dir, "go.mod")
	if m := goToolchainRE.FindStringSubmatch(data); m != nil {
		return m[1]
	}
	if m := goDirectiveRE.FindStringSubmatch(data); m != nil {
		return m[1]
	}
	return ""
}

func cleanNodeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.ToLower(v), "v")
	v = strings.TrimLeft(v, ">=<^~ ")
	re := regexp.MustCompile(`^(\d+)(?:\.(\d+))?`)
	if m := re.FindStringSubmatch(v); m != nil {
		if m[2] != "" {
			return m[1] + "." + m[2]
		}
		return m[1]
	}
	return ""
}

func detectPythonVersion(dir string) string {
	if v := readTrimmed(dir, ".python-version"); v != "" {
		return firstNumericVersion(v)
	}
	if v := readTrimmed(dir, "runtime.txt"); v != "" {
		return firstNumericVersion(v)
	}
	low := strings.ToLower(readFiles(dir, "pyproject.toml", "Pipfile"))
	re := regexp.MustCompile(`(?:requires-python|python_version)\s*=\s*["']?[^\d]*(\d+\.\d+)`)
	if m := re.FindStringSubmatch(low); m != nil {
		return m[1]
	}
	return ""
}

func detectJavaVersion(dir string) string {
	low := strings.ToLower(readFiles(dir, "pom.xml", "build.gradle", "build.gradle.kts"))
	res := []*regexp.Regexp{
		regexp.MustCompile(`java\.version>\s*(\d+)`),
		regexp.MustCompile(`sourcecompatibility\s*=\s*["']?([\d.]+)`),
		regexp.MustCompile(`<source>\s*(\d+)`),
		regexp.MustCompile(`<target>\s*(\d+)`),
	}
	for _, re := range res {
		if m := re.FindStringSubmatch(low); m != nil {
			return m[1]
		}
	}
	return ""
}

func firstNumericVersion(s string) string {
	re := regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// InferStartCommand fills in a start command using Procfile, package.json,
// and framework conventions. It returns whether it found one.
func InferStartCommand(dir string, info projectInfo, pkg *packageJSON) (cmd, source string, warnings []Warning) {
	if cmd, ok := procfileStart(dir); ok {
		return cmd, "procfile", nil
	}
	if pkg != nil {
		if script := strings.TrimSpace(pkg.Scripts["start"]); script != "" {
			if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
				return "pnpm start", "package.json", nil
			}
			if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
				return "yarn start", "package.json", nil
			}
			if _, err := os.Stat(filepath.Join(dir, "bun.lockb")); err == nil {
				return "bun start", "package.json", nil
			}
			return "npm start", "package.json", nil
		}
	}

	switch info.language {
	case "python":
		if fileExists(dir, "manage.py") {
			module := findDjangoModule(dir)
			if module != "" {
				warnings = appendMissingDependencyWarning(dir, "gunicorn", warnings)
				return "gunicorn " + module + ".wsgi:application --bind 0.0.0.0:$PORT", "detected", warnings
			}
		}
		if app := detectPythonApp(dir); app != "" {
			if info.framework == "fastapi" {
				warnings = appendMissingDependencyWarning(dir, "uvicorn", warnings)
				return "uvicorn " + app + " --host 0.0.0.0 --port $PORT", "detected", warnings
			}
			warnings = appendMissingDependencyWarning(dir, "gunicorn", warnings)
			return "gunicorn " + app + " --bind 0.0.0.0:$PORT", "detected", warnings
		}
	case "java":
		if info.framework == "spring" {
			return "java -jar target/*.jar", "detected", nil
		}
	case "node":
		if pkg != nil && pkg.Main != "" {
			return "node " + pkg.Main, "detected", nil
		}
		for _, candidate := range []string{"index.js", "server.js", "app.js", "src/index.js"} {
			if fileExists(dir, candidate) {
				return "node " + candidate, "detected", nil
			}
		}
		warnings = append(warnings, Warning{
			Code:    "no_start_command",
			Message: "No start script or entry file was found in package.json.",
			Fix:     "Add a \"start\" script to package.json (for example \"node index.js\") or enter the command to run.",
		})
	}
	return "", "none", warnings
}

func procfileStart(dir string) (string, bool) {
	for _, name := range []string{"Procfile", "procfile"} {
		data := readFile(dir, name)
		if data == "" {
			continue
		}
		for _, line := range strings.Split(data, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "web:") {
				cmd := strings.TrimSpace(strings.TrimPrefix(line, "web:"))
				if cmd != "" {
					return cmd, true
				}
			}
		}
	}
	return "", false
}

func findDjangoModule(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if fileExists(dir, e.Name(), "settings.py") {
			return e.Name()
		}
	}
	return ""
}

func detectPythonApp(dir string) string {
	for _, name := range []string{"app.py", "main.py"} {
		data := strings.ToLower(readFile(dir, name))
		if data == "" {
			continue
		}
		if strings.Contains(data, "fastapi(") || strings.Contains(data, "flask(") {
			return strings.TrimSuffix(name, ".py") + ":app"
		}
	}
	return ""
}

func appendMissingDependencyWarning(dir, dep string, warnings []Warning) []Warning {
	low := strings.ToLower(readFiles(dir, "requirements.txt", "pyproject.toml", "Pipfile"))
	if strings.Contains(low, dep) {
		return warnings
	}
	return append(warnings, Warning{
		Code:    "missing_start_dependency",
		Message: "The inferred start command needs " + dep + ", but it is not declared in the project.",
		Fix:     "Add " + dep + " to requirements.txt, or enter a start command that uses a server you already depend on.",
	})
}

func readPackageJSON(dir string) *packageJSON {
	data := readFile(dir, "package.json")
	if data == "" {
		return nil
	}
	var pkg packageJSON
	if err := json.Unmarshal([]byte(data), &pkg); err != nil {
		return nil
	}
	return &pkg
}

func readFile(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return string(b)
}

func readFiles(dir string, names ...string) string {
	var b strings.Builder
	for _, n := range names {
		if data := readFile(dir, n); data != "" {
			b.WriteString(data)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func readTrimmed(dir, name string) string {
	return strings.TrimSpace(readFile(dir, name))
}

func fileExists(dir string, elems ...string) bool {
	_, err := os.Stat(filepath.Join(append([]string{dir}, elems...)...))
	return err == nil
}

func parsePort(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}
