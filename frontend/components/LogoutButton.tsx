"use client";

import { useAuth } from "@/lib/auth";
import { LogOut } from "lucide-react";

export function LogoutButton() {
  const { isAuthenticated, logout } = useAuth();

  if (!isAuthenticated) return null;

  return (
    <button
      onClick={logout}
      className="text-sm font-medium text-neutral-600 hover:text-black dark:text-neutral-400 dark:hover:text-white transition-colors flex items-center gap-2 cursor-pointer"
    >
      <LogOut className="w-4 h-4" />
      Sign out
    </button>
  );
}
