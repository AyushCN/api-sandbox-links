import { useState } from "react";

export interface EnvironmentChangeMessage {
  type: string;
  file_path: string;
  user_id: string;
  user_name: string;
  action: string;
  timestamp: string;
  diff?: string;
  commit_hash?: string;
  message?: string;
}

interface UseEnvironmentChangesOptions {
  onReloadReady?: () => void;
  onReloadFailed?: () => void;
}

export function useEnvironmentChanges(
  _envId: string | undefined,
  _options?: UseEnvironmentChangesOptions,
) {
  const [activeEditors, setActiveEditors] = useState<string[]>([]);
  const [hasUncommittedChanges, setHasUncommittedChanges] = useState<boolean>(false);
  const [recentChanges, setRecentChanges] = useState<EnvironmentChangeMessage[]>([]);

  return {
    hasUncommittedChanges,
    setHasUncommittedChanges,
    activeEditors,
    recentChanges,
  };
}
