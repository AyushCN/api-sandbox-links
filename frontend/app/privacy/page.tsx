"use client";

import React from "react";
import Link from "next/link";
import { Lock, Shield, Database, Trash2, Server, ArrowLeft, ArrowRight } from "lucide-react";
import { ThemeToggle } from "@/components/ThemeToggle";

export default function PrivacyPage() {
  return (
    <div className="min-h-screen bg-background text-on-surface flex flex-col justify-between transition-colors duration-200 font-sans selection:bg-[#FF416C] selection:text-white">
      {/* Top Header */}
      <header className="sticky top-0 z-50 w-full px-4 sm:px-8 py-3 bg-surface/85 backdrop-blur-xl border-b border-outline-variant transition-colors">
        <div className="max-w-5xl mx-auto flex items-center justify-between h-12">
          <Link
            href="/"
            className="flex items-center gap-2.5 hover:opacity-90 transition-opacity"
          >
            <div className="w-8 h-8 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center text-white font-bold text-xs shadow-md">
              A
            </div>
            <span className="font-bold tracking-tight text-base text-on-surface">
              API Sandbox
            </span>
          </Link>

          <div className="flex items-center gap-4 text-xs font-semibold uppercase tracking-wider text-on-surface-variant">
            <Link
              href="/"
              className="hover:text-on-surface transition-colors hidden sm:flex items-center gap-1"
            >
              <ArrowLeft className="w-3.5 h-3.5" />
              <span>Home</span>
            </Link>
            <Link
              href="/terms"
              className="hover:text-on-surface transition-colors hidden sm:inline"
            >
              Terms
            </Link>
            <Link
              href="/dashboard"
              className="hover:text-on-surface transition-colors"
            >
              Dashboard
            </Link>
            <ThemeToggle />
          </div>
        </div>
      </header>

      {/* Main Content */}
      <main className="flex-1 w-full max-w-4xl mx-auto px-6 py-12 flex flex-col gap-10">
        <div>
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-surface-container border border-outline-variant text-[11px] font-mono font-bold uppercase tracking-wider text-on-surface-variant mb-4">
            Security & Governance
          </div>
          <h1 className="text-3xl sm:text-4xl font-extrabold tracking-tight text-on-surface mb-2">
            Privacy Policy
          </h1>
          <p className="text-sm text-on-surface-variant">
            Last updated: November 2026 • Zero-knowledge token lifecycle & ephemeral workspace policies.
          </p>
        </div>

        <div className="space-y-8 text-sm leading-relaxed text-on-surface/90">
          <section id="github-oauth" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <Lock className="w-5 h-5 text-[#FF416C]" />
              1. GitHub OAuth and Scopes
            </h2>
            <p className="text-on-surface-variant">
              API Sandbox utilizes GitHub OAuth exclusively for zero-friction developer authentication and repository orchestration. We only request the minimum permissions required to run your ephemeral environments:
            </p>
            <ul className="list-disc pl-5 space-y-2 text-on-surface-variant">
              <li>
                <strong className="text-on-surface">user:email</strong>: Identifies your engineering user session and prevents unauthorized access to private sandbox instances.
              </li>
              <li>
                <strong className="text-on-surface">repo</strong>: Required solely to securely clone the target repositories into your ephemeral container volume and push user-initiated commits from the in-browser IDE back to your remote branches.
              </li>
            </ul>
          </section>

          <section id="security" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <Shield className="w-5 h-5 text-emerald-500" />
              2. Token Storage & Encryption at Rest
            </h2>
            <p className="text-on-surface-variant">
              Your OAuth tokens are never transmitted unencrypted or exposed in client bundles. Tokens are encrypted at rest using AES-256-GCM authenticated encryption utilizing a host-isolated master key (<code className="text-xs font-mono bg-surface-container px-1.5 py-0.5 rounded">SECRETS_ENCRYPTION_KEY</code>). Decryption is permitted strictly inside server-side Go orchestrators when performing authenticated Git pulls and pushes.
            </p>
          </section>

          <section id="data-collected" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <Database className="w-5 h-5 text-sky-500" />
              3. Data We Collect and Store
            </h2>
            <p className="text-on-surface-variant">
              To provide deterministic runtime execution, our control plane temporarily stores:
            </p>
            <ul className="list-disc pl-5 space-y-2 text-on-surface-variant">
              <li><strong className="text-on-surface">Developer Profile</strong>: GitHub username, avatar, and linked email address.</li>
              <li><strong className="text-on-surface">Environment Metadata</strong>: Repository URL, selected branch, container port, and builder type.</li>
              <li><strong className="text-on-surface">Runtime Telemetry</strong>: Real-time container exit codes, memory metrics, and live stdout/stderr logs streamed over WebSockets.</li>
              <li><strong className="text-on-surface">Workspace Files</strong>: Temporary project source code on the dedicated sandbox host disk.</li>
            </ul>
          </section>

          <section id="ephemerality" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <Trash2 className="w-5 h-5 text-amber-500" />
              4. Ephemeral Lifecycles & Automated Idle Reaping
            </h2>
            <p className="text-on-surface-variant">
              Sandboxes are strictly ephemeral by design. When an environment is destroyed—whether explicitly deleted by you or automatically reaped by the background inactivity cleaner—all ephemeral container filesystems, sidecar volumes, and host directories are permanently wiped clean. We do not retain, backup, or sell your source code.
            </p>
          </section>

          <section id="self-hosting" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <Server className="w-5 h-5 text-purple-500" />
              5. Self-Hosting Disclosures & Security Reports
            </h2>
            <p className="text-on-surface-variant">
              When deployed on your own infrastructure, your team retains 100% control over the host boundaries, Docker daemon socket, and network proxies. To report platform vulnerabilities or clarify security policies, please reach out to your system administrator or open an issue on our GitHub repository.
            </p>
          </section>
        </div>
      </main>

      {/* Footer */}
      <footer className="w-full border-t border-outline-variant bg-surface-container-lowest/50 py-8 px-6 mt-12 transition-colors">
        <div className="max-w-5xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-4 text-xs text-on-surface-variant">
          <div className="flex items-center gap-2">
            <div className="w-5 h-5 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center text-white font-bold text-[10px]">
              A
            </div>
            <span>© 2026 API Sandbox Links. All rights reserved.</span>
          </div>
          <div className="flex items-center gap-6">
            <Link href="/" className="hover:text-on-surface transition-colors">Home</Link>
            <Link href="/terms" className="hover:text-on-surface transition-colors">Terms of Service</Link>
            <Link href="/dashboard" className="hover:text-on-surface transition-colors">Dashboard</Link>
          </div>
        </div>
      </footer>
    </div>
  );
}
