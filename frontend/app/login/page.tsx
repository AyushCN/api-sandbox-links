"use client";

import React, { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { Terminal, Shield, ArrowRight, Sparkles, CheckCircle2 } from "lucide-react";
import { ThemeToggle } from "@/components/ThemeToggle";

export default function LoginPage() {
  const router = useRouter();
  const { login } = useAuth();
  const [showLoader, setShowLoader] = useState(false);

  const handleDevLogin = () => {
    setShowLoader(true);
    setTimeout(() => {
      login();
    }, 1000);
  };

  if (showLoader) {
    return (
      <div className="bg-[#FAF9F5] text-[#1B1B1B] min-h-screen flex flex-col items-center justify-center relative font-sans selection:bg-black selection:text-white">
        <div className="w-16 h-16 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center text-white mb-8 shadow-2xl animate-pulse">
          <Sparkles className="w-8 h-8" />
        </div>
        <h2 className="text-2xl font-bold tracking-tight text-black mb-2">
          Initializing Workspace...
        </h2>
        <p className="text-xs text-neutral-500 font-mono">
          Loading credentials and active sandbox sessions
        </p>
      </div>
    );
  }

  return (
    <div className="bg-[#FAF9F5] dark:bg-[#0C0C0F] text-[#1B1B1B] dark:text-[#EDEDF2] min-h-screen flex flex-col justify-between selection:bg-black selection:text-white font-sans antialiased relative overflow-hidden transition-colors duration-200">
      {/* Background Decorative Rings */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[900px] h-[900px] rounded-full border border-black/[0.04] dark:border-white/[0.04] pointer-events-none" />
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] rounded-full border border-black/[0.04] dark:border-white/[0.04] pointer-events-none" />

      {/* Top Header */}
      <header className="w-full max-w-6xl mx-auto px-6 py-8 flex items-center justify-between z-10">
        <Link href="/" className="flex items-center gap-2.5 group">
          <div className="w-8 h-8 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center text-white font-black text-xs shrink-0 shadow-md group-hover:scale-105 transition-transform">
            A
          </div>
          <span className="font-bold text-sm tracking-tight text-black dark:text-white">
            API Sandbox
          </span>
        </Link>
        <div className="flex items-center gap-4">
          <ThemeToggle />
          <Link
            href="/"
            className="text-xs font-semibold text-neutral-500 hover:text-black dark:text-neutral-400 dark:hover:text-white transition-colors uppercase tracking-wider"
          >
            Back to Home
          </Link>
        </div>
      </header>

      {/* Main Centered Glass Card */}
      <main className="w-full max-w-md mx-auto px-6 py-12 z-10">
        <div className="bg-white/80 dark:bg-[#16161B]/85 backdrop-blur-2xl rounded-[36px] p-8 sm:p-10 border border-white/60 dark:border-white/10 shadow-[0_20px_50px_rgba(0,0,0,0.06)] text-center transition-colors">
          <div className="w-12 h-12 rounded-full bg-black/5 dark:bg-white/10 mx-auto flex items-center justify-center mb-6">
            <Terminal className="w-6 h-6 text-black dark:text-white" />
          </div>

          <h1 className="text-2xl sm:text-3xl font-extrabold tracking-tight text-neutral-900 dark:text-neutral-50 mb-2">
            Welcome Back
          </h1>
          <p className="text-xs sm:text-sm text-neutral-600 dark:text-neutral-400 mb-8 leading-relaxed">
            Access your cloud sandboxes, build telemetry, and collaborative
            workspaces.
          </p>

          <div className="space-y-3 mb-8">
            <button
              type="button"
              onClick={handleDevLogin}
              className="w-full h-12 rounded-full bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] text-white text-xs font-bold uppercase tracking-wider hover:opacity-95 active:scale-[0.99] transition-all shadow-lg flex items-center justify-center gap-2 group cursor-pointer"
            >
              <span>Enter as Dev User (Local)</span>
              <ArrowRight className="w-4 h-4 group-hover:translate-x-0.5 transition-transform" />
            </button>

            <a
              href="/api/auth/github"
              onClick={() => {
                if (typeof window !== "undefined") {
                  localStorage.removeItem("asl_logged_out");
                }
              }}
              className="w-full h-12 rounded-full bg-black dark:bg-white text-white dark:text-black text-xs font-bold uppercase tracking-wider hover:bg-neutral-800 dark:hover:bg-neutral-200 active:scale-[0.99] transition-all shadow-md flex items-center justify-center gap-2.5 border border-black/10 dark:border-white/10"
            >
              <svg
                height="18"
                aria-hidden="true"
                viewBox="0 0 16 16"
                version="1.1"
                width="18"
                className="fill-current"
              >
                <path d="M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.38A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z" />
              </svg>
              <span>Continue with GitHub</span>
            </a>
          </div>

          <div className="pt-6 border-t border-black/5 dark:border-white/10 flex items-center justify-center gap-6 text-[11px] text-neutral-500 dark:text-neutral-400">
            <span className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" />
              Zero Configuration
            </span>
            <span className="flex items-center gap-1.5">
              <Shield className="w-3.5 h-3.5 text-emerald-500" />
              Isolated Sandboxes
            </span>
          </div>
        </div>
      </main>

      {/* Footer */}
      <footer className="w-full max-w-6xl mx-auto px-6 py-8 text-center text-[11px] text-neutral-400 z-10">
        <p>© 2026 API Sandbox Links. All rights reserved.</p>
      </footer>
    </div>
  );
}
