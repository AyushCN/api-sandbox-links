import { NextRequest, NextResponse } from "next/server";
import { getProjects, addProject } from "@/lib/projects-store";
import { BACKEND_ORIGIN } from "@/lib/api-config";

export async function GET() {
  const projects = getProjects();
  return NextResponse.json(projects);
}

export async function POST(req: NextRequest) {
  try {
    const body = await req.json().catch(() => ({}));
    const name = (body.name || "").trim();
    if (!name) {
      return NextResponse.json({ error: "Project name is required" }, { status: 400 });
    }
    const description = (body.description || "").trim();

    // Determine creator
    let creator = body.creator;
    if (!creator?.username) {
      const backend = BACKEND_ORIGIN;
      const cookie = req.headers.get("cookie") || "";
      const authHeader = req.headers.get("authorization") || "";
      try {
        const meRes = await fetch(`${backend}/api/me`, {
          headers: {
            cookie,
            ...(authHeader ? { authorization: authHeader } : {}),
          },
          cache: "no-store",
        });
        if (meRes.ok) {
          const meData = await meRes.json();
          if (meData?.username) {
            creator = {
              userId: meData.id || `user-${meData.username.toLowerCase()}`,
              username: meData.username,
              email: meData.email || `${meData.username.toLowerCase()}@api-sandbox.local`,
            };
          }
        }
      } catch {
        // Fallback to default owner
      }
    }

    const newProj = addProject(name, description, creator);
    return NextResponse.json(newProj, { status: 201 });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 500 }
    );
  }
}
