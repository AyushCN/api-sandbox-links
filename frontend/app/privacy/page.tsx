import React from "react";
import Link from "next/link";
import { Lock } from "lucide-react";

export default function PrivacyPage() {
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
            Privacy Policy
          </h1>
          <p className="text-lg text-white/60">Last updated: August 2026</p>
        </div>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white flex items-center gap-2">
            <Lock className="w-6 h-6 text-primary" />
            1. GitHub OAuth and Permissions
          </h2>
          <p>
            API Sandbox uses GitHub OAuth to authenticate users and orchestrate
            development environments directly from your repositories. We request
            the following OAuth scopes:
          </p>
          <ul className="list-disc pl-6 space-y-2 text-white/70">
            <li>
              <strong>`user:email`</strong>: Used strictly to uniquely identify
              your account and send necessary service notifications.
            </li>
            <li>
              <strong>`repo`</strong>: Required to clone repositories into your
              ephemeral sandbox and push commits back to GitHub.{" "}
              <em>
                Note: The `repo` scope is broad and grants read/write access to
                your accessible repositories. We only interact with repositories
                you explicitly orchestrate or push from within the platform UI.
              </em>
            </li>
          </ul>
        </section>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white">
            2. Token Storage & Security
          </h2>
          <p>
            Your GitHub OAuth token is stored <strong>encrypted</strong>{" "}
            server-side using AES-256. It is solely utilized by the backend
            worker to securely orchestrate git clones and execute pushes on your
            behalf.
          </p>
        </section>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white">
            3. Data We Collect and Store
          </h2>
          <p>
            In order to provide the sandbox orchestration service, we store:
          </p>
          <ul className="list-disc pl-6 space-y-2 text-white/70">
            <li>
              <strong>Account Data:</strong> Your GitHub username, email, and
              encrypted OAuth token.
            </li>
            <li>
              <strong>Environment Metadata:</strong> Repository URLs, active
              branch names, and sandbox lifecycle status.
            </li>
            <li>
              <strong>Logs & Metrics:</strong> System crash logs and basic
              telemetry (e.g., container memory/CPU) to ensure platform
              stability.
            </li>
            <li>
              <strong>Workspace Files:</strong> Your code is temporarily
              persisted on the host disk while the sandbox environment exists.
            </li>
          </ul>
        </section>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white">
            4. Data Deletion & Ephemerality
          </h2>
          <p>
            When a sandbox environment is destroyed (either manually by you, or
            automatically via the idle garbage collector), all associated
            workspace files on disk are completely deleted on a best-effort
            basis. Your code only lives on our host while the environment
            remains active.
          </p>
          <p>We do not sell your personal data to third parties.</p>
        </section>

        <section className="flex flex-col gap-4">
          <h2 className="text-2xl font-bold text-white">
            5. Self-Hosting Disclosures
          </h2>
          <p>
            The API Sandbox control plane uses Docker on a host infrastructure.
            Operators of a self-hosted instance of API Sandbox are solely
            responsible for their own legal and privacy compliance. For privacy
            or security reports concerning this specific deployment, please
            contact the administrators of this instance.
          </p>
        </section>
      </main>
    </div>
  );
}
