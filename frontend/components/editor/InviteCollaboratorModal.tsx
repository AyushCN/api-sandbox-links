"use client";

import { useState, useEffect } from "react";
import { Loader2, Plus, Search, X } from "lucide-react";

interface UserSearchResult {
  id: string;
  username: string;
  email: string;
}

interface InviteCollaboratorModalProps {
  isOpen: boolean;
  onClose: () => void;
  onInvite: (identifier: string, role: string) => Promise<void>;
  isInviting: boolean;
}

export function InviteCollaboratorModal({
  isOpen,
  onClose,
  onInvite,
  isInviting,
}: InviteCollaboratorModalProps) {
  const [inviteIdentifier, setInviteIdentifier] = useState("");
  const [inviteRole, setInviteRole] = useState("COLLABORATOR");
  const [userSearchResults, setUserSearchResults] = useState<UserSearchResult[]>([]);
  const [isSearchingUsers, setIsSearchingUsers] = useState(false);
  const [showUserDropdown, setShowUserDropdown] = useState(false);

  const visibleResults = inviteIdentifier.trim().length >= 2 ? userSearchResults : [];

  useEffect(() => {
    const trimmed = inviteIdentifier.trim();
    if (!trimmed || trimmed.length < 2) {
      return;
    }

    const timer = setTimeout(async () => {
      setIsSearchingUsers(true);
      try {
        const token = localStorage.getItem("token");
        const res = await fetch(
          `/api/users/search?q=${encodeURIComponent(trimmed)}`,
          {
            headers: {
              Authorization: `Bearer ${token}`,
            },
          },
        );
        if (res.ok) {
          const data = await res.json();
          setUserSearchResults(data.users || []);
          setShowUserDropdown(true);
        }
      } catch {
        // Ignore user search network errors
      } finally {
        setIsSearchingUsers(false);
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [inviteIdentifier]);

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inviteIdentifier.trim()) return;
    await onInvite(inviteIdentifier.trim(), inviteRole);
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ backgroundColor: "rgba(0,0,0,0.75)" }}
    >
      <div className="w-full max-w-md bg-surface-container-lowest rounded-2xl border border-outline-variant shadow-2xl overflow-hidden">
        <div className="flex items-center justify-between px-5 py-4 border-b border-outline-variant bg-surface-container/30">
          <h3 className="font-semibold text-on-surface">Invite Collaborator</h3>
          <button
            onClick={onClose}
            className="text-on-surface-variant hover:text-on-surface"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="p-5 space-y-4">
          <div className="relative">
            <label className="text-sm font-bold tracking-wide text-on-surface-variant uppercase mb-1.5 block">
              Search User
            </label>
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-on-surface-variant/50" />
              <input
                type="text"
                value={inviteIdentifier}
                onChange={(e) => setInviteIdentifier(e.target.value)}
                onFocus={() => setShowUserDropdown(true)}
                placeholder="Email or username"
                className="w-full bg-surface-container pl-9 pr-4 py-3 rounded-lg border border-outline-variant text-on-surface focus:border-primary-fixed focus:ring-1 focus:ring-primary-fixed transition-all"
              />
              {isSearchingUsers && (
                <div className="absolute right-3 top-1/2 -translate-y-1/2">
                  <Loader2 className="w-4 h-4 animate-spin text-primary-fixed" />
                </div>
              )}
            </div>

            {showUserDropdown && visibleResults.length > 0 && (
              <div className="absolute z-10 w-full mt-1 bg-surface-container-lowest border border-outline-variant rounded-lg shadow-xl max-h-48 overflow-y-auto">
                {visibleResults.map((user) => (
                  <button
                    key={user.id}
                    type="button"
                    onClick={() => {
                      setInviteIdentifier(user.username || user.email);
                      setShowUserDropdown(false);
                    }}
                    className="w-full text-left px-4 py-2.5 hover:bg-primary-fixed/10 transition-colors flex items-center justify-between group"
                  >
                    <div className="flex items-center gap-2">
                      <div className="w-6 h-6 rounded bg-primary-container text-on-primary-fixed flex items-center justify-center text-xs font-bold shrink-0">
                        {(user.username || user.email || "?")[0].toUpperCase()}
                      </div>
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-on-surface truncate group-hover:text-primary-fixed transition-colors">
                          {user.username || "No Username"}
                        </p>
                        <p className="text-xs text-on-surface-variant truncate">
                          {user.email}
                        </p>
                      </div>
                    </div>
                    <Plus className="w-4 h-4 text-on-surface-variant group-hover:text-primary-fixed opacity-0 group-hover:opacity-100 transition-all shrink-0 ml-2" />
                  </button>
                ))}
              </div>
            )}
          </div>

          <div>
            <label className="text-sm font-bold tracking-wide text-on-surface-variant uppercase mb-1.5 block">
              Role
            </label>
            <select
              value={inviteRole}
              onChange={(e) => setInviteRole(e.target.value)}
              className="w-full bg-surface-container px-4 py-3 rounded-lg border border-outline-variant text-on-surface focus:border-primary-fixed focus:ring-1 focus:ring-primary-fixed transition-all"
            >
              <option value="COLLABORATOR">Collaborator (Edit & Push)</option>
              <option value="VIEWER">Viewer (Read Only)</option>
              <option value="ADMIN">Admin (Manage Team)</option>
            </select>
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
              disabled={isInviting || !inviteIdentifier.trim()}
              className="px-5 py-2 bg-primary-container text-on-primary-fixed-variant rounded-lg font-bold hover:shadow-[0_0_15px_rgba(0,240,255,0.2)] disabled:opacity-50 disabled:pointer-events-none flex items-center gap-2"
            >
              {isInviting && <Loader2 className="w-4 h-4 animate-spin" />}
              Send Invite
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
