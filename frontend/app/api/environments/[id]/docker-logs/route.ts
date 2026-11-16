import { NextRequest, NextResponse } from "next/server";
import { BACKEND_ORIGIN } from "@/lib/api-config";

export async function GET(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  const backend = BACKEND_ORIGIN;
  const cookie = req.headers.get("cookie") || "";

  try {
    const res = await fetch(`${backend}/api/sandboxes/${id}/logs`, {
      headers: { cookie },
      cache: "no-store",
    });

    if (!res.ok) {
      if (res.status === 404) {
        return NextResponse.json(
          { status: "NOT_FOUND", available: false, message: "Sandbox not found" },
          { status: 404 }
        );
      }
      const errData = await res.json().catch(() => ({}));
      return NextResponse.json(
        { status: "ERROR", available: false, message: errData.error || "Failed to fetch logs" },
        { status: res.status }
      );
    }

    const data = await res.json();
    return NextResponse.json(data);
  } catch (err: unknown) {
    return NextResponse.json(
      { status: "UNAVAILABLE", available: false, message: (err as Error).message },
      { status: 500 }
    );
  }
}

