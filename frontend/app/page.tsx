"use client";

import React, { useState } from "react";
import Link from "next/link";
import {
  Terminal as TerminalIcon,
  Database,
  Cpu,
  Layers,
  ArrowRight,
  GitBranch,
  Check,
  Sparkles,
  RefreshCw,
  Folder,
  FileCode,
  ShieldCheck,
  Server,
  Zap,
  Play,
  Share2,
  Globe,
  Radio,
  Shield,
} from "lucide-react";
import { ThemeToggle } from "@/components/ThemeToggle";
import { useTheme } from "@/lib/theme";
import toast from "react-hot-toast";

export default function LandingPage() {
  const { isDark } = useTheme();
  const [activeTeamTab, setActiveTeamTab] = useState<
    "backend" | "frontend" | "qa" | "devops"
  >("backend");

  const [activeFeatureIndex, setActiveFeatureIndex] = useState(0);

  const handleShare = async () => {
    try {
      if (typeof navigator !== "undefined" && navigator.clipboard) {
        await navigator.clipboard.writeText(window.location.origin);
        toast.success("Platform link copied to clipboard!");
      } else {
        toast.success("API Sandbox: " + window.location.origin);
      }
    } catch {
      toast.success("API Sandbox Platform");
    }
  };

  const teamUseCases = {
    backend: [
      {
        title: "Feature Branch Previews",
        desc: "Spin up isolated microservices with fresh databases for every pull request.",
        image:
          "https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?q=80&w=2064&auto=format&fit=crop",
      },
      {
        title: "Database Migration Staging",
        desc: "Safely execute and test MySQL and PostgreSQL migrations in disposable sandboxes.",
        image:
          "https://images.unsplash.com/photo-1634017839464-5c339ebe3cb4?q=80&w=2000&auto=format&fit=crop",
      },
      {
        title: "Multi-Service Mocking",
        desc: "Wire together mock APIs and real databases without port conflicts.",
        image:
          "https://images.unsplash.com/photo-1614852207233-e8d771f8e9fc?q=80&w=2070&auto=format&fit=crop",
      },
      {
        title: "Zero-Drift Dependency Testing",
        desc: "Test Dockerfiles and Railpack builds against clean Linux environments.",
        image:
          "https://images.unsplash.com/photo-1770791779732-7d1ef12524f2?q=80&w=2021&auto=format&fit=crop",
      },
    ],
    frontend: [
      {
        title: "Instant Live Backend URLs",
        desc: "Develop client apps against live, shareable backend sandboxes.",
        image:
          "https://images.unsplash.com/photo-1645811791249-93a1e10d0169?q=80&w=869&auto=format&fit=crop",
      },
      {
        title: "Deterministic API Contracts",
        desc: "No more localhost surprises: inspect exact request payloads in real-time.",
        image:
          "https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?q=80&w=2064&auto=format&fit=crop",
      },
      {
        title: "Interactive Client Demos",
        desc: "Send stakeholders a live URL with a dedicated backend in one click.",
        image:
          "https://images.unsplash.com/photo-1774420073915-96cc5e3abdb7?q=80&w=2021&auto=format&fit=crop",
      },
      {
        title: "Live Container Logs Feed",
        desc: "Stream backend logs directly into your browser via WebSockets.",
        image:
          "https://images.unsplash.com/photo-1634017839464-5c339ebe3cb4?q=80&w=2000&auto=format&fit=crop",
      },
    ],
    qa: [
      {
        title: "Automated E2E Smoke Suites",
        desc: "Run Playwright and Cypress against isolated, freshly-seeded instances.",
        image:
          "https://images.unsplash.com/photo-1774420073915-96cc5e3abdb7?q=80&w=2021&auto=format&fit=crop",
      },
      {
        title: "Chaos & Failure Injection",
        desc: "Test container crash loops, OOM kills, and network partitions safely.",
        image:
          "https://images.unsplash.com/photo-1614852207233-e8d771f8e9fc?q=80&w=2070&auto=format&fit=crop",
      },
      {
        title: "Stateful Rollback Testing",
        desc: "Fork active sandbox volumes to reproduce tricky edge-case bugs.",
        image:
          "https://images.unsplash.com/photo-1770791779732-7d1ef12524f2?q=80&w=2021&auto=format&fit=crop",
      },
      {
        title: "Deterministic Teardown",
        desc: "Ensure zero leaked Docker networks, containers, or zombie volumes.",
        image:
          "https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?q=80&w=2064&auto=format&fit=crop",
      },
    ],
    devops: [
      {
        title: "Ephemeral Infrastructure",
        desc: "Auto-hibernating containers and soft lifetime caps keep cloud bills zero.",
        image:
          "https://images.unsplash.com/photo-1634017839464-5c339ebe3cb4?q=80&w=2000&auto=format&fit=crop",
      },
      {
        title: "BuildKit Cache Acceleration",
        desc: "Persistent layer caching eliminates redundant language downloads.",
        image:
          "https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?q=80&w=2064&auto=format&fit=crop",
      },
      {
        title: "gVisor Runsc Hardening",
        desc: "Multi-tenant container isolation protects your host filesystem.",
        image:
          "https://images.unsplash.com/photo-1770791779732-7d1ef12524f2?q=80&w=2021&auto=format&fit=crop",
      },
      {
        title: "Autonomous Idle Reaping",
        desc: "Unused sandboxes hibernate automatically after 15 minutes of inactivity.",
        image:
          "https://images.unsplash.com/photo-1774420073915-96cc5e3abdb7?q=80&w=2021&auto=format&fit=crop",
      },
    ],
  };

  const logos = [
    "DOCKER ENGINE",
    "RAILPACK",
    "BUILDKIT",
    "POSTGRESQL 16",
    "MYSQL 8.0",
    "REDIS CACHE",
    "FASTAPI",
    "PYTHON 3.11",
    "NODE 20",
    "GO 1.22",
  ];

  return (
    <div
      className={`min-h-screen selection:bg-black selection:text-white font-sans overflow-x-hidden antialiased transition-colors duration-300 ${
        isDark ? "bg-[#0C0C0F] text-[#EDEDF2]" : "bg-[#FAF9F5] text-[#1B1B1B]"
      }`}
    >
      {/* 1. Floating Pill Nav (TopAppBar) */}
      <header className="fixed top-6 left-0 right-0 z-50 flex items-center justify-center px-4 sm:px-6 w-full pointer-events-none">
        <div className="pointer-events-auto w-full max-w-[920px] flex items-center justify-between lumio-pill-nav rounded-full px-5 py-2 shadow-2xl text-white h-[60px] border border-white/15">
          <div className="flex items-center gap-7 sm:gap-9 pl-1">
            <Link
              href="/"
              className="flex items-center gap-2.5 hover:opacity-90 transition-opacity shrink-0"
            >
              <div className="w-[34px] h-[34px] rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center shrink-0 shadow-md">
                <span className="text-white font-black text-sm leading-none">
                  A
                </span>
              </div>
              <span className="font-bold text-sm tracking-tight hidden sm:inline whitespace-nowrap">
                API Sandbox
              </span>
            </Link>

            <nav className="hidden md:flex items-center gap-6 sm:gap-7 text-[11px] font-semibold uppercase tracking-wider text-white/75">
              <a
                href="#builder-engine"
                className="hover:text-white transition-colors duration-200"
              >
                Engine
              </a>
              <a
                href="#sidecars"
                className="hover:text-white transition-colors duration-200"
              >
                Sidecars
              </a>
              <a
                href="#architecture"
                className="hover:text-white transition-colors duration-200"
              >
                Architecture
              </a>
              <a
                href="#use-cases"
                className="hover:text-white transition-colors duration-200"
              >
                Workflows
              </a>
            </nav>
          </div>

          <div className="flex items-center gap-3.5 shrink-0">
            <Link
              href="/login"
              className="text-[11px] font-semibold text-white/80 hover:text-white transition-colors px-2 py-1.5 uppercase tracking-wider hidden sm:block whitespace-nowrap"
            >
              Sign In
            </Link>
            <ThemeToggle className="text-white/85 hover:text-white" />
            <Link
              href="/dashboard"
              className="border border-white/20 text-white hover:bg-white hover:text-black font-semibold rounded-full transition-all duration-300 shrink-0 flex items-center px-5 text-[10px] h-[36px] uppercase tracking-wider shadow-sm whitespace-nowrap"
            >
              Launch Sandbox
            </Link>
          </div>
        </div>
      </header>

      <main>
        {/* 1. Hero Section */}
        <section className="pt-[160px] md:pt-[210px] px-6 max-w-[1728px] mx-auto flex flex-col items-center text-center relative overflow-visible pb-[120px]">
          <div
            className={`inline-flex items-center gap-2 px-3 py-1 rounded-full border text-[11px] font-semibold uppercase tracking-wider mb-8 backdrop-blur-md transition-colors ${
              isDark
                ? "bg-white/[0.06] border-white/10 text-neutral-300"
                : "bg-black/5 border-black/10 text-neutral-600"
            }`}
          >
            <span className="w-2 h-2 rounded-full bg-[#FF416C] animate-pulse" />
            Deterministic Builder & Sidecar Orchestration
          </div>

          <h1
            className={`text-[52px] sm:text-[72px] lg:text-[96px] font-extrabold tracking-tighter leading-[1.02] max-w-5xl mb-6 text-balance transition-colors ${
              isDark ? "text-white" : "text-[#1B1B1B]"
            }`}
          >
            Bring every API into focus
          </h1>

          <p
            className={`text-lg md:text-xl max-w-2xl mx-auto mb-12 leading-relaxed font-normal transition-colors ${
              isDark ? "text-neutral-400" : "text-[#8C8880]"
            }`}
          >
            Autonomous ephemeral environments, deterministic container builds,
            and isolated database sidecars for modern full-stack engineering
            teams.
          </p>

          <div className="flex flex-col sm:flex-row items-center gap-4 mb-16">
            <Link
              href="/dashboard"
              className={`w-full sm:w-auto px-8 py-3.5 rounded-full text-xs font-bold uppercase tracking-wider shadow-xl transition-all flex items-center justify-center gap-2 group ${
                isDark
                  ? "bg-white text-black hover:bg-neutral-200"
                  : "bg-[#181818] text-white hover:bg-black"
              }`}
            >
              Deploy Sandbox Free
              <ArrowRight className="w-4 h-4 group-hover:translate-x-1 transition-transform" />
            </Link>
            <a
              href="#architecture"
              className={`w-full sm:w-auto px-8 py-3.5 rounded-full border text-xs font-bold uppercase tracking-wider transition-all shadow-sm flex items-center justify-center gap-2 ${
                isDark
                  ? "bg-white/[0.06] border-white/15 text-white hover:bg-white/[0.12]"
                  : "bg-white/70 border-[#D9D7D0] text-[#1B1B1B] hover:bg-white"
              }`}
            >
              Explore Architecture
            </a>
          </div>

          {/* Large Hero Card with Live Interactive Preview */}
          <div
            className={`w-full max-w-[1440px] overflow-hidden shadow-2xl relative z-10 rounded-[32px] md:rounded-[44px] border h-[480px] sm:h-[620px] md:h-[720px] group transition-colors ${
              isDark
                ? "bg-[#131317] border-white/10"
                : "bg-[#F0EFEB] border-black/5"
            }`}
          >
            {/* Background rich gradient art */}
            <img
              alt="Cloud architecture gradient"
              className="w-full h-full object-cover transition-transform duration-700 group-hover:scale-[1.02]"
              src="https://images.unsplash.com/photo-1776009009253-759b31cd3908?q=80&w=2664&auto=format&fit=crop"
            />
            <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-black/20 to-transparent" />

            {/* Centered Floating Mock Terminal Box */}
            <div className="absolute inset-x-4 md:inset-x-auto md:left-1/2 md:-translate-x-1/2 bottom-8 md:bottom-12 w-auto md:w-[680px] lumio-glass-dark rounded-2xl md:rounded-3xl p-5 md:p-6 text-left shadow-2xl border border-white/10 text-white">
              <div className="flex items-center justify-between pb-3 mb-3 border-b border-white/10 text-xs font-mono">
                <div className="flex items-center gap-2">
                  <span className="w-3 h-3 rounded-full bg-red-500/80 inline-block" />
                  <span className="w-3 h-3 rounded-full bg-yellow-500/80 inline-block" />
                  <span className="w-3 h-3 rounded-full bg-green-500/80 inline-block" />
                  <span className="text-white/40 ml-2">sandbox-deploy.log</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />
                  <span className="text-emerald-400 font-bold text-[10px] uppercase tracking-wider">
                    HEALTHY • :5000
                  </span>
                </div>
              </div>

              <div className="font-mono text-[11px] md:text-xs text-white/80 space-y-1.5 leading-relaxed overflow-hidden">
                <div className="text-white/40">
                  #1 [resolve] inspecting repository build strategy...
                </div>
                <div className="text-emerald-300">
                  =&gt; BUILDER: Dockerfile resolved (python:3.11-slim)
                </div>
                <div className="text-sky-300">
                  =&gt; SIDECAR: Auto-provisioned MySQL 8.0 on isolated network
                </div>
                <div className="text-purple-300">
                  =&gt; INJECT: DB_HOST=db_6ad93e DB_USER=sandbox DB_PORT=3306
                </div>
                <div className="text-white/60">
                  =&gt; DISCOVERY: Port 5000 verified active in 0.4s
                </div>
                <div className="text-white font-bold pt-1">
                  =&gt; LIVE URL: https://pharmacy-dbms.sandbox.localhost:8090
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* 2. Trust Strip (Marquee) */}
        <section
          className={`py-12 px-6 border-y max-w-[1728px] mx-auto overflow-hidden transition-colors ${
            isDark
              ? "bg-[#0C0C0F] border-white/10"
              : "bg-[#FAF9F5] border-[#D9D7D0]/40"
          }`}
        >
          <p
            className={`text-[11px] font-semibold uppercase tracking-widest text-center mb-6 transition-colors ${
              isDark ? "text-neutral-400" : "text-[#8C8880]"
            }`}
          >
            Engineered for modern production architectures
          </p>
          <div className="flex overflow-hidden relative w-full">
            <div className="animate-marquee flex gap-16 items-center shrink-0">
              {logos.concat(logos).map((item, idx) => (
                <span
                  key={idx}
                  className={`font-bold text-sm tracking-widest uppercase whitespace-nowrap cursor-default transition-colors ${
                    isDark
                      ? "text-neutral-400 hover:text-white"
                      : "text-[#8C8880] hover:text-black"
                  }`}
                >
                  {item}
                </span>
              ))}
            </div>
          </div>
        </section>

        {/* 3. Floating Visual Collage ("Ephemeral Intelligence") */}
        <section
          className={`min-h-[1100px] lg:min-h-[1350px] w-full max-w-[1728px] mx-auto relative overflow-hidden py-20 px-6 transition-colors ${
            isDark ? "bg-[#0C0C0F]" : "bg-[#FAF9F5]"
          }`}
        >
          {/* Central Giant Headline Anchor */}
          <div className="absolute inset-0 flex justify-center items-center pointer-events-none mt-12">
            <h2 className="text-[64px] sm:text-[100px] lg:text-[140px] font-black text-[#1B1B1B]/8 dark:text-white/5 tracking-tighter leading-[0.9] text-center select-none">
              Ephemeral
              <br />
              Intelligence
            </h2>
          </div>

          <div className="relative max-w-6xl mx-auto min-h-[900px] hidden md:block">
            {/* 1. Top-Left: Live Build Progress Card */}
            <div
              className="absolute rounded-[28px] overflow-hidden bg-white/70 backdrop-blur-2xl border border-white/60 shadow-2xl p-4 w-[320px] transition-transform hover:-translate-y-1"
              style={{ left: "4%", top: "8%", zIndex: 10 }}
            >
              <div className="flex justify-between items-center mb-2">
                <span className="text-[10px] font-bold text-black uppercase tracking-wider flex items-center gap-1.5">
                  <span className="w-2 h-2 rounded-full bg-[#FF416C] animate-ping" />
                  BuildKit Pipeline
                </span>
                <span className="text-[10px] font-mono font-bold text-black">
                  Layer Cached
                </span>
              </div>
              <div className="w-full bg-black/10 rounded-full h-1.5 mb-3">
                <div className="bg-black w-[100%] h-1.5 rounded-full" />
              </div>
              <p className="text-[11px] font-mono text-neutral-600">
                =&gt; [cached] python:3.11-slim
              </p>
            </div>

            {/* 2. Top-Right: Smart Builder Card */}
            <div
              className="absolute rounded-[28px] overflow-hidden bg-white/70 backdrop-blur-2xl border border-white/60 shadow-2xl p-5 w-[280px] transition-transform hover:-translate-y-1"
              style={{ right: "5%", top: "10%", zIndex: 10 }}
            >
              <div className="w-9 h-9 rounded-full bg-black/5 flex items-center justify-center mb-3">
                <Sparkles className="w-4 h-4 text-[#FF416C]" />
              </div>
              <h4 className="font-bold text-sm text-black mb-1">
                Builder Resolver
              </h4>
              <p className="text-xs text-neutral-500 leading-relaxed">
                Prioritizes native Dockerfiles, falls back safely to Railpack or
                Nixpacks.
              </p>
            </div>

            {/* 3. Mid-Left: Live Auto-Sync Control */}
            <div
              className="absolute rounded-[28px] bg-white/80 backdrop-blur-3xl border border-white/80 shadow-[0_12px_40px_rgba(0,0,0,0.08)] flex flex-col p-5 w-[270px] transition-transform hover:-translate-y-1"
              style={{ left: "2%", top: "38%", zIndex: 15 }}
            >
              <div className="flex items-center justify-between mb-4">
                <span className="text-xs font-bold text-black">Git Sync</span>
                <div className="w-9 h-5 bg-black rounded-full relative cursor-pointer">
                  <div className="absolute right-0.5 top-0.5 w-4 h-4 bg-white rounded-full shadow-sm" />
                </div>
              </div>
              <div className="flex items-center gap-3">
                <div className="w-9 h-9 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center shadow-md text-white">
                  <GitBranch className="w-4 h-4" />
                </div>
                <div className="flex flex-col">
                  <span className="text-[11px] font-bold text-black">
                    Branch: main
                  </span>
                  <span className="text-[11px] text-emerald-600 font-medium">
                    Commit Hook Active
                  </span>
                </div>
              </div>
            </div>

            {/* 4. Mid-Right: Sidecar Database Widget */}
            <div
              className="absolute rounded-[28px] bg-white/80 backdrop-blur-3xl border border-white/80 shadow-[0_12px_40px_rgba(0,0,0,0.08)] p-5 w-[310px] transition-transform hover:-translate-y-1"
              style={{ right: "3%", top: "42%", zIndex: 15 }}
            >
              <div className="flex items-center justify-between mb-3">
                <span className="text-[10px] font-bold uppercase tracking-wider text-neutral-500 flex items-center gap-1.5">
                  <Database className="w-3.5 h-3.5 text-amber-500" />
                  Sidecar DB
                </span>
                <span className="px-2 py-0.5 rounded-full bg-emerald-50 text-emerald-600 text-[10px] font-bold border border-emerald-200">
                  MYSQL 8.0
                </span>
              </div>
              <p className="text-xs font-mono text-neutral-700 bg-black/5 p-2 rounded-lg">
                DB_HOST: db_6ad93e:3306
              </p>
            </div>

            {/* 5. Bottom-Left: Security Hardening Pill */}
            <div
              className="absolute rounded-[28px] bg-white/80 backdrop-blur-3xl border border-white/80 shadow-2xl p-5 w-[320px] transition-transform hover:-translate-y-1"
              style={{ left: "8%", top: "68%", zIndex: 10 }}
            >
              <div className="flex items-center gap-3 mb-2">
                <ShieldCheck className="w-5 h-5 text-emerald-600" />
                <span className="text-xs font-bold text-black uppercase tracking-wider">
                  gVisor Sandbox
                </span>
              </div>
              <p className="text-xs text-neutral-500">
                Virtualised kernel syscall isolation with strict CPU & memory
                limits.
              </p>
            </div>

            {/* 6. Bottom-Right: Latency Telemetry */}
            <div
              className="absolute rounded-[28px] bg-white/80 backdrop-blur-3xl border border-white/80 shadow-2xl p-5 w-[290px] transition-transform hover:-translate-y-1"
              style={{ right: "8%", top: "72%", zIndex: 10 }}
            >
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs font-bold text-neutral-600">
                  Gateway Telemetry
                </span>
                <span className="text-xs font-mono text-emerald-600 font-bold">
                  2.4 ms
                </span>
              </div>
              <div className="flex items-end gap-1 h-8 pt-1">
                {[40, 65, 30, 85, 45, 90, 35, 70, 50, 95].map((h, i) => (
                  <div
                    key={i}
                    style={{ height: `${h}%` }}
                    className="flex-1 bg-black/15 rounded-t-sm"
                  />
                ))}
              </div>
            </div>

            {/* 7. Floating Presence Testimonial Comment */}
            <div
              className="absolute lumio-glass-light rounded-full flex items-center gap-3 px-4 py-2.5 shadow-xl border border-white/80"
              style={{
                left: "50%",
                top: "84%",
                transform: "translateX(-50%)",
                zIndex: 25,
              }}
            >
              <div className="w-8 h-8 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center text-white font-bold text-xs shrink-0">
                SC
              </div>
              <div className="flex flex-col pr-2">
                <span className="text-[11px] font-bold text-black">
                  Sarah Chen • Platform Lead
                </span>
                <span className="text-[12px] text-neutral-600">
                  &ldquo;Deployed full Python API + MySQL in 1.4 seconds!&rdquo;
                </span>
              </div>
            </div>
          </div>

          {/* Mobile Fallback for Collage */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 md:hidden">
            <div className="p-5 rounded-2xl bg-white/80 border border-black/5 shadow-sm">
              <span className="text-xs font-bold text-black mb-1 block">
                Deterministic Builder
              </span>
              <p className="text-xs text-neutral-500">
                Dockerfiles, Railpack & Nixpacks prioritized automatically.
              </p>
            </div>
            <div className="p-5 rounded-2xl bg-white/80 border border-black/5 shadow-sm">
              <span className="text-xs font-bold text-black mb-1 block">
                Sidecar Databases
              </span>
              <p className="text-xs text-neutral-500">
                PostgreSQL and MySQL provisioned on isolated networks.
              </p>
            </div>
            <div className="p-5 rounded-2xl bg-white/80 border border-black/5 shadow-sm">
              <span className="text-xs font-bold text-black mb-1 block">
                Git Push & Webhooks
              </span>
              <p className="text-xs text-neutral-500">
                Commit from the web editor directly to GitHub repositories.
              </p>
            </div>
            <div className="p-5 rounded-2xl bg-white/80 border border-black/5 shadow-sm">
              <span className="text-xs font-bold text-black mb-1 block">
                Zero-Leak Teardown
              </span>
              <p className="text-xs text-neutral-500">
                Idempotent deletion cleans containers, volumes & networks.
              </p>
            </div>
          </div>
        </section>

        {/* 4. Manifesto Section */}
        <section
          className={`py-[140px] md:py-[180px] px-6 max-w-[1728px] mx-auto relative flex flex-col items-center text-center transition-colors ${
            isDark ? "bg-[#0C0C0F]" : "bg-[#FAF9F5]"
          }`}
        >
          <div
            className={`w-12 h-12 rounded-full border flex items-center justify-center mb-8 ${
              isDark ? "border-white/20 text-white" : "border-black/20 text-neutral-600"
            }`}
          >
            <Layers className="w-5 h-5" />
          </div>

          <h2
            className={`text-[32px] sm:text-[44px] md:text-[52px] leading-[1.12] tracking-tight max-w-[960px] font-normal text-balance mb-8 transition-colors ${
              isDark ? "text-white" : "text-[#1B1B1B]"
            }`}
          >
            As microservice applications expand across engineering teams, the
            need for a deterministic, zero-config sandbox has never been more
            critical. API Sandbox unifies your deployment strategy.
          </h2>

          <div className="h-1 w-24 bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] rounded-full" />
        </section>

        {/* 5. Brand OS: The Foundation Section */}
        <section
          id="architecture"
          className={`py-[100px] px-6 max-w-[1728px] mx-auto transition-colors ${
            isDark ? "bg-[#0C0C0F]" : "bg-[#FAF9F5]"
          }`}
        >
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-16 lg:gap-24 items-center">
            {/* Left: Interactive File Tree Window */}
            <div
              className={`rounded-[36px] md:rounded-[44px] p-6 sm:p-10 h-[560px] sm:h-[640px] flex items-center justify-center relative overflow-hidden border shadow-sm transition-colors ${
                isDark
                  ? "bg-[#141418] border-white/10"
                  : "bg-gradient-to-br from-[#F4F3EF] to-[#E9E8E4] border-[#D9D7D0]/40"
              }`}
            >
              <div
                className={`w-full max-w-md rounded-2xl shadow-xl border p-6 transition-colors ${
                  isDark ? "bg-[#18181E] border-white/10 text-white" : "bg-white border-black/5"
                }`}
              >
                <div className="flex items-center justify-between mb-6 pb-3 border-b border-black/5">
                  <span className="font-bold text-xs uppercase tracking-wider text-black flex items-center gap-2">
                    <Folder className="w-4 h-4 text-amber-500" />
                    Sandbox Filesystem
                  </span>
                  <span className="text-[10px] font-mono text-neutral-400">
                    /app
                  </span>
                </div>

                <div className="space-y-3 font-mono text-xs">
                  <div className="flex items-center justify-between p-2.5 hover:bg-neutral-50 rounded-xl transition-colors border border-transparent hover:border-black/5">
                    <div className="flex items-center gap-3">
                      <FileCode className="w-4 h-4 text-[#FF4B2B]" />
                      <span className="text-black font-medium">server.py</span>
                    </div>
                    <span className="text-[10px] text-neutral-400">
                      Flask / FastAPI
                    </span>
                  </div>

                  <div className="flex items-center justify-between p-2.5 hover:bg-neutral-50 rounded-xl transition-colors border border-transparent hover:border-black/5">
                    <div className="flex items-center gap-3">
                      <FileCode className="w-4 h-4 text-[#8A2387]" />
                      <span className="text-black font-medium">Dockerfile</span>
                    </div>
                    <span className="text-[10px] text-emerald-600 font-bold">
                      Detected
                    </span>
                  </div>

                  <div className="flex items-center justify-between p-2.5 hover:bg-neutral-50 rounded-xl transition-colors border border-transparent hover:border-black/5">
                    <div className="flex items-center gap-3">
                      <FileCode className="w-4 h-4 text-sky-500" />
                      <span className="text-black font-medium">
                        docker-compose.yml
                      </span>
                    </div>
                    <span className="text-[10px] text-neutral-400">
                      Multi-service
                    </span>
                  </div>

                  <div className="flex items-center justify-between p-2.5 hover:bg-neutral-50 rounded-xl transition-colors border border-transparent hover:border-black/5">
                    <div className="flex items-center gap-3">
                      <Database className="w-4 h-4 text-amber-500" />
                      <span className="text-black font-medium">schema.sql</span>
                    </div>
                    <span className="text-[10px] text-amber-600 font-bold">
                      Auto-Seed
                    </span>
                  </div>
                </div>
              </div>
            </div>

            {/* Right: Feature Descriptions with Vertical Accent Line */}
            <div className="flex flex-col justify-center max-w-lg">
              <h3 className={`text-3xl sm:text-4xl font-extrabold tracking-tight mb-10 leading-tight transition-colors ${isDark ? "text-white" : "text-[#1B1B1B]"}`}>
                The intelligent foundation
                <br />
                for your backend.
              </h3>

              <div className={`relative pl-8 border-l-2 space-y-9 transition-colors ${isDark ? "border-white/10" : "border-[#D9D7D0]"}`}>
                <div className="absolute left-[-2px] top-0 w-[2px] h-1/4 bg-gradient-to-b from-[#FF416C] to-[#FF4B2B]" />

                <div id="builder-engine" className="scroll-mt-32">
                  <h4 className={`text-lg font-bold mb-2 transition-colors ${isDark ? "text-white" : "text-[#1B1B1B]"}`}>
                    Deterministic Builder Engine
                  </h4>
                  <p className={`text-sm leading-relaxed transition-colors ${isDark ? "text-neutral-400" : "text-[#8C8880]"}`}>
                    Prioritizes explicit user configuration, existing
                    Dockerfiles, and intelligent Railpack buildpack detection
                    without brittle guesswork.
                  </p>
                </div>

                <div id="sidecars" className="scroll-mt-32">
                  <h4 className={`text-lg font-bold mb-2 transition-colors ${isDark ? "text-white" : "text-[#1B1B1B]"}`}>
                    Zero-Leak Sidecar Databases
                  </h4>
                  <p className={`text-sm leading-relaxed transition-colors ${isDark ? "text-neutral-400" : "text-[#8C8880]"}`}>
                    Automatically scans dependency descriptors and starts
                    isolated MySQL or PostgreSQL containers with volume
                    persistence and encrypted secrets.
                  </p>
                </div>

                <div id="gvisor-hardening" className="scroll-mt-32">
                  <h4 className={`text-lg font-bold mb-2 transition-colors ${isDark ? "text-white" : "text-[#1B1B1B]"}`}>
                    gVisor Kernel Hardening
                  </h4>
                  <p className={`text-sm leading-relaxed transition-colors ${isDark ? "text-neutral-400" : "text-[#8C8880]"}`}>
                    Multi-tenant process confinement powered by Google gVisor (<code className="font-mono text-xs px-1.5 py-0.5 rounded bg-black/5 dark:bg-white/10">runsc</code>). Virtualizes kernel syscalls with zero host kernel exposure.
                  </p>
                </div>

                <div id="buildkit-cache" className="scroll-mt-32">
                  <h4 className={`text-lg font-bold mb-2 transition-colors ${isDark ? "text-white" : "text-[#1B1B1B]"}`}>
                    BuildKit Cache Pipeline
                  </h4>
                  <p className={`text-sm leading-relaxed transition-colors ${isDark ? "text-neutral-400" : "text-[#8C8880]"}`}>
                    Distributed layer cache daemon over TCP. Reuses dependency build steps across container images to provide sub-second incremental sandbox rebuilds.
                  </p>
                </div>

                <div id="idle-reaping" className="scroll-mt-32">
                  <h4 className={`text-lg font-bold mb-2 transition-colors ${isDark ? "text-white" : "text-[#1B1B1B]"}`}>
                    Automated Inactivity Reaping
                  </h4>
                  <p className={`text-sm leading-relaxed transition-colors ${isDark ? "text-neutral-400" : "text-[#8C8880]"}`}>
                    Idempotent resource teardown that cleanly reclaims dormant containers, sidecars, networks, and ephemeral workspaces while keeping Git as your single source of truth.
                  </p>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* 6. Studio Section: Instant Deploy in Seconds */}
        <section
          id="features"
          className={`py-[100px] px-6 max-w-[1728px] mx-auto transition-colors ${
            isDark ? "bg-[#0C0C0F]" : "bg-[#FAF9F5]"
          }`}
        >
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-16 lg:gap-24 items-center">
            {/* Left Column: Feature Highlights */}
            <div className="flex flex-col justify-center max-w-lg order-2 lg:order-1">
              <h3
                className={`text-3xl sm:text-4xl font-extrabold tracking-tight mb-10 leading-tight transition-colors ${
                  isDark ? "text-white" : "text-[#1B1B1B]"
                }`}
              >
                Deploy full-stack
                <br />
                sandboxes in seconds.
              </h3>

              <div
                className={`relative pl-8 border-l-2 space-y-8 ${
                  isDark ? "border-white/10" : "border-[#D9D7D0]"
                }`}
              >
                <div className="absolute left-[-2px] top-0 w-[2px] h-1/4 bg-gradient-to-b from-[#FF416C] to-[#FF4B2B]" />

                <div id="ingress-gateway" className="scroll-mt-32">
                  <h4
                    className={`text-lg font-bold mb-1 transition-colors ${
                      isDark ? "text-white" : "text-[#1B1B1B]"
                    }`}
                  >
                    Multi-Port Ingress Gateway
                  </h4>
                  <p
                    className={`text-sm leading-relaxed transition-colors ${
                      isDark ? "text-neutral-400" : "text-[#8C8880]"
                    }`}
                  >
                    Reverse proxy automatically routes subdomains to internal
                    ports (3000, 5000, 8000, 8080) with zero manual port
                    mapping.
                  </p>
                </div>

                <div id="telemetry" className="scroll-mt-32">
                  <h4
                    className={`text-lg font-bold mb-1 transition-colors ${
                      isDark ? "text-white" : "text-[#1B1B1B]"
                    }`}
                  >
                    Live WebSocket Container Logs
                  </h4>
                  <p
                    className={`text-sm leading-relaxed transition-colors ${
                      isDark ? "text-neutral-400" : "text-[#8C8880]"
                    }`}
                  >
                    Stream container stdout and stderr to the browser with state
                    awareness across building, starting, and running phases.
                  </p>
                </div>

                <div>
                  <h4
                    className={`text-lg font-bold mb-1 transition-colors ${
                      isDark ? "text-white" : "text-[#1B1B1B]"
                    }`}
                  >
                    In-Browser File Editor & Git Push
                  </h4>
                  <p
                    className={`text-sm leading-relaxed transition-colors ${
                      isDark ? "text-neutral-400" : "text-[#8C8880]"
                    }`}
                  >
                    Edit source files live, test hot reload, and commit/push
                    directly back to GitHub branches without leaving the UI.
                  </p>
                </div>

                <div>
                  <h4 className="text-lg font-bold text-[#1B1B1B] mb-1">
                    Idle Hibernation & Resource Limits
                  </h4>
                  <p className="text-sm text-[#8C8880] leading-relaxed">
                    Automated background reapers pause idle sandboxes to conserve
                    host resources and wake instantly upon restart.
                  </p>
                </div>
              </div>
            </div>

            {/* Right Column: Interactive Studio Card */}
            <div className="bg-gradient-to-tr from-[#E0EAFC] to-[#CFDEF3] rounded-[36px] md:rounded-[44px] p-6 sm:p-10 h-[560px] sm:h-[640px] flex items-center justify-center relative overflow-hidden border border-[#D9D7D0]/40 shadow-inner order-1 lg:order-2">
              <div className="w-full max-w-lg bg-white/80 backdrop-blur-xl rounded-3xl shadow-2xl p-6 sm:p-8 border border-white/50">
                <div className="flex items-center gap-3 mb-6">
                  <div className="w-6 h-6 rounded-full bg-black flex items-center justify-center text-white text-[10px] font-bold">
                    A
                  </div>
                  <span className="text-xs font-bold text-neutral-500 uppercase tracking-wider">
                    SANDBOX CREATOR
                  </span>
                </div>

                <div className="bg-white/60 backdrop-blur-md rounded-2xl p-4 mb-5 border border-white/50 shadow-sm space-y-2">
                  <span className="text-[10px] uppercase font-bold text-neutral-400">
                    Repository URL
                  </span>
                  <p className="font-mono text-xs text-black truncate">
                    https://github.com/pharma/inventory-api
                  </p>
                </div>

                <div className="grid grid-cols-2 gap-3 mb-6 text-xs">
                  <div className="p-3 rounded-xl bg-white/60 border border-white/50">
                    <span className="text-[10px] uppercase text-neutral-400 block font-bold">
                      Builder
                    </span>
                    <span className="font-bold text-black">
                      Dockerfile (auto)
                    </span>
                  </div>
                  <div className="p-3 rounded-xl bg-white/60 border border-white/50">
                    <span className="text-[10px] uppercase text-neutral-400 block font-bold">
                      Database
                    </span>
                    <span className="font-bold text-black">MySQL 8.0</span>
                  </div>
                </div>

                <div className="flex justify-end">
                  <Link
                    href="/dashboard"
                    className="w-10 h-10 rounded-full bg-gradient-to-r from-[#FF416C] to-[#FF4B2B] flex items-center justify-center shadow-lg text-white hover:scale-105 transition-transform"
                  >
                    <Play className="w-4 h-4 ml-0.5 fill-current" />
                  </Link>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* 7. Team Use Cases Section */}
        <section
          id="use-cases"
          className={`py-[120px] px-6 max-w-[1728px] mx-auto flex flex-col items-center transition-colors ${
            isDark ? "bg-[#0C0C0F]" : "bg-[#FAF9F5]"
          }`}
        >
          <h2
            className={`text-3xl sm:text-4xl font-extrabold tracking-tight text-center mb-10 transition-colors ${
              isDark ? "text-white" : "text-[#1B1B1B]"
            }`}
          >
            Built for every engineering workflow.
          </h2>

          {/* Team Filter Pills */}
          <div className="flex flex-wrap justify-center gap-2 mb-14">
            {(["backend", "frontend", "qa", "devops"] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTeamTab(tab)}
                className={`px-6 py-2 rounded-full text-xs font-bold uppercase tracking-wider transition-all cursor-pointer ${
                  activeTeamTab === tab
                    ? isDark
                      ? "bg-white text-black shadow-md"
                      : "bg-black text-white shadow-md"
                    : isDark
                    ? "bg-[#18181E] text-neutral-300 hover:bg-[#202028] border border-white/10 shadow-sm"
                    : "bg-white text-neutral-600 hover:bg-neutral-50 border border-[#D9D7D0]/60 shadow-sm"
                }`}
              >
                {tab}
              </button>
            ))}
          </div>

          {/* 4 Cards Grid */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-8 w-full max-w-6xl">
            {teamUseCases[activeTeamTab].map((item, index) => (
              <div
                key={index}
                className={`relative h-[380px] rounded-[32px] p-8 flex flex-col justify-end group overflow-hidden shadow-sm border ${
                  isDark ? "border-white/10" : "border-black/5"
                }`}
              >
                <img
                  alt={item.title}
                  className="absolute inset-0 w-full h-full object-cover transition-transform duration-700 group-hover:scale-105"
                  src={item.image}
                />
                <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-black/30 to-transparent" />
                <div className="relative z-10 text-white">
                  <h4 className="text-xl font-bold mb-2">{item.title}</h4>
                  <p className="text-xs text-white/80 leading-relaxed max-w-md">
                    {item.desc}
                  </p>
                </div>
              </div>
            ))}
          </div>
        </section>

        {/* 8. Testimonial Section */}
        <section
          className={`py-[140px] px-6 max-w-[1200px] mx-auto flex flex-col items-center text-center transition-colors ${
            isDark ? "bg-[#0C0C0F]" : "bg-[#FAF9F5]"
          }`}
        >
          <h2
            className={`text-[32px] sm:text-[44px] md:text-[52px] leading-[1.12] tracking-tight mb-12 max-w-[980px] font-normal text-balance transition-colors ${
              isDark ? "text-white" : "text-[#1B1B1B]"
            }`}
          >
            &ldquo;API Sandbox liberated our engineers from local Docker drift
            and broken databases, allowing us to focus on shipping reliable APIs
            with complete confidence.&rdquo;
          </h2>

          <div className="flex items-center gap-4">
            <div className="w-14 h-14 rounded-full bg-neutral-200 overflow-hidden shadow-md">
              <img
                alt="Alex Morgan"
                className="w-full h-full object-cover"
                src="https://lh3.googleusercontent.com/aida-public/AB6AXuD_dQOYUXm-9ocUk-1cRA5ZyP6tu7OdWwNIU7SRhouWt3u89anzj3A5_pr7FGCCICOiz6a2FoSpC-bN53AYbiVQ-spgKPawBtGZtLq-9c6QyNclym7oEuOsCnQllJenEGuMkAeM3mwnTOLdgJakEE63ozCYcDA2l-C2EQnZiozAakUbDrwJG_jxTwrvV9I5p2oiVPXIDBv-rltdG9P0TX6X2OUHQcwckpskXXJEawplcI8ff_rzn1YnIDgrniF27Vjc0Lz4LKcazue9"
              />
            </div>
            <div className="text-left">
              <p
                className={`text-sm font-bold transition-colors ${
                  isDark ? "text-white" : "text-black"
                }`}
              >
                Alex Morgan
              </p>
              <p className="text-xs text-neutral-500">
                VP of Platform Engineering, Arcform
              </p>
            </div>
          </div>
        </section>

        {/* 9. Latest Updates Section */}
        <section
          className={`py-[100px] px-6 max-w-[1728px] mx-auto transition-colors ${
            isDark ? "bg-[#0C0C0F]" : "bg-[#FAF9F5]"
          }`}
        >
          <div className="flex justify-between items-end mb-12 max-w-6xl mx-auto">
            <div>
              <span className="text-[11px] font-bold uppercase tracking-widest text-neutral-400 block mb-2">
                Changelog
              </span>
              <h2
                className={`text-3xl sm:text-4xl font-extrabold tracking-tight transition-colors ${
                  isDark ? "text-white" : "text-[#1B1B1B]"
                }`}
              >
                Platform Releases
              </h2>
            </div>
            <Link
              href="/dashboard"
              className={`text-xs font-bold uppercase tracking-wider border-b pb-1 hover:text-[#FF416C] hover:border-[#FF416C] transition-colors ${
                isDark ? "text-white border-white" : "text-black border-black"
              }`}
            >
              Open Dashboard
            </Link>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-8 max-w-6xl mx-auto">
            <div className="group cursor-pointer">
              <div
                className={`rounded-[32px] overflow-hidden mb-5 border shadow-md aspect-[4/3] ${
                  isDark
                    ? "border-white/10 bg-[#16161B]"
                    : "border-black/5 bg-neutral-100"
                }`}
              >
                <img
                  alt="Deterministic Builder & Railpack Engine"
                  className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500"
                  src="https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?q=80&w=2064&auto=format&fit=crop"
                />
              </div>
              <p className="text-[11px] font-mono text-neutral-500 mb-2">
                Engine • v2.4 Release
              </p>
              <h4
                className={`text-lg font-bold group-hover:text-[#FF416C] transition-colors leading-snug ${
                  isDark ? "text-white" : "text-black"
                }`}
              >
                Deterministic Builder & Precedence Resolver
              </h4>
            </div>

            <div className="group cursor-pointer">
              <div
                className={`rounded-[32px] overflow-hidden mb-5 border shadow-md aspect-[4/3] ${
                  isDark
                    ? "border-white/10 bg-[#16161B]"
                    : "border-black/5 bg-neutral-100"
                }`}
              >
                <img
                  alt="Sidecar Database Isolation"
                  className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500"
                  src="https://images.unsplash.com/photo-1634017839464-5c339ebe3cb4?q=80&w=2000&auto=format&fit=crop"
                />
              </div>
              <p className="text-[11px] font-mono text-neutral-500 mb-2">
                Database • v2.3 Release
              </p>
              <h4
                className={`text-lg font-bold group-hover:text-[#FF416C] transition-colors leading-snug ${
                  isDark ? "text-white" : "text-black"
                }`}
              >
                Zero-Leak Sidecar Database Orchestration
              </h4>
            </div>

            <div className="group cursor-pointer">
              <div
                className={`rounded-[32px] overflow-hidden mb-5 border shadow-md aspect-[4/3] ${
                  isDark
                    ? "border-white/10 bg-[#16161B]"
                    : "border-black/5 bg-neutral-100"
                }`}
              >
                <img
                  alt="Idempotent Teardown"
                  className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500"
                  src="https://images.unsplash.com/photo-1770791779732-7d1ef12524f2?q=80&w=2021&auto=format&fit=crop"
                />
              </div>
              <p className="text-[11px] font-mono text-neutral-500 mb-2">
                Reliability • v2.2 Release
              </p>
              <h4 className="text-lg font-bold text-black group-hover:text-[#FF416C] transition-colors leading-snug">
                Idempotent Teardown & Gateway Healthcheck Contract
              </h4>
            </div>
          </div>
        </section>

        {/* 10. Large CTA Section */}
        <section className="py-[120px] px-6">
          <div className="max-w-4xl mx-auto flex flex-col items-center text-center">
            <div
              className={`w-12 h-12 rounded-full border flex items-center justify-center mb-8 ${
                isDark ? "border-white/20 text-white" : "border-black/20 text-black"
              }`}
            >
              <span className="font-extrabold text-xl leading-none">
                A
              </span>
            </div>

            <h2
              className={`text-[44px] sm:text-[64px] font-extrabold tracking-tighter leading-[1.05] mb-8 text-balance transition-colors ${
                isDark ? "text-white" : "text-[#1B1B1B]"
              }`}
            >
              Experience the future of ephemeral cloud sandboxes
            </h2>

            <div className="flex flex-col sm:flex-row gap-4">
              <Link
                href="/dashboard"
                className={`px-8 py-3.5 rounded-full text-xs font-bold uppercase tracking-wider transition-all shadow-xl ${
                  isDark
                    ? "bg-white text-black hover:bg-neutral-200"
                    : "bg-black text-white hover:bg-neutral-800"
                }`}
              >
                Launch Sandbox
              </Link>
              <Link
                href="/login"
                className={`px-8 py-3.5 rounded-full border text-xs font-bold uppercase tracking-wider transition-all shadow-sm ${
                  isDark
                    ? "border-white/30 text-white hover:bg-white hover:text-black"
                    : "border-black/30 text-black hover:bg-black hover:text-white"
                }`}
              >
                Sign In
              </Link>
            </div>
          </div>
        </section>
      </main>

      {/* 11. Signature Lumio Curved Dark Footer */}
      <footer className="bg-black text-white pt-20 pb-12 px-6 sm:px-12 rounded-t-[44px] md:rounded-t-[64px] relative z-20">
        <div className="max-w-[1440px] mx-auto">
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-12 mb-16">
            {/* Brand Column */}
            <div className="flex flex-col gap-6">
              <div className="flex items-center gap-2.5">
                <div className="w-8 h-8 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center text-white font-bold text-xs shrink-0">
                  A
                </div>
                <span className="font-bold text-base tracking-tight text-white">
                  API Sandbox
                </span>
              </div>
              <p className="text-xs text-white/50 leading-relaxed max-w-xs">
                The operating system for modern ephemeral cloud environments.
                Empowering engineering teams with zero-drift disposable
                sandboxes.
              </p>
              <div className="flex gap-3">
                <button
                  type="button"
                  onClick={handleShare}
                  aria-label="Share platform"
                  title="Share platform"
                  className="w-9 h-9 rounded-full border border-white/10 flex items-center justify-center hover:bg-white/10 transition-colors text-white/70 hover:text-white cursor-pointer"
                >
                  <Share2 className="w-4 h-4" />
                </button>
                <Link
                  href="/"
                  aria-label="API Sandbox Home"
                  title="Home"
                  className="w-9 h-9 rounded-full border border-white/10 flex items-center justify-center hover:bg-white/10 transition-colors text-white/70 hover:text-white"
                >
                  <Globe className="w-4 h-4" />
                </Link>
                <Link
                  href="/dashboard"
                  aria-label="Live Telemetry & Dashboard"
                  title="Live Telemetry"
                  className="w-9 h-9 rounded-full border border-white/10 flex items-center justify-center hover:bg-white/10 transition-colors text-white/70 hover:text-white"
                >
                  <Radio className="w-4 h-4" />
                </Link>
              </div>
            </div>

            {/* Navigation Columns */}
            <div>
              <h4 className="text-[10px] font-bold text-white mb-6 uppercase tracking-widest">
                Platform
              </h4>
              <ul className="flex flex-col gap-3 text-xs text-white/60">
                <li>
                  <a
                    href="#builder-engine"
                    className="hover:text-white transition-colors"
                  >
                    Builder Engine
                  </a>
                </li>
                <li>
                  <a
                    href="#sidecars"
                    className="hover:text-white transition-colors"
                  >
                    Sidecar Databases
                  </a>
                </li>
                <li>
                  <a
                    href="#ingress-gateway"
                    className="hover:text-white transition-colors"
                  >
                    Ingress Gateway
                  </a>
                </li>
                <li>
                  <a
                    href="#telemetry"
                    className="hover:text-white transition-colors"
                  >
                    WebSocket Telemetry
                  </a>
                </li>
              </ul>
            </div>

            <div>
              <h4 className="text-[10px] font-bold text-white mb-6 uppercase tracking-widest">
                Architecture
              </h4>
              <ul className="flex flex-col gap-3 text-xs text-white/60">
                <li>
                  <a href="#gvisor-hardening" className="hover:text-white transition-colors">
                    gVisor Hardening
                  </a>
                </li>
                <li>
                  <a href="#buildkit-cache" className="hover:text-white transition-colors">
                    BuildKit Cache
                  </a>
                </li>
                <li>
                  <a href="#idle-reaping" className="hover:text-white transition-colors">
                    Idle Reaping
                  </a>
                </li>
                <li>
                  <Link href="/terms#security" className="hover:text-white transition-colors">
                    Security Policy
                  </Link>
                </li>
              </ul>
            </div>

            <div>
              <h4 className="text-[10px] font-bold text-white mb-6 uppercase tracking-widest">
                Resources
              </h4>
              <ul className="flex flex-col gap-3 text-xs text-white/60">
                <li>
                  <Link
                    href="/dashboard"
                    className="hover:text-white transition-colors"
                  >
                    Dashboard
                  </Link>
                </li>
                <li>
                  <Link
                    href="/login"
                    className="hover:text-white transition-colors"
                  >
                    Authentication
                  </Link>
                </li>
                <li>
                  <Link
                    href="/terms"
                    className="hover:text-white transition-colors"
                  >
                    Terms of Service
                  </Link>
                </li>
                <li>
                  <Link
                    href="/privacy"
                    className="hover:text-white transition-colors"
                  >
                    Privacy Policy
                  </Link>
                </li>
              </ul>
            </div>
          </div>

          <div className="pt-8 border-t border-white/10 flex flex-col md:flex-row justify-between items-center gap-4 text-[11px] text-white/40">
            <p>© 2026 API Sandbox Links. All rights reserved.</p>
            <div className="flex gap-6">
              <Link
                href="/privacy"
                className="hover:text-white transition-colors"
              >
                Privacy Policy
              </Link>
              <Link href="/terms" className="hover:text-white transition-colors">
                Terms of Service
              </Link>
            </div>
          </div>
        </div>
      </footer>
    </div>
  );
}
