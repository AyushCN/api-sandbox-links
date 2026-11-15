"use client";

import { useState } from "react";
import { Loader2, X } from "lucide-react";

interface ProjectOption {
  id: string;
  name: string;
}

interface TransferSandboxModalProps {
  isOpen: boolean;
  onClose: () => void;
  onTransfer: (projectId: string) => Promise<void>;
  isTransferring: boolean;
  currentProjectId?: string;
  projects?: ProjectOption[];
  isProjectsLoading: boolean;
}

export function TransferSandboxModal({
  isOpen,
  onClose,
  onTransfer,
  isTransferring,
  currentProjectId,
  projects,
  isProjectsLoading,
}: TransferSandboxModalProps) {
  const [transferProjectId, setTransferProjectId] = useState("");

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!transferProjectId) return;
    await onTransfer(transferProjectId);
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ backgroundColor: "rgba(0,0,0,0.75)" }}
    >
      <div className="w-full max-w-md bg-surface-container-lowest rounded-2xl border border-outline-variant shadow-2xl overflow-hidden">
        <div className="flex items-center justify-between px-5 py-4 border-b border-outline-variant bg-surface-container/30">
          <h3 className="font-semibold text-on-surface">Transfer Sandbox</h3>
          <button
            onClick={onClose}
            className="text-on-surface-variant hover:text-on-surface"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="p-5 space-y-4">
          <div>
            <label className="text-sm font-bold tracking-wide text-on-surface-variant uppercase mb-1.5 block">
              Select Destination Project
            </label>
            {isProjectsLoading ? (
              <div className="w-full bg-surface-container px-4 py-3 rounded-lg border border-outline-variant text-on-surface opacity-50 flex items-center">
                <Loader2 className="w-4 h-4 mr-2 animate-spin" /> Loading
                projects...
              </div>
            ) : (
              <select
                value={transferProjectId}
                onChange={(e) => setTransferProjectId(e.target.value)}
                className="w-full bg-surface-container px-4 py-3 rounded-lg border border-outline-variant text-on-surface focus:border-primary-fixed focus:ring-1 focus:ring-primary-fixed transition-all"
              >
                <option value="" disabled>
                  -- Select a Project --
                </option>
                {projects
                  ?.filter((p) => p.id !== currentProjectId)
                  .map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
              </select>
            )}
            <p className="text-xs text-on-surface-variant mt-2">
              Transferring this sandbox will instantly grant access to all
              collaborators in the destination project.
            </p>
          </div>
          <div className="pt-2 flex justify-end gap-3">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-on-surface-variant hover:text-on-surface"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isTransferring || !transferProjectId}
              className="px-5 py-2 bg-indigo-500/10 text-indigo-400 border border-indigo-500/30 rounded-lg font-bold hover:bg-indigo-500/20 disabled:opacity-50 disabled:pointer-events-none flex items-center gap-2"
            >
              {isTransferring && <Loader2 className="w-4 h-4 animate-spin" />}
              Confirm Transfer
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
