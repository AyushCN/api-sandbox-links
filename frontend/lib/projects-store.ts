import fs from "fs";
import path from "path";

const PROJECTS_FILE = "/tmp/api-sandbox-links/projects.json";

export interface ProjectRepository {
  id: string;
  name: string;
  gitUrl: string;
  defaultBranch: string;
}

export interface StoredProject {
  id: string;
  name: string;
  description: string;
  createdAt: string;
  collaborators: any[];
  sandboxIds: string[];
  repositories?: ProjectRepository[];
}

const DEFAULT_PROJECT: StoredProject = {
  id: "default-project",
  name: "Default Project",
  description: "Default workspace for sandboxes",
  createdAt: "2026-01-01T00:00:00Z",
  collaborators: [
    {
      id: "collab-owner",
      userId: "user-ayushcn",
      role: "OWNER",
      status: "ACTIVE",
      acceptedAt: "2026-01-01T00:00:00Z",
      user: {
        username: "AyushCN",
        email: "ayush@api-sandbox.local",
      },
    },
  ],
  sandboxIds: [],
  repositories: [],
};

export function getProjects(): StoredProject[] {
  try {
    if (!fs.existsSync(PROJECTS_FILE)) {
      fs.mkdirSync(path.dirname(PROJECTS_FILE), { recursive: true });
      fs.writeFileSync(PROJECTS_FILE, JSON.stringify([DEFAULT_PROJECT], null, 2));
      return [DEFAULT_PROJECT];
    }
    const raw = fs.readFileSync(PROJECTS_FILE, "utf-8");
    const list = JSON.parse(raw);
    if (!Array.isArray(list) || list.length === 0) {
      return [DEFAULT_PROJECT];
    }
    // Ensure default-project exists in the list
    if (!list.some((p: StoredProject) => p.id === "default-project")) {
      list.unshift(DEFAULT_PROJECT);
    }

    // Auto-migrate any legacy "devuser" references to logged-in user "AyushCN"
    let mutated = false;
    for (const p of list) {
      if (Array.isArray(p.collaborators)) {
        for (const c of p.collaborators) {
          if (c.user?.username === "devuser" || c.user?.email === "dev@api-sandbox.local") {
            c.user.username = "AyushCN";
            c.user.email = "ayush@api-sandbox.local";
            mutated = true;
          }
        }
      }
      if (!Array.isArray(p.sandboxIds)) {
        p.sandboxIds = [];
        mutated = true;
      }
      if (!Array.isArray(p.repositories)) {
        p.repositories = [];
        mutated = true;
      }
    }
    if (mutated) {
      saveProjects(list);
    }

    return list;
  } catch {
    return [DEFAULT_PROJECT];
  }
}

export function saveProjects(projects: StoredProject[]): void {
  try {
    fs.mkdirSync(path.dirname(PROJECTS_FILE), { recursive: true });
    fs.writeFileSync(PROJECTS_FILE, JSON.stringify(projects, null, 2));
  } catch (err) {
    console.error("Failed to save projects:", err);
  }
}

export function getProjectById(id: string): StoredProject | undefined {
  const list = getProjects();
  let proj = list.find((p) => p.id === id);
  if (!proj && id === "default-project") return DEFAULT_PROJECT;
  if (!proj) {
    // If not found in list, synthesize a clean fallback project
    const name = id.replace(/^proj-/, "").replace(/-/g, " ");
    return {
      id,
      name: name.charAt(0).toUpperCase() + name.slice(1),
      description: "Active engineering workspace",
      createdAt: new Date().toISOString(),
      collaborators: DEFAULT_PROJECT.collaborators,
      sandboxIds: [],
      repositories: [],
    };
  }
  return proj;
}

export function addProject(
  name: string,
  description: string,
  creator?: { userId?: string; username?: string; email?: string }
): StoredProject {
  const list = getProjects();
  const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  const id = `proj-${slug || Date.now()}`;

  const username = creator?.username || "AyushCN";
  const email = creator?.email || `${username.toLowerCase()}@api-sandbox.local`;
  const userId = creator?.userId || `user-${username.toLowerCase()}`;

  const ownerCollab = {
    id: `collab-owner-${Date.now()}`,
    userId,
    role: "OWNER",
    status: "ACTIVE",
    acceptedAt: new Date().toISOString(),
    user: {
      username,
      email,
    },
  };

  const newProj: StoredProject = {
    id,
    name: name.trim(),
    description: description.trim(),
    createdAt: new Date().toISOString(),
    collaborators: [ownerCollab],
    sandboxIds: [],
    repositories: [],
  };
  list.push(newProj);
  saveProjects(list);
  return newProj;
}

export function associateSandboxWithProject(projectId: string, sandboxId: string): void {
  const list = getProjects();
  const proj = list.find((p) => p.id === projectId);
  if (proj) {
    if (!proj.sandboxIds) proj.sandboxIds = [];
    if (!proj.sandboxIds.includes(sandboxId)) {
      proj.sandboxIds.push(sandboxId);
      saveProjects(list);
    }
  }
}

export function inviteCollaborator(projectId: string, identifier: string, role = "EDITOR"): any {
  const list = getProjects();
  let proj = list.find((p) => p.id === projectId);
  if (!proj) {
    proj = getProjectById(projectId);
    if (proj) list.push(proj);
  }
  if (!proj) return null;
  if (!proj.collaborators) proj.collaborators = [];

  const isEmail = identifier.includes("@");
  const username = isEmail ? identifier.split("@")[0] : identifier;
  const email = isEmail ? identifier : `${identifier}@api-sandbox.local`;
  const collab = {
    id: `collab-${Date.now()}`,
    userId: `user-${Date.now()}`,
    role: role.toUpperCase(),
    status: "ACTIVE",
    acceptedAt: new Date().toISOString(),
    user: {
      username,
      email,
    },
  };
  proj.collaborators.push(collab);
  saveProjects(list);
  return collab;
}

export function removeCollaborator(projectId: string, userId: string): boolean {
  const list = getProjects();
  const proj = list.find((p) => p.id === projectId);
  if (!proj || !proj.collaborators) return false;
  proj.collaborators = proj.collaborators.filter((c: any) => c.userId !== userId && c.id !== userId);
  saveProjects(list);
  return true;
}

export function addProjectRepository(
  projectId: string,
  repo: { name: string; gitUrl: string; defaultBranch?: string }
): ProjectRepository | null {
  const list = getProjects();
  let proj = list.find((p) => p.id === projectId);
  if (!proj) {
    proj = getProjectById(projectId);
    if (proj) list.push(proj);
  }
  if (!proj) return null;
  if (!proj.repositories) proj.repositories = [];
  const newRepo: ProjectRepository = {
    id: `repo-${Date.now()}`,
    name: repo.name.trim(),
    gitUrl: repo.gitUrl.trim(),
    defaultBranch: repo.defaultBranch?.trim() || "main",
  };
  proj.repositories.push(newRepo);
  saveProjects(list);
  return newRepo;
}

export function filterSandboxesForProject(projectId: string, allSandboxes: any[]): any[] {
  if (!Array.isArray(allSandboxes)) return [];
  const list = getProjects();

  if (!projectId || projectId === "default-project") {
    // Collect all sandboxIds assigned to other custom projects
    const otherProjectSandboxIds = new Set<string>();
    for (const p of list) {
      if (p.id !== "default-project" && Array.isArray(p.sandboxIds)) {
        for (const sid of p.sandboxIds) {
          otherProjectSandboxIds.add(sid);
        }
      }
    }
    const defaultProj = list.find((p) => p.id === "default-project");
    const explicitDefaultIds = new Set(defaultProj?.sandboxIds || []);
    return allSandboxes.filter((sb) => explicitDefaultIds.has(sb.id) || !otherProjectSandboxIds.has(sb.id));
  }

  const proj = list.find((p) => p.id === projectId);
  if (!proj || !Array.isArray(proj.sandboxIds) || proj.sandboxIds.length === 0) {
    return [];
  }
  const set = new Set(proj.sandboxIds);
  return allSandboxes.filter((sb) => set.has(sb.id));
}
