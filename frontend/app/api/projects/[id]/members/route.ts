import { NextRequest, NextResponse } from "next/server";
import { getProjectById } from "@/lib/projects-store";

export async function GET(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  const project = getProjectById(id);
  const collabs = (project?.collaborators && project.collaborators.length > 0)
    ? project.collaborators
    : [
        {
          id: "m-owner",
          userId: "user-ayushcn",
          role: "OWNER",
          status: "ACTIVE",
          user: {
            username: "AyushCN",
            email: "ayush@api-sandbox.local",
          },
        },
      ];
  return NextResponse.json(collabs);
}
