"use client";
import React, { use, useState } from "react";
import useSWR from "swr";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  Folder,
  Users,
  Loader2,
  GitBranch,
  Box,
  XCircle,
  Code,
  ArrowRight,
  GitMerge,
  Play,
  Database,
  AlertCircle,
  CheckCircle2,
  CircleDot,
  Plus,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { fetchWithAuth } from "@/lib/auth";
import { motion } from "framer-motion";
import { InviteCollaboratorModal } from "@/components/editor/InviteCollaboratorModal";

const fetcher = (url: string) => fetchWithAuth(url);

type Tab = "overview" | "repositories" | "members" | "workspaces" | "environments" | "change-requests";

// ── Domain types ───────────────────────────────────────────────────────────────

interface Project {
  id: string;
  name: string;
  description?: string;
  createdAt?: string;
}

interface ProjectMember {
  id: string;
  userId: string;
  role: string;
  status: string;
  user?: { username?: string; email?: string };
}

interface ProjectRepository {
  id: string;
  name?: string;
  gitUrl: string;
  defaultBranch?: string;
}

interface WorkspaceRepository {
  id: string;
  branch: string;
  currentCommit?: string;
  workingDirectory?: string;
}

interface Workspace {
  id: string;
  type: string;
  environmentId?: string;
  environment?: { status: string };
  repositories?: WorkspaceRepository[];
}

interface Environment {
  id: string;
  name: string;
  status: string;
  createdAt?: string;
}

interface ChangeRequest {
  id: string;
  title: string;
  description?: string;
  status: string;
  createdAt?: string;
}

// ── Status helpers ─────────────────────────────────────────────────────────────

const envStatusConfig: Record<string, { color: string; dot: string; label: string }> = {
  IDLE:     { color: "text-gray-400 bg-gray-400/10 border-gray-400/20",        dot: "bg-gray-400",                                   label: "Idle"     },
  BUILDING: { color: "text-blue-400 bg-blue-400/10 border-blue-400/20",        dot: "bg-blue-400 animate-bounce",                    label: "Building" },
  RUNNING:  { color: "text-emerald-400 bg-emerald-400/10 border-emerald-400/20", dot: "bg-emerald-400 animate-pulse shadow-[0_0_6px_#34d399]", label: "Running" },
  STOPPED:  { color: "text-orange-400 bg-orange-400/10 border-orange-400/20",  dot: "bg-orange-400",                                 label: "Stopped"  },
  FAILED:   { color: "text-red-400 bg-red-400/10 border-red-400/20",           dot: "bg-red-400",                                    label: "Failed"   },
};

function StatusBadge({ status }: { status: string }) {
  const cfg = envStatusConfig[status] ?? envStatusConfig.IDLE;
  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-bold tracking-widest uppercase border ${cfg.color}`}>
      <span className={`w-1.5 h-1.5 rounded-full ${cfg.dot}`} />
      {cfg.label}
    </span>
  );
}

const roleConfig: Record<string, { color: string; label: string }> = {
  OWNER:  { color: "text-amber-400 bg-amber-400/10 border-amber-400/20",   label: "Owner"  },
  EDITOR: { color: "text-blue-400 bg-blue-400/10 border-blue-400/20",      label: "Editor" },
  VIEWER: { color: "text-gray-400 bg-gray-400/10 border-gray-400/20",      label: "Viewer" },
};

function RoleBadge({ role }: { role: string }) {
  const cfg = roleConfig[role] ?? roleConfig.VIEWER;
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-bold tracking-widest uppercase border ${cfg.color}`}>
      {cfg.label}
    </span>
  );
}

const crStatusConfig: Record<string, { color: string; icon: React.ElementType; label: string }> = {
  OPEN:       { color: "text-blue-400 bg-blue-400/10 border-blue-400/20",       icon: CircleDot,     label: "Open"       },
  APPROVED:   { color: "text-emerald-400 bg-emerald-400/10 border-emerald-400/20", icon: CheckCircle2, label: "Approved"   },
  MERGED:     { color: "text-purple-400 bg-purple-400/10 border-purple-400/20", icon: GitMerge,      label: "Merged"     },
  CONFLICTED: { color: "text-red-400 bg-red-400/10 border-red-400/20",          icon: AlertCircle,   label: "Conflicted" },
  REJECTED:   { color: "text-gray-400 bg-gray-400/10 border-gray-400/20",       icon: XCircle,       label: "Rejected"   },
};

function CRStatusBadge({ status }: { status: string }) {
  const cfg = crStatusConfig[status] ?? crStatusConfig.OPEN;
  const Icon = cfg.icon;
  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-bold tracking-widest uppercase border ${cfg.color}`}>
      <Icon className="w-3 h-3" />
      {cfg.label}
    </span>
  );
}

// ── Main component ─────────────────────────────────────────────────────────────

export default function ProjectDetailsPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();
  const [activeTab, setActiveTab] = useState<Tab>("overview");
  const [isOpening, setIsOpening] = useState(false);

  const handleOpenWorkspace = async () => {
    setIsOpening(true);
    try {
      const res = await fetch(`/api/projects/${id}/edit`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${localStorage.getItem("token")}`
        }
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok && data.environmentId) {
        router.push(`/environments/${data.environmentId}`);
      } else {
        router.push(`/upload?projectId=${id}`);
      }
    } catch {
      router.push(`/upload?projectId=${id}`);
    } finally {
      setIsOpening(false);
    }
  };

  const { data: projectData, error: projectError, isLoading: projectLoading } = useSWR<Project>(`/api/projects/${id}`, fetcher);
  const fallbackProject: Project = {
    id,
    name: id === "default-project" ? "Default Project" : id.replace(/^proj-/, "").replace(/-/g, " ").replace(/\b\w/g, (l) => l.toUpperCase()),
    description: "Active engineering workspace",
    createdAt: new Date().toISOString(),
  };
  const project = projectData || fallbackProject;

  const { data: members, mutate: mutateMembers } = useSWR<ProjectMember[]>(`/api/projects/${id}/members`, fetcher, { refreshInterval: 10000 });
  const { data: repositories, mutate: mutateRepositories } = useSWR<ProjectRepository[]>(`/api/projects/${id}/repositories`, fetcher);
  const { data: workspaces }   = useSWR<Workspace[]>(`/api/projects/${id}/workspaces`, fetcher, { refreshInterval: 5000 });
  const { data: changeRequests } = useSWR<ChangeRequest[]>(`/api/projects/${id}/change-requests`, fetcher, { refreshInterval: 5000 });
  const { data: envsRaw } = useSWR<any>(`/api/environments?projectId=${id}`, fetcher, { refreshInterval: 3000 });
  const environments: Environment[] = (Array.isArray(envsRaw)
    ? envsRaw
    : envsRaw?.sandboxes || []).map((sb: any) => ({
      id: sb.id,
      name: sb.name || sb.subdomain || `sandbox-${sb.id.slice(0, 8)}`,
      status: (sb.status || "IDLE").toUpperCase(),
      createdAt: sb.createdAt || sb.created_at || new Date().toISOString(),
      gitUrl: sb.gitUrl || sb.url || "",
      githubBranch: sb.githubBranch || sb.branch || "master",
    }));

  if (projectLoading && !projectData) {
    return (
      <div className="flex items-center justify-center h-64 text-on-surface-variant">
        <Loader2 className="w-8 h-8 animate-spin" />
      </div>
    );
  }

  const openCRs = Array.isArray(changeRequests) ? changeRequests.filter((cr: ChangeRequest) => cr.status === "OPEN").length : 0;

  const tabs: { id: Tab; label: string; icon: React.ElementType; count?: number }[] = [
    { id: "overview",        label: "Overview",        icon: Folder    },
    { id: "repositories",    label: "Repositories",    icon: Database,  count: Array.isArray(repositories) ? repositories.length : undefined },
    { id: "members",         label: "Members",         icon: Users,     count: Array.isArray(members) ? members.length : undefined },
    { id: "workspaces",      label: "Workspaces",      icon: Box,       count: Array.isArray(workspaces) ? workspaces.length : undefined },
    { id: "environments",    label: "Environments",    icon: Play,      count: Array.isArray(environments) ? environments.length : undefined },
    { id: "change-requests", label: "Change Requests", icon: GitMerge,  count: openCRs || undefined },
  ];

  return (
    <div className="flex flex-col gap-6 pb-12">
      {/* Header */}
      <div className="flex items-center gap-4">
        <Link href="/projects" className="w-10 h-10 rounded-full border border-outline-variant flex items-center justify-center hover:bg-surface-container-high transition-colors">
          <ArrowLeft className="w-5 h-5 text-on-surface-variant" />
        </Link>
        <div className="flex items-center gap-3 flex-1 min-w-0">
          <div className="w-12 h-12 rounded-xl bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center shrink-0">
            <Folder className="w-6 h-6 text-primary-fixed" />
          </div>
          <div className="min-w-0">
            <h1 className="text-2xl font-bold tracking-tight text-on-surface truncate">{project.name}</h1>
            {project.description && <p className="text-sm text-on-surface-variant truncate">{project.description}</p>}
          </div>
        </div>
        <div className="flex items-center gap-3 shrink-0">
          <Link
            href={`/upload?projectId=${id}`}
            className="flex items-center gap-1.5 px-4 py-2.5 rounded-xl text-xs font-bold uppercase tracking-wider bg-surface-container-high text-on-surface hover:bg-surface-container-highest border border-outline-variant transition-all shadow-sm"
          >
            <Plus className="w-4 h-4 text-[#FF416C]" />
            <span>New Sandbox</span>
          </Link>
          <button 
            onClick={handleOpenWorkspace}
            disabled={isOpening}
            className="flex items-center gap-2 bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] text-white px-5 py-2.5 rounded-xl font-bold text-sm hover:opacity-95 active:scale-95 transition-all disabled:opacity-50 shadow-md"
          >
            {isOpening ? <Loader2 className="w-4 h-4 animate-spin" /> : <Code className="w-4 h-4" />}
            Open Workspace
          </button>
        </div>
      </div>

      {/* Tab Navigation */}
      <div className="flex items-center gap-1 border-b border-outline-variant overflow-x-auto">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;
          return (
            <button
              key={tab.id}
              id={`tab-${tab.id}`}
              onClick={() => setActiveTab(tab.id)}
              className={`flex items-center gap-2 px-4 py-3 text-sm font-semibold whitespace-nowrap border-b-2 transition-all duration-200 ${
                isActive
                  ? "border-primary-fixed text-primary-fixed"
                  : "border-transparent text-on-surface-variant hover:text-on-surface hover:border-outline-variant"
              }`}
            >
              <Icon className="w-4 h-4" />
              {tab.label}
              {tab.count != null && (
                <span className={`ml-1 px-1.5 py-0.5 rounded-full text-[10px] font-bold ${isActive ? "bg-primary-fixed/20 text-primary-fixed" : "bg-surface-container text-on-surface-variant"}`}>
                  {tab.count}
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Tab Content */}
      <motion.div key={activeTab} initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.2 }}>
        {activeTab === "overview"        && <OverviewTab project={project} members={members} repositories={repositories} workspaces={workspaces} openCRs={openCRs} />}
        {activeTab === "repositories"    && <RepositoriesTab repositories={repositories} projectId={id} mutateRepositories={mutateRepositories} />}
        {activeTab === "members"         && <MembersTab members={members} projectId={id} mutateMembers={mutateMembers} />}
        {activeTab === "workspaces"      && <WorkspacesTab workspaces={workspaces} projectId={id} />}
        {activeTab === "environments"    && <EnvironmentsTab environments={environments} projectId={id} />}
        {activeTab === "change-requests" && <ChangeRequestsTab changeRequests={changeRequests} />}
      </motion.div>
    </div>
  );
}

// ── Overview Tab ───────────────────────────────────────────────────────────────

function OverviewTab({ project, members, repositories, workspaces, openCRs }: {
  project: Project;
  members?: ProjectMember[];
  repositories?: ProjectRepository[];
  workspaces?: Workspace[];
  openCRs: number;
}) {
  const stats = [
    { label: "Repositories",    value: Array.isArray(repositories) ? repositories.length : "–", icon: Database },
    { label: "Members",         value: Array.isArray(members) ? members.length : "–",            icon: Users    },
    { label: "Workspaces",      value: Array.isArray(workspaces) ? workspaces.length : "–",      icon: Box      },
    { label: "Open Change Reqs",value: openCRs,                                                  icon: GitMerge },
  ];

  return (
    <div className="space-y-6">
      {/* Stats */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        {stats.map((s, i) => {
          const Icon = s.icon;
          return (
            <motion.div key={s.label} initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.05 }}
              className="bg-surface-container-lowest border border-outline-variant rounded-xl p-5 flex items-center gap-4"
            >
              <div className="w-10 h-10 rounded-lg bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center shrink-0">
                <Icon className="w-5 h-5 text-primary-fixed" />
              </div>
              <div>
                <p className="text-2xl font-bold text-on-surface">{s.value}</p>
                <p className="text-xs text-on-surface-variant font-medium">{s.label}</p>
              </div>
            </motion.div>
          );
        })}
      </div>

      {/* Project Info */}
      <div className="bg-surface-container-lowest border border-outline-variant rounded-xl p-6">
        <h3 className="text-sm font-bold text-on-surface-variant uppercase tracking-wider mb-4">Project Details</h3>
        <dl className="space-y-3">
          <div className="flex items-center justify-between">
            <dt className="text-sm text-on-surface-variant">Project ID</dt>
            <dd className="text-sm font-mono text-on-surface">{project.id}</dd>
          </div>
          <div className="flex items-center justify-between">
            <dt className="text-sm text-on-surface-variant">Created</dt>
            <dd className="text-sm text-on-surface">{project.createdAt ? formatDistanceToNow(new Date(project.createdAt), { addSuffix: true }) : "–"}</dd>
          </div>
          {project.description && (
            <div className="flex items-start justify-between gap-4">
              <dt className="text-sm text-on-surface-variant shrink-0">Description</dt>
              <dd className="text-sm text-on-surface text-right">{project.description}</dd>
            </div>
          )}
        </dl>
      </div>
    </div>
  );
}

// ── Repositories Tab ───────────────────────────────────────────────────────────

function RepositoriesTab({ repositories, projectId, mutateRepositories }: { repositories?: ProjectRepository[], projectId: string, mutateRepositories: () => void }) {
  const [isAddOpen, setIsAddOpen] = useState(false);
  const [name, setName] = useState("");
  const [gitUrl, setGitUrl] = useState("");
  const [defaultBranch, setDefaultBranch] = useState("main");
  const [isAdding, setIsAdding] = useState(false);

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !gitUrl) return;
    setIsAdding(true);
    try {
      const res = await fetch(`/api/projects/${projectId}/repositories`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${localStorage.getItem("token")}` },
        body: JSON.stringify({ name, gitUrl, defaultBranch })
      });
      if (res.ok) {
        setIsAddOpen(false);
        setName("");
        setGitUrl("");
        setDefaultBranch("main");
        mutateRepositories();
      } else {
        const errorData = await res.json();
        alert(errorData.error || "Failed to add repository");
      }
    } finally {
      setIsAdding(false);
    }
  };

  if (!Array.isArray(repositories)) return <LoadingPlaceholder />;

  return (
    <div className="space-y-3">
      <div className="flex justify-between items-center pb-2">
        <h2 className="text-sm font-bold tracking-wide text-on-surface-variant uppercase">Project Repositories</h2>
        <button
          onClick={() => setIsAddOpen(true)}
          className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-primary-fixed/10 text-primary-fixed hover:bg-primary-fixed/20 text-sm font-semibold transition-colors border border-primary-fixed/20"
        >
          <Plus className="w-4 h-4" /> Add Repository
        </button>
      </div>

      {isAddOpen && (
        <form onSubmit={handleAdd} className="bg-surface-container-lowest border border-outline-variant rounded-xl p-5 mb-4">
          <div className="space-y-4">
            <div>
              <label className="text-xs font-bold text-on-surface-variant uppercase mb-1 block">Name</label>
              <input type="text" required value={name} onChange={e => setName(e.target.value)} className="w-full bg-surface-container border border-outline-variant rounded-lg px-3 py-2 text-sm text-on-surface focus:outline-none focus:border-primary-fixed" placeholder="frontend-app" />
            </div>
            <div>
              <label className="text-xs font-bold text-on-surface-variant uppercase mb-1 block">Git URL</label>
              <input type="text" required value={gitUrl} onChange={e => setGitUrl(e.target.value)} className="w-full bg-surface-container border border-outline-variant rounded-lg px-3 py-2 text-sm text-on-surface focus:outline-none focus:border-primary-fixed" placeholder="https://github.com/user/repo" />
            </div>
            <div>
              <label className="text-xs font-bold text-on-surface-variant uppercase mb-1 block">Default Branch</label>
              <input type="text" value={defaultBranch} onChange={e => setDefaultBranch(e.target.value)} className="w-full bg-surface-container border border-outline-variant rounded-lg px-3 py-2 text-sm text-on-surface focus:outline-none focus:border-primary-fixed" placeholder="main" />
            </div>
            <div className="flex justify-end gap-3 pt-2">
              <button type="button" onClick={() => setIsAddOpen(false)} className="px-4 py-2 text-sm font-semibold text-on-surface-variant hover:text-on-surface">Cancel</button>
              <button type="submit" disabled={isAdding} className="px-4 py-2 text-sm font-semibold bg-primary-fixed text-on-primary-fixed rounded-lg hover:bg-primary-fixed/90 disabled:opacity-50 flex items-center gap-2">
                {isAdding && <Loader2 className="w-4 h-4 animate-spin" />}
                Add Repository
              </button>
            </div>
          </div>
        </form>
      )}

      {repositories.length === 0 && !isAddOpen ? (
        <EmptyState icon={Database} title="No repositories" description="Add a Git repository to this project to get started." />
      ) : (
        repositories.map((repo: ProjectRepository, idx: number) => (
          <motion.div key={repo.id} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: idx * 0.04 }}
            className="bg-surface-container-lowest border border-outline-variant rounded-xl p-5 flex items-center gap-4"
          >
            <div className="w-10 h-10 rounded-lg bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center shrink-0">
              <GitBranch className="w-5 h-5 text-primary-fixed" />
            </div>
            <div className="flex-1 min-w-0">
              <p className="font-semibold text-on-surface truncate">{repo.name || repo.gitUrl}</p>
              <p className="text-xs font-mono text-on-surface-variant truncate">{repo.gitUrl}</p>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <span className="text-xs text-on-surface-variant font-mono bg-surface-container px-2 py-1 rounded-lg">
                {repo.defaultBranch || "main"}
              </span>
            </div>
          </motion.div>
        ))
      )}
    </div>
  );
}

// ── Members Tab ────────────────────────────────────────────────────────────────

function MembersTab({ members, projectId, mutateMembers }: { members?: ProjectMember[], projectId: string, mutateMembers: () => void }) {
  const [isInviteOpen, setIsInviteOpen] = useState(false);
  const [isInviting, setIsInviting] = useState(false);

  const handleInvite = async (identifier: string, role: string) => {
    setIsInviting(true);
    try {
      const res = await fetch(`/api/projects/${projectId}/invite`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${localStorage.getItem("token")}` },
        body: JSON.stringify({ identifier, role: role === "COLLABORATOR" ? "EDITOR" : role })
      });
      if (res.ok) {
        setIsInviteOpen(false);
        mutateMembers();
      } else {
        const errorData = await res.json();
        alert(errorData.error || "Failed to invite user");
      }
    } finally {
      setIsInviting(false);
    }
  };

  if (!Array.isArray(members)) return <LoadingPlaceholder />;

  return (
    <div className="space-y-3">
      <div className="flex justify-between items-center pb-2">
        <h2 className="text-sm font-bold tracking-wide text-on-surface-variant uppercase">Project Members</h2>
        <button
          onClick={() => setIsInviteOpen(true)}
          className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-primary-fixed/10 text-primary-fixed hover:bg-primary-fixed/20 text-sm font-semibold transition-colors border border-primary-fixed/20"
        >
          <Plus className="w-4 h-4" /> Add Member
        </button>
      </div>

      {members.length === 0 ? (
        <EmptyState icon={Users} title="No members" description="Invite collaborators to this project." />
      ) : (
        members.map((m: ProjectMember, idx: number) => (
          <motion.div key={m.id} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: idx * 0.04 }}
            className="bg-surface-container-lowest border border-outline-variant rounded-xl p-5 flex items-center gap-4"
          >
            <div className="w-10 h-10 rounded-full bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center shrink-0 text-primary-fixed font-bold text-sm">
              {(m.user?.username || m.user?.email || "?")[0].toUpperCase()}
            </div>
            <div className="flex-1 min-w-0">
              <p className="font-semibold text-on-surface">{m.user?.username || m.user?.email || m.userId}</p>
              <p className="text-xs text-on-surface-variant">{m.user?.email}</p>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <RoleBadge role={m.role} />
              {m.status === "PENDING" && (
                <span className="text-[10px] font-bold tracking-wider px-2 py-0.5 rounded-full bg-yellow-400/10 text-yellow-400 border border-yellow-400/20 uppercase">Pending</span>
              )}
            </div>
          </motion.div>
        ))
      )}

      <InviteCollaboratorModal
        isOpen={isInviteOpen}
        onClose={() => setIsInviteOpen(false)}
        onInvite={handleInvite}
        isInviting={isInviting}
      />
    </div>
  );
}

// ── Workspaces Tab ─────────────────────────────────────────────────────────────

function WorkspacesTab({ workspaces, projectId }: { workspaces?: Workspace[]; projectId: string }) {
  if (!Array.isArray(workspaces)) return <LoadingPlaceholder />;
  if (workspaces.length === 0) return <EmptyState icon={Box} title="No workspaces" description="Workspaces are created when editors join the project." />;

  return (
    <div className="space-y-3">
      {workspaces.map((ws: Workspace, idx: number) => (
        <motion.div key={ws.id} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: idx * 0.04 }}
          className="bg-surface-container-lowest border border-outline-variant rounded-xl p-5"
        >
          <div className="flex items-center gap-4">
            <div className="w-10 h-10 rounded-lg bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center shrink-0">
              {ws.type === "CANONICAL" ? <Folder className="w-5 h-5 text-amber-400" /> : <Code className="w-5 h-5 text-primary-fixed" />}
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <p className="font-semibold text-on-surface">{ws.type === "CANONICAL" ? "Canonical Workspace" : `Editor Workspace`}</p>
                <span className={`text-[10px] font-bold tracking-wider px-2 py-0.5 rounded-full uppercase border ${ws.type === "CANONICAL" ? "bg-amber-400/10 text-amber-400 border-amber-400/20" : "bg-blue-400/10 text-blue-400 border-blue-400/20"}`}>
                  {ws.type}
                </span>
              </div>
              <p className="text-xs text-on-surface-variant font-mono">{ws.id}</p>
            </div>
            {ws.environment && (
              <StatusBadge status={ws.environment.status} />
            )}
            {ws.environmentId && (
              <Link href={`/environments/${ws.environmentId}`}
                className="flex items-center gap-1 text-xs font-semibold text-primary-fixed hover:underline shrink-0"
              >
                Open <ArrowRight className="w-3.5 h-3.5" />
              </Link>
            )}
          </div>
          {Array.isArray(ws.repositories) && ws.repositories.length > 0 && (
            <div className="mt-3 pt-3 border-t border-outline-variant/50 flex flex-wrap gap-2">
              {ws.repositories.map((r: WorkspaceRepository) => (
                <span key={r.id} className="text-xs font-mono bg-surface-container px-2 py-1 rounded-lg text-on-surface-variant flex items-center gap-1.5">
                  <GitBranch className="w-3 h-3" />
                  {r.branch}
                  {r.currentCommit && <span className="text-[10px] opacity-60">{r.currentCommit.slice(0, 7)}</span>}
                </span>
              ))}
            </div>
          )}
        </motion.div>
      ))}
    </div>
  );
}

// ── Environments Tab ───────────────────────────────────────────────────────────

function EnvironmentsTab({ environments, projectId }: { environments?: Environment[]; projectId: string }) {
  if (!Array.isArray(environments)) return <LoadingPlaceholder />;
  if (environments.length === 0) {
    return (
      <div className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-8 text-center flex flex-col items-center max-w-lg mx-auto">
        <div className="w-12 h-12 rounded-xl bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center text-primary-fixed mb-4">
          <Play className="w-6 h-6" />
        </div>
        <h3 className="font-bold text-on-surface text-lg mb-1">No Sandboxes in this Project</h3>
        <p className="text-sm text-on-surface-variant mb-6">Deploy a sandbox to start coding and testing within this workspace project.</p>
        <Link
          href={`/upload?projectId=${projectId}`}
          className="flex items-center gap-2 bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] text-white px-5 py-2.5 rounded-xl font-bold text-xs uppercase tracking-wider hover:opacity-95 transition-all shadow-md"
        >
          <Plus className="w-4 h-4" />
          <span>Deploy First Sandbox</span>
        </Link>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      {environments.map((env: Environment, idx: number) => (
        <motion.div key={env.id} initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: idx * 0.04 }}>
          <Link href={`/environments/${env.id}`} className="block group h-full">
            <div className="bg-surface-container-lowest border border-outline-variant rounded-xl p-5 h-full flex flex-col gap-4 transition-all duration-300 hover:border-primary-fixed/40 hover:shadow-[0_0_24px_rgba(0,240,255,0.08)] relative overflow-hidden">
              <div className="absolute inset-0 bg-gradient-to-br from-primary-fixed/5 to-transparent opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none" />
              <div className="flex items-start justify-between gap-3 relative z-10">
                <div className="w-9 h-9 rounded-lg bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center shrink-0">
                  <Box className="w-4 h-4 text-primary-fixed" />
                </div>
                <StatusBadge status={env.status} />
              </div>
              <div className="relative z-10 flex-1 min-w-0">
                <h3 className="font-bold text-base text-on-surface group-hover:text-primary-fixed transition-colors truncate mb-1.5">{env.name}</h3>
                <p className="text-xs text-on-surface-variant font-mono truncate">
                  {env.createdAt ? formatDistanceToNow(new Date(env.createdAt), { addSuffix: true }) : ""}
                </p>
              </div>
              <div className="relative z-10 flex justify-end">
                <ArrowRight className="w-3.5 h-3.5 opacity-0 group-hover:opacity-100 text-primary-fixed transition-all group-hover:translate-x-0.5 duration-200" />
              </div>
            </div>
          </Link>
        </motion.div>
      ))}
    </div>
  );
}

// ── Change Requests Tab ────────────────────────────────────────────────────────

function ChangeRequestsTab({ changeRequests }: { changeRequests?: ChangeRequest[] }) {
  if (!Array.isArray(changeRequests)) return <LoadingPlaceholder />;
  if (changeRequests.length === 0) return <EmptyState icon={GitMerge} title="No change requests" description="Change requests appear here when editors submit their work for review." />;

  return (
    <div className="space-y-3">
      {changeRequests.map((cr: ChangeRequest, idx: number) => (
        <motion.div key={cr.id} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: idx * 0.04 }}
          className="bg-surface-container-lowest border border-outline-variant rounded-xl p-5"
        >
          <div className="flex items-start gap-4">
            <div className="w-10 h-10 rounded-lg bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center shrink-0">
              <GitMerge className="w-5 h-5 text-primary-fixed" />
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 flex-wrap mb-1">
                <p className="font-semibold text-on-surface">{cr.title}</p>
                <CRStatusBadge status={cr.status} />
              </div>
              {cr.description && <p className="text-sm text-on-surface-variant mb-2">{cr.description}</p>}
              <p className="text-xs text-on-surface-variant">
                {cr.createdAt ? formatDistanceToNow(new Date(cr.createdAt), { addSuffix: true }) : ""}
              </p>
            </div>
          </div>
        </motion.div>
      ))}
    </div>
  );
}

// ── Shared helpers ─────────────────────────────────────────────────────────────

function LoadingPlaceholder() {
  return (
    <div className="space-y-3">
      {[1, 2, 3].map((i) => (
        <div key={i} className="bg-surface-container-lowest border border-outline-variant rounded-xl h-20 animate-pulse" />
      ))}
    </div>
  );
}

function EmptyState({ icon: Icon, title, description }: { icon: React.ElementType; title: string; description: string }) {
  return (
    <div className="bg-surface-container-lowest border border-outline-variant border-dashed rounded-xl py-16 text-center flex flex-col items-center gap-3">
      <div className="w-12 h-12 rounded-2xl bg-primary-fixed/5 border border-primary-fixed/10 flex items-center justify-center">
        <Icon className="w-6 h-6 text-on-surface-variant/30" />
      </div>
      <h3 className="text-lg font-bold text-on-surface">{title}</h3>
      <p className="text-sm text-on-surface-variant max-w-xs">{description}</p>
    </div>
  );
}
