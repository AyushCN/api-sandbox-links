# Troubleshooting

This guide covers common issues and their fixes for the API Sandbox Links platform.

## 1. Backend won't start / `curl: (7) Failed to connect`

**Symptom**: `curl: (7) Failed to connect to 127.0.0.1 port 8080: Connection refused`

**Causes and fixes**:

| Symptom | Cause and fix |
|---|---|
| `curl: (7) Failed to connect ... :8080` after a cleanup | `backend/server` was removed. Rebuild: `cd backend && go build -o server ./cmd/server`, then run from the repo root. |
| Backend fails on startup | Run `./backend/server doctor` to see config validation errors. Fix any missing `DATABASE_URL`, `REDIS_URL`, or `SECRETS_ENCRYPTION_KEY`. |
| `config: invalid configuration` on startup | All problems are listed before returning. Fix each listed issue, typically missing env vars or invalid base64 encryption key. |

## 2. Build times out / "killed"

**Symptom**: Build runs for 20 minutes then exits, or `process killed`

**Causes and fixes**:

| Symptom | Cause and fix |
|---|---|
| Build times out | Pre-warm builds before a demo. The timeout is 20 min. If a build genuinely exceeds 20 minutes, check `docker ps` and kill orphaned processes: `ps -eo pid,etime,args \| grep -E 'nixpacks|docker build|nix-env'`; then destroy the sandbox. |
| `go1.27 ... toolchain not available` | The repo's `go.mod` requests a newer Go than nixpacks provides (tested: `go1.27`). Use a repo with an older `go` directive, or lower the go directive: `go 1.22` (or whatever nixpacks supports). |
| Build appears stuck | Same orphan-process issue, or a build genuinely exceeds 20 minutes. Check `docker ps` and the processes above. |

## 3. Public URL returns 404 on :80

**Symptom**: `curl -s http://localhost/` returns 404, but works on :8090

**Cause**: Traefik file provider flags missing in `docker-compose.dev.yml`; check `traefik/dynamic.yml` is mounted and `--providers.docker=false` is set.

**Fix**: Ensure `traefik/dynamic.yml` is mounted and the Traefik container uses the file provider:

```bash
# Verify in docker compose
docker compose logs sandbox-platform-traefik
# Should show no Docker provider errors (expected in production mode)
```

## 4. Go build: `toolchain not available`

**Symptom**: `nixpacks: Go toolchain not available`

**Fix**: Use a repo with an older `go` directive in `go.mod`. Tested: `go1.27` fails, but `go1.22` works. Lower the go directive or upgrade nixpacks on the host.

## 5. 409 on create

**Symptom**: `409 {"error":"active sandbox quota reached (3); ..."}`

**Fix**: Destroy one existing sandbox or wait for expiry. Per-user limit of 3 live sandboxes.

## 6. 503 on create

**Symptom**: `503 {"error":"global running cap reached"}`

**Fix**: Destroy one running sandbox or wait for expiry. Global running cap defaults to 6.

## 7. Tests fail on DB

**Fix**: Set `ASL_TEST_DATABASE_URL` and make sure the `asl_test` database exists.

## 8. `curl: (7) Failed to connect ... :8080` after cleanup

**Fix**: Rebuild: `cd backend && go build -o server ./cmd/server`, then run from the repo root.

## 9. Buildx component is missing

**Fix**: `nixpacksEnv` must preserve `HOME`; it does in the current code. Confirm `HOME` points to the user with `~/.docker/cli-plugins`.

## 10. Public URL 404 on :80

**Fix**: Traefik must mount `traefik/dynamic.yml`, disable the Docker provider, and reach `host.docker.internal:8090`. Check `docker compose logs sandbox-platform-traefik`.

## 11. Python: `No start command could be found`

**Fix**: The repo has no Procfile and nixpacks cannot infer a start command. There is no platform override. Add a `start` script to `package.json` or a Procfile.

## 12. Java build never finishes

**Fix**: The 20-minute timeout does not kill orphaned `docker buildx`/`nix-env` children. Find and kill them: `ps -eo pid,etime,args \| grep -E 'nixpacks|docker build|nix-env'`; then destroy the sandbox.

## 13. Sandbox stuck `building`

**Fix**: Same orphan-process issue, or a build genuinely exceeds 20 minutes. Check `docker ps` and the processes above.

## 14. Sandbox is reachable but platform DB is not

**Expected**: compose binds Postgres/Redis to `127.0.0.1`; the bridge gateway ports are blocked. This is intentional for isolation.

## 15. `git clone` times out

**Fix**: Transient network failure; retry. The failed clone cleans up after itself.

## 16. `npm test` exits 1

**Fix**: No test files exist. This is expected for this repository.

## 17. `npm run build` fails with `Type '\"zinc\"' is not assignable to type '\"blue\" ...`

**Fix**: Known frontend type error in `sandbox-card.tsx` and `sandbox-detail.tsx`. This is a known issue that does not block functionality.

## 18. `GET /api/auth/github` returns 501

**Expected**: in dev-auth mode; it is only available when `GITHUB_CLIENT_ID` is set.

## 19. Sandbox is reachable but platform DB is not

**Expected**: compose binds Postgres/Redis to `127.0.0.1`; the bridge gateway ports are blocked. This is a security vector, not a bug.

## 20. File API: path traversal blocked but symlinks not resolved

**Fix**: Lexical traversal is blocked (`../../etc/passwd` returns 400). Symlinks are not resolved, so a symlink committed inside the repo can escape the work directory. Treat the editor as unsafe for untrusted repositories until this is fixed.

## 21. `.env` remains in git history

**Fix**: Rotate all secrets and purge history before production use. The `.env` file was previously committed and removed from git history, but history was not purged.

## 22. No CLI and no API-token management

**Note**: There is no CLI in this repository. `cli/` does not exist.

## 23. No webhook endpoint, signature verification, pending-redeploy event, or approve/dismiss flow

**Note**: Not implemented. All of these return 404.

## 24. No Redis or MongoDB sidecars

**Note**: Only Postgres and MySQL are recognized. Redis and MongoDB are not implemented.

## 25. Private repositories are not supported

**Note**: `git.Clone` uses no auth; OAuth `repo` scope is unused for cloning.

## 26. Builds run on the host through nixpacks/Docker; there is no gVisor or other user-container runtime isolation

**Note**: This is a known limitation.

## 27. Egress from sandboxes is unrestricted; metadata and port-25 blocking are not enforced

**Note**: Known limitation.

## 28. HTTPS is not configured; local mode is HTTP-only

**Note**: Requires a real domain and wildcard certificate.

## 29. No startup reconciliation for containers removed outside the platform

**Note**: A stale row is not repaired automatically.

## 30. The 20-minute deploy timeout does not reliably terminate builds; orphaned Docker/nix processes may outlive it

**Note**: Known finding.

---

## Quick diagnosis checklist

When something goes wrong, run these checks in order:

1. **Backend health**: `curl -fsS http://127.0.0.1:8080/api/health`
2. **Backend ready**: `curl -fsS http://127.0.0.1:8080/api/ready`
3. **Doctor**: `/opt/api-sandbox/server doctor`
4. **Docker ps**: `docker ps -a --format '{{.Names}}'` — look for orphaned containers
5. **Docker logs**: `docker logs <container>` — look for build errors
6. **Disk space**: `df -h /tmp/api-sandbox-links/work` — ensure min free GB
7. **Env vars**: `cat /etc/api-sandbox/backend.env` — verify all required keys are present
8. **Traefik routes**: `docker compose ps` — verify caddy/traefik are up
9. **DNS**: verify `*.sandbox.localhost` resolves to this host