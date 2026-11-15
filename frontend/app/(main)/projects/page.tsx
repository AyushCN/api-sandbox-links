"use client";

import React, { useState } from "react";
import useSWR from "swr";
import Link from "next/link";
import {
  Folder,
  Users,
  ArrowRight,
  Loader2,
  Plus,
  XCircle,
  X,
  Layers,
} from "lucide-react";
import { fetchWithAuth } from "@/lib/auth";
import toast from "react-hot-toast";
import { motion } from "framer-motion";

const fetcher = (url: string) => fetchWithAuth(url);

export default function ProjectsPage() {
  const { data: currentUser } = useSWR<{ id?: string; username?: string; email?: string }>("/api/me", fetcher);
  const {
    data: projects,
    error,
    isLoading,
    mutate,
  } = useSWR("/api/projects", fetcher);

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");
  const [newProjectDesc, setNewProjectDesc] = useState("");
  const [isCreating, setIsCreating] = useState(false);

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newProjectName.trim()) return;
    setIsCreating(true);
    try {
      await fetchWithAuth("/api/projects", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: newProjectName,
          description: newProjectDesc,
          creator: currentUser ? {
            userId: currentUser.id,
            username: currentUser.username,
            email: currentUser.email,
          } : undefined,
        }),
      });
      toast.success("Workspace Project created successfully!");
      setIsCreateModalOpen(false);
      setNewProjectName("");
      setNewProjectDesc("");
      mutate();
    } catch (err: unknown) {
      toast.error((err as Error).message || "Failed to create project");
    } finally {
      setIsCreating(false);
    }
  };

  return (
    <div className="space-y-10 pb-16">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-6 pb-4 border-b border-outline-variant">
        <div>
          <div className="flex items-center gap-2 mb-2">
            <span className="w-2 h-2 rounded-full bg-sky-500 dark:bg-sky-400" />
            <span className="text-[11px] font-mono font-bold uppercase tracking-wider text-on-surface-variant">
              Workspace Groups
            </span>
          </div>
          <h1 className="text-3xl sm:text-4xl font-extrabold tracking-tight text-on-surface">
            Workspace Projects
          </h1>
          <p className="text-sm text-on-surface-variant mt-1">
            Group sandboxes by microservice and collaborate with engineering teams.
          </p>
        </div>

        <button
          onClick={() => setIsCreateModalOpen(true)}
          className="h-11 px-6 rounded-full bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] text-white text-xs font-bold uppercase tracking-wider hover:opacity-95 transition-all shadow-lg flex items-center gap-2 self-start sm:self-auto cursor-pointer"
        >
          <Plus className="w-4 h-4" />
          <span>New Workspace</span>
        </button>
      </div>

      {/* Grid */}
      <div>
        {isLoading ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {[1, 2, 3].map((i) => (
              <div
                key={i}
                className="bg-surface-container-lowest border border-outline-variant rounded-[28px] h-48 animate-pulse"
              />
            ))}
          </div>
        ) : error ? (
          <div className="bg-error/10 border border-error/20 p-12 rounded-[32px] text-center">
            <XCircle className="w-8 h-8 text-error mx-auto mb-3" />
            <p className="text-error font-semibold text-sm">
              Failed to load projects
            </p>
          </div>
        ) : !Array.isArray(projects) || projects.length === 0 ? (
          <div className="bg-surface-container-lowest border border-outline-variant rounded-[32px] py-20 px-6 text-center flex flex-col items-center">
            <div className="w-14 h-14 rounded-2xl bg-surface-container border border-outline-variant flex items-center justify-center mb-4 text-on-surface-variant">
              <Folder className="w-6 h-6" />
            </div>
            <h3 className="text-lg font-bold text-on-surface mb-1">
              No projects yet
            </h3>
            <p className="text-xs text-on-surface-variant mb-6 max-w-sm">
              Create your first project to organize sandboxes and assign collaborators.
            </p>
            <button
              onClick={() => setIsCreateModalOpen(true)}
              className="px-6 py-2.5 rounded-full bg-on-surface text-surface text-xs font-bold uppercase tracking-wider hover:opacity-90 transition-all shadow-sm cursor-pointer"
            >
              + Create Project
            </button>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {projects.map((proj: any, idx: number) => (
              <motion.div
                key={proj.id}
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: idx * 0.03, duration: 0.3 }}
              >
                <Link
                  href={`/projects/${proj.id}`}
                  className="block group h-full"
                >
                  <div className="bg-surface-container-lowest border border-outline-variant rounded-[28px] p-6 h-full flex flex-col justify-between transition-all duration-300 hover:border-outline-variant/80 hover:shadow-lg dark:hover:shadow-[0_16px_48px_rgba(0,0,0,0.45)] hover:-translate-y-1">
                    <div>
                      <div className="w-10 h-10 rounded-xl bg-surface-container border border-outline-variant flex items-center justify-center mb-4 text-on-surface group-hover:text-primary-fixed transition-colors">
                        <Folder className="w-5 h-5 text-sky-500 dark:text-sky-400" />
                      </div>

                      <h3 className="font-bold text-lg text-on-surface group-hover:text-primary-fixed transition-colors truncate mb-1">
                        {proj.name}
                      </h3>
                      <p className="text-xs text-on-surface-variant line-clamp-2 leading-relaxed mb-4">
                        {proj.description || "Active engineering workspace"}
                      </p>
                    </div>

                    <div className="pt-4 border-t border-outline-variant flex items-center justify-between text-xs text-on-surface-variant">
                      <div className="flex items-center gap-1.5 font-mono text-[11px]">
                        <Users className="w-3.5 h-3.5 text-on-surface-variant/70" />
                        <span>Team Project</span>
                      </div>
                      <div className="flex items-center gap-1 text-on-surface font-bold group-hover:text-[#FF416C] transition-colors">
                        <span>View</span>
                        <ArrowRight className="w-3.5 h-3.5 group-hover:translate-x-1 transition-transform" />
                      </div>
                    </div>
                  </div>
                </Link>
              </motion.div>
            ))}
          </div>
        )}
      </div>

      {/* Create Modal */}
      {isCreateModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-md p-4">
          <div className="bg-surface-container-lowest border border-outline-variant rounded-[32px] p-8 max-w-md w-full shadow-2xl relative">
            <button
              onClick={() => setIsCreateModalOpen(false)}
              className="absolute top-6 right-6 text-on-surface-variant hover:text-on-surface transition-colors cursor-pointer"
            >
              <X className="w-5 h-5" />
            </button>

            <h2 className="text-xl font-bold text-on-surface mb-1">
              New Workspace Project
            </h2>
            <p className="text-xs text-on-surface-variant mb-6">
              Create an organization folder to group sandboxes.
            </p>

            <form onSubmit={handleCreateProject} className="space-y-4">
              <div>
                <label className="text-xs font-bold uppercase tracking-wider text-on-surface-variant block mb-1.5">
                  Project Name
                </label>
                <input
                  type="text"
                  required
                  placeholder="e.g. Payment Microservices"
                  value={newProjectName}
                  onChange={(e) => setNewProjectName(e.target.value)}
                  className="w-full h-11 px-4 rounded-xl bg-surface-container border border-outline-variant text-xs font-mono text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-primary-fixed transition-colors"
                />
              </div>

              <div>
                <label className="text-xs font-bold uppercase tracking-wider text-on-surface-variant block mb-1.5">
                  Description (optional)
                </label>
                <textarea
                  rows={3}
                  placeholder="e.g. Internal payment APIs and webhook testing"
                  value={newProjectDesc}
                  onChange={(e) => setNewProjectDesc(e.target.value)}
                  className="w-full p-3 rounded-xl bg-surface-container border border-outline-variant text-xs font-mono text-on-surface placeholder:text-on-surface-variant/40 focus:outline-none focus:border-primary-fixed transition-colors"
                />
              </div>

              <div className="pt-2 flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setIsCreateModalOpen(false)}
                  className="px-5 py-2.5 rounded-full text-xs font-bold uppercase tracking-wider text-on-surface-variant hover:text-on-surface transition-colors cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isCreating}
                  className="px-6 py-2.5 rounded-full bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] text-white text-xs font-bold uppercase tracking-wider hover:opacity-95 transition-all shadow-md disabled:opacity-50 cursor-pointer"
                >
                  {isCreating ? "Creating..." : "Create Project"}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
