import { NextRequest, NextResponse } from "next/server";
import { filterSandboxesForProject } from "@/lib/projects-store";

export async function GET(req: NextRequest) {
  const backend = process.env.BACKEND_URL || "http://localhost:8080";
  const cookie = req.headers.get("cookie") || "";
  const projectId = req.nextUrl.searchParams.get("projectId");

  try {
    const res = await fetch(`${backend}/api/sandboxes`, {
      headers: { cookie },
      cache: "no-store",
    });

    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      return NextResponse.json(data, { status: res.status });
    }

    const data = await res.json();
    let list = Array.isArray(data) ? data : data?.sandboxes || [];

    if (projectId) {
      list = filterSandboxesForProject(projectId, list);
    }

    return NextResponse.json({ sandboxes: list });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 500 }
    );
  }
}
