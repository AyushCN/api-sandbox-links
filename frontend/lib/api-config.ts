// Single source of truth for the backend API link.
//
// Change it in frontend/.env.local (copy frontend/.env.example) — never edit
// the defaults below by hand.
//
//   NEXT_PUBLIC_API_BASE  Public API origin the browser talks to.
//   NEXT_PUBLIC_WS_URL    WebSocket origin; defaults to NEXT_PUBLIC_API_BASE.
//   BACKEND_URL           Optional server-only override used by the Next.js
//                         rewrite proxy and route handlers (e.g. an internal
//                         service URL that differs from the public one).

function read(value: string | undefined): string {
  return (value ?? "").trim();
}

const DEFAULT_ORIGIN = "http://localhost:8080";

// Server-side origin for route handlers and the rewrite proxy.
export const BACKEND_ORIGIN =
  read(process.env.BACKEND_URL) ||
  read(process.env.NEXT_PUBLIC_API_BASE) ||
  DEFAULT_ORIGIN;

// Browser-facing API origin. Empty means "same origin" (go through the Next.js
// proxy), which is the safe default when unset.
export const PUBLIC_API_BASE = read(process.env.NEXT_PUBLIC_API_BASE);

// Browser-facing WebSocket origin, derived from PUBLIC_API_BASE when unset.
// Returns "" when neither is configured so callers can fall back to the
// current page origin.
export function publicWsBase(): string {
  const explicit = read(process.env.NEXT_PUBLIC_WS_URL);
  if (explicit) return explicit;
  const base = PUBLIC_API_BASE;
  if (!base) return "";
  return base.replace(/^http/, "ws");
}
