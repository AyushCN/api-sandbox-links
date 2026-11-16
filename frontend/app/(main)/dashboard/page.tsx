"use client";

import React, { useMemo, useState } from "react";
import useSWR from "swr";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  Plus,
  GitBranch,
  Clock,
  Box,
  XCircle,
  ArrowRight,
  Database,
  Cpu,
  Layers,
  Sparkles,
  Server,
  ExternalLink,
  Copy,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { motion } from "framer-motion";
import toast from "react-hot-toast";
import { fetchWithAuth } from "@/lib/auth";

const fetcher = (url: string) => fetchWithAuth(url);

interface Environment {
  id: string;
  name: string;
  gitUrl: string;
  githubBranch: string;
  status: string;
  createdAt: string;
  databaseEngine?: string;
  builder?: string;
  subdomain?: string;
  previewUrl?: string;
}

const statusConfig: Record<
  string,
  { color: string; dot: string; label: string }
> = {
  IDLE: {
    color: "text-neutral-400 bg-neutral-400/10 border-neutral-400/20",
    dot: "bg-neutral-400",
    label: "Idle",
  },
  BUILDING: {
    color: "text-blue-400 bg-blue-400/10 border-blue-400/20",
    dot: "bg-blue-400 animate-pulse",
    label: "Building",
  },
  RUNNING: {
    color: "text-emerald-400 bg-emerald-400/10 border-emerald-400/20",
    dot: "bg-emerald-400 animate-pulse shadow-[0_0_8px_#34d399]",
    label: "Running",
  },
  STOPPED: {
    color: "text-orange-400 bg-orange-400/10 border-orange-400/20",
    dot: "bg-orange-400",
    label: "Stopped",
  },
  FAILED: {
    color: "text-red-400 bg-red-400/10 border-red-400/20",
    dot: "bg-red-400",
    label: "Failed",
  },
  EXPIRED: {
    color: "text-zinc-400 bg-zinc-400/10 border-zinc-400/20",
    dot: "bg-zinc-400",
    label: "Expired",
  },
  HIBERNATED: {
    color: "text-amber-400 bg-amber-400/10 border-amber-400/20",
    dot: "bg-amber-400",
    label: "Hibernated",
  },
  QUEUED: {
    color: "text-yellow-400 bg-yellow-400/10 border-yellow-400/20",
    dot: "bg-yellow-400 animate-pulse",
    label: "Queued",
  },
  DELETED: {
    color: "text-zinc-500 bg-zinc-500/10 border-zinc-500/20",
    dot: "bg-zinc-500",
    label: "Deleted",
  },
};

function StatusBadge({ status }: { status: string }) {
  const cfg = statusConfig[status] ?? statusConfig.IDLE;
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-bold tracking-wider uppercase border ${cfg.color}`}
    >
      <span className={`w-1.5 h-1.5 rounded-full ${cfg.dot}`} />
      {cfg.label}
    </span>
  );
}

export default function SandboxesDashboard() {
  const router = useRouter();
  const {
    data: dataRaw,
    error,
    isLoading,
  } = useSWR<any>("/api/environments", fetcher, {
    refreshInterval: 3000,
  });

  const [searchQuery, setSearchQuery] = useState("");

  const rawList = Array.isArray(dataRaw)
    ? dataRaw
    : dataRaw?.sandboxes || [];

  const environments: Environment[] = rawList.map((item: any) => {
    const sub = item.subdomain || "";
    let pUrl = item.publicUrl || item.url || (sub ? `http://${sub}.sandbox.localhost:8090` : "");
    if (pUrl && pUrl.includes(".sandbox.localhost") && !pUrl.includes(":8090")) {
      pUrl = pUrl.replace(".sandbox.localhost", ".sandbox.localhost:8090");
    }
    return {
      id: item.id,
      name: item.name || item.subdomain || `sandbox-${item.id.slice(0, 8)}`,
      gitUrl: item.gitUrl || item.repo_url || item.url || "",
      githubBranch: item.branch || item.githubBranch || "master",
      status: (item.status || "IDLE").toUpperCase(),
      createdAt: item.createdAt || item.created_at || new Date().toISOString(),
      databaseEngine: item.database_engine || "",
      builder: item.builder || "",
      subdomain: sub,
      previewUrl: pUrl,
    };
  });

  const filteredEnvs = useMemo(() => {
    if (!searchQuery.trim()) return environments;
    const q = searchQuery.toLowerCase();
    return environments.filter(
      (e) =>
        e.name.toLowerCase().includes(q) ||
        e.githubBranch.toLowerCase().includes(q) ||
        e.gitUrl.toLowerCase().includes(q)
    );
  }, [environments, searchQuery]);

  const activeCount = environments.filter((e) => e.status === "RUNNING").length;
  const buildingCount = environments.filter(
    (e) => e.status === "BUILDING" || e.status === "QUEUED"
  ).length;

  return (
    <div className="space-y-10 pb-16">
      {/* Header with Title and Create Action */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-6 pb-4 border-b border-outline-variant">
        <div>
          <div className="flex items-center gap-2 mb-2">
            <span className="w-2 h-2 rounded-full bg-emerald-500 dark:bg-emerald-400 animate-pulse" />
            <span className="text-[11px] font-mono font-bold uppercase tracking-wider text-on-surface-variant">
              Ephemeral Engine Active
            </span>
          </div>
          <h1 className="text-3xl sm:text-4xl font-extrabold tracking-tight text-on-surface">
            Cloud Sandboxes
          </h1>
          <p className="text-sm text-on-surface-variant mt-1">
            Manage live ephemeral environments, deterministic builds, and sidecars.
          </p>
        </div>

        <div className="flex items-center gap-3">
          <Link
            href="/upload"
            className="h-11 px-6 rounded-full bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] text-white text-xs font-bold uppercase tracking-wider hover:opacity-95 transition-all shadow-lg flex items-center gap-2"
          >
            <Plus className="w-4 h-4" />
            <span>New Sandbox</span>
          </Link>
        </div>
      </div>

      {/* Metric Stat Pills (Lumio Style) */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="p-4 rounded-2xl bg-surface-container-lowest border border-outline-variant shadow-xs flex flex-col justify-between transition-colors">
          <span className="text-[10px] font-bold uppercase tracking-widest text-on-surface-variant">
            Total Sandboxes
          </span>
          <div className="flex items-baseline gap-2 mt-2">
            <span className="text-2xl font-extrabold text-on-surface">
              {environments.length}
            </span>
            <span className="text-xs text-on-surface-variant">instances</span>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-surface-container-lowest border border-outline-variant shadow-xs flex flex-col justify-between transition-colors">
          <span className="text-[10px] font-bold uppercase tracking-widest text-on-surface-variant">
            Active Running
          </span>
          <div className="flex items-baseline gap-2 mt-2">
            <span className="text-2xl font-extrabold text-emerald-600 dark:text-emerald-400">
              {activeCount}
            </span>
            <span className="text-xs text-emerald-600/70 dark:text-emerald-400/60">live on port :8090</span>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-surface-container-lowest border border-outline-variant shadow-xs flex flex-col justify-between transition-colors">
          <span className="text-[10px] font-bold uppercase tracking-widest text-on-surface-variant">
            In-Flight Builds
          </span>
          <div className="flex items-baseline gap-2 mt-2">
            <span className="text-2xl font-extrabold text-sky-600 dark:text-sky-400">
              {buildingCount}
            </span>
            <span className="text-xs text-sky-600/70 dark:text-sky-400/60">BuildKit queue</span>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-surface-container-lowest border border-outline-variant shadow-xs flex flex-col justify-between transition-colors">
          <span className="text-[10px] font-bold uppercase tracking-widest text-on-surface-variant">
            Isolation Protocol
          </span>
          <div className="flex items-baseline gap-2 mt-2">
            <span className="text-sm font-bold text-on-surface font-mono">
              gVisor runsc
            </span>
            <span className="text-xs text-emerald-600 dark:text-emerald-400">hardened</span>
          </div>
        </div>
      </div>

      {/* Filter / Search Bar */}
      <div className="flex items-center justify-between gap-4">
        <input
          type="text"
          placeholder="Filter sandboxes by name, branch, or repo..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="w-full max-w-md h-10 px-4 rounded-full bg-surface-container-lowest border border-outline-variant text-xs text-on-surface placeholder:text-on-surface-variant/50 focus:outline-none focus:border-outline-variant transition-colors shadow-xs"
        />
        <span className="text-xs font-mono text-on-surface-variant hidden sm:block">
          Showing {filteredEnvs.length} environments
        </span>
      </div>

      {/* Sandboxes Grid */}
      <div>
        {isLoading ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {[1, 2, 3].map((i) => (
              <div
                key={i}
                className="bg-surface-container-lowest border border-outline-variant rounded-[28px] h-60 animate-pulse"
              />
            ))}
          </div>
        ) : error ? (
          <div className="bg-red-500/10 border border-red-500/20 p-12 rounded-[32px] text-center">
            <XCircle className="w-8 h-8 text-red-500 dark:text-red-400 mx-auto mb-3" />
            <p className="text-red-500 dark:text-red-400 font-semibold text-sm">
              Failed to load sandboxes
            </p>
          </div>
        ) : filteredEnvs.length === 0 ? (
          <div className="bg-surface-container-lowest border border-outline-variant rounded-[32px] py-20 px-6 text-center flex flex-col items-center">
            <div className="w-14 h-14 rounded-2xl bg-surface-container border border-outline-variant flex items-center justify-center mb-4 text-on-surface-variant">
              <Box className="w-6 h-6" />
            </div>
            <h3 className="text-lg font-bold text-on-surface mb-1">
              No sandboxes found
            </h3>
            <p className="text-xs text-on-surface-variant mb-6 max-w-sm">
              {searchQuery
                ? "No environments match your search query."
                : "Deploy your first isolated code environment with zero configuration."}
            </p>
            <Link
              href="/upload"
              className="px-6 py-2.5 rounded-full bg-on-surface text-surface text-xs font-bold uppercase tracking-wider hover:opacity-90 transition-all shadow-sm"
            >
              + Create Sandbox
            </Link>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {filteredEnvs.map((env, idx) => (
              <motion.div
                key={env.id}
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: idx * 0.03, duration: 0.3 }}
              >
                <div
                  onClick={() => router.push(`/environments/${env.id}`)}
                  className="bg-surface-container-lowest border border-outline-variant rounded-[28px] p-6 h-full flex flex-col justify-between transition-all duration-300 hover:border-outline-variant/80 hover:shadow-lg dark:hover:shadow-[0_16px_48px_rgba(0,0,0,0.45)] hover:-translate-y-1 relative overflow-hidden group cursor-pointer"
                >
                  <div>
                    {/* Top Row: Icon + Status */}
                    <div className="flex items-start justify-between gap-3 mb-4">
                      <div className="w-10 h-10 rounded-xl bg-surface-container border border-outline-variant flex items-center justify-center shrink-0 text-on-surface group-hover:text-primary-fixed transition-colors">
                        <Box className="w-5 h-5" />
                      </div>
                      <StatusBadge status={env.status} />
                    </div>

                    {/* Name & Branch */}
                    <h3 className="font-bold text-lg text-on-surface group-hover:text-primary-fixed transition-colors truncate mb-2">
                      {env.name}
                    </h3>

                    <div className="flex items-center gap-2 text-xs text-on-surface-variant font-mono mb-4">
                      <GitBranch className="w-3.5 h-3.5 text-[#FF416C] shrink-0" />
                      <span className="truncate">{env.githubBranch}</span>
                    </div>

                    {/* Extra Badges */}
                    <div className="flex items-center gap-2 flex-wrap mb-4">
                      {env.databaseEngine && (
                        <span className="px-2 py-0.5 rounded-full bg-amber-500/10 text-amber-600 dark:text-amber-300 border border-amber-500/20 text-[10px] font-mono uppercase flex items-center gap-1">
                          <Database className="w-2.5 h-2.5" />
                          {env.databaseEngine}
                        </span>
                      )}
                      <span className="px-2 py-0.5 rounded-full bg-surface-container text-on-surface-variant border border-outline-variant text-[10px] font-mono">
                        ID: {env.id.slice(0, 8)}
                      </span>
                    </div>

                    {/* Preview URL snippet with copy button */}
                    {env.previewUrl && env.status !== "DELETED" && (
                      <div
                        onClick={(e) => {
                          e.stopPropagation();
                        }}
                        className="flex items-center justify-between gap-2 p-2 rounded-xl bg-surface-container/60 border border-outline-variant/60 text-xs font-mono mb-4 text-on-surface-variant hover:border-outline-variant cursor-default"
                      >
                        <a
                          href={env.previewUrl}
                          target="_blank"
                          rel="noreferrer"
                          className="truncate text-primary-fixed hover:underline flex items-center gap-1 font-semibold"
                          title={env.previewUrl}
                          onClick={(e) => e.stopPropagation()}
                        >
                          <span className="truncate">{env.previewUrl.replace(/^https?:\/\//, "")}</span>
                          <ExternalLink className="w-3 h-3 shrink-0 opacity-70" />
                        </a>
                        <div className="flex items-center gap-1 shrink-0">
                          {env.status === "HIBERNATED" && (
                            <span className="text-[9px] px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-500 border border-amber-500/20 whitespace-nowrap font-sans font-medium">
                              Wakes
                            </span>
                          )}
                          <button
                            type="button"
                            onClick={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              navigator.clipboard.writeText(env.previewUrl!);
                              toast.success("Preview link copied!");
                            }}
                            title="Copy Preview URL"
                            className="p-1 rounded hover:bg-surface-container-highest text-on-surface-variant hover:text-on-surface transition-colors cursor-pointer"
                          >
                            <Copy className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </div>
                    )}
                  </div>

                  {/* Footer Row: Timestamp + Open Workspace arrow */}
                  <div className="pt-4 border-t border-outline-variant flex items-center justify-between text-[11px] font-mono text-on-surface-variant">
                    <div className="flex items-center gap-1.5">
                      <Clock className="w-3 h-3" />
                      <span>
                        {formatDistanceToNow(new Date(env.createdAt), {
                          addSuffix: true,
                        })}
                      </span>
                    </div>
                    <div className="flex items-center gap-1 text-on-surface font-sans font-bold text-xs group-hover:text-[#FF416C] transition-colors">
                      <span>Open Workspace</span>
                      <ArrowRight className="w-3.5 h-3.5 group-hover:translate-x-1 transition-transform" />
                    </div>
                  </div>
                </div>
              </motion.div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
