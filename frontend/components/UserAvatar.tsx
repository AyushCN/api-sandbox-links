"use client";

import Link from "next/link";
import useSWR from "swr";
import { fetchWithAuth } from "@/lib/auth";
import { User } from "lucide-react";

const fetcher = (url: string) => fetchWithAuth(url);

export function UserAvatar() {
  const { data: user } = useSWR<{ email?: string; username?: string }>("/api/me", fetcher, {
    revalidateOnFocus: false,
  });

  const display = user?.username || user?.email;
  const initials = display ? display.slice(0, 2).toUpperCase() : null;

  return (
    <Link
      href="/profile"
      className="w-8 h-8 rounded-lg bg-primary-fixed/10 border border-primary-fixed/20 flex items-center justify-center text-primary-fixed hover:bg-primary-fixed/20 transition-colors"
      title="Account"
    >
      {initials ? (
        <span className="text-xs font-black leading-none">{initials}</span>
      ) : (
        <User className="w-4 h-4" />
      )}
    </Link>
  );
}
