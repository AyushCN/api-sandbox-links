"use client";

import React, { useEffect, useState } from "react";
import { Contrast } from "lucide-react";
import { useTheme } from "@/lib/theme";

export function ThemeToggle({ className = "" }: { className?: string }) {
  const { theme, toggleTheme } = useTheme();
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  const isDark = mounted ? theme === "dark" : false;

  return (
    <button
      type="button"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        toggleTheme();
      }}
      aria-label="Toggle display theme"
      title={isDark ? "Switch to light mode" : "Switch to dark mode"}
      className={`group relative inline-flex items-center justify-center w-8 h-8 rounded-full border transition-all duration-300 cursor-pointer shrink-0 ${
        isDark
          ? "border-white/20 bg-white/[0.08] hover:bg-white/[0.18] text-white"
          : "border-black/15 bg-black/[0.05] hover:bg-black/[0.1] text-neutral-800 shadow-xs"
      } ${className}`}
    >
      <Contrast
        className={`w-3.5 h-3.5 transition-transform duration-500 ease-out ${
          isDark ? "rotate-180 text-white" : "rotate-0 text-neutral-800"
        } group-hover:scale-110`}
      />
      <span className="sr-only">Toggle theme</span>
    </button>
  );
}
