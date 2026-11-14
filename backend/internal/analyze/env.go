package analyze

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EnvFileCandidates are the supported example-file names, in priority order.
// Matching is case-insensitive so ".ENV.Example" is still found.
var EnvFileCandidates = []string{
	".env.example", ".env.sample", ".env.template", ".env.dist",
	"example.env", "env.example", "sample.env",
}

// EnvVar is one setting a project expects. It is deliberately the public API
// shape (JSON tags included) because the frontend renders it verbatim.
type EnvVar struct {
	Key       string `json:"key"`
	Required  bool   `json:"required"`
	Secret    bool   `json:"secret"`
	Default   string `json:"default,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Managed   bool   `json:"managed"`
	ManagedBy string `json:"managed_by,omitempty"`
}

var (
	envKeyPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	keyName       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	commentedKey  = regexp.MustCompile(`^#\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
)

// FindEnvFile returns the path of the first example env file that exists in
// dir. It returns "" when the project ships none.
func FindEnvFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	lower := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			lower[strings.ToLower(e.Name())] = e.Name()
		}
	}
	for _, want := range EnvFileCandidates {
		if actual, ok := lower[strings.ToLower(want)]; ok {
			return filepath.Join(dir, actual)
		}
	}
	return ""
}

// ParseEnvExampleFile reads and parses an example env file.
func ParseEnvExampleFile(path string) ([]EnvVar, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseEnvExample(string(b)), nil
}

// ParseEnvExample parses the small, widely-used dotenv dialect found in example
// files: KEY=VALUE, optional "export " prefix, single/double quotes, inline
// comments, preceding comment lines used as help text, and commented-out
// "# KEY=value" lines that document optional settings.
func ParseEnvExample(content string) []EnvVar {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := make([]EnvVar, 0, 16)
	seen := map[string]int{}
	var comments []string

	add := func(v EnvVar) {
		if idx, ok := seen[v.Key]; ok {
			out[idx] = v
			return
		}
		seen[v.Key] = len(out)
		out = append(out, v)
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			comments = nil
			continue
		}
		if strings.HasPrefix(line, "#") {
			if m := commentedKey.FindStringSubmatch(line); m != nil {
				val := parseEnvValue(m[2])
				v := EnvVar{
					Key:      m[1],
					Required: false,
					Secret:   looksSecret(m[1]),
					Default:  defaultFor(val),
					Comment:  strings.Join(comments, " "),
				}
				add(v)
				comments = nil
				continue
			}
			text := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if text != "" {
				comments = append(comments, text)
			}
			continue
		}

		line = strings.TrimPrefix(line, "export ")
		line = strings.TrimSpace(line)
		m := envKeyPattern.FindStringSubmatch(line)
		if m == nil {
			comments = nil
			continue
		}
		value := parseEnvValue(m[2])
		placeholder := isPlaceholder(value)
		v := EnvVar{
			Key:      m[1],
			Required: placeholder,
			Secret:   looksSecret(m[1]),
			Comment:  strings.Join(comments, " "),
		}
		if !placeholder {
			v.Default = value
		}
		add(v)
		comments = nil
	}
	return out
}

// parseEnvValue removes quotes and inline comments from the right-hand side.
func parseEnvValue(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if s[0] == '"' {
		var b strings.Builder
		escaped := false
		for i := 1; i < len(s); i++ {
			c := s[i]
			if escaped {
				switch c {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case 'r':
					b.WriteByte('\r')
				default:
					b.WriteByte(c)
				}
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				return b.String()
			}
			b.WriteByte(c)
		}
		return b.String()
	}
	if s[0] == '\'' {
		if idx := strings.IndexByte(s[1:], '\''); idx >= 0 {
			return s[1 : 1+idx]
		}
		return s[1:]
	}
	// Unquoted: drop an inline comment introduced by whitespace + '#'.
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			s = s[:i]
			break
		}
	}
	return strings.TrimSpace(s)
}

var placeholders = []string{
	"changeme", "change-me", "replace-me", "replace_me", "todo", "...", "xxx",
}

func isPlaceholder(v string) bool {
	s := strings.ToLower(strings.TrimSpace(strings.Trim(v, `"'`)))
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "your_") || strings.HasPrefix(s, "your-") {
		return true
	}
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		return true
	}
	for _, p := range placeholders {
		if s == p {
			return true
		}
	}
	return false
}

func defaultFor(value string) string {
	if isPlaceholder(value) {
		return ""
	}
	return value
}

func looksSecret(key string) bool {
	k := strings.ToUpper(key)
	for _, token := range []string{"SECRET", "TOKEN", "PASSWORD", "PASSWD", "KEY", "PRIVATE", "CREDENTIAL"} {
		if strings.Contains(k, token) {
			return true
		}
	}
	return false
}

// IsManagedName reports whether the platform injects key itself given the
// enabled services. PORT/HOST are always managed. Database and Redis variables
// are managed only when the matching service is on; otherwise they are normal
// settings the user must provide.
func IsManagedName(key string, svc Services) (bool, string) {
	k := strings.ToUpper(key)
	switch k {
	case "PORT", "HOST":
		return true, "platform"
	}
	if strings.HasPrefix(k, "DOCKER_") {
		return false, ""
	}
	pgLike := k == "DATABASE_URL" || strings.HasPrefix(k, "DB_") || strings.HasPrefix(k, "DATABASE_")
	if pgLike && (svc.Postgres || svc.MySQL) {
		if svc.MySQL && !svc.Postgres {
			return true, "mysql"
		}
		return true, "postgres"
	}
	if strings.HasPrefix(k, "POSTGRES_") || (strings.HasPrefix(k, "PG") && len(k) > 2) {
		if svc.Postgres {
			return true, "postgres"
		}
	}
	if strings.HasPrefix(k, "MYSQL_") {
		if svc.MySQL {
			return true, "mysql"
		}
	}
	if k == "REDIS_URL" || strings.HasPrefix(k, "REDIS_") {
		if svc.Redis {
			return true, "redis"
		}
	}
	return false, ""
}

// ApplyManaged fills Managed/ManagedBy and clears Required for injected vars.
func ApplyManaged(vars []EnvVar, svc Services) {
	for i := range vars {
		if managed, by := IsManagedName(vars[i].Key, svc); managed {
			vars[i].Managed = true
			vars[i].ManagedBy = by
			vars[i].Required = false
		}
	}
}

// MissingRequired returns the required, non-managed keys that have no
// non-empty value in provided.
func MissingRequired(vars []EnvVar, provided map[string]string) []string {
	var missing []string
	for _, v := range vars {
		if !v.Required || v.Managed {
			continue
		}
		if strings.TrimSpace(provided[v.Key]) == "" {
			missing = append(missing, v.Key)
		}
	}
	return missing
}

// ValidateEnvValues enforces the platform's env-var contract. It returns
// human-readable problems; callers translate them into a 400 response.
func ValidateEnvValues(vals map[string]string, svc Services) []string {
	var problems []string
	if len(vals) > 100 {
		problems = append(problems, "at most 100 environment variables are allowed")
		return problems
	}
	for key, value := range vals {
		if !keyName.MatchString(key) {
			problems = append(problems, "invalid environment variable name: "+key)
			continue
		}
		switch strings.ToUpper(key) {
		case "PORT", "PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "HOME":
			problems = append(problems, key+" is reserved by the platform")
			continue
		}
		if strings.HasPrefix(strings.ToUpper(key), "DOCKER_") {
			problems = append(problems, key+" is reserved by the platform")
			continue
		}
		if managed, _ := IsManagedName(key, svc); managed {
			problems = append(problems, key+" is provided automatically by the platform")
			continue
		}
		if len(value) > 8*1024 {
			problems = append(problems, key+" exceeds the 8 KB value limit")
		}
	}
	return problems
}
