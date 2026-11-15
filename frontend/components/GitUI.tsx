"use client";

import { useState, useEffect, useRef } from "react";
import useSWR from "swr";
import { GitBranch, Loader2, Save, X, Plus, Check, DownloadCloud, UploadCloud, RefreshCw, GitCommit, Clock, User } from "lucide-react";
import toast from "react-hot-toast";
import { fetchWithAuth } from "@/lib/auth";

export function CommitModal({
  isOpen,
  onClose,
  onCommit,
  onCommitAndPush,
  isCommitting,
  isPushing,
  hasUncommittedChanges
}: {
  isOpen: boolean;
  onClose: () => void;
  onCommit: (msg: string) => Promise<void>;
  onCommitAndPush: (msg: string) => Promise<void>;
  isCommitting: boolean;
  isPushing: boolean;
  hasUncommittedChanges: boolean;
}) {
  const [message, setMessage] = useState("");

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="bg-surface-container-lowest border border-outline-variant rounded-2xl shadow-2xl w-full max-w-md overflow-hidden flex flex-col">
        <div className="flex items-center justify-between p-4 border-b border-outline-variant bg-surface-container/30">
          <h2 className="text-lg font-bold text-on-surface flex items-center gap-2">
            <Save className="w-5 h-5 text-amber-500" />
            Commit Changes
          </h2>
          <button onClick={onClose} className="text-on-surface-variant hover:text-on-surface transition-colors cursor-pointer">
            <X className="w-5 h-5" />
          </button>
        </div>
        
        <div className="p-4 flex flex-col gap-4">
          {!hasUncommittedChanges && (
            <div className="p-3 bg-amber-500/10 border border-amber-500/30 rounded-lg text-amber-600 dark:text-amber-400 text-sm">
              Warning: You have no uncommitted changes detected.
            </div>
          )}
          <div>
            <label className="block text-sm font-medium text-on-surface-variant mb-1">Commit Message</label>
            <textarea
              autoFocus
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              placeholder="What did you change?"
              className="w-full h-24 bg-surface-container border border-outline-variant rounded-lg p-3 text-sm text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-primary-fixed focus:ring-1 focus:ring-primary-fixed transition-all resize-none"
            />
          </div>
        </div>

        <div className="p-4 bg-surface-container/20 border-t border-outline-variant flex items-center justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-lg text-sm font-medium text-on-surface-variant hover:text-on-surface hover:bg-surface-container transition-colors cursor-pointer"
          >
            Cancel
          </button>
          <button
            onClick={() => onCommit(message)}
            disabled={!message.trim() || isCommitting || isPushing}
            className="px-4 py-2 rounded-lg text-sm font-bold bg-surface-container-high text-on-surface hover:bg-surface-container-highest border border-outline-variant transition-colors disabled:opacity-50 flex items-center gap-2 cursor-pointer"
          >
            {isCommitting ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
            Commit Only
          </button>
          <button
            onClick={() => onCommitAndPush(message)}
            disabled={!message.trim() || isCommitting || isPushing}
            className="px-4 py-2 rounded-lg text-sm font-bold bg-[#2ea44f] text-white hover:bg-[#2ea44f]/90 transition-colors disabled:opacity-50 flex items-center gap-2 shadow-[0_0_10px_rgba(46,164,79,0.2)] cursor-pointer"
          >
            {isPushing ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
            Commit & Push
          </button>
        </div>
      </div>
    </div>
  );
}

export function BranchPicker({
  envId,
  currentBranch,
  onBranchChanged
}: {
  envId: string;
  currentBranch: string;
  onBranchChanged: () => void;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const [branches, setBranches] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isCreating, setIsCreating] = useState(false);
  const [isCheckingOut, setIsCheckingOut] = useState(false);
  const [newBranchName, setNewBranchName] = useState("");
  const dropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const fetchBranches = async () => {
    setIsLoading(true);
    try {
      const res = await fetch(`/api/environments/${envId}/git/branches`, {
        headers: { "Authorization": `Bearer ${localStorage.getItem("token")}` }
      });
      if (!res.ok) {
        return;
      }
      const data = await res.json().catch(() => ({}));
      const list = Array.isArray(data)
        ? data
        : Array.isArray(data?.branches)
        ? data.branches
        : [];
      setBranches(list);
    } catch {
      // Gracefully ignore fetch errors so Next.js dev overlay is not triggered
    } finally {
      setIsLoading(false);
    }
  };

  const handleToggle = () => {
    const nextOpen = !isOpen;
    setIsOpen(nextOpen);
    if (nextOpen) {
      fetchBranches();
    }
  };

  const handleCheckout = async (branch: string) => {
    setIsCheckingOut(true);
    try {
      const res = await fetch(`/api/environments/${envId}/git/checkout`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Authorization": `Bearer ${localStorage.getItem("token")}`
        },
        body: JSON.stringify({ ref: branch })
      });
      const data = await res.json().catch(() => ({}));
      
      if (!res.ok) {
        if (data.error && data.error.includes("Working tree is dirty")) {
           const force = confirm("Working tree is dirty. Do you want to force checkout and discard your changes?");
           if (force) {
             const forceRes = await fetch(`/api/environments/${envId}/git/checkout`, {
               method: "POST",
               headers: {
                 "Content-Type": "application/json",
                 "Authorization": `Bearer ${localStorage.getItem("token")}`
               },
               body: JSON.stringify({ ref: branch, force: true })
             });
             if (!forceRes.ok) {
               const forceData = await forceRes.json().catch(() => ({}));
               throw new Error(forceData.error || "Force checkout failed");
             }
           } else {
             return;
           }
        } else {
          throw new Error(data.error || "Checkout failed");
        }
      }
      
      toast.success(`Checked out ${branch}`);
      setIsOpen(false);
      onBranchChanged();
    } catch (err: unknown) {
      toast.error((err as Error).message);
    } finally {
      setIsCheckingOut(false);
    }
  };

  const handleCreateBranch = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newBranchName.trim()) return;
    setIsCreating(true);
    try {
      const res = await fetch(`/api/environments/${envId}/git/branch`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Authorization": `Bearer ${localStorage.getItem("token")}`
        },
        body: JSON.stringify({ branch: newBranchName.trim() })
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || "Failed to create branch");
      
      toast.success(`Created branch ${newBranchName}`);
      setNewBranchName("");
      setIsOpen(false);
      onBranchChanged();
    } catch (err: unknown) {
      toast.error((err as Error).message);
    } finally {
      setIsCreating(false);
    }
  };

  // Clean branch name from remotes
  const displayBranch = (currentBranch || "master").replace("remotes/origin/", "");

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={handleToggle}
        className="flex items-center gap-1.5 px-2 py-1 rounded bg-surface-container hover:bg-surface-container-high text-xs font-mono font-bold text-on-surface border border-outline-variant transition-colors cursor-pointer"
      >
        <GitBranch className="w-3.5 h-3.5 text-primary-fixed" />
        {displayBranch}
      </button>

      {isOpen && (
        <div className="absolute top-full left-0 mt-1 w-64 bg-surface-container-lowest border border-outline-variant rounded-xl shadow-xl overflow-hidden z-50">
          <div className="p-2 border-b border-outline-variant">
            <form onSubmit={handleCreateBranch} className="relative">
              <input
                type="text"
                placeholder="Create new branch..."
                value={newBranchName}
                onChange={(e) => setNewBranchName(e.target.value)}
                className="w-full bg-surface-container border border-outline-variant rounded px-2 py-1.5 text-xs text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-primary-fixed pr-8"
              />
              <button
                type="submit"
                disabled={!newBranchName.trim() || isCreating}
                className="absolute right-1 top-1/2 -translate-y-1/2 p-1 text-on-surface-variant hover:text-on-surface disabled:opacity-50 cursor-pointer"
              >
                {isCreating ? <Loader2 className="w-3 h-3 animate-spin" /> : <Plus className="w-3 h-3" />}
              </button>
            </form>
          </div>
          <div className="max-h-48 overflow-y-auto p-1">
            {isLoading ? (
              <div className="flex justify-center p-4">
                <Loader2 className="w-4 h-4 animate-spin text-on-surface-variant/40" />
              </div>
            ) : branches.length === 0 ? (
              <div className="text-xs text-on-surface-variant/50 p-2 text-center">No branches found</div>
            ) : (
              branches.map((b) => {
                const isCurrent = b === currentBranch || b === `remotes/origin/${currentBranch}`;
                return (
                  <button
                    key={b}
                    onClick={() => !isCurrent && handleCheckout(b)}
                    disabled={isCheckingOut}
                    className={`w-full text-left px-2 py-1.5 rounded text-xs font-mono flex items-center justify-between cursor-pointer ${
                      isCurrent ? "bg-primary-fixed/10 text-primary-fixed" : "text-on-surface-variant hover:bg-surface-container hover:text-on-surface"
                    }`}
                  >
                    <span className="truncate">{b.replace("remotes/origin/", "")}</span>
                    {isCurrent && <Check className="w-3.5 h-3.5" />}
                  </button>
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}

export function GitStatusPanel({ envId }: { envId: string }) {
  const [isPulling, setIsPulling] = useState(false);
  const { data: status, mutate, error } = useSWR(`/api/environments/${envId}/git/status`, fetchWithAuth, { refreshInterval: 10000 });

  const handlePull = async () => {
    setIsPulling(true);
    try {
      const res = await fetch(`/api/environments/${envId}/git/pull`, {
        method: "POST",
        headers: { "Authorization": `Bearer ${localStorage.getItem("token")}` }
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || "Failed to pull");
      toast.success("Successfully pulled latest changes");
      mutate();
    } catch (e: unknown) {
      toast.error((e as Error).message);
    } finally {
      setIsPulling(false);
    }
  };

  if (error) return <div className="text-error text-sm">Failed to load git status</div>;
  if (!status) return <div className="flex items-center justify-center p-8"><Loader2 className="w-5 h-5 animate-spin text-on-surface-variant/40" /></div>;

  return (
    <div className="bg-surface-container-lowest border border-outline-variant rounded-xl p-4 flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-bold text-on-surface flex items-center gap-2">
          <GitBranch className="w-4 h-4 text-primary-fixed" />
          Git Sync Status
        </h3>
        <button onClick={() => mutate()} className="text-on-surface-variant hover:text-on-surface transition-colors cursor-pointer" title="Refresh">
          <RefreshCw className="w-3.5 h-3.5" />
        </button>
      </div>
      
      <div className="flex items-center justify-between bg-surface-container border border-outline-variant rounded-lg p-3">
        <div className="flex flex-col">
          <span className="text-xs text-on-surface-variant">Current Branch</span>
          <span className="text-sm font-mono text-on-surface font-semibold">{status.branch.replace("remotes/origin/", "")}</span>
        </div>
        <div className="flex items-center gap-4 text-sm font-mono">
          <div className="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400" title="Commits ahead of remote">
            <UploadCloud className="w-4 h-4" />
            {status.ahead}
          </div>
          <div className="flex items-center gap-1.5 text-sky-600 dark:text-sky-400" title="Commits behind remote">
            <DownloadCloud className="w-4 h-4" />
            {status.behind}
          </div>
        </div>
      </div>

      {/* Pull button — always visible, disabled when not behind or when dirty */}
      <button
        onClick={handlePull}
        disabled={isPulling || status.behind === 0 || status.dirty}
        className="w-full py-2 rounded-lg text-sm font-semibold bg-sky-500/10 text-sky-600 dark:text-sky-400 border border-sky-500/30 hover:bg-sky-500/20 transition-colors disabled:opacity-40 flex items-center justify-center gap-2 cursor-pointer"
        title={status.dirty ? "Commit your changes before pulling" : status.behind === 0 ? "Already up to date" : `Pull ${status.behind} commit(s)`}
      >
        {isPulling ? <Loader2 className="w-4 h-4 animate-spin" /> : <DownloadCloud className="w-4 h-4" />}
        {isPulling ? "Pulling..." : status.behind > 0 ? `Pull ${status.behind} commit${status.behind === 1 ? "" : "s"}` : "Up to date"}
      </button>

      {status.dirty && status.behind > 0 && (
        <div className="text-xs text-amber-600 dark:text-amber-400 bg-amber-500/10 border border-amber-500/20 p-2 rounded">
          Commit or stash your changes before pulling.
        </div>
      )}
    </div>
  );
}

interface CommitEntry {
  hash: string;
  shortHash: string;
  message: string;
  author: string;
  date: string;
}

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export function CommitHistoryPanel({ envId }: { envId: string }) {
  const { data, error, isLoading, mutate } = useSWR<{ commits: CommitEntry[] }>(
    `/api/environments/${envId}/git/log`,
    fetchWithAuth,
    { refreshInterval: 15000 }
  );

  return (
    <div className="bg-surface-container-lowest border border-outline-variant rounded-xl p-4 flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-bold text-on-surface flex items-center gap-2">
          <GitCommit className="w-4 h-4 text-primary-fixed" />
          Commit History
        </h3>
        <button onClick={() => mutate()} className="text-on-surface-variant hover:text-on-surface transition-colors cursor-pointer" title="Refresh">
          <RefreshCw className="w-3.5 h-3.5" />
        </button>
      </div>

      {isLoading && (
        <div className="flex justify-center p-4">
          <Loader2 className="w-4 h-4 animate-spin text-on-surface-variant/40" />
        </div>
      )}

      {error && (
        <div className="text-xs text-error p-2">Failed to load commit history.</div>
      )}

      {!isLoading && !error && (!data?.commits || data.commits.length === 0) && (
        <div className="text-xs text-on-surface-variant/50 p-2 text-center">No commits yet.</div>
      )}

      {data?.commits && data.commits.length > 0 && (
        <div className="flex flex-col divide-y divide-outline-variant max-h-64 overflow-y-auto">
          {data.commits.map((c) => (
            <div key={c.hash} className="py-2.5 flex flex-col gap-0.5">
              <div className="flex items-start justify-between gap-2">
                <span className="text-xs text-on-surface leading-snug line-clamp-2 flex-1">{c.message}</span>
                <span className="font-mono text-xs text-on-surface-variant/60 shrink-0 pt-0.5">{c.shortHash}</span>
              </div>
              <div className="flex items-center gap-3 text-xs text-on-surface-variant">
                <span className="flex items-center gap-1"><User className="w-3 h-3" />{c.author}</span>
                <span className="flex items-center gap-1"><Clock className="w-3 h-3" />{relativeTime(c.date)}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
