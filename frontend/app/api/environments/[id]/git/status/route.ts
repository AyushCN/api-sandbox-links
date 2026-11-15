import { NextRequest, NextResponse } from "next/server";
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
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const cookie = req.headers.get("cookie") || "";
  const authHeader = req.headers.get("authorization") || "";

  let subdomain = "";
  let branch = "master";

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
      branch = data.branch || data.githubBranch || "master";
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
      const { stdout: statusOut } = await execFileAsync(
        "git",
        ["status", "--porcelain", "-b"],
        { cwd: workDir, timeout: 5000 }
      );

      const lines = statusOut.split("\n");
      const firstLine = lines[0] || "";
      const isDirty = lines.slice(1).some((l) => l.trim().length > 0);

      // Parse branch name from "## master...origin/master" or "## master"
      const branchMatch = firstLine.match(/^##\s+([^\s.]+)/);
      if (branchMatch && branchMatch[1]) {
        branch = branchMatch[1];
      }

      return NextResponse.json({
        branch,
        clean: !isDirty,
        dirty: isDirty,
        ahead: 0,
        behind: 0,
      });
    } catch {
      // Fallback if git binary fails
    }
  }

  return NextResponse.json({
    branch,
    clean: true,
    dirty: false,
    ahead: 0,
    behind: 0,
  });
}
