"use client";

import React, { useState } from "react";
import useSWR from "swr";
import { fetchWithAuth } from "@/lib/auth";
import ActivityFeed from "./ActivityFeed";
import BlockerAlerts from "./BlockerAlerts";
import { Activity, AlertTriangle, Loader2 } from "lucide-react";

interface TeamCollaborationDashboardProps {
  projectId: string;
}

const fetcher = async (url: string) => fetchWithAuth(url);

export default function TeamCollaborationDashboard({ projectId }: TeamCollaborationDashboardProps) {
  const [activeTab, setActiveTab] = useState<"activity" | "blockers">("activity");

  const { data: activityData, error: activityError } = useSWR(
    projectId ? `/api/projects/${projectId}/activity` : null,
    fetcher,
    { refreshInterval: 10000 }
  );

  const { data: teamStatusData, error: statusError } = useSWR(
    projectId ? `/api/projects/${projectId}/team-status` : null,
    fetcher,
    { refreshInterval: 10000 }
  );

  if (activityError || statusError) {
    return (
      <div className="p-8 text-center text-error border border-error/30 rounded-xl bg-error/5">
        Failed to load team collaboration data.
      </div>
    );
  }

  if (!activityData || !teamStatusData) {
    return (
      <div className="p-16 flex flex-col items-center justify-center text-on-surface-variant/50 h-full">
        <Loader2 className="w-8 h-8 animate-spin text-primary-fixed mb-4" />
        <p>Gathering team activity...</p>
      </div>
    );
  }

  const blockersCount = teamStatusData.blockers?.length || 0;

  return (
    <div className="flex flex-col h-full bg-surface-container-lowest">
      {/* Dashboard Sub-Header */}
      <div className="border-b border-outline-variant bg-surface-container/30 px-6 py-4">
        <div className="flex gap-4">
          <button
            onClick={() => setActiveTab("activity")}
            className={`px-4 py-2 rounded-lg text-sm font-semibold transition-all flex items-center gap-2 cursor-pointer ${
              activeTab === "activity"
                ? "bg-primary-fixed/10 text-primary-fixed border border-primary-fixed/20"
                : "text-on-surface-variant hover:bg-surface-container-high"
            }`}
          >
            <Activity className="w-4 h-4" />
            Activity Feed
          </button>
          
          <button
            onClick={() => setActiveTab("blockers")}
            className={`px-4 py-2 rounded-lg text-sm font-semibold transition-all flex items-center gap-2 cursor-pointer ${
              activeTab === "blockers"
                ? "bg-error/10 text-error border border-error/20"
                : "text-on-surface-variant hover:bg-surface-container-high"
            }`}
          >
            <AlertTriangle className="w-4 h-4" />
            Blockers & Alerts
            {blockersCount > 0 && (
              <span className="bg-error text-on-error text-[10px] px-1.5 py-0.5 rounded-full font-bold ml-1">
                {blockersCount}
              </span>
            )}
          </button>
        </div>
      </div>

      {/* Main Content Area */}
      <div className="flex-1 overflow-y-auto p-6">
        <div className="max-w-4xl mx-auto">
          {activeTab === "activity" && (
            <ActivityFeed activities={activityData.activities || []} />
          )}

          {activeTab === "blockers" && (
            <BlockerAlerts 
              branches={teamStatusData.branches || []}
              blockers={teamStatusData.blockers || []}
            />
          )}
        </div>
      </div>
    </div>
  );
}
