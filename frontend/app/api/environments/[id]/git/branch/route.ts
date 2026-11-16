import { NextRequest, NextResponse } from "next/server";
import { BACKEND_ORIGIN } from "@/lib/api-config";
import { execFile } from "child_process";
import { promisify } from "util";
import fs from "fs";
import path from "path";

const execFileAsync = promisify(execFile);

export async function POST(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  const backend = BACKEND_ORIGIN;
  const cookie = req.headers.get("cookie") || "";

  try {
    const body = await req.json().catch(() => ({}));
    const newBranch = (body.branch || "").trim();
    if (!newBranch) {
      return NextResponse.json({ error: "Branch name is required" }, { status: 400 });
    }

    // 1. Fetch sandbox info
    let subdomain = "";
    try {
      const res = await fetch(`${backend}/api/sandboxes/${id}`, {
        headers: { cookie },
        cache: "no-store",
      });
      if (res.ok) {
        const data = await res.json();
        subdomain = data.subdomain || "";
      }
    } catch {
      // Ignore
    }

    const workDir = subdomain ? path.join("/tmp/api-sandbox-links/work", subdomain) : "";
    if (workDir && fs.existsSync(path.join(workDir, ".git"))) {
      await execFileAsync("git", ["checkout", "-b", newBranch], { cwd: workDir, timeout: 5000 });
    }

    // 2. Update PostgreSQL database
    await fetch(`${backend}/api/sandboxes/${id}/branch`, {
      method: "POST",
      headers: {
        cookie,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ branch: newBranch }),
    }).catch(() => {});

    return NextResponse.json({
      success: true,
      branch: newBranch,
      message: `Created and checked out branch ${newBranch}`,
    });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message || "Failed to create branch" },
      { status: 500 }
    );
  }
}
