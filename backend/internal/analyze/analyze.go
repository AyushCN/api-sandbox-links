// Package analyze implements the repository preflight used by the guided
// deploy flow. It clones a repo shallowly, inspects its manifests and example
// env file, asks nixpacks for a build plan when appropriate, and returns one
// plain, complete JSON object the frontend can render without guessing.
package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/api-sandbox-links/backend/internal/git"
)

const (
	// CloneTimeout bounds the shallow clone. A preflight that hangs is worse
	// than one that fails: the user is staring at a spinner with no feedback.
	CloneTimeout = 60 * time.Second
	// MaxRepoBytes caps the on-disk size of a shallow clone. The cap is checked
	// after checkout; it protects the host from a repository that expands
	// dramatically (large binaries, generated files).
	MaxRepoBytes = 250 * 1024 * 1024
	// PlanTimeout bounds `nixpacks plan`; a lost plan falls back to manifest
	// heuristics rather than failing the whole analysis.
	PlanTimeout = 25 * time.Second
	// CacheTTL is how long an analysis for a commit SHA is reused.
	CacheTTL = 10 * time.Minute
)

// MaxSupportedGo is the newest Go toolchain this nixpacks version is known to
// provide. Nixpacks exposes Go through a single nixpkgs attribute (`go`), so a
// go.mod directive newer than the installed nixpkgs quietly fails during the
// build. Returning a blocker with an exact fix is much kinder.
const MaxSupportedGo = "1.25"

// Analyzer is safe for concurrent use. Its only mutable state is the per-SHA
// cache, which is what makes repeated "Check repository" clicks cheap.
type Analyzer struct {
	nixpacksBin string
	mu          sync.Mutex
	cache       map[string]cacheEntry
	now         func() time.Time
}

type cacheEntry struct {
	result *Result
	at     time.Time
}

// New builds an Analyzer using the configured nixpacks binary.
func New(nixpacksBin string) *Analyzer {
	if strings.TrimSpace(nixpacksBin) == "" {
		nixpacksBin = "nixpacks"
	}
	return &Analyzer{nixpacksBin: nixpacksBin, cache: map[string]cacheEntry{}, now: time.Now}
}

// Analyze clones and inspects the given repository. An empty branch resolves to
// the remote's default branch.
func (a *Analyzer) Analyze(ctx context.Context, cloneURL, branch string) (*Result, error) {
	cloneCtx, cancel := context.WithTimeout(ctx, CloneTimeout)
	defer cancel()

	tmp, err := os.MkdirTemp("", "asl-analyze-")
	if err != nil {
		return nil, fmt.Errorf("analyze: creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	dir := filepath.Join(tmp, "repo")
	resolvedBranch, err := git.Clone(cloneCtx, cloneURL, branch, dir)
	if err != nil {
		return nil, err
	}

	if size, err := dirSize(dir); err != nil {
		return nil, fmt.Errorf("analyze: measuring clone: %w", err)
	} else if size > MaxRepoBytes {
		return nil, fmt.Errorf("analyze: repository is larger than %d MB after a shallow clone", MaxRepoBytes/1024/1024)
	}

	sha, err := headSHA(dir)
	if err != nil {
		return nil, err
	}
	if cached := a.lookup(sha); cached != nil {
		return cached, nil
	}

	res := a.analyzeDir(ctx, dir)
	res.CommitSHA = sha
	res.Branch = resolvedBranch
	a.store(sha, res)
	return res, nil
}

func (a *Analyzer) analyzeDir(ctx context.Context, dir string) *Result {
	res := &Result{
		Builder:            "nixpacks",
		Language:           "unknown",
		Framework:          "",
		StartCommandSource: "none",
		PortSource:         "default",
		EnvVars:            []EnvVar{},
		Warnings:           []Warning{},
		Blockers:           []Blocker{},
	}

	info := Detect(dir)
	res.Language = info.language
	res.Framework = info.framework
	res.LanguageVersion = info.version

	svc := DetectServices(dir)
	res.Services = svc

	envPath := FindEnvFile(dir)
	if envPath != "" {
		res.EnvFile = filepath.Base(envPath)
		if vars, err := ParseEnvExampleFile(envPath); err == nil {
			res.EnvVars = vars
		}
	}
	ApplyManaged(res.EnvVars, svc)

	pkg := readPackageJSON(dir)
	dockerfile := findDockerfile(dir)
	if dockerfile != "" {
		res.Builder = "dockerfile"
	}

	var plan *nixpacksPlan
	if dockerfile == "" {
		plan = a.plan(ctx, dir)
		if plan != nil {
			mergePlan(res, plan)
		}
	}

	// Recompute language/version from manifests when the plan did not supply
	// them; the manifest fallback is deliberately never allowed to leave the
	// language blank when a manifest exists.
	if res.Language == "unknown" && info.language != "unknown" {
		res.Language = info.language
	}
	if res.Framework == "" {
		res.Framework = info.framework
	}
	if res.LanguageVersion == "" {
		res.LanguageVersion = info.version
	}

	if res.Language == "go" {
		applyGoToolchain(res)
	}

	// Start command: plan first (most accurate), then Procfile/package/framework
	// inference.
	if dockerfile != "" {
		if cmd := dockerfileStart(dir, dockerfile); cmd != "" {
			res.StartCommand = cmd
			res.StartCommandSource = "detected"
		}
	} else if plan != nil && strings.TrimSpace(plan.Start.Cmd) != "" {
		res.StartCommand = strings.TrimSpace(plan.Start.Cmd)
		res.StartCommandSource = "detected"
	} else {
		cmd, source, warnings := InferStartCommand(dir, info, pkg)
		res.StartCommand = cmd
		res.StartCommandSource = source
		res.Warnings = append(res.Warnings, warnings...)
	}

	if dockerfile == "" && strings.TrimSpace(res.StartCommand) == "" {
		res.Warnings = append(res.Warnings, Warning{
			Code:    "no_start_command",
			Message: "We could not work out how to start this project from its files.",
			Fix:     "Enter the command that starts your app (for example \"python app.py\"). It will be passed to the builder as NIXPACKS_START_CMD.",
		})
	}

	// Node: warn independently of the start command because a missing start
	// script is a common source of a container that exits immediately.
	if info.language == "node" && pkg != nil {
		if strings.TrimSpace(pkg.Scripts["start"]) == "" && strings.TrimSpace(pkg.Main) == "" &&
			!fileExists(dir, "index.js") && !fileExists(dir, "server.js") && !fileExists(dir, "app.js") {
			res.Warnings = append(res.Warnings, Warning{
				Code:    "no_start_script",
				Message: "package.json has no \"start\" script and no entry file was found.",
				Fix:     "Add a \"start\" script (for example \"node index.js\") or enter the command manually.",
			})
		}
	}

	res.Port, res.PortSource = resolvePort(dir, res, info, plan, dockerfile)

	// A service that is enabled but has no corresponding env file is fine: the
	// platform still injects its URL.
	return res
}

func mergePlan(res *Result, p *nixpacksPlan) {
	provider := ""
	if len(p.Providers) > 0 {
		provider = p.Providers[0]
	} else if meta := strings.TrimSpace(p.Variables["NIXPACKS_METADATA"]); meta != "" {
		provider = strings.TrimSpace(strings.Split(meta, ",")[0])
	}
	if lang := providerLanguage(provider); lang != "" {
		if res.Language == "unknown" || res.Language == "" {
			res.Language = lang
		}
	}
	for _, mapping := range []struct {
		key string
		lang string
	}{
		{"NIXPACKS_NODE_VERSION", "node"},
		{"NIXPACKS_PYTHON_VERSION", "python"},
		{"NIXPACKS_GO_VERSION", "go"},
		{"NIXPACKS_JAVA_VERSION", "java"},
	} {
		if v := strings.TrimSpace(p.Variables[mapping.key]); v != "" && res.LanguageVersion == "" {
			res.LanguageVersion = v
		}
	}
}

func providerLanguage(provider string) string {
	p := strings.ToLower(provider)
	switch {
	case strings.Contains(p, "node"):
		return "node"
	case strings.Contains(p, "python"):
		return "python"
	case strings.Contains(p, "go"):
		return "go"
	case strings.Contains(p, "java"), strings.Contains(p, "jdk"), strings.Contains(p, "maven"):
		return "java"
	case strings.Contains(p, "ruby"):
		return "ruby"
	case strings.Contains(p, "php"):
		return "php"
	case strings.Contains(p, "rust"):
		return "rust"
	case strings.Contains(p, "static"):
		return "static"
	default:
		return ""
	}
}

func applyGoToolchain(res *Result) {
	required := strings.TrimSpace(res.LanguageVersion)
	if required == "" {
		return
	}
	req := normalizeGoVersion(required)
	max := normalizeGoVersion(MaxSupportedGo)
	if req == "" || max == "" {
		return
	}
	if compareVersions(req, max) <= 0 {
		res.LanguageVersion = req
		return
	}
	res.LanguageVersion = MaxSupportedGo
	res.Blockers = append(res.Blockers, Blocker{
		Code: "unsupported_go_version",
		Message: fmt.Sprintf("This project asks for Go %s, but the installed nixpacks toolchain supports up to Go %s.",
			required, MaxSupportedGo),
		Fix: fmt.Sprintf("Lower the go directive in go.mod to %s (for example: go %s), or upgrade nixpacks on the host, then check the repository again.",
			MaxSupportedGo, MaxSupportedGo),
	})
}

func normalizeGoVersion(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "go"))
	re := regexp.MustCompile(`^(\d+)\.(\d+)`)
	if m := re.FindStringSubmatch(v); m != nil {
		return m[1] + "." + m[2]
	}
	return ""
}

func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		av, bv := 0, 0
		if i < len(pa) {
			av = atoi(pa[i])
		}
		if i < len(pb) {
			bv = atoi(pb[i])
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func atoi(s string) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

var portInCommandRE = regexp.MustCompile(`(?:--port[= ]|:)(\d{2,5})`)

func resolvePort(dir string, res *Result, info projectInfo, plan *nixpacksPlan, dockerfile string) (int, string) {
	for _, v := range res.EnvVars {
		if strings.EqualFold(v.Key, "PORT") && !isPlaceholder(v.Default) {
			if n := parsePort(v.Default); n > 0 {
				return n, "env"
			}
		}
	}
	if plan != nil {
		for _, key := range []string{"PORT", "NIXPACKS_PORT"} {
			if n := parsePort(plan.Variables[key]); n > 0 {
				return n, "detected"
			}
		}
	}
	if dockerfile != "" {
		if text := readFile(dir, dockerfile); text != "" {
			re := regexp.MustCompile(`(?mi)^\s*EXPOSE\s+(\d+)`)
			if m := re.FindStringSubmatch(text); m != nil {
				if n := parsePort(m[1]); n > 0 {
					return n, "detected"
				}
			}
		}
	}
	if m := portInCommandRE.FindStringSubmatch(res.StartCommand); m != nil {
		if n := parsePort(m[1]); n > 0 {
			return n, "detected"
		}
	}
	return defaultPort(info.language, info.framework), "default"
}

func defaultPort(language, framework string) int {
	switch language {
	case "node":
		return 3000
	case "python":
		if framework == "flask" {
			return 5000
		}
		return 8000
	case "go", "java", "ruby", "rust":
		return 8080
	case "php":
		return 80
	case "static":
		return 80
	default:
		return 3000
	}
}

func findDockerfile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "Dockerfile") {
			return e.Name()
		}
	}
	return ""
}

func dockerfileStart(dir, name string) string {
	text := readFile(dir, name)
	if text == "" {
		return ""
	}
	var cmd string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "CMD ") || strings.HasPrefix(upper, "ENTRYPOINT ") {
			cmd = strings.TrimSpace(line[strings.IndexByte(line, ' ')+1:])
		}
	}
	cmd = strings.TrimPrefix(cmd, "[")
	cmd = strings.TrimSuffix(cmd, "]")
	cmd = strings.ReplaceAll(cmd, "\"", "")
	cmd = strings.ReplaceAll(cmd, ",", " ")
	return strings.Join(strings.Fields(cmd), " ")
}

// nixpacksPlan mirrors the fields we consume from `nixpacks plan --format json`.
// nixpacks 1.41 leaves providers empty and reports the detected stack in
// variables.NIXPACKS_METADATA, so both are read.
type nixpacksPlan struct {
	Providers []string          `json:"providers"`
	Variables map[string]string `json:"variables"`
	Start     struct {
		Cmd string `json:"cmd"`
	} `json:"start"`
}

func (a *Analyzer) plan(ctx context.Context, dir string) *nixpacksPlan {
	planCtx, cancel := context.WithTimeout(ctx, PlanTimeout)
	defer cancel()
	cmd := exec.CommandContext(planCtx, a.nixpacksBin, "plan", "--format", "json", dir)
	cmd.Env = analysisEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil
	}
	var p nixpacksPlan
	if err := json.Unmarshal(stdout.Bytes(), &p); err != nil {
		return nil
	}
	return &p
}

// analysisEnv is a deny-by-default child environment for nixpacks. It keeps
// platform secrets out while preserving HOME so the nixpacks/Docker CLI can
// find its own configuration.
func analysisEnv() []string {
	allowed := map[string]bool{
		"PATH": true, "USER": true, "LOGNAME": true, "HOME": true,
		"LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TZ": true,
		"TMPDIR": true, "TMP": true, "TEMP": true,
	}
	var out []string
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if ok && allowed[key] {
			out = append(out, kv)
		}
	}
	return out
}

func headSHA(dir string) (string, error) {
	// Reading the packed HEAD directly avoids pulling go-git into this package
	// twice; the git package owns cloning and reports the branch separately.
	data, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		return "", fmt.Errorf("analyze: reading HEAD: %w", err)
	}
	head := strings.TrimSpace(string(data))
	if strings.HasPrefix(head, "ref: ") {
		ref := strings.TrimPrefix(head, "ref: ")
		b, err := os.ReadFile(filepath.Join(dir, ".git", filepath.FromSlash(ref)))
		if err == nil {
			return strings.TrimSpace(string(b)), nil
		}
	}
	if len(head) >= 40 {
		return head, nil
	}
	return "", fmt.Errorf("analyze: could not resolve commit SHA")
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func (a *Analyzer) lookup(sha string) *Result {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.cache[sha]
	if !ok || a.now().Sub(e.at) > CacheTTL {
		if ok {
			delete(a.cache, sha)
		}
		return nil
	}
	cp := *e.result
	cp.EnvVars = append([]EnvVar(nil), e.result.EnvVars...)
	cp.Warnings = append([]Warning(nil), e.result.Warnings...)
	cp.Blockers = append([]Blocker(nil), e.result.Blockers...)
	cp.Services.Reasons = append([]string(nil), e.result.Services.Reasons...)
	return &cp
}

func (a *Analyzer) store(sha string, res *Result) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache[sha] = cacheEntry{result: res, at: a.now()}
}
