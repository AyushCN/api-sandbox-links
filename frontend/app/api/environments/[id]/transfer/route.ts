import { NextRequest, NextResponse } from "next/server";
import {
  getProjects,
  saveProjects,
  associateSandboxWithProject,
} from "@/lib/projects-store";

export async function POST(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  try {
    const body = await req.json().catch(() => ({}));
    const projectId = body.projectId;
    if (!projectId) {
      return NextResponse.json(
        { error: "projectId is required" },
        { status: 400 }
      );
    }

    const projects = getProjects();
    for (const p of projects) {
      if (p.sandboxIds?.includes(id)) {
        p.sandboxIds = p.sandboxIds.filter((s) => s !== id);
      }
    }
    saveProjects(projects);
    associateSandboxWithProject(projectId, id);

    return NextResponse.json({ success: true, projectId, sandboxId: id });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 500 }
    );
  }
}
