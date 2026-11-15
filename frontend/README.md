# Frontend: Next.js + React IDE

The frontend provides the graphical user interface for the API Sandbox platform. It behaves as a complete web-based Integrated Development Environment (IDE) built on top of React, Next.js, and Monaco Editor.

## Core Architecture

- **Framework**: Next.js (App Router)
- **Language**: TypeScript (Strict)
- **Styling**: Tailwind CSS
- **Code Editor**: Microsoft Monaco Editor (`@monaco-editor/react`)
- **State Management**: React Hooks + WebSocket live syncing
- **Terminal Integration**: Xterm.js

## Components

The UI is divided into several main panels:

1. **Dashboard (`/app/(main)/page.tsx`)**: Lists user-owned sandboxes, project memberships, and active invitations.
2. **Environment IDE (`/app/(main)/environments/[id]/page.tsx`)**: The primary workspace. It features a File Explorer, the Monaco code editor, an embedded Xterm.js terminal, and real-time Git sync controls.
3. **Collaboration Plane**: Live presence indicators show who else is currently viewing or editing the sandbox, synced instantly via WebSockets from the Go backend.

## Environment Variables

Ensure the frontend has access to the backend API. During local development, this is handled via standard ports.

```env
NEXT_PUBLIC_API_URL=http://localhost:8080
NEXT_PUBLIC_WS_URL=ws://localhost:8080
```

*Note in Production:* When orchestrated via `docker-compose.yml`, Traefik handles routing, so both API and WS URLs typically share the standard `http://domain.com` path rather than needing explicit port 8080 targeting.

## Running Locally (Outside Docker)

If you wish to iterate on the UI rapidly without rebuilding containers:

```bash
cd frontend
npm install
npm run dev
```

The UI will be accessible at `http://localhost:3000`. Ensure the backend and database are running (e.g., via `docker compose up -d postgres redis backend`).

## Build Integrity & Typing

The codebase enforces strict TypeScript typing. There are no loose `any` declarations. Before committing changes to the frontend, you must ensure it builds correctly:

```bash
npm run build
```
Any type checking errors here will fail CI pipelines. Ensure object payloads from WebSocket events and API responses are accurately typed against their corresponding definitions in `types/index.ts`.
