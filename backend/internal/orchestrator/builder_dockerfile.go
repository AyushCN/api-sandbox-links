package orchestrator

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DockerfileBuilder builds container images using standard Dockerfiles via Docker/BuildKit.
type DockerfileBuilder struct {
	dockerHost string
	logger     *slog.Logger
}

// NewDockerfileBuilder constructs a new DockerfileBuilder.
func NewDockerfileBuilder(dockerHost string, logger *slog.Logger) *DockerfileBuilder {
	return &DockerfileBuilder{
		dockerHost: dockerHost,
		logger:     logger,
	}
}

// Name returns the canonical identifier "dockerfile".
func (b *DockerfileBuilder) Name() string {
	return "dockerfile"
}

// findDockerfile looks for Dockerfile or dockerfile in the provided directory.
func findDockerfile(dir string) (string, error) {
	for _, name := range []string{"Dockerfile", "dockerfile"} {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("dockerfile builder: no Dockerfile found in %s", dir)
}

// resolveDockerfilePath resolves a Dockerfile path against baseDir.
// It handles relative paths, absolute paths within baseDir or filesystem,
// and falls back to searching for a standard Dockerfile if configuredPath is empty.
func resolveDockerfilePath(baseDir, configuredPath string) (string, error) {
	clean := strings.TrimSpace(configuredPath)
	if clean == "" {
		return findDockerfile(baseDir)
	}
	// If it's already an existing absolute path, accept it directly
	if filepath.IsAbs(clean) {
		if fi, err := os.Stat(clean); err == nil && !fi.IsDir() {
			return clean, nil
		}
		// If clean was absolute but doesn't exist directly, check if it's relative to baseDir
		// (e.g. "/Dockerfile" or misconfigured leading slash)
		rel := strings.TrimPrefix(clean, "/")
		joined := filepath.Join(baseDir, rel)
		if fi, err := os.Stat(joined); err == nil && !fi.IsDir() {
			return joined, nil
		}
		return "", fmt.Errorf("configured Dockerfile %q not found", configuredPath)
	}
	target := filepath.Join(baseDir, clean)
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		return target, nil
	}
	return "", fmt.Errorf("configured Dockerfile %q not found in %s", configuredPath, baseDir)
}

var (
	exposeRegex     = regexp.MustCompile(`(?i)^\s*EXPOSE\s+(\d+)`)
	fromRegex       = regexp.MustCompile(`(?i)^\s*FROM\s+([^\s]+)`)
	cmdRegex        = regexp.MustCompile(`(?i)^\s*CMD\s+(.+)`)
	entrypointRegex = regexp.MustCompile(`(?i)^\s*ENTRYPOINT\s+(.+)`)
	envRegex        = regexp.MustCompile(`(?i)^\s*ENV\s+([A-Za-z_][A-Za-z0-9_]*)[=\s](.+)`)
)

// PlanAnalyze inspects the Dockerfile in dir and returns a normalized BuildPlan.
func (b *DockerfileBuilder) PlanAnalyze(ctx context.Context, dir string) (*BuildPlan, error) {
	dockerfilePath, err := findDockerfile(dir)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(dockerfilePath)
	if err != nil {
		return nil, fmt.Errorf("dockerfile builder: opening %s: %w", dockerfilePath, err)
	}
	defer f.Close()

	var (
		port             = 8080 // default container port
		detectedLanguage = "dockerfile"
		provider         = "dockerfile"
		startCmd         = ""
		entrypoint       = ""
		variables        = make(map[string]string)
	)

	scanner := bufio.NewScanner(f)
	var fullLine strings.Builder
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		// Handle line continuation backslashes
		if strings.HasSuffix(line, "\\") {
			fullLine.WriteString(strings.TrimSuffix(line, "\\"))
			fullLine.WriteString(" ")
			continue
		}
		fullLine.WriteString(line)
		current := strings.TrimSpace(fullLine.String())
		fullLine.Reset()

		if strings.HasPrefix(current, "#") || current == "" {
			continue
		}

		// EXPOSE
		if m := exposeRegex.FindStringSubmatch(current); len(m) > 1 {
			if p, err := strconv.Atoi(m[1]); err == nil && p > 0 {
				port = p
			}
		}

		// FROM
		if m := fromRegex.FindStringSubmatch(current); len(m) > 1 {
			img := strings.ToLower(m[1])
			switch {
			case strings.Contains(img, "python"):
				detectedLanguage = "python"
				provider = "python"
			case strings.Contains(img, "node") || strings.Contains(img, "bun") || strings.Contains(img, "deno"):
				detectedLanguage = "node"
				provider = "node"
			case strings.Contains(img, "golang") || strings.Contains(img, "go:"):
				detectedLanguage = "go"
				provider = "go"
			case strings.Contains(img, "ruby"):
				detectedLanguage = "ruby"
				provider = "ruby"
			case strings.Contains(img, "java") || strings.Contains(img, "openjdk") || strings.Contains(img, "maven") || strings.Contains(img, "gradle"):
				detectedLanguage = "java"
				provider = "java"
			case strings.Contains(img, "rust"):
				detectedLanguage = "rust"
				provider = "rust"
			case strings.Contains(img, "php"):
				detectedLanguage = "php"
				provider = "php"
			}
		}

		// ENTRYPOINT
		if m := entrypointRegex.FindStringSubmatch(current); len(m) > 1 {
			entrypoint = cleanCommandString(m[1])
		}

		// CMD
		if m := cmdRegex.FindStringSubmatch(current); len(m) > 1 {
			startCmd = cleanCommandString(m[1])
		}

		// ENV
		if strings.HasPrefix(strings.ToUpper(current), "ENV ") {
			for k, v := range parseEnvLine(current) {
				variables[k] = v
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dockerfile builder: scanning %s: %w", dockerfilePath, err)
	}

	// Composite start command if entrypoint and CMD both exist
	resolvedCmd := startCmd
	if entrypoint != "" && startCmd != "" {
		resolvedCmd = entrypoint + " " + startCmd
	} else if entrypoint != "" {
		resolvedCmd = entrypoint
	}

	return &BuildPlan{
		Builder:          "dockerfile",
		Provider:         provider,
		StartCmd:         resolvedCmd,
		InternalPort:     port,
		DetectedLanguage: detectedLanguage,
		Variables:        variables,
	}, nil
}

// parseEnvLine extracts one or more KEY=VAL pairs from an ENV instruction.
func parseEnvLine(raw string) map[string]string {
	res := make(map[string]string)
	upper := strings.ToUpper(raw)
	idx := strings.Index(upper, "ENV")
	if idx < 0 {
		return res
	}
	trimmed := strings.TrimSpace(raw[idx+3:])
	fields := strings.Fields(trimmed)
	for _, f := range fields {
		if k, v, ok := strings.Cut(f, "="); ok {
			res[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	if len(res) == 0 && len(fields) >= 2 {
		res[fields[0]] = strings.Trim(fields[1], `"'`)
	}
	return res
}

// cleanCommandString normalizes JSON array syntax like ["python", "app.py"] or shell strings into readable command strings.
func cleanCommandString(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		inner := strings.TrimPrefix(strings.TrimSuffix(raw, "]"), "[")
		parts := strings.Split(inner, ",")
		var cleaned []string
		for _, part := range parts {
			cleaned = append(cleaned, strings.Trim(strings.TrimSpace(part), `"'`))
		}
		return strings.Join(cleaned, " ")
	}
	return raw
}

// BuildImage compiles source code in dir into an image named `name` using `docker build`.
func (b *DockerfileBuilder) BuildImage(ctx context.Context, dir, name string, progress io.Writer) (string, error) {
	dockerfilePath, err := findDockerfile(dir)
	if err != nil {
		return "", err
	}

	args := []string{"build", "-t", name, "-f", dockerfilePath, dir}
	cmd := exec.CommandContext(ctx, "docker", args...)

	env := os.Environ()
	if b.dockerHost != "" {
		env = append(env, "DOCKER_HOST="+b.dockerHost)
	}
	cmd.Env = env

	var outputBuf bytes.Buffer
	if progress != nil {
		mw := io.MultiWriter(progress, &outputBuf)
		cmd.Stdout = mw
		cmd.Stderr = mw
	} else {
		cmd.Stdout = &outputBuf
		cmd.Stderr = &outputBuf
	}

	if err := cmd.Run(); err != nil {
		outStr := strings.TrimSpace(outputBuf.String())
		if outStr != "" {
			return "", fmt.Errorf("dockerfile builder: build failed: %w\nOutput:\n%s", err, outStr)
		}
		return "", fmt.Errorf("dockerfile builder: build failed: %w", err)
	}

	return name + ":latest", nil
}
