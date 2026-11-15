"use client";

import React from "react";
import Link from "next/link";
import { Shield, AlertTriangle, CheckCircle2, RefreshCw, Cpu, ArrowLeft } from "lucide-react";
import { ThemeToggle } from "@/components/ThemeToggle";

export default function TermsPage() {
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
              href="/privacy"
              className="hover:text-on-surface transition-colors hidden sm:inline"
            >
              Privacy
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
            Platform Agreement
          </div>
          <h1 className="text-3xl sm:text-4xl font-extrabold tracking-tight text-on-surface mb-2">
            Terms of Service
          </h1>
          <p className="text-sm text-on-surface-variant">
            Last updated: November 2026 • Acceptable usage guidelines and ephemeral workload boundaries.
          </p>
        </div>

        <div className="space-y-8 text-sm leading-relaxed text-on-surface/90">
          <section id="ephemeral" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <RefreshCw className="w-5 h-5 text-amber-500" />
              1. Ephemeral & Non-Production Nature
            </h2>
            <p className="text-on-surface-variant">
              API Sandbox is designed specifically for <strong className="text-on-surface">ephemeral development, manual code review, and automated testing</strong>. Environments are not permanent virtual machines or long-term production hosting solutions:
            </p>
            <ul className="list-disc pl-5 space-y-2 text-on-surface-variant">
              <li>
                <strong className="text-on-surface">Git is your source of truth</strong>: Any uncommitted changes within an active sandbox will be permanently discarded when the container is stopped or reclaimed. Engineers must commit and push changes back to remote repositories.
              </li>
              <li>
                <strong className="text-on-surface">No production workloads</strong>: You must not use sandbox preview subdomains as mission-critical customer-facing endpoints or high-availability hosting.
              </li>
              <li>
                <strong className="text-on-surface">Zero sensitive secrets</strong>: Do not embed production API credentials, unencrypted private keys, or customer PII inside disposable test containers.
              </li>
            </ul>
          </section>

          <section id="security" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <Shield className="w-5 h-5 text-emerald-500" />
              2. Best-Effort Isolation & Security Boundaries
            </h2>
            <p className="text-on-surface-variant">
              Every sandbox operates within standard Linux container namespaces and cgroups, fortified optionally by Google gVisor (<code className="text-xs font-mono bg-surface-container px-1.5 py-0.5 rounded">runsc</code>) user-space kernel virtualization. While designed to prevent accidental cross-tenant interference:
            </p>
            <ul className="list-disc pl-5 space-y-2 text-on-surface-variant">
              <li>You agree not to intentionally exploit kernel vulnerabilities or attempt host privilege escalation.</li>
              <li>You agree not to probe internal container networks, Docker daemon control sockets, or peer sandbox instances.</li>
            </ul>
          </section>

          <section id="acceptable-use" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <AlertTriangle className="w-5 h-5 text-rose-500" />
              3. Acceptable Use Policy
            </h2>
            <p className="text-on-surface-variant">
              You may not use the API Sandbox platform to engage in abusive or harmful activities, including but not limited to:
            </p>
            <ul className="list-disc pl-5 space-y-2 text-on-surface-variant">
              <li>Cryptocurrency mining, botnets, or distributed computing hijacking.</li>
              <li>Denial-of-service (DoS/DDoS) attacks, automated vulnerability port scanning, or spoofing.</li>
              <li>Hosting malware, phishing schemes, or copyright-infringing content.</li>
            </ul>
            <p className="text-on-surface-variant">
              Instances found violating this policy will be terminated immediately and associated GitHub accounts blacklisted.
            </p>
          </section>

          <section id="service-limits" className="bg-surface-container-lowest border border-outline-variant rounded-2xl p-6 sm:p-8 space-y-4 shadow-sm scroll-mt-24">
            <h2 className="text-xl font-bold text-on-surface flex items-center gap-2.5">
              <Cpu className="w-5 h-5 text-sky-500" />
              4. Quotas, Capacity & Inactivity Reaping
            </h2>
            <p className="text-on-surface-variant">
              To guarantee cluster stability, default quotas apply to all developer accounts (up to 3 concurrent active sandboxes per user, 6 total system running instances). Sandboxes idle for more than 7 days without inbound HTTP gateway traffic or terminal activity are automatically hibernated or reaped.
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
            <Link href="/privacy" className="hover:text-on-surface transition-colors">Privacy Policy</Link>
            <Link href="/dashboard" className="hover:text-on-surface transition-colors">Dashboard</Link>
          </div>
        </div>
      </footer>
    </div>
  );
}
