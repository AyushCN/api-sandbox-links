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

  let subdomain = "";
  let currentBranch = "master";

  try {
    const res = await fetch(`${backend}/api/sandboxes/${id}`, {
      headers: { cookie },
      cache: "no-store",
    });

    if (res.ok) {
      const data = await res.json();
      subdomain = data.subdomain || "";
      currentBranch = data.branch || data.githubBranch || "master";
    }
  } catch {
    // Backend unreachable
  }

  const workDir = subdomain
    ? path.join("/tmp/api-sandbox-links/work", subdomain)
    : "";

  if (workDir && fs.existsSync(path.join(workDir, ".git"))) {
    try {
      const { stdout } = await execFileAsync("git", ["branch", "-a"], {
        cwd: workDir,
        timeout: 5000,
      });

      const branchSet = new Set<string>();
      if (currentBranch) branchSet.add(currentBranch);

      for (const line of stdout.split("\n")) {
        const clean = line.replace(/^\*?\s+/, "").trim();
        if (!clean || clean.includes("->")) continue;
        branchSet.add(clean.replace(/^remotes\/origin\//, ""));
      }

      const branches = Array.from(branchSet);
      if (branches.length > 0) {
        return NextResponse.json(branches);
      }
    } catch {
      // Fallback
    }
  }

  return NextResponse.json([currentBranch]);
}
