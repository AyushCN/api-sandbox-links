import { NextRequest, NextResponse } from "next/server";
import { removeCollaborator } from "@/lib/projects-store";

export async function DELETE(
  req: NextRequest,
  props: { params: Promise<{ id: string; userId: string }> }
) {
  const { id, userId } = await props.params;
  try {
    const ok = removeCollaborator(id, userId);
    if (!ok) {
      return NextResponse.json({ error: "Collaborator not found" }, { status: 404 });
    }
    return NextResponse.json({ success: true });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 500 }
    );
  }
}

