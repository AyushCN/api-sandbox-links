"use client";

import React from "react";
import { Clock, GitCommit, User } from "lucide-react";
import { formatDistanceToNow } from "date-fns";

interface Activity {
  timestamp: string;
  actorName: string;
  actorEmail: string;
  action: string;
  message: string;
  hash: string;
  environmentId: string;
  branch: string;
}

interface ActivityFeedProps {
  activities: Activity[];
}

export default function ActivityFeed({ activities }: ActivityFeedProps) {
  if (!activities || activities.length === 0) {
    return (
      <div className="p-8 text-center text-on-surface-variant/70 border border-outline-variant/30 rounded-xl bg-surface-container-low/30">
        No team activity found yet.
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <h3 className="text-sm font-semibold tracking-wider text-on-surface-variant uppercase flex items-center gap-2">
        <Clock className="w-4 h-4" />
        Latest Activity
      </h3>
      
      <div className="relative">
        <div className="absolute top-0 bottom-0 left-[19px] w-px bg-outline-variant/50" />
        
        <div className="space-y-6">
          {activities.map((activity, index) => (
            <div key={`${activity.hash}-${index}`} className="relative pl-12">
              {/* Timeline dot */}
              <div className="absolute left-[15px] top-1.5 w-2.5 h-2.5 rounded-full bg-primary-fixed shadow-[0_0_8px_rgba(var(--color-primary-fixed),0.6)]" />
              
              <div className="bg-surface-container border border-outline-variant rounded-xl p-4 hover:border-primary-fixed/30 transition-colors">
                <div className="flex items-start justify-between gap-4 mb-2">
                  <div className="flex items-center gap-2">
                    <div className="w-8 h-8 rounded-full bg-primary-fixed/20 flex items-center justify-center text-primary-fixed font-bold text-sm shrink-0">
                      {activity.actorName ? activity.actorName.charAt(0).toUpperCase() : <User className="w-4 h-4" />}
                    </div>
                    <div>
                      <div className="font-semibold text-on-surface text-sm">
                        {activity.actorName}{' '}
                        <span className="font-normal text-on-surface-variant">
                          {activity.action === "pushed" ? "pushed to" : activity.action}
                        </span>{' '}
                        {activity.action === "pushed" && <span className="font-mono text-primary-fixed">{activity.branch}</span>}
                      </div>
                      <div className="text-xs text-on-surface-variant/70">
                        {formatDistanceToNow(new Date(activity.timestamp), { addSuffix: true })}
                      </div>
                    </div>
                  </div>
                </div>

                <div className="bg-surface-container-lowest border border-outline-variant/50 rounded-lg p-3 mt-3">
                  <div className="flex items-start gap-3">
                    {activity.action === "pushed" ? (
                      <GitCommit className="w-4 h-4 text-on-surface-variant/50 mt-0.5 shrink-0" />
                    ) : (
                      <div className="w-4 h-4 text-on-surface-variant/50 mt-0.5 shrink-0 flex items-center justify-center text-[10px]">📝</div>
                    )}
                    <div className="min-w-0">
                      <div className="text-sm text-on-surface font-medium truncate" title={activity.message}>
                        {activity.message}
                      </div>
                      <div className="flex items-center gap-3 mt-1.5 text-xs text-on-surface-variant font-mono">
                        <span className="text-primary-fixed">{activity.hash}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
