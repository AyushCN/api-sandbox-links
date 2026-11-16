import { NextRequest, NextResponse } from "next/server";
import { getProjectById, filterSandboxesForProject, addProjectRepository } from "@/lib/projects-store";
import { BACKEND_ORIGIN } from "@/lib/api-config";

export async function GET(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  const backend = BACKEND_ORIGIN;
  const cookie = req.headers.get("cookie") || "";
  const proj = getProjectById(id);

  const repos: any[] = [];
  const seen = new Set<string>();

  // Add any explicitly registered repositories in this project
  if (proj?.repositories && Array.isArray(proj.repositories)) {
    for (const r of proj.repositories) {
      if (!seen.has(r.gitUrl)) {
        seen.add(r.gitUrl);
        repos.push(r);
      }
    }
  }

  try {
    const res = await fetch(`${backend}/api/sandboxes`, {
      headers: { cookie },
      cache: "no-store",
    });

    if (res.ok) {
      const data = await res.json();
      const allSandboxes = Array.isArray(data) ? data : data?.sandboxes || [];
      const projectSandboxes = filterSandboxesForProject(id, allSandboxes);

      for (const sb of projectSandboxes) {
        const url = sb.repo_url || sb.gitUrl || sb.url;
        if (url && !seen.has(url)) {
          seen.add(url);
          repos.push({
            id: `repo-${sb.id}`,
            name: sb.name || sb.subdomain || "sandbox-repo",
            gitUrl: url,
            defaultBranch: sb.branch || "main",
          });
        }
      }
    }
  } catch {
    // Fallback if backend is unreachable
  }

  return NextResponse.json(repos);
}

export async function POST(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  try {
    const body = await req.json();
    const { name, gitUrl, defaultBranch } = body;
    if (!name || !gitUrl) {
      return NextResponse.json({ error: "Name and gitUrl are required" }, { status: 400 });
    }
    const newRepo = addProjectRepository(id, { name, gitUrl, defaultBranch });
    if (!newRepo) {
      return NextResponse.json({ error: "Project not found" }, { status: 404 });
    }
    return NextResponse.json(newRepo, { status: 201 });
  } catch (err: unknown) {
    return NextResponse.json({ error: (err as Error).message }, { status: 500 });
  }
}
