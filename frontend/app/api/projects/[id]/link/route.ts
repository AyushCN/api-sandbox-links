import { NextRequest, NextResponse } from "next/server";
import { associateSandboxWithProject } from "@/lib/projects-store";

export async function POST(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  try {
    const body = await req.json().catch(() => ({}));
    const sandboxId = body.sandboxId;
    if (sandboxId) {
      associateSandboxWithProject(id, sandboxId);
    }
    return NextResponse.json({ success: true, projectId: id, sandboxId });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 500 }
    );
  }
}

