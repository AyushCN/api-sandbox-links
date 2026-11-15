"use client";

import { useState, useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import { useRouter, useSearchParams } from "next/navigation";
import toast from "react-hot-toast";
import { Loader2, ArrowRight, GitBranch, Database, Sparkles, Folder } from "lucide-react";
import { fetchWithAuth } from "@/lib/auth";
import useSWR from "swr";

const schema = z.object({
  name: z.string().min(3, "Name must be at least 3 characters").max(50),
  gitUrl: z
    .string()
    .url("Must be a valid URL")
    .regex(/^https:\/\/github\.com/, "Must be a GitHub repository"),
  githubBranch: z.string().min(1, "Branch is required"),
  database: z.enum(["auto", "postgres", "mysql", "none"]),
  projectId: z.string().optional(),
  newProjectName: z.string().optional(),
});

type FormData = z.infer<typeof schema>;

export default function UploadPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const queryProjectId = searchParams.get("projectId");

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [branches, setBranches] = useState<string[]>([]);
  const [isFetchingBranches, setIsFetchingBranches] = useState(false);

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    formState: { errors },
  } = useForm<FormData>({
    resolver: zodResolver(schema),
    defaultValues: { githubBranch: "main", database: "auto", projectId: queryProjectId || undefined },
  });

  const { data: projects, isLoading: isProjectsLoading } = useSWR(
    "/api/projects",
    (url) => fetchWithAuth(url)
  );

  const gitUrl = watch("gitUrl");
  const projectId = watch("projectId");
  const selectedDb = watch("database");

  // Pre-select project from query param or auto-select first project when loaded
  useEffect(() => {
    if (queryProjectId) {
      setValue("projectId", queryProjectId);
    } else if (projects && projects.length > 0 && !projectId) {
      setValue("projectId", projects[0].id);
    }
  }, [projects, projectId, queryProjectId, setValue]);

  useEffect(() => {
    const fetchBranches = async () => {
      if (!gitUrl || !gitUrl.startsWith("https://github.com/")) return;

      const match = gitUrl.match(/https:\/\/github\.com\/([^/]+)\/([^/.]+)/);
      if (!match) return;

      const owner = match[1];
      const repo = match[2];

      setIsFetchingBranches(true);
      try {
        const res = await fetch(
          `https://api.github.com/repos/${owner}/${repo}/branches`
        );
        if (!res.ok) {
          setBranches([]);
          return;
        }
        const data = await res.json();
        const branchNames = data.map((b: { name: string }) => b.name);
        setBranches(branchNames);

        if (branchNames.includes("main")) setValue("githubBranch", "main");
        else if (branchNames.includes("master"))
          setValue("githubBranch", "master");
        else if (branchNames.length > 0)
          setValue("githubBranch", branchNames[0]);
      } catch {
        setBranches([]);
      } finally {
        setIsFetchingBranches(false);
      }
    };

    const timeout = setTimeout(fetchBranches, 800);
    return () => clearTimeout(timeout);
  }, [gitUrl, setValue]);

  const onSubmit = async (data: FormData) => {
    setIsSubmitting(true);
    try {
      const response = await fetchWithAuth("/api/sandboxes", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          repo_url: data.gitUrl,
          branch: data.githubBranch || "main",
          lifetime_seconds: 86400,
          idle_timeout_seconds: 900,
          database: data.database,
        }),
      });

      const env = response;
      if (data.projectId && env?.id) {
        await fetch(`/api/projects/${data.projectId}/link`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ sandboxId: env.id }),
        }).catch(() => {});
      }
      toast.success("Sandbox queued! Initializing build...");
      router.push(`/environments/${env.id}`);
    } catch (error: unknown) {
      toast.error((error as Error).message);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="max-w-2xl mx-auto py-6">
      <div className="bg-surface-container-lowest border border-outline-variant rounded-[32px] p-8 sm:p-12 shadow-xl relative overflow-hidden transition-colors">
        {/* Header Icon + Title */}
        <div className="text-center mb-10">
          <div className="w-14 h-14 rounded-2xl bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] text-white flex items-center justify-center mx-auto mb-5 shadow-lg">
            <Sparkles className="w-7 h-7" />
          </div>
          <h1 className="text-2xl sm:text-3xl font-extrabold tracking-tight text-on-surface mb-2">
            Deploy New Sandbox
          </h1>
          <p className="text-xs sm:text-sm text-on-surface-variant max-w-sm mx-auto leading-relaxed">
            Connect a GitHub repository to build an isolated container
            environment with optional sidecar databases.
          </p>
        </div>

        <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
          {/* Project Destination */}
          <div className="space-y-2">
            <label className="text-xs font-bold uppercase tracking-wider text-on-surface-variant flex items-center gap-1.5">
              <Folder className="w-3.5 h-3.5" />
              Project Destination
            </label>
            {isProjectsLoading ? (
              <div className="w-full h-11 bg-surface-container-low px-4 rounded-xl border border-outline-variant text-on-surface-variant/50 flex items-center text-xs font-mono">
                <Loader2 className="w-3.5 h-3.5 mr-2 animate-spin" /> Loading projects...
              </div>
            ) : (
              <select
                {...register("projectId")}
                className="w-full h-11 bg-surface-container-low px-4 rounded-xl border border-outline-variant text-xs font-mono text-on-surface focus:outline-none focus:border-outline-variant/60 transition-colors"
              >
                {projects?.map((p: { id: string; name: string }) => (
                  <option key={p.id} value={p.id} className="bg-surface text-on-surface">
                    {p.name}
                  </option>
                ))}
                <option value="new" className="bg-surface text-on-surface">
                  + Create New Project
                </option>
              </select>
            )}
          </div>

          {/* Sandbox Name */}
          <div className="space-y-2">
            <label className="text-xs font-bold uppercase tracking-wider text-on-surface-variant">
              Sandbox Label
            </label>
            <input
              {...register("name")}
              placeholder="e.g. inventory-service-api"
              className="w-full h-11 bg-surface-container-low px-4 rounded-xl border border-outline-variant text-xs font-mono text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-outline-variant/60 transition-colors"
            />
            {errors.name && (
              <p className="text-xs text-red-500 font-mono mt-1">
                {errors.name.message}
              </p>
            )}
          </div>

          {/* GitHub Repo URL */}
          <div className="space-y-2">
            <label className="text-xs font-bold uppercase tracking-wider text-on-surface-variant">
              GitHub Repository URL
            </label>
            <input
              {...register("gitUrl")}
              placeholder="https://github.com/username/repository"
              className="w-full h-11 bg-surface-container-low px-4 rounded-xl border border-outline-variant text-xs font-mono text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-outline-variant/60 transition-colors"
            />
            {errors.gitUrl && (
              <p className="text-xs text-red-500 font-mono mt-1">
                {errors.gitUrl.message}
              </p>
            )}
          </div>

          {/* Branch Picker */}
          <div className="space-y-2">
            <label className="text-xs font-bold uppercase tracking-wider text-on-surface-variant flex items-center gap-1.5">
              <GitBranch className="w-3.5 h-3.5 text-[#FF416C]" />
              Branch
              {isFetchingBranches && (
                <Loader2 className="w-3 h-3 animate-spin text-on-surface-variant ml-1" />
              )}
            </label>
            {branches.length > 0 ? (
              <select
                {...register("githubBranch")}
                className="w-full h-11 bg-surface-container-low px-4 rounded-xl border border-outline-variant text-xs font-mono text-on-surface focus:outline-none focus:border-outline-variant/60 transition-colors"
              >
                {branches.map((b) => (
                  <option key={b} value={b} className="bg-surface text-on-surface">
                    {b}
                  </option>
                ))}
              </select>
            ) : (
              <input
                {...register("githubBranch")}
                placeholder="main"
                className="w-full h-11 bg-surface-container-low px-4 rounded-xl border border-outline-variant text-xs font-mono text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-outline-variant/60 transition-colors"
              />
            )}
            {errors.githubBranch && (
              <p className="text-xs text-red-500 font-mono mt-1">
                {errors.githubBranch.message}
              </p>
            )}
          </div>

          {/* Sidecar Database Option */}
          <div className="space-y-2 pt-2">
            <label className="text-xs font-bold uppercase tracking-wider text-on-surface-variant flex items-center gap-1.5">
              <Database className="w-3.5 h-3.5 text-amber-500" />
              Sidecar Database Provisioning
            </label>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
              {[
                { value: "auto", label: "Auto-Detect" },
                { value: "mysql", label: "MySQL 8.0" },
                { value: "postgres", label: "PostgreSQL 16" },
                { value: "none", label: "None" },
              ].map((opt) => (
                <button
                  type="button"
                  key={opt.value}
                  onClick={() => setValue("database", opt.value as any)}
                  className={`h-11 rounded-xl text-xs font-mono font-bold border transition-all cursor-pointer ${
                    selectedDb === opt.value
                      ? "bg-on-surface text-surface border-on-surface shadow-md"
                      : "bg-surface-container-low text-on-surface-variant border-outline-variant hover:border-outline-variant/60 hover:text-on-surface"
                  }`}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>

          {/* Submit Button */}
          <div className="pt-4">
            <button
              type="submit"
              disabled={isSubmitting}
              className="w-full h-12 rounded-full bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] text-white text-xs font-bold uppercase tracking-wider shadow-xl hover:opacity-95 active:scale-[0.99] transition-all flex items-center justify-center gap-2 disabled:opacity-50"
            >
              {isSubmitting ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  <span>Provisioning Sandbox...</span>
                </>
              ) : (
                <>
                  <span>Deploy Project</span>
                  <ArrowRight className="w-4 h-4" />
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
