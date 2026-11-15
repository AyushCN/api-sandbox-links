import { NextRequest, NextResponse } from "next/server";
import { inviteCollaborator } from "@/lib/projects-store";

export async function POST(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  try {
    const body = await req.json().catch(() => ({}));
    const { identifier, role } = body;
    if (!identifier) {
      return NextResponse.json(
        { error: "Username or email is required" },
        { status: 400 }
      );
    }
    const collab = inviteCollaborator(id, identifier, role || "EDITOR");
    if (!collab) {
      return NextResponse.json(
        { error: "Project not found" },
        { status: 404 }
      );
    }
    return NextResponse.json({ success: true, collaborator: collab });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 500 }
    );
  }
}

