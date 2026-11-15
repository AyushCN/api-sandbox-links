package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/api-sandbox-links/backend/internal/analyze"
	"github.com/api-sandbox-links/backend/internal/git"
)

// serviceOverride is the wire shape of the wizard's editable service chips.
type serviceOverride struct {
	Postgres bool `json:"postgres"`
	MySQL    bool `json:"mysql"`
	Redis    bool `json:"redis"`
}

// effectiveServices applies the user's explicit choice over auto-detection.
func effectiveServices(override *serviceOverride, database string, detected analyze.Services) analyze.Services {
	out := detected
	switch strings.ToLower(strings.TrimSpace(database)) {
	case "postgres":
		out.Postgres, out.MySQL = true, false
	case "mysql":
		out.Postgres, out.MySQL = false, true
	case "none":
		out.Postgres, out.MySQL = false, false
	}
	if override != nil {
		out.Postgres = override.Postgres
		out.MySQL = override.MySQL
		out.Redis = override.Redis
	}
	if out.Postgres && out.MySQL {
		out.MySQL = false
	}
	return out
}

func servicesFromJSON(raw json.RawMessage, database string, detected analyze.Services) (analyze.Services, error) {
	if len(raw) == 0 {
		return effectiveServices(nil, database, detected), nil
	}
	var ov serviceOverride
	if err := json.Unmarshal(raw, &ov); err != nil {
		return detected, err
	}
	return effectiveServices(&ov, database, detected), nil
}

func servicesJSON(svc analyze.Services) map[string]any {
	return map[string]any{
		"postgres": svc.Postgres,
		"mysql":    svc.MySQL,
		"redis":    svc.Redis,
		"reasons":  svc.Reasons,
	}
}

// prepareEnv merges user values with the example file's prefilled defaults,
// validates names/values, and reports required keys that are still empty.
func prepareEnv(provided map[string]string, result *analyze.Result, svc analyze.Services) (merged map[string]string, missing, problems []string) {
	merged = map[string]string{}
	for k, v := range provided {
		merged[k] = v
	}
	for _, v := range result.EnvVars {
		managed := v.Managed
		if !managed {
			if m, _ := analyze.IsManagedName(v.Key, svc); m {
				managed = true
			}
		}
		if managed {
			delete(merged, v.Key)
			continue
		}
		if _, ok := merged[v.Key]; !ok && v.Default != "" {
			merged[v.Key] = v.Default
		}
	}
	problems = analyze.ValidateEnvValues(merged, svc)
	if len(problems) > 0 {
		return merged, nil, problems
	}
	missing = analyze.MissingRequired(withManaged(result.EnvVars, svc), merged)
	return merged, missing, nil
}

// withManaged refreshes managed classification against the service set that
// will actually run (which can differ from auto-detection).
func withManaged(vars []analyze.EnvVar, svc analyze.Services) []analyze.EnvVar {
	out := make([]analyze.EnvVar, len(vars))
	copy(out, vars)
	for i := range out {
		out[i].Managed = false
		out[i].ManagedBy = ""
	}
	analyze.ApplyManaged(out, svc)
	return out
}

func writeMissingEnv(w http.ResponseWriter, missing []string) {
	sort.Strings(missing)
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error":   "missing_env",
		"missing": missing,
	})
}

func writeBlockers(w http.ResponseWriter, blockers []analyze.Blocker) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error":    "blocked",
		"blockers": blockers,
	})
}

// envKeysJSON returns env keys and whether each has a value. Values are never
// serialized, logged, or returned by any API response.
func (s *Server) envKeysJSON(sandboxID string) []map[string]any {
	vars, err := s.store.ListEnvVars(sandboxID)
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(vars))
	for _, v := range vars {
		out = append(out, map[string]any{"key": v.Key, "is_set": len(v.Value) > 0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["key"].(string) < out[j]["key"].(string) })
	return out
}

func (s *Server) plainEnv(sandboxID string) (map[string]string, error) {
	vars, err := s.store.ListEnvVars(sandboxID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(vars))
	for _, v := range vars {
		plain, err := s.box.Decrypt(v.Value)
		if err != nil {
			continue
		}
		out[v.Key] = string(plain)
	}
	return out, nil
}

// replaceEnv upserts the provided values and deletes keys no longer present.
func (s *Server) replaceEnv(sandboxID string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for k, v := range values {
		enc, err := s.box.Encrypt([]byte(v))
		if err != nil {
			return err
		}
		if err := s.store.UpsertEnvVar(sandboxID, k, enc, false); err != nil {
			return err
		}
		keys = append(keys, k)
	}
	_, err := s.store.DeleteEnvVarsNotIn(sandboxID, keys)
	return err
}

type updateEnvRequest struct {
	EnvVars      map[string]string `json:"env_vars"`
	StartCommand string            `json:"start_command"`
}

// handleUpdateEnv replaces a sandbox's settings after creation. It applies the
// same validation/gate as creation and asks the user to redeploy rather than
// restarting the app behind their back.
func (s *Server) handleUpdateEnv(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "This sandbox has been destroyed and cannot be edited.", "sandbox_destroyed_cannot_edit")
		return
	}
	var req updateEnvRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Send a JSON body with env_vars.", "validation_missing_param")
		return
	}
	if req.EnvVars == nil {
		req.EnvVars = map[string]string{}
	}
	repo, err := s.store.GetRepository(sb.RepositoryID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not load this sandbox's repository.", "load_sandbox_repo_failed")
		return
	}
	info, err := git.ParseGitHubURL(repo.GithubURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not read this sandbox's repository URL.", "read_sandbox_repo_url_failed")
		return
	}
	result, err := s.analyzer.Analyze(r.Context(), info.CloneURL, sb.BranchName)
	if err != nil {
		status, msg := friendlyAnalyzeError(err)
		writeErr(w, status, msg)
		return
	}
	svc, err := servicesFromJSON(json.RawMessage(sb.ServicesOverride), "", result.Services)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid services selection.", "validation_invalid_services")
		return
	}
	// The editor deliberately starts with blank inputs and the API never
	// returns decrypted values. Treat a blank submission as "leave unchanged":
	// otherwise opening the editor and pressing save would silently erase every
	// secret the user did not retype.
	existing, err := s.plainEnv(sb.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not read the existing settings.")
		return
	}
	provided := make(map[string]string, len(existing)+len(req.EnvVars))
	for k, v := range existing {
		provided[k] = v
	}
	for k, v := range req.EnvVars {
		if strings.TrimSpace(v) == "" {
			if _, ok := existing[k]; ok {
				continue
			}
		}
		provided[k] = v
	}
	merged, missing, problems := prepareEnv(provided, result, svc)
	if len(problems) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_env", "problems": problems})
		return
	}
	if len(missing) > 0 {
		writeMissingEnv(w, missing)
		return
	}
	if len(result.Blockers) > 0 {
		writeBlockers(w, result.Blockers)
		return
	}
	if err := s.replaceEnv(sb.ID, merged); err != nil {
		s.logger.Warn("env update failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "Could not save these settings.", "save_settings_failed")
		return
	}
	patch := map[string]any{}
	if strings.TrimSpace(req.StartCommand) != "" {
		patch["start_command"] = strings.TrimSpace(req.StartCommand)
		sb.StartCommand = strings.TrimSpace(req.StartCommand)
	}
	if len(patch) > 0 {
		_ = s.store.UpdateSandbox(sb.ID, patch)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sandbox":           s.sandboxJSON(sb),
		"redeploy_required": true,
	})
}
