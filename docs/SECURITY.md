# Security Architecture & Threat Model

This document outlines the security controls, boundary guarantees, isolation mechanisms, and threat mitigation models implemented across **API Sandbox Links**.

---

## 1. Trust Boundaries & Design Intent

API Sandbox Links is engineered to safely run untrusted and semi-trusted third-party application code:
- Disposable pull-request and preview environments.
- Take-home engineering interview evaluations.
- Bug reproduction and security testing sandboxes.
- Microservice development across teams.

---

## 2. Active Security Controls

### 2.1 Container Sandboxing & Hardening
Every sandbox container is deployed with strict containment policies defined in [`backend/internal/orchestrator/lifecycle.go`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/orchestrator/lifecycle.go):

| Control | Implementation | Security Purpose |
|---|---|---|
| **Capability Dropping** | `CapDrop: ["ALL"]` | Strips all Linux capabilities by default (`SYS_ADMIN`, `NET_RAW`, `CHOWN`, etc.). Only `NET_BIND_SERVICE` is optionally retained to permit binding port 80. |
| **No Privilege Escalation** | `SecurityOpt: ["no-new-privileges:true"]` | Prevents setuid binaries or filesystem capabilities from elevating process privileges. |
| **Process Supervision** | `Init: true` (tini) | Reaps zombie processes and ensures POSIX signals (`SIGTERM`, `SIGKILL`) propagate cleanly. |
| **Resource Quotas** | Memory (512MB), Swap (disabled), NanoCPUs (75% core), PIDs Limit (256) | Neutralizes fork-bombs, memory exhaustion attacks, and CPU starvation. |
| **Network Isolation** | Dedicated bridge `sandbox_<id[:8]>` | Sandboxes cannot inspect packets from or talk to other sandboxes, host services, or the Docker socket. |
| **Read-Only Rootfs (Opt-in)**| `SANDBOX_HARDENED=true` | Read-only root filesystem with ephemeral 64MB `tmpfs` mounted at `/tmp` (`noexec,nosuid`). |
| **Google gVisor (Opt-in)** | `SANDBOX_RUNTIME="runsc"` | Replaces standard `runc` with gVisor's user-space kernel (`runsc`), intercepting all syscalls and preventing container escape. |

### 2.2 Pluggable Builder Isolation
- **Environment Scrubbing**: Child processes for Railpack and BuildKit run under a strict deny-by-default environment. Host environment variables, backend database credentials, and Docker tokens are completely stripped.
- **Rootless BuildKit**: BuildKit builds run in an isolated rootless container (`:1234`), ensuring build scripts (e.g. `npm postinstall`, `make`) cannot escalate privileges on the host.
- **Build Concurrency Limiter**: A global semaphore (`BUILD_CONCURRENCY=2`) prevents compile storms from degrading the responsiveness of running sandboxes.

### 2.3 Secrets & Token Encryption at Rest
- **AES-256-GCM**: GitHub OAuth tokens and sandbox environment variables are encrypted at rest using AES-256-GCM with keys derived from `SECRETS_ENCRYPTION_KEY`.
- **Ephemerality**: Decrypted tokens only exist in memory during explicit Git operations (e.g. pushing a commit from the in-browser IDE).
- **Stateless Session Cookies**: The `asl_session` cookie contains AES-256-GCM ciphertext of the user ID, doubling as proof of authenticity without requiring server session state.

### 2.4 Workspace Multi-Tenancy & RBAC
- **Strict Project Scoping**: Sandboxes are isolated by project boundaries via `filterSandboxesForProject()`. Custom workspaces only display environments explicitly associated with that project.
- **Collaborator Role Matrix**:
  - `OWNER`: Full administrative authority (manage members, rename project, delete project).
  - `EDITOR`: Deploy sandboxes, edit code in Monaco IDE, save-redeploy, and push Git branches.
  - `VIEWER`: Read-only access to preview URLs, application stdout logs, and metrics.

### 2.5 Ingress Gateway & Edge Protection
- **Single-Flight Wake Coalescing**: Bursts of incoming HTTP requests for a hibernated container are collapsed into a single `docker start` operation, preventing boot storms and race conditions.
- **Rate Limiting**: Token-bucket rate limiting on Gateway `:8090` mitigates Layer 7 request floods.
- **Payload Boundaries**: Request bodies are capped to 32MB by `MaxBytesReader`.
- **TLS Wildcards**: Traefik automatically provisions Let's Encrypt certificates using ACME DNS-01 verification with HSTS and strict origin isolation headers.

---

## 3. Residual Risks & Recommended Mitigations

1. **Shared Linux Kernel (Default `runc` runtime)**:
   - *Risk*: Standard Docker containers share the host Linux kernel. A zero-day kernel exploit could potentially facilitate a container breakout.
   - *Mitigation*: For public or untrusted multi-tenant environments, enable Google gVisor (`SANDBOX_RUNTIME="runsc"`).
2. **Build-Phase Script Execution**:
   - *Risk*: Malicious packages can execute arbitrary code during `npm install` or `pip install`.
   - *Mitigation*: The rootless BuildKit daemon confines builds to an unprivileged OCI namespace.
3. **Outbound Egress**:
   - *Risk*: Sandboxes have default outbound Internet access to fetch APIs and remote assets.
   - *Mitigation*: Apply host-level firewall rules or egress security groups if strict isolation is mandated.
