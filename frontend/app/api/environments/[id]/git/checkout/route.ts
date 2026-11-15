import { NextRequest, NextResponse } from "next/server";
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
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const cookie = req.headers.get("cookie") || "";

  let body: { ref?: string; force?: boolean } = {};
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "Invalid JSON body" }, { status: 400 });
  }

  const rawRef = body.ref?.trim();
  if (!rawRef) {
    return NextResponse.json({ error: "Branch ref is required" }, { status: 400 });
  }

  const targetBranch = rawRef.replace(/^remotes\/origin\//, "");

  // 1. Fetch sandbox info from backend
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
  if (!workDir || !fs.existsSync(path.join(workDir, ".git"))) {
    return NextResponse.json(
      { error: "Working directory not found for this sandbox" },
      { status: 404 }
    );
  }

  // 2. Check if working tree is dirty
  try {
    const { stdout: statusOut } = await execFileAsync("git", ["status", "--porcelain"], {
      cwd: workDir,
      timeout: 5000,
    });
    const isDirty = statusOut.trim().length > 0;
    if (isDirty && !body.force) {
      return NextResponse.json(
        { error: "Working tree is dirty. Commit or stash your changes before switching branches." },
        { status: 400 }
      );
    }

    if (isDirty && body.force) {
      await execFileAsync("git", ["reset", "--hard", "HEAD"], { cwd: workDir, timeout: 5000 });
      await execFileAsync("git", ["clean", "-fd"], { cwd: workDir, timeout: 5000 });
    }

    // 3. Checkout branch
    try {
      await execFileAsync("git", ["checkout", targetBranch], { cwd: workDir, timeout: 5000 });
    } catch {
      // Try checkout from remote tracking
      await execFileAsync("git", ["checkout", "-B", targetBranch, `origin/${targetBranch}`], {
        cwd: workDir,
        timeout: 5000,
      });
    }

    // 4. Update the database record in PostgreSQL via Go backend
    await fetch(`${backend}/api/sandboxes/${id}/branch`, {
      method: "POST",
      headers: {
        cookie,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ branch: targetBranch }),
    }).catch(() => {});

    return NextResponse.json({
      success: true,
      branch: targetBranch,
      message: `Checked out branch ${targetBranch}`,
    });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message || "Checkout failed" },
      { status: 500 }
    );
  }
}

