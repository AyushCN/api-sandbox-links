"use client";

import React from "react";
import { AlertTriangle, CheckCircle2, GitBranch, AlertCircle, Clock } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import Link from "next/link";

interface BranchStatus {
  environment_id: string;
  environment_name: string;
  name: string;
  status: string;
  latest_commit: {
    hash: string;
    message: string;
    time: string;
  };
  author: {
    name: string;
    email: string;
  };
  has_uncommitted: boolean;
}

interface Blocker {
  type: string;
  environment: string;
  branch: string;
  resolution: string;
  files?: string[];
}

interface BlockerAlertsProps {
  branches: BranchStatus[];
  blockers: Blocker[];
}

export default function BlockerAlerts({ branches, blockers }: BlockerAlertsProps) {
  return (
    <div className="space-y-8">
      {/* Blockers Section */}
      <div>
        <h3 className="text-sm font-semibold tracking-wider text-error uppercase flex items-center gap-2 mb-4">
          <AlertTriangle className="w-4 h-4" />
          Blockers & Alerts ({blockers?.length || 0})
        </h3>
        
        {blockers && blockers.length > 0 ? (
          <div className="space-y-3">
            {blockers.map((blocker, i) => (
              <div key={i} className="bg-error/10 border border-error/30 rounded-xl p-4 flex items-start gap-3">
                <AlertCircle className="w-5 h-5 text-error mt-0.5 shrink-0" />
                <div className="w-full">
                  <div className="font-semibold text-error text-sm">
                    {blocker.type === 'conflict' ? 'Merge Conflict' : 'Uncommitted Changes'}
                  </div>
                  <div className="text-sm text-error/80 mt-1">
                    <span className="font-mono bg-error/20 px-1 py-0.5 rounded text-xs">{blocker.branch}</span> in <strong>{blocker.environment}</strong>
                  </div>
                  
                  {blocker.type === 'conflict' && blocker.files && blocker.files.length > 0 && (
                    <div className="mt-3 bg-error/5 border border-error/20 rounded p-2">
                      <div className="text-xs font-semibold text-error mb-1">Conflicting Files:</div>
                      <ul className="list-disc pl-4 space-y-1">
                        {blocker.files.map((file, idx) => (
                          <li key={idx} className="text-xs text-error/90 font-mono break-all">{file}</li>
                        ))}
                      </ul>
                    </div>
                  )}

                  <div className="text-xs text-error/60 mt-3 font-medium bg-error/10 inline-block px-2 py-1 rounded">
                    Action required: {blocker.resolution}
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="bg-emerald-500/10 border border-emerald-500/30 rounded-xl p-4 flex items-center gap-3 text-emerald-400">
            <CheckCircle2 className="w-5 h-5" />
            <span className="text-sm font-medium">No blockers detected! The team is moving fast.</span>
          </div>
        )}
      </div>

      {/* Branches Status Section */}
      <div>
        <h3 className="text-sm font-semibold tracking-wider text-on-surface-variant uppercase flex items-center gap-2 mb-4">
          <GitBranch className="w-4 h-4" />
          Active Branches Status
        </h3>
        
        <div className="grid gap-3">
          {branches?.map((branch, i) => (
            <div key={i} className="bg-surface-container border border-outline-variant rounded-xl p-4">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="font-mono font-bold text-primary-fixed">{branch.name}</span>
                    <span className="text-xs text-on-surface-variant/50">in</span>
                    <Link href={`/environments/${branch.environment_id}`} className="text-xs font-semibold text-on-surface hover:text-primary-fixed underline decoration-on-surface-variant/30 underline-offset-2">
                      {branch.environment_name}
                    </Link>
                  </div>
                  
                  {branch.latest_commit && branch.latest_commit.hash && (
                    <div className="mt-2 text-sm text-on-surface-variant">
                      <span className="font-mono text-xs opacity-70 mr-2">{branch.latest_commit.hash}</span>
                      {branch.latest_commit.message}
                    </div>
                  )}
                  
                  {branch.latest_commit && branch.latest_commit.time && (
                    <div className="mt-1 flex items-center gap-1 text-xs text-on-surface-variant/60">
                      <Clock className="w-3 h-3" />
                      {formatDistanceToNow(new Date(branch.latest_commit.time), { addSuffix: true })} by {branch.author.name}
                    </div>
                  )}
                </div>

                <div className="flex flex-col items-end gap-2 shrink-0">
                  {branch.status === 'ready_to_merge' && (
                    <span className="px-2.5 py-1 rounded-full bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-[10px] font-bold uppercase tracking-wider flex items-center gap-1.5">
                      <CheckCircle2 className="w-3 h-3" />
                      Ready to Merge
                    </span>
                  )}
                  {branch.status === 'in_progress' && (
                    <span className="px-2.5 py-1 rounded-full bg-blue-500/10 border border-blue-500/20 text-blue-400 text-[10px] font-bold uppercase tracking-wider flex items-center gap-1.5">
                      <Clock className="w-3 h-3" />
                      In Progress
                    </span>
                  )}
                  {branch.status === 'conflict' && (
                    <span className="px-2.5 py-1 rounded-full bg-error/10 border border-error/20 text-error text-[10px] font-bold uppercase tracking-wider flex items-center gap-1.5">
                      <AlertTriangle className="w-3 h-3" />
                      Conflict
                    </span>
                  )}
                </div>
              </div>
            </div>
          ))}
          {(!branches || branches.length === 0) && (
            <div className="text-center p-6 text-on-surface-variant/50 text-sm">
              No active branches found.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
