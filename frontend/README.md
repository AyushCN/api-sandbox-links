# Frontend: Next.js Cloud IDE & Platform Console

[![Next.js](https://img.shields.io/badge/Next.js-16.2-black?style=flat&logo=next.js)](https://nextjs.org)
[![React](https://img.shields.io/badge/React-19.0-61DAFB?style=flat&logo=react)](https://react.dev)
[![Tailwind CSS](https://img.shields.io/badge/Tailwind-v4.0-38B2AC?style=flat&logo=tailwind-css)](https://tailwindcss.com)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.0-3178C6?style=flat&logo=typescript)](https://www.typescriptlang.org)

The frontend provides the graphical user interface and developer console for the **API Sandbox** platform. It combines a production-grade cloud sandbox management dashboard with an in-browser Web IDE powered by Microsoft Monaco Editor and real-time WebSocket streams.

---

## Key Features

* 💻 **In-Browser Cloud IDE**: Full-screen workspace with Monaco Editor, dynamic file tree explorer, live file save, and automated save-redeploy pipeline.
* 🖥️ **Theme-Synchronized Terminal Consoles**: Real-time BuildKit log streams and live application stdout/stderr outputs styled in unified **Ash Grey** (`#EAECEF` light / `#1C1F24` dark) with custom scrollbars and step indicators.
* 👥 **Workspace Projects Management**: Create and manage microservice workspaces, invite engineering team members with role-based access (`OWNER`, `EDITOR`, `VIEWER`), and isolate project environments with zero cross-tenant leakage.
* 🌿 **Zero-Drift Git UI**: Visual branch management, current commit tracking, one-click commit & push to GitHub branches, and safe change preview boundaries.
* 🌓 **Refined Dual-Theme System**: Seamless switching between dark obsidian mode and warm ash-cream light mode using an accessible, subtle toggle.
* ⚡ **Live WebSocket Telemetry**: Stream real-time container metrics (CPU %, memory usage, network I/O) and container lifecycle states without manual polling.

---

## Application Route Structure

```
frontend/app/
├── (main)/                                 # Authenticated console layout
│   ├── layout.tsx                          # Persistent top navigation & footer
│   ├── dashboard/page.tsx                  # Active sandboxes overview & quick actions
│   ├── projects/page.tsx                   # Workspace projects listing & creation modal
│   ├── projects/[id]/page.tsx              # Project details: Repos, Members, Workspaces, Sandboxes
│   ├── environments/[id]/page.tsx          # Full cloud IDE: Editor, Logs, Terminal & Git UI
│   ├── upload/page.tsx                     # Deploy new sandbox wizard (Repo, Branch, Database)
│   └── profile/page.tsx                    # User settings, activity feed & credentials
├── api/                                    # Next.js BFF (Backend-for-Frontend) route handlers
│   ├── environments/                       # Sandbox proxy & project-scoped filtering
│   ├── projects/                           # Project creation, members, repos, workspaces
│   └── me/                                 # Rewrites to backend /api/me
├── login/page.tsx                          # Developer login & GitHub OAuth sign-in
├── privacy/page.tsx                        # Privacy policy, token encryption & ephemerality
├── terms/page.tsx                          # Terms of service & container isolation agreements
├── globals.css                             # Unified Tailwind v4 design tokens & CSS variables
└── page.tsx                                # High-conversion marketing landing page
```

---

## Local Development Setup

### 1. Installation
Install project dependencies using Node.js 20+ (Node 24 recommended):

```bash
cd frontend
npm install
```

### 2. Environment Configuration
Create a `.env.local` file in the `frontend/` directory (or use defaults):

```env
# Backend API service URL
BACKEND_URL=http://localhost:8080

# Public URLs for client browser requests
NEXT_PUBLIC_API_URL=http://localhost:8080
NEXT_PUBLIC_WS_URL=ws://localhost:8080
```

### 3. Launch Development Server
```bash
npm run dev
```
Open **[http://localhost:3000](http://localhost:3000)** in your browser. Requests to `/api/*` are automatically proxied to the Go backend (`:8080`) via `next.config.ts` rewrites.

---

## Design System & Theme Tokens

The application utilizes a custom theme defined in [`app/globals.css`](app/globals.css) that dynamically syncs with the user's display preference:

| Token | Light Mode | Dark Mode | Usage |
|---|---|---|---|
| `--background` | `#FAF9F5` (Ash cream) | `#0C0C0E` (Obsidian) | Root document canvas |
| `--surface` | `#FFFFFF` | `#131313` | Panels, header, modals |
| `--surface-container` | `#EFEFEA` | `#1E1E24` | Input fields, active tabs |
| `--primary-fixed` | `#E11D48` (Rose-600) | `#FF4D6D` (Coral-rose) | Active accents, brand badges |
| `--on-surface` | `#1B1B1B` | `#F3F3F6` | High-contrast body text |
| `--on-surface-variant` | `#6B7280` | `#9CA3AF` | Subtitles, labels, timestamps |
| Terminal Console | `#EAECEF` | `#1C1F24` | Build logs & app stdout |

---

## Build Integrity & Quality Checks

Ensure there are zero TypeScript errors or broken imports before committing:

```bash
# Typecheck
npx tsc --noEmit

# Production Build
npm run build
```
