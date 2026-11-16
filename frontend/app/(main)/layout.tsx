import Link from "next/link";
import { LogoutButton } from "@/components/LogoutButton";
import { UserAvatar } from "@/components/UserAvatar";
import { ThemeToggle } from "@/components/ThemeToggle";

export default function MainLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <div className="min-h-screen bg-background text-on-surface flex flex-col font-sans selection:bg-[#FF416C] selection:text-white antialiased transition-colors duration-200">
      <header className="sticky top-0 z-50 w-full px-4 sm:px-8 py-3 bg-surface/85 backdrop-blur-xl border-b border-outline-variant transition-colors">
        <div className="max-w-7xl mx-auto flex items-center justify-between h-12">
          <div className="flex items-center gap-8">
            <Link
              href="/dashboard"
              className="flex items-center gap-2.5 hover:opacity-90 transition-opacity"
            >
              <div className="w-8 h-8 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center text-white font-bold text-xs shadow-md">
                A
              </div>
              <span className="font-bold tracking-tight text-base text-on-surface">
                API Sandbox
              </span>
            </Link>

            <nav className="hidden sm:flex items-center gap-6 text-xs font-semibold uppercase tracking-wider text-on-surface-variant">
              <Link
                href="/dashboard"
                className="hover:text-on-surface transition-colors"
              >
                Sandboxes
              </Link>
              <Link
                href="/projects"
                className="hover:text-on-surface transition-colors"
              >
                Projects
              </Link>
            </nav>
          </div>

          <div className="flex items-center gap-3">
            <ThemeToggle />
            <Link
              href="/upload"
              className="px-4 py-1.5 rounded-full bg-on-surface text-surface text-xs font-bold uppercase tracking-wider hover:opacity-90 transition-all shadow-sm hidden sm:inline-flex items-center gap-1.5"
            >
              + New Sandbox
            </Link>
            <UserAvatar />
            <LogoutButton />
          </div>
        </div>
      </header>

      <main className="flex-1 w-full max-w-7xl mx-auto px-4 sm:px-8 py-8">
        {children}
      </main>

      <footer className="w-full border-t border-outline-variant bg-surface-container-lowest/40 py-6 px-4 sm:px-8 transition-colors mt-auto">
        <div className="max-w-7xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-4 text-xs text-on-surface-variant">
          <div className="flex items-center gap-2">
            <div className="w-5 h-5 rounded-full bg-gradient-to-tr from-[#FF416C] to-[#FF4B2B] flex items-center justify-center text-white font-bold text-[10px]">
              A
            </div>
            <span>© 2026 API Sandbox Links. All rights reserved.</span>
          </div>
          <div className="flex items-center gap-6">
            <Link href="/" className="hover:text-on-surface transition-colors">Home</Link>
            <Link href="/dashboard" className="hover:text-on-surface transition-colors">Sandboxes</Link>
            <Link href="/projects" className="hover:text-on-surface transition-colors">Projects</Link>
            <Link href="/privacy" className="hover:text-on-surface transition-colors">Privacy Policy</Link>
            <Link href="/terms" className="hover:text-on-surface transition-colors">Terms of Service</Link>
          </div>
        </div>
      </footer>
    </div>
  );
}
