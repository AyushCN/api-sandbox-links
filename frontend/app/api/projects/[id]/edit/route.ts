import { NextRequest, NextResponse } from "next/server";
import { filterSandboxesForProject } from "@/lib/projects-store";
import { BACKEND_ORIGIN } from "@/lib/api-config";

export async function POST(
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
      const list = Array.isArray(data) ? data : data?.sandboxes || [];
      const projectSandboxes = filterSandboxesForProject(id, list);

      const first = projectSandboxes.find((sb: any) => sb.status !== "DELETED") || projectSandboxes[0];
      if (first) {
        return NextResponse.json({ environmentId: first.id });
      }
    }
  } catch {
    // Fallback
  }

  return NextResponse.json(
    { error: "No active sandboxes found in this workspace project." },
    { status: 404 }
  );
}
