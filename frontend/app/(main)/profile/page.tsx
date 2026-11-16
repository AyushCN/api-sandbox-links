

"use client";

import { useState } from "react";
import useSWR from "swr";
import toast from "react-hot-toast";
import {
  formatDistanceToNow,
  format,
  subDays,
  eachDayOfInterval,
} from "date-fns";
import {
  User,
  Mail,
  MapPin,
  Link as LinkIcon,
  MessageSquare,
  Code,
  BookOpen,
  Activity,
  Loader2,
  AlertCircle,
  Clock,
  CheckCircle2,
  X,
  Save,
  ArrowRight,
  Settings,
  AlertTriangle,
} from "lucide-react";
import { fetchWithAuth } from "@/lib/auth";
import Link from "next/link";
import { useRouter } from "next/navigation";

const fetcher = (url: string) => fetchWithAuth(url);

interface UserProfile {
  id: string;
  email: string;
  username: string;
  isEmailVerified: boolean;
  maxEnvironments: number;
  maxBuildsPerHour: number;
  createdAt: string;
  envCount: number;
  orgName: string;
  orgRole: string;
  bio: string;
  pronouns: string;
  location: string;
  website: string;
  twitter: string;
  github: string;
  githubUsername?: string;
  avatarUrl?: string;
}

interface Environment {
  id: string;
  name: string;
  status: string;
  githubBranch: string;
  gitUrl: string;
  createdAt: string;
}

interface AuditLog {
  id: string;
  action: string;
  resource: string;
  timestamp: string;
}

interface Invite {
  projectId: string;
  userId: string;
  role: string;
  invitedByUserId: string;
  project: {
    id: string;
    name: string;
    description: string;
  };
}

/* ─── Contribution Graph ─── */
function ContributionGraph({ activities = [] }: { activities: AuditLog[] }) {
  const activityCounts = activities.reduce(
    (acc: Record<string, number>, log) => {
      const d = new Date(log.timestamp).toISOString().split("T")[0];
      acc[d] = (acc[d] || 0) + 1;
      return acc;
    },
    {},
  );

  const end = new Date();
  const start = subDays(end, 365);
  const days = eachDayOfInterval({ start, end });

  const contributionData = days.map((date) => {
    const dString = date.toISOString().split("T")[0];
    const count = activityCounts[dString] || 0;
    let level = 0;
    if (count >= 5) level = 4;
    else if (count >= 3) level = 3;
    else if (count >= 2) level = 2;
    else if (count === 1) level = 1;
    return { date, level, count };
  });

  const weeks: { date: Date; level: number; count: number }[][] = [];
  let currentWeek: { date: Date; level: number; count: number }[] = [];
  contributionData.forEach((day) => {
    if (day.date.getDay() === 0 && currentWeek.length > 0) {
      weeks.push(currentWeek);
      currentWeek = [];
    }
    currentWeek.push(day);
  });
  if (currentWeek.length > 0) weeks.push(currentWeek);

  const getLevelColor = (level: number) => {
    switch (level) {
      case 1:
        return "bg-emerald-900/40 border border-emerald-900/50";
      case 2:
        return "bg-emerald-700/60 border border-emerald-700/50";
      case 3:
        return "bg-emerald-500/80 border border-emerald-500/50";
      case 4:
        return "bg-emerald-400 border border-emerald-400";
      default:
        return "bg-surface-container-high border border-outline-variant/30";
    }
  };

  return (
    <div className="border border-outline-variant rounded-xl p-5 bg-surface-container-lowest">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-sm font-semibold text-on-surface">
          {activities.length} contributions in the last year
        </h2>
      </div>
      <div className="flex gap-1 overflow-x-auto pb-2">
        {weeks.map((week, wIdx) => (
          <div key={wIdx} className="flex flex-col gap-1">
            {week.map((day, dIdx) => (
              <div
                key={dIdx}
                title={`${day.count} contributions on ${format(day.date, "MMM d, yyyy")}`}
                className={`w-3 h-3 rounded-[2px] ${getLevelColor(day.level)}`}
              />
            ))}
          </div>
        ))}
      </div>
      <div className="flex items-center justify-end mt-4 text-xs text-on-surface-variant">
        <div className="flex items-center gap-2">
          <span>Less</span>
          <div className="flex gap-1">
            {[0, 1, 2, 3, 4].map((l) => (
              <div
                key={l}
                className={`w-3 h-3 rounded-[2px] ${getLevelColor(l)}`}
              />
            ))}
          </div>
          <span>More</span>
        </div>
      </div>
    </div>
  );
}

/* ─── Edit Profile Modal ─── */
function EditProfileModal({
  user,
  onClose,
  onSaved,
}: {
  user: UserProfile;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [username, setUsername] = useState(user.username || "");
  const [bio, setBio] = useState(user.bio || "");
  const [pronouns, setPronouns] = useState(user.pronouns || "");
  const [location, setLocation] = useState(user.location || "");
  const [website, setWebsite] = useState(user.website || "");
  const [saving, setSaving] = useState(false);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      const res = await fetchWithAuth("/api/user/me", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, bio, pronouns, location, website }),
      });
      toast.success("Profile updated!");
      onSaved();
      onClose();
    } catch (err: unknown) {
      toast.error((err as Error).message || "Failed to update profile");
    } finally {
      setSaving(false);
    }
  };

  const inputCls =
    "w-full bg-surface-container border border-outline-variant rounded-lg px-4 py-2.5 text-sm text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-primary-fixed/60 transition-colors";

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="bg-surface-container-lowest border border-outline-variant rounded-2xl shadow-2xl w-full max-w-lg mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-outline-variant">
          <h2 className="text-lg font-bold text-on-surface">Edit profile</h2>
          <button
            onClick={onClose}
            className="text-on-surface-variant hover:text-on-surface transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <form
          onSubmit={handleSave}
          className="p-6 space-y-4 max-h-[70vh] overflow-y-auto"
        >
          <div>
            <label className="block text-xs font-bold text-on-surface-variant mb-1.5 tracking-wide uppercase">
              Username
            </label>
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="username"
              className={inputCls}
            />
          </div>
          <div>
            <label className="block text-xs font-bold text-on-surface-variant mb-1.5 tracking-wide uppercase">
              Bio
            </label>
            <textarea
              value={bio}
              onChange={(e) => setBio(e.target.value)}
              rows={3}
              placeholder="Tell the world about yourself"
              className={inputCls + " resize-none"}
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-bold text-on-surface-variant mb-1.5 tracking-wide uppercase">
                Pronouns
              </label>
              <input
                value={pronouns}
                onChange={(e) => setPronouns(e.target.value)}
                placeholder="they/them"
                className={inputCls}
              />
            </div>
            <div>
              <label className="block text-xs font-bold text-on-surface-variant mb-1.5 tracking-wide uppercase">
                Location
              </label>
              <input
                value={location}
                onChange={(e) => setLocation(e.target.value)}
                placeholder="San Francisco, CA"
                className={inputCls}
              />
            </div>
          </div>
          <div>
            <label className="block text-xs font-bold text-on-surface-variant mb-1.5 tracking-wide uppercase">
              Website
            </label>
            <input
              value={website}
              onChange={(e) => setWebsite(e.target.value)}
              placeholder="https://yoursite.dev"
              className={inputCls}
            />
          </div>

          <div className="flex justify-end gap-3 pt-4 border-t border-outline-variant">
            <button
              type="button"
              onClick={onClose}
              className="px-5 py-2 rounded-xl text-sm font-semibold text-on-surface-variant hover:bg-surface-container-high transition-colors border border-outline-variant"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={saving}
              className="flex items-center gap-2 bg-primary-container text-on-primary-fixed-variant px-6 py-2 rounded-xl font-bold text-sm hover:shadow-[0_0_20px_rgba(0,240,255,0.2)] active:scale-95 transition-all disabled:opacity-50"
            >
              {saving ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Save className="w-4 h-4" />
              )}
              Save
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

/* ─── Delete Account Modal ─── */
function DeleteAccountModal({
  user,
  onClose,
}: {
  user: UserProfile;
  onClose: () => void;
}) {
  const router = useRouter();
  const [confirmText, setConfirmText] = useState("");
  const [deleting, setDeleting] = useState(false);
  const expectedText = `delete account ${user.username}`;

  const handleDelete = async (e: React.FormEvent) => {
    e.preventDefault();
    if (confirmText !== expectedText) return;
    setDeleting(true);
    try {
      await fetchWithAuth("/api/user/me", { method: "DELETE" });
      toast.success("Account deleted successfully.");
      localStorage.removeItem("token");
      window.location.href = "/login";
    } catch (err: unknown) {
      toast.error((err as Error).message || "Failed to delete account");
      setDeleting(false);
    }
  };

  const inputCls =
    "w-full bg-surface-container border border-outline-variant rounded-lg px-4 py-2.5 text-sm text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-error/60 transition-colors";

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="bg-surface-container-lowest border border-error/30 rounded-2xl shadow-2xl w-full max-w-md mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-outline-variant">
          <h2 className="text-lg font-bold text-error flex items-center gap-2">
            <AlertTriangle className="w-5 h-5" />
            Delete Account
          </h2>
          <button
            onClick={onClose}
            className="text-on-surface-variant hover:text-on-surface transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <form onSubmit={handleDelete} className="p-6 space-y-4">
          <p className="text-sm text-on-surface-variant leading-relaxed">
            This action is{" "}
            <span className="font-bold text-on-surface">permanent</span> and
            cannot be undone. All your environments, projects, and data will be
            permanently wiped.
          </p>
          <div className="bg-error/10 border border-error/20 p-3 rounded-lg text-xs text-error font-mono">
            Please type{" "}
            <span className="font-bold select-all bg-error/20 px-1 py-0.5 rounded">
              {expectedText}
            </span>{" "}
            to confirm.
          </div>
          <div>
            <input
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              placeholder={expectedText}
              className={inputCls}
            />
          </div>

          <div className="flex justify-end gap-3 pt-4 border-t border-outline-variant">
            <button
              type="button"
              onClick={onClose}
              className="px-5 py-2 rounded-xl text-sm font-semibold text-on-surface-variant hover:bg-surface-container-high transition-colors border border-outline-variant"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={deleting || confirmText !== expectedText}
              className="flex items-center gap-2 bg-error text-white px-6 py-2 rounded-xl font-bold text-sm hover:bg-error/90 active:scale-95 transition-all disabled:opacity-50"
            >
              {deleting ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
              Delete Account
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

/* ─── Status Badge ─── */
function StatusBadge({ status }: { status: string }) {
  const map: Record<string, string> = {
    RUNNING: "text-emerald-400 bg-emerald-400/10 border-emerald-400/20",
    BUILDING: "text-amber-400 bg-amber-400/10 border-amber-400/20",
    FAILED: "text-red-400 bg-red-400/10 border-red-400/20",
    IDLE: "text-on-surface-variant bg-surface-container-high border-outline-variant",
    STOPPED:
      "text-on-surface-variant bg-surface-container-high border-outline-variant",
  };
  return (
    <span
      className={`inline-flex items-center gap-1 text-[10px] font-bold tracking-wider uppercase border px-2 py-0.5 rounded-full ${map[status] || map.IDLE}`}
    >
      {status === "RUNNING" && (
        <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
      )}
      {status}
    </span>
  );
}

/* ─── Main Page ─── */
export default function ProfilePage() {
  const {
    data: user,
    error,
    isLoading,
    mutate: mutateUser,
  } = useSWR<UserProfile>("/api/me", fetcher);
  const { data: environments } = useSWR<Environment[]>(
    "/api/environments",
    fetcher,
  );
  const { data: activities } = useSWR<AuditLog[]>(
    "/api/user/activity",
    fetcher,
  );
  const { data: invites, mutate: mutateInvites } = useSWR<Invite[]>(
    "/api/user/invites",
    fetcher,
  );

  const [activeTab, setActiveTab] = useState("overview");
  const [showEditModal, setShowEditModal] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-32">
        <Loader2 className="w-8 h-8 animate-spin text-primary-fixed" />
      </div>
    );
  }

  if (error || !user) {
    return (
      <div className="flex flex-col items-center justify-center py-32 gap-3">
        <AlertCircle className="w-10 h-10 text-error" />
        <p className="text-error font-semibold">Failed to load profile</p>
      </div>
    );
  }

  const initials = (user.username || user.email || "devuser").slice(0, 2).toUpperCase();
  const rawEnvs = Array.isArray(environments)
    ? environments
    : (environments as any)?.sandboxes || [];

  const allEnvs: Environment[] = rawEnvs.map((env: any) => {
    const sub = env.subdomain || "";
    let pUrl = env.publicUrl || env.url || (sub ? `http://${sub}.sandbox.localhost:8090` : "");
    if (pUrl && pUrl.includes(".sandbox.localhost") && !pUrl.includes(":8090")) {
      pUrl = pUrl.replace(".sandbox.localhost", ".sandbox.localhost:8090");
    }
    return {
      id: env.id,
      name: env.name || env.subdomain || `sandbox-${env.id.slice(0, 8)}`,
      gitUrl: pUrl || env.gitUrl || env.repo_url || "",
      githubBranch: env.githubBranch || env.branch || "master",
      status: (env.status || "IDLE").toUpperCase(),
      createdAt: env.createdAt || env.created_at || new Date().toISOString(),
    };
  });

  const pinnedEnvs = allEnvs.slice(0, 4);
  const recentActivities = Array.isArray(activities) ? activities.slice(0, 5) : [];

  return (
    <div className="max-w-7xl mx-auto py-8">
      {/* Modals */}
      {showEditModal && (
        <EditProfileModal
          user={user}
          onClose={() => setShowEditModal(false)}
          onSaved={() => mutateUser()}
        />
      )}
      {showDeleteModal && (
        <DeleteAccountModal
          user={user}
          onClose={() => setShowDeleteModal(false)}
        />
      )}

      {/* Clean Tab Navigation */}
      <div className="border-b border-outline-variant mb-8 mt-4 sticky top-0 bg-surface-container-lowest/80 backdrop-blur-md z-10">
        <nav className="flex gap-6">
          {[
            { id: "overview", label: "Overview", icon: Activity },
            {
              id: "environments",
              label: "Environments",
              icon: BookOpen,
              count: user.envCount,
            },
            { id: "settings", label: "Settings", icon: Settings },
          ].map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={`flex items-center gap-2 py-3 border-b-2 transition-colors ${
                activeTab === tab.id
                  ? "border-primary-fixed text-on-surface font-semibold"
                  : "border-transparent text-on-surface-variant hover:text-on-surface hover:border-outline-variant"
              }`}
            >
              <tab.icon className="w-4 h-4 opacity-70" />
              <span className="text-sm">{tab.label}</span>
              {tab.count !== undefined && (
                <span className="bg-surface-container-high text-on-surface-variant text-xs py-0.5 px-2 rounded-full font-medium">
                  {tab.count}
                </span>
              )}
            </button>
          ))}
        </nav>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-4 gap-8">
        {/* ───── Left Sidebar ───── */}
        <div className="lg:col-span-1 relative z-20">
          <div className="relative mb-6">
            <div className="w-[280px] h-[280px] rounded-full bg-gradient-to-br from-primary-fixed/30 to-primary-container/20 border-8 border-surface-container-lowest flex items-center justify-center shadow-lg overflow-hidden">
              {user.avatarUrl ? (
                /* eslint-disable-next-line @next/next/no-img-element */
                <img
                  src={user.avatarUrl}
                  alt="Avatar"
                  className="w-full h-full object-cover"
                />
              ) : (
                <span className="text-7xl font-black text-primary-fixed tracking-tight">
                  {initials}
                </span>
              )}
            </div>
          </div>

          <div className="space-y-4 px-2">
            <div>
              <h1 className="text-2xl font-bold text-on-surface leading-tight">
                {user.username || user.email.split("@")[0]}
              </h1>
              <h2 className="text-xl text-on-surface-variant font-light">
                {user.email}
              </h2>
            </div>

            <button
              onClick={() => setShowEditModal(true)}
              className="w-full py-1.5 bg-surface-container-low hover:bg-surface-container-high border border-outline-variant rounded-md text-sm font-semibold transition-colors"
            >
              Edit profile
            </button>

            <div className="pt-2">
              <p className="text-on-surface text-sm">
                {user.bio || "No bio yet — click Edit profile to add one."}
              </p>
            </div>

            <ul className="space-y-2 pt-4 text-sm text-on-surface">
              {user.pronouns && (
                <li className="flex items-center gap-2">
                  <User className="w-4 h-4 text-on-surface-variant" />
                  {user.pronouns}
                </li>
              )}
              {user.location && (
                <li className="flex items-center gap-2">
                  <MapPin className="w-4 h-4 text-on-surface-variant" />
                  {user.location}
                </li>
              )}
              <li className="flex items-center gap-2">
                <Mail className="w-4 h-4 text-on-surface-variant" />
                <a
                  href={`mailto:${user.email}`}
                  className="hover:text-primary-fixed"
                >
                  {user.email}
                </a>
              </li>
              {user.website && (
                <li className="flex items-center gap-2">
                  <LinkIcon className="w-4 h-4 text-on-surface-variant" />
                  <a
                    href={user.website}
                    target="_blank"
                    className="hover:text-primary-fixed hover:underline"
                  >
                    {user.website.replace(/^https?:\/\//, "")}
                  </a>
                </li>
              )}
              {(user.githubUsername || user.github) && (
                <li className="flex items-center gap-2">
                  <Code className="w-4 h-4 text-on-surface-variant" />
                  <a
                    href={`https://github.com/${user.githubUsername || user.github}`}
                    target="_blank"
                    className="hover:text-primary-fixed hover:underline"
                  >
                    {user.githubUsername || user.github}
                  </a>
                </li>
              )}
            </ul>

            <div className="pt-6 border-t border-outline-variant">
              <h3 className="text-sm font-semibold text-on-surface mb-3 flex items-center justify-between">
                Pending Invites
                {Array.isArray(invites) && invites.length > 0 && (
                  <span className="bg-primary-fixed text-on-primary-fixed text-[10px] px-2 rounded-full font-bold">
                    {invites.length}
                  </span>
                )}
              </h3>
              {!Array.isArray(invites) || invites.length === 0 ? (
                <span className="text-xs text-on-surface-variant">
                  No pending invites
                </span>
              ) : (
                <div className="space-y-3">
                  {invites.map((invite) => (
                    <div
                      key={invite.projectId}
                      className="bg-surface-container border border-outline-variant rounded-lg p-3 hover:border-primary-fixed/30 transition-colors"
                    >
                      <div className="flex items-center gap-2 min-w-0 mb-3">
                        <span className="text-sm font-bold text-on-surface truncate">
                          {invite.project?.name || "Unknown Project"}
                        </span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="text-[10px] font-mono font-bold text-on-surface-variant bg-surface-container-high px-1.5 py-0.5 rounded uppercase tracking-widest">
                          {invite.role}
                        </span>
                        <div className="flex items-center gap-1.5">
                          <button
                            onClick={async () => {
                              try {
                                await fetchWithAuth(
                                  `/api/projects/${invite.projectId}/invites/accept`,
                                  { method: "POST" },
                                );
                                toast.success("Invite accepted!");
                                mutateInvites();
                                mutateUser();
                              } catch (err: unknown) {
                                toast.error(
                                  (err as Error).message || "Failed to accept",
                                );
                              }
                            }}
                            className="w-7 h-7 flex items-center justify-center bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 rounded transition-colors"
                            title="Accept"
                          >
                            <CheckCircle2 className="w-4 h-4" />
                          </button>
                          <button
                            onClick={async () => {
                              try {
                                await fetchWithAuth(
                                  `/api/projects/${invite.projectId}/invites/decline`,
                                  { method: "POST" },
                                );
                                toast.success("Invite declined");
                                mutateInvites();
                              } catch (err: unknown) {
                                toast.error(
                                  (err as Error).message || "Failed to decline",
                                );
                              }
                            }}
                            className="w-7 h-7 flex items-center justify-center bg-error/10 hover:bg-error/20 text-error rounded transition-colors"
                            title="Decline"
                          >
                            <X className="w-4 h-4" />
                          </button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>

        {/* ───── Main Content ───── */}
        <div className="lg:col-span-3 space-y-6">
          {/* ── Overview Tab ── */}
          {activeTab === "overview" && (
            <>
              {/* Pinned Environments */}
              <div>
                <div className="flex items-center justify-between mb-4">
                  <h2 className="text-sm font-semibold text-on-surface">
                    Recent Environments
                  </h2>
                </div>

                {pinnedEnvs.length === 0 ? (
                  <div className="border border-outline-variant rounded-xl p-8 bg-surface-container-lowest text-center text-on-surface-variant">
                    No environments created yet.{" "}
                    <Link
                      href="/upload"
                      className="text-primary-fixed hover:underline"
                    >
                      Create one →
                    </Link>
                  </div>
                ) : (
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {pinnedEnvs.map((env) => (
                      <Link
                        key={env.id}
                        href={`/environments/${env.id}`}
                        className="border border-outline-variant rounded-xl p-4 bg-surface-container-lowest hover:border-primary-fixed/40 transition-all group flex flex-col justify-between"
                      >
                        <div>
                          <div className="flex items-center justify-between mb-2">
                            <div className="flex items-center gap-2 min-w-0">
                              <BookOpen className="w-4 h-4 text-on-surface-variant shrink-0" />
                              <span className="font-semibold text-primary-fixed group-hover:underline text-sm truncate">
                                {env.name}
                              </span>
                            </div>
                            <StatusBadge status={env.status} />
                          </div>
                          <p className="text-xs text-on-surface-variant mb-4 truncate">
                            {env.gitUrl}
                          </p>
                        </div>
                        <div className="flex items-center justify-between text-xs text-on-surface-variant">
                          <div className="flex items-center gap-4">
                            <div className="flex items-center gap-1.5">
                              <span className="w-2.5 h-2.5 rounded-full bg-blue-400" />
                              {env.githubBranch}
                            </div>
                            <div className="flex items-center gap-1">
                              <Clock className="w-3.5 h-3.5" />
                              {formatDistanceToNow(new Date(env.createdAt), {
                                addSuffix: true,
                              })}
                            </div>
                          </div>
                          <ArrowRight className="w-4 h-4 opacity-0 group-hover:opacity-100 transition-opacity text-primary-fixed" />
                        </div>
                      </Link>
                    ))}
                  </div>
                )}
              </div>

              {/* Contribution Graph */}
              <ContributionGraph activities={activities || []} />

              {/* Activity Feed */}
              <div className="mt-8">
                <div className="flex items-center justify-between border-b border-outline-variant pb-2 mb-4">
                  <h2 className="text-sm font-semibold text-on-surface">
                    Recent activity
                  </h2>
                </div>
                <div className="relative border-l border-outline-variant ml-3 space-y-6 pb-6 mt-6">
                  {recentActivities.length === 0 ? (
                    <div className="pl-6 text-sm text-on-surface-variant">
                      No recent activity.
                    </div>
                  ) : (
                    recentActivities.map((log) => (
                      <div key={log.id} className="relative pl-6">
                        <div className="absolute -left-[9px] top-1 w-4 h-4 bg-primary-container rounded-full border-2 border-surface-container-lowest" />
                        <div className="text-xs text-on-surface-variant mb-2">
                          {formatDistanceToNow(new Date(log.timestamp), {
                            addSuffix: true,
                          })}
                        </div>
                        <div className="bg-surface-container-lowest border border-outline-variant rounded-lg p-3">
                          <div className="flex items-center gap-2 text-sm text-on-surface font-semibold mb-1">
                            <Activity className="w-4 h-4 text-primary-fixed" />
                            {log.action.replace(/_/g, " ")}
                          </div>
                          {log.resource && (
                            <div className="text-xs text-on-surface-variant ml-6 font-mono truncate">
                              {log.resource}
                            </div>
                          )}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </>
          )}

          {/* ── Environments Tab ── */}
          {activeTab === "environments" && (
            <div>
              <div className="flex items-center justify-between mb-4">
                <p className="text-sm text-on-surface-variant">
                  {allEnvs.length} environments
                </p>
                <Link
                  href="/upload"
                  className="flex items-center gap-2 bg-primary-container text-on-primary-fixed-variant px-4 py-1.5 rounded-lg font-bold text-sm hover:shadow-[0_0_20px_rgba(0,240,255,0.15)] active:scale-95 transition-all"
                >
                  New
                </Link>
              </div>
              {allEnvs.length === 0 ? (
                <div className="border border-outline-variant rounded-xl p-12 bg-surface-container-lowest text-center">
                  <BookOpen className="w-12 h-12 mx-auto mb-4 text-on-surface-variant/50" />
                  <h3 className="text-lg font-semibold text-on-surface mb-2">
                    No environments yet
                  </h3>
                  <p className="text-on-surface-variant mb-4">
                    Get started by creating your first sandbox.
                  </p>
                  <Link
                    href="/upload"
                    className="text-primary-fixed hover:underline font-semibold"
                  >
                    Create environment →
                  </Link>
                </div>
              ) : (
                <div className="space-y-3">
                  {allEnvs.map((env) => (
                    <Link
                      key={env.id}
                      href={`/environments/${env.id}`}
                      className="block border border-outline-variant rounded-xl p-4 bg-surface-container-lowest hover:border-primary-fixed/40 transition-all group"
                    >
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-3 min-w-0">
                          <BookOpen className="w-5 h-5 text-on-surface-variant shrink-0" />
                          <div className="min-w-0">
                            <span className="font-semibold text-primary-fixed group-hover:underline text-sm">
                              {env.name}
                            </span>
                            <p className="text-xs text-on-surface-variant truncate mt-0.5">
                              {env.gitUrl}
                            </p>
                          </div>
                        </div>
                        <div className="flex items-center gap-3 shrink-0">
                          <div className="hidden sm:flex items-center gap-1.5 text-xs text-on-surface-variant">
                            <span className="w-2.5 h-2.5 rounded-full bg-blue-400" />
                            {env.githubBranch}
                          </div>
                          <div className="hidden sm:flex items-center gap-1 text-xs text-on-surface-variant">
                            <Clock className="w-3.5 h-3.5" />
                            {formatDistanceToNow(new Date(env.createdAt), {
                              addSuffix: true,
                            })}
                          </div>
                          <StatusBadge status={env.status} />
                          <ArrowRight className="w-4 h-4 opacity-0 group-hover:opacity-100 transition-opacity text-primary-fixed" />
                        </div>
                      </div>
                    </Link>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* ── Settings Tab ── */}
          {activeTab === "settings" && (
            <div className="space-y-6">
              <div>
                <h3 className="text-lg font-bold text-on-surface mb-1">
                  Account Settings
                </h3>
                <p className="text-sm text-on-surface-variant mb-6">
                  Manage your account preferences and danger zone settings.
                </p>
              </div>

              <div className="border border-error/30 rounded-xl p-6 bg-error/5">
                <h4 className="text-base font-bold text-error mb-2">
                  Danger Zone
                </h4>
                <p className="text-sm text-on-surface-variant mb-4">
                  Permanently delete your account and all associated data. This
                  action is irreversible.
                </p>
                <button
                  onClick={() => setShowDeleteModal(true)}
                  className="bg-error text-white px-4 py-2 rounded-lg text-sm font-semibold hover:bg-error/90 transition-colors"
                >
                  Delete Account
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
