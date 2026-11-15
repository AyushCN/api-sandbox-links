import React from "react";
import Link from "next/link";
import { Shield } from "lucide-react";

export default function TermsPage() {
  return (
    <div className="min-h-screen bg-[#0d0e12] flex flex-col items-center">
      <nav className="w-full max-w-4xl mx-auto px-6 py-6 flex justify-between items-center border-b border-white/5">
        <Link
          href="/"
          className="text-xl font-bold text-white flex items-center gap-2"
        >
          <div className="w-8 h-8 bg-primary rounded-lg flex items-center justify-center shadow-[0_0_15px_rgba(var(--primary-rgb),0.5)]">
            <span className="text-on-primary text-lg leading-none">{"<"}</span>
          </div>
          API Sandbox
        </Link>
        <Link
          href="/"
          className="text-sm font-medium text-white/50 hover:text-white transition-colors"
        >
          Back to Home
        </Link>
      </nav>

      <main className="w-full max-w-4xl mx-auto px-6 py-12 flex flex-col gap-10 text-white/80">
        <div>
          <h1 className="text-4xl font-black text-white mb-4">
            Terms of Service
          </h1>
          <p className="text-lg text-white/60">Last updated: August 2026</p>
        </div>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white flex items-center gap-2">
            <Shield className="w-6 h-6 text-amber-500" />
            1. Ephemeral & Non-Production Nature
          </h2>
          <p>
            API Sandbox provides{" "}
            <strong>ephemeral, temporary development environments</strong>.
            Sandboxes are not guaranteed to persist and may be wiped, deleted,
            or stopped at any time due to idle timeouts, host failures, or
            administrative cleanup operations.
          </p>
          <ul className="list-disc pl-6 space-y-2 text-white/70">
            <li>
              <strong>GitHub is your source of truth.</strong> You must commit
              and push your work to retain it. Any unpushed changes are
              permanently lost when a sandbox is destroyed.
            </li>
            <li>
              <strong>Do not store production secrets</strong>, customer PII, or
              sensitive credentials inside these sandboxes. They are designed
              exclusively for development and testing.
            </li>
            <li>
              Preview URLs are strictly for <strong>dev/test purposes</strong>{" "}
              and are not a production hosting solution.
            </li>
          </ul>
        </section>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white">
            2. Best-Effort Isolation & Security Constraints
          </h2>
          <p>
            This platform uses containerization to provide{" "}
            <strong>best-effort isolation</strong> between users. It is{" "}
            <strong>not</strong> designed as a hardened security boundary
            against hostile, malicious multi-tenant workloads.
          </p>
          <p>
            You agree to not intentionally subvert the sandbox constraints,
            attempt privilege escalation, or use the platform to attack other
            tenants or the host infrastructure.
          </p>
        </section>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white">
            3. Acceptable Use Policy
          </h2>
          <p>
            You agree not to use the API Sandbox for any unlawful, abusive, or
            disruptive activities. Prohibited activities include, but are not
            limited to:
          </p>
          <ul className="list-disc pl-6 space-y-2 text-white/70">
            <li>Cryptocurrency mining or resource abuse.</li>
            <li>
              Launching network attacks, port scanning, or distributing malware.
            </li>
            <li>Hosting illegal content or copyright-infringing material.</li>
          </ul>
          <p>
            We reserve the right to immediately suspend or terminate access to
            any account or environment that violates these terms or threatens
            the stability and security of the host.
          </p>
        </section>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white">
            4. &quot;As Is&quot; Warranty Disclaimer
          </h2>
          <p>
            API Sandbox is provided on an <strong>&quot;AS IS&quot;</strong> and{" "}
            <strong>&quot;AS AVAILABLE&quot;</strong> basis. We make no
            warranties, explicit or implied, regarding the uptime, availability,
            or reliability of the service. Self-hosted operators are solely
            responsible for their own infrastructure uptime, data retention, and
            compliance.
          </p>
        </section>
      </main>
    </div>
  );
}
