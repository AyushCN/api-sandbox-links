import React from "react";
import { Users } from "lucide-react";

interface ActiveEditorsProps {
  editors: string[];
}

export default function ActiveEditors({ editors }: ActiveEditorsProps) {
  if (!editors || editors.length === 0) {
    return null;
  }

  return (
    <div className="flex items-center gap-2 px-3 py-1.5 bg-primary-fixed/10 border border-primary-fixed/20 rounded-full text-xs text-primary-fixed font-medium animate-in fade-in slide-in-from-bottom-2">
      <div className="relative flex items-center">
        <Users className="w-3.5 h-3.5 mr-1.5" />
        <span className="relative flex h-2 w-2 mr-2">
          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-primary-fixed opacity-75"></span>
          <span className="relative inline-flex rounded-full h-2 w-2 bg-primary-fixed"></span>
        </span>
      </div>
      <span>
        {editors.length === 1 
          ? `${editors[0]} is editing` 
          : `${editors.join(", ")} are editing`}
      </span>
    </div>
  );
}
