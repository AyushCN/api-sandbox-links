import { NextRequest, NextResponse } from "next/server";
import { filterSandboxesForProject } from "@/lib/projects-store";
import { BACKEND_ORIGIN } from "@/lib/api-config";

export async function GET(req: NextRequest) {
  const backend = BACKEND_ORIGIN;
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
