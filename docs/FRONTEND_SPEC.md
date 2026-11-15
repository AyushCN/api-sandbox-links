# API Sandbox: Frontend Spec (full rebuild)

Save this file as `docs/FRONTEND_SPEC.md` in the repo. Every build phase refers to it.
If the spec and the real backend disagree, the real backend wins. Record the difference in
`docs/FRONTEND_NOTES.md`.

---

## 1. Goals

1. A first-time user can paste a GitHub link and get a live URL without reading anything.
2. A required setting (env var) can never be missed. The Deploy button explains why it is disabled.
3. Every state (loading, empty, error, sleeping, expired) is designed, not left as a blank or raw JSON.
4. Fast and accessible: usable on a phone, keyboard friendly, AA contrast, works in dark and light.

Principles: one obvious primary action per screen, plain language over jargon, status first
and logs one click away, every error says what happened and what to do next.

## 2. Stack decisions

| Concern | Choice |
|---|---|
| Framework | Next.js (App Router), TypeScript `strict`, React 19 |
| Styling | Tailwind v4 with CSS-variable theme tokens; shadcn/ui primitives (Radix) |
| Server state | TanStack Query v5 |
| Forms | react-hook-form + zod |
| Realtime | Native WebSocket wrapped in a `useReconnectingSocket` hook |
| Editor | `@monaco-editor/react` (lazy loaded, only on the Editor tab) |
| Charts | recharts (lazy loaded, only on the Metrics tab) |
| Log viewer | Custom virtualized component + `anser` for ANSI colors (no xterm) |
| Toasts | sonner |
| Theme | next-themes (class strategy, default dark, system option) |
| Icons | lucide-react |
| Dates | date-fns |
| Unit tests | Vitest + Testing Library + MSW |
| E2E | Playwright |
| Package manager | npm |

No global state library. URL state for tabs and wizard step; TanStack Query for server data;
`useState`/`useReducer` for local UI.

## 3. Backend contract

All paths are under the API origin. REST goes through a Next.js rewrite
(`/api/:path*` -> `${API_ORIGIN}/api/:path*`) so cookies are same-origin. WebSockets connect
directly to `NEXT_PUBLIC_WS_URL` (default `ws://localhost:8080`), and the backend must allow the
frontend origin for WebSocket upgrades. In production everything is on one domain behind the proxy.

### Exists today
| Purpose | Endpoint |
|---|---|
| Session | `GET /api/me`, `POST /api/auth/logout`, login = browser navigation to `/api/auth/github` |
| List / get | `GET /api/sandboxes/`, `GET /api/sandboxes/{id}` |
| Analyze repo | `POST /api/repos/analyze` `{repo_url, branch?}` |
| Create | `POST /api/sandboxes/` `{repo_url, branch?, lifetime_seconds, idle_timeout_seconds, database, env_vars, start_command, services}` |
| Env edit | `PUT /api/sandboxes/{id}/env` `{env_vars, start_command}` -> `{sandbox, redeploy_required}` |
| Extend | `POST /api/sandboxes/{id}/actions/extend` `{additional_seconds}` -> `{sandbox, extended_by, at_cap}` |
| Destroy / recreate / fork | `POST .../actions/destroy`, `POST .../actions/recreate`, `POST .../fork` |
| Redeploy | `POST .../deploy` -> `{redeploying:true}` |
| History | `GET .../deployments`, `GET .../warnings`, `GET .../stats/history?range=1h|6h|24h|7d` |
| Files | `GET .../files`, `GET .../file?path=`, `PUT .../file`, `POST .../commit` |
| Live | WS `.../logs/ws` (frames `log`, `unavailable`, `closed`), WS `.../stats/ws` (frames `stats`, `unavailable`, `closed`) |

### To be added by backend "Prompt A" (frontend must degrade gracefully if 404)
| Purpose | Endpoint | Fallback when missing |
|---|---|---|
| Quota | `GET /api/quota` -> `{active, limit, global_running, global_cap}` | Count non-terminal sandboxes from the list; assume limit 3 |
| Build logs | WS `.../build/ws` | Show `log_excerpt` from the latest deployment, refetch every 3s while building |
| Containers | `GET .../containers`, `?container=` on logs WS | Single "App" log source; hide the selector |
| Status events | WS `/api/events/ws` | Poll `GET /api/sandboxes/` every 4s while any sandbox is queued/building, every 30s otherwise |
| Retry | `POST .../actions/retry` | Use `POST .../deploy` |
| Stop / start | `POST .../actions/stop`, `.../actions/start` | Hide the buttons |
| Idle setting | `PUT .../settings` `{idle_timeout_seconds}` | Hide the Settings tab control |
| Error codes | `error_code` field on every error | Map by HTTP status and `error` text |

Detect missing endpoints by 404 (cache the result for the session in a capability store) so the
UI never shows a control that cannot work.

### Data shapes (confirm against `backend` before use)
- `Sandbox`: `id, branch, repo_url, start_command, services{postgres,mysql,redis,reasons[]}|null, env_vars[{key,is_set}], build_config, container_id, created_at, destroyed_at|null, destruction_reason, detected_language, expires_at, activated_at|null, idle_timeout_seconds (SECONDS), lifetime_seconds, parent_sandbox_id|null, status, subdomain, url, last_request_at|null`
- `status` in `queued | building | running | hibernated | failed | expired | deleted`. Not a linear flow.
- `Analysis`: `commit_sha, branch, builder, language, framework, language_version, start_command, start_command_source, port, port_source, services{...}, env_file, env_vars[{key,required,secret,default,comment,managed,managed_by}], warnings[{code,message,fix}], blockers[{code,message,fix}]`
- `Deployment`: `id, sandbox_id, trigger, status building|success|failed, log_excerpt, started_at, finished_at|null`
- `Warning`: `threshold, title, message`. Warnings endpoint returns `remaining_seconds`.
- Errors: `{error, error_code?, ...extras}`. Known extras: `missing[]` (422 missing_env), `problems[]` (422 invalid_env), `limit` (409 quota).

Generate TypeScript types from the old `frontend/src/lib/types.ts` before deleting it, then
verify each field against a real response.

## 4. Design system

### Color tokens (CSS variables on `:root` and `.dark`; Tailwind theme maps to them)
| Token | Dark | Light |
|---|---|---|
| `--bg` | #0B0F17 | #F7F8FA |
| `--surface` | #121826 | #FFFFFF |
| `--surface-2` | #1A2234 | #F0F2F6 |
| `--border` | #263047 | #E1E5EC |
| `--text` | #E6EAF2 | #101828 |
| `--muted` | #8B96AB | #5B677A |
| `--primary` | #6366F1 | #4F46E5 |
| `--primary-hover` | #818CF8 | #4338CA |
| `--primary-fg` | #FFFFFF | #FFFFFF |
| `--danger` | #EF4444 | #DC2626 |
| `--warning` | #F59E0B | #B45309 |
| `--success` | #22C55E | #15803D |
| `--info` | #38BDF8 | #0369A1 |

Verify that text on every surface and button meets 4.5:1 (3:1 for large text). Adjust the light-mode
status colors if they fail.

### Status system (always icon + label + color)
| Status | Icon | Label | Color |
|---|---|---|---|
| queued | Clock | Waiting in line | slate #94A3B8 |
| building | Hammer (pulse) | Building | info |
| running | CheckCircle2 | Live | success |
| hibernated | Moon | Sleeping (wakes on visit) | violet #A78BFA |
| failed | AlertTriangle | Failed | danger |
| expired | TimerOff | Expired | slate #64748B |
| deleted | Trash2 | Destroyed | slate #64748B |
| action needed | Bell | Action needed | warning |

### Type, space, shape
Inter (UI) and JetBrains Mono (logs, URLs, env keys) via `next/font`. Base 15px, scale 12/13/15/17/20/24/30.
4px spacing grid, radius 10px (cards 14px, pills full). 1px borders, almost no shadows. Motion 150ms
ease-out; disabled under `prefers-reduced-motion`.

### Layout
App shell: top bar (logo, quota pill, "New sandbox", user menu). Content max width 1200px,
padding 16px mobile / 24px desktop. Breakpoints 360, 640, 768, 1024, 1280.

## 5. Routes

| Route | Auth | Purpose |
|---|---|---|
| `/` | public (redirect to `/dashboard` if signed in) | Landing, Continue with GitHub |
| `/dashboard` | required | Sandbox grid |
| `/new` | required | Wizard (`?step=link|review|deploy&id=`) |
| `/sandboxes/[id]` | required | Detail, tab in `?tab=overview|logs|editor|metrics|env|deployments|settings` |
| `/settings` | required | Profile, theme, tokens placeholder |
| `not-found`, `error` | n/a | Friendly pages |

Guard: Next middleware redirects to `/` if the session cookie is absent; the client also handles
a 401 from any call by redirecting to `/?reason=expired` with a toast.
Show a visible "Dev mode" chip when `/api/me` returns `dev: true`.

## 6. Folder structure

```
frontend/
  src/
    app/ (routes, layouts, loading.tsx, error.tsx, not-found.tsx)
    components/
      ui/            shadcn primitives
      shell/         AppShell, TopBar, UserMenu, ThemeToggle, QuotaPill
      sandbox/       SandboxCard, SandboxGrid, StatusBadge, CountdownBadge, ServiceChips,
                     CopyField, ActionsMenu, ConfirmDestroyDialog, LifetimeBanner
      wizard/        WizardStepper, LinkStep, ReviewStep, DeployStep, DetectedCard,
                     EnvForm, EnvRow, SecretInput, PasteEnvDialog, LifetimePicker
      detail/        DetailHeader, OverviewTab, LogsTab, EditorTab, MetricsTab,
                     EnvTab, DeploymentsTab, SettingsTab
      common/        EmptyState, ErrorState, Skeletons, LogViewer, DataTable
    lib/
      api/           client.ts, errors.ts, endpoints.ts, types.ts, capabilities.ts
      realtime/      useReconnectingSocket.ts, useSandboxEvents.ts, useLogStream.ts
      format/        time.ts (countdown, relative), bytes.ts
      env/           parseDotenv.ts, validateEnv.ts
      sandbox/       status.ts, stages.ts
      messages.ts    error/status copy
    hooks/           useSandboxes, useSandbox, useAnalyze, useCreateSandbox, useQuota ...
    styles/globals.css
  tests/ (unit) and e2e/
```

## 7. Data layer rules

- One `request()` wrapper: same-origin, `credentials: "include"`, JSON, timeout 30s, throws
  `ApiError{status, code, message, payload}`. 401 triggers a single global sign-out redirect.
- Query keys: `["me"]`, `["sandboxes"]`, `["sandbox",id]`, `["deployments",id]`,
  `["warnings",id]`, `["stats",id,range]`, `["files",id]`, `["file",id,path]`, `["quota"]`,
  `["analysis",url,branch]`.
- Mutations invalidate the right keys; optimistic update only for rename-like trivial changes
  (not for create/destroy).
- Realtime: `useSandboxEvents` (if available) patches the `["sandboxes"]` and `["sandbox",id]`
  caches; otherwise polling intervals from section 3.
- Sockets: exponential backoff 1s..15s, max 5 attempts, then a manual Reconnect button; do not
  reconnect after an `unavailable` or `closed` frame; pause when the tab is hidden and resume on focus.
- Never keep secrets in query cache or logs. Env values live only in form state and are sent once.

## 8. Screens

### 8.1 Landing
Headline, one sentence, "Continue with GitHub", and a 3-step strip (Paste link, Fill settings,
Get a URL). Show a footer line with limits (3 sandboxes, 1 hour minimum). Error banners for
`?reason=expired` and `?error=`.

### 8.2 Dashboard
- Header row: title, quota pill ("2 of 3 sandboxes"), New sandbox button (disabled with tooltip when full).
- Alert strip: lifetime warnings (under 1h amber, under 5m red) and failed sandboxes needing attention.
- Grid of `SandboxCard`: repo name and branch, `StatusBadge`, URL with copy and open, language and
  service chips, `CountdownBadge`, primary action by state (Open for running, Wake for sleeping,
  View logs for failed, Recreate for expired or destroyed), kebab menu (Extend, Fork, Redeploy, Stop,
  Destroy).
- Filters: All / Live / Sleeping / Failed / Ended and search by name. Ended sandboxes collapse
  into a "History" section.
- States: skeleton (3 cards), empty (illustration + "Deploy your first API"), error with Retry.

### 8.3 Wizard `/new` (state machine, URL synced)
States: `link -> analyzing -> review -> submitting -> deploying -> done | failed`.

**Link step.** Large input with paste detection, optional branch, "Check repository". Normalize
input (trim, strip `.git` and trailing slash, accept `github.com/owner/repo` without protocol).
Friendly errors: not found, not a GitHub URL, rate limited (429, show wait hint), unreachable.
Loading copy: "Reading your repository...".

**Review step.**
1. `DetectedCard`: language, framework, version, start command, port, builder. Each value shows its
   source in a tooltip. Services appear as toggle chips with reasons ("pg found in package.json").
2. Blockers: red panel, fix text, Deploy disabled. Warnings: amber panels.
3. If `start_command` is empty: required "Start command" input with a helper example.
4. `EnvForm`: group order = Required, Optional, Provided automatically. Row = key (mono), helper
   from comment, Required badge, input (masked with show/hide when `secret`), prefilled default,
   inline validation (non-empty for required, name regex, value length). "Paste your .env"
   dialog fills matching keys and lists unknown keys and skipped managed keys. Count badge "3 of 5 required settings filled".
5. `LifetimePicker`: presets 1h, 3h, 6h, 12h, 24h, 7d (never below 1h), custom stepper; one-line
   idle sleep explainer.
6. Footer bar (sticky on mobile): Deploy button with live reason when disabled
   ("Fill in 2 required settings"), Back button, quota note.
7. On 422 `missing_env` / `invalid_env`: scroll to and highlight the exact rows. On 409 quota: dialog
   linking to the dashboard. On 503 capacity: friendly message and a Retry in 30s hint.

**Deploy step.** Stage stepper (Queued, Fetching code, Building, Starting services, Health check,
Live), collapsible log (auto-open on failure), success card with large copyable URL, Open,
time left, Extend, "Go to dashboard". Failure card: plain reason, last 50 lines, Retry, Edit settings,
Copy logs. Persist `id` in the URL so a refresh resumes.

### 8.4 Sandbox detail
Header: name/branch, status, URL + copy + open, countdown, buttons Extend, Stop/Start (if supported),
Redeploy, Destroy (type the sandbox name to confirm). `LifetimeBanner` from `/warnings`.
Tabs (URL synced, lazy):
- **Overview**: status explanation sentence, container health list, live CPU/memory gauges, last 5
  deployments, services, lifetime and idle info.
- **Logs**: container selector (if supported), search, pause/resume autoscroll, wrap toggle,
  download, clear; handles `unavailable` with a "Wake sandbox" action.
- **Editor**: file tree (search, folders collapsible) + Monaco; dirty markers; Save & redeploy
  and Commit to GitHub as separate buttons with explanatory tooltips; unsaved-changes guard;
  binary and over-limit files show a message; below 768px show "best on desktop".
- **Metrics**: range tabs 1h/6h/24h/7d; CPU, memory, network charts; live updates from stats WS.
- **Environment**: rows from `env_vars` (value never shown, "Set" badge, "Replace" action), start
  command editor, save -> "Redeploy to apply" banner with a button.
- **Deployments**: table with trigger, status, duration, started, expandable log excerpt, Retry.
- **Settings**: idle timeout control (if supported), danger zone.

### 8.5 Settings
Profile (avatar, username, dev chip), theme (System/Light/Dark), API tokens placeholder card
("Coming soon, CLI access"), Sign out.

## 9. Logic specs

**Countdown.** `remaining = expires_at - now` (server time offset corrected using the `Date` header
from responses). Formats: >24h "2d 3h", >1h "4h 12m", >1m "12m 04s", else "45s". Tone: >30m
neutral, 5-30m amber, <5m red. Ticks every second only for visible cards; a single shared timer.

**Stage derivation.** From `status` and build log text (case-insensitive markers): queued -> Queued;
building: contains "clone/cloning/fetch" -> Fetching code; "nixpacks/docker build/building/npm
install/pip install/go build/mvn" -> Building; "database/postgres/mysql/redis" -> Starting services;
running with no `activated_at` -> Health check; running -> Live; failed -> mark the last active
stage failed. If no marker matches, show generic "Building". Never block on missing markers.

**.env paste parser.** Handles `KEY=VALUE`, `export KEY=VALUE`, quotes (single, double), inline
`# comments` outside quotes, blank lines, `\n` inside double quotes, CRLF. Ignores invalid names.
Returns `{values, unknown, skippedManaged, errors}`.

**Env validation.** Name regex `^[A-Za-z_][A-Za-z0-9_]*$`; max 100 vars; value max 8 KB; required
non-empty after trim; reserved names (PORT, PATH, HOME, LD_PRELOAD, LD_LIBRARY_PATH, DOCKER_*) are
rejected with an explanation.

**Error messages (`lib/messages.ts`).** Map `error_code` first, then status:
400 invalid input -> field-level message; 401 -> "Your session expired. Sign in again."; 404 ->
"We could not find that."; 409 quota -> "You already have 3 sandboxes. Destroy one or wait for one to end.";
422 -> highlight fields; 429 -> "Too many requests. Try again in a minute."; 503 capacity ->
"We are at capacity right now. Try again in a few minutes."; network -> "Cannot reach the server.";
unknown -> "Something went wrong. Try again." plus a "Copy details" button holding status, code and
request path (never secrets).

## 10. Copywriting glossary
Use: Deploy, Redeploy, Extend, Destroy, Sleeping, Live, Settings (for env vars), Sandbox.
Avoid: hibernated, container, env var (in the UI use "setting"), teardown, reconcile.
Buttons are verbs. Errors never blame the user. Sentences are short.

## 11. Accessibility, responsiveness, performance
- All inputs labelled, errors linked with `aria-describedby`, status changes in an `aria-live="polite"` region.
- Focus rings visible, focus trapped in dialogs, Esc closes, full keyboard path through the wizard.
- No color-only meaning. Touch targets at least 40px.
- Verified at 360px, 768px and 1280px.
- Lazy load Monaco and recharts. Route-level code splitting. Lighthouse targets on `/dashboard` and `/new`:
  performance 85, accessibility 95.
- No layout shift on status changes (reserve badge width).

## 12. Testing and acceptance
- Unit (Vitest): `parseDotenv`, `validateEnv`, countdown formatter, stage derivation, status mapping,
  error mapping, URL normalizer, capability fallback.
- Component (Testing Library + MSW): EnvForm (required blocking, secret masking, paste import, 422 highlight),
  SandboxCard (state-specific actions), Wizard flow (success, blocker, quota full, failed deploy),
  DestroyDialog (name confirmation).
- E2E (Playwright against the mock server or the real stack): sign in (dev auth) -> wizard with required env
  -> deploy -> detail tabs -> extend -> destroy; plus mobile viewport smoke test.
- Done means: `npm run build`, `npm run lint`, `npm run typecheck`, `npm test` all pass; screenshots
  of dashboard, wizard review step, detail page in dark, light and 390px mobile; a table of endpoints
  wired versus fallback.

## 13. Build phases

| Phase | Scope |
|---|---|
| 0 | Backup, delete old frontend, scaffold, tokens, shell, API client, auth, theme |
| 1 | Dashboard (cards, filters, quota, alerts, events/polling) |
| 2 | Wizard (link, review with env form, deploy) |
| 3 | Detail: header, overview, logs, deployments |
| 4 | Detail: editor, metrics, environment, settings; Settings page |
| 5 | Polish, accessibility, tests, docs, production build and deploy config |
