import { NextRequest, NextResponse } from "next/server";
import { filterSandboxesForProject } from "@/lib/projects-store";
import { BACKEND_ORIGIN } from "@/lib/api-config";

export async function GET(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  const backend = BACKEND_ORIGIN;
  const cookie = req.headers.get("cookie") || "";

  try {
    const res = await fetch(`${backend}/api/sandboxes`, {
      headers: { cookie },
      cache: "no-store",
    });

    if (res.ok) {
      const data = await res.json();
      const allSandboxes = Array.isArray(data) ? data : data?.sandboxes || [];
      const projectSandboxes = filterSandboxesForProject(id, allSandboxes);

      const workspaces = projectSandboxes.map((sb: any) => ({
        id: `ws-${sb.id}`,
        type: sb.builder || "sandbox",
        environmentId: sb.id,
        environment: {
          status: sb.status || "IDLE",
        },
        repositories: [
          {
            id: `repo-${sb.id}`,
            branch: sb.branch || "main",
            workingDirectory: sb.subdomain || "",
          },
        ],
      }));

      return NextResponse.json(workspaces);
    }
  } catch {
    // Fallback
  }

  return NextResponse.json([]);
}
