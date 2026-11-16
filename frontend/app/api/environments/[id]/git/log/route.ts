import { NextRequest, NextResponse } from "next/server";
import { BACKEND_ORIGIN } from "@/lib/api-config";
import { execFile } from "child_process";
import { promisify } from "util";
import fs from "fs";
import path from "path";

const execFileAsync = promisify(execFile);

export async function GET(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  const backend = BACKEND_ORIGIN;
  const cookie = req.headers.get("cookie") || "";
  const authHeader = req.headers.get("authorization") || "";

  let subdomain = "";
  try {
    const headers: Record<string, string> = {};
    if (cookie) headers["cookie"] = cookie;
    if (authHeader) headers["authorization"] = authHeader;

    const res = await fetch(`${backend}/api/sandboxes/${id}`, {
      headers,
      cache: "no-store",
    });

    if (res.ok) {
      const data = await res.json();
      subdomain = data.subdomain || "";
    }
  } catch {
    // Backend unreachable
  }

  let workDir = subdomain
    ? path.join("/tmp/api-sandbox-links/work", subdomain)
    : "";

  if (!workDir || !fs.existsSync(workDir)) {
    try {
      const baseDir = "/tmp/api-sandbox-links/work";
      if (fs.existsSync(baseDir)) {
        const dirs = fs.readdirSync(baseDir);
        const shortId = id.slice(0, 6);
        const match = dirs.find(
          (d) =>
            d.includes(id) ||
            (subdomain && d.includes(subdomain)) ||
            d.includes(shortId)
        );
        if (match) {
          workDir = path.join(baseDir, match);
        } else if (dirs.length === 1) {
          workDir = path.join(baseDir, dirs[0]);
        }
      }
    } catch {
      // Ignore
    }
  }

  if (workDir && fs.existsSync(path.join(workDir, ".git"))) {
    try {
      const { stdout } = await execFileAsync(
        "git",
        ["log", "-n", "10", "--format=%H|%h|%s|%an|%cI"],
        { cwd: workDir, timeout: 5000 }
      );

      const commits = stdout
        .trim()
        .split("\n")
        .filter(Boolean)
        .map((line) => {
          const [hash, shortHash, message, author, date] = line.split("|");
          return {
            hash: hash || "",
            shortHash: shortHash || "",
            message: message || "Update repository",
            author: author || "Developer",
            date: date || new Date().toISOString(),
          };
        });

      return NextResponse.json({ commits });
    } catch {
      // Fallback
    }
  }

  return NextResponse.json({ commits: [] });
}

