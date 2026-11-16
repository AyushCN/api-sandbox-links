import { NextRequest, NextResponse } from "next/server";
import { BACKEND_ORIGIN } from "@/lib/api-config";

export async function DELETE(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  const backend = BACKEND_ORIGIN;
  const cookie = req.headers.get("cookie") || "";

  try {
    const res = await fetch(`${backend}/api/sandboxes/${id}/actions/destroy`, {
      method: "POST",
      headers: { cookie },
    });

    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      return NextResponse.json(
        { error: data.error || "Failed to destroy sandbox" },
        { status: res.status }
      );
    }

    const data = await res.json();
    return NextResponse.json(data);
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 500 }
    );
  }
}

export async function GET(
  req: NextRequest,
  props: { params: Promise<{ id: string }> }
) {
  const { id } = await props.params;
  const backend = BACKEND_ORIGIN;
  const cookie = req.headers.get("cookie") || "";

  try {
    const res = await fetch(`${backend}/api/sandboxes/${id}`, {
      headers: { cookie },
      cache: "no-store",
    });

    if (!res.ok) {
      const text = await res.text().catch(() => "");
      try {
        const json = JSON.parse(text);
        return NextResponse.json(json, { status: res.status });
      } catch {
        return NextResponse.json({ error: text.trim() || "Request failed" }, { status: res.status });
      }
    }

    const data = await res.json();
    return NextResponse.json(data, { status: res.status });
  } catch (err: unknown) {
    return NextResponse.json(
      { error: (err as Error).message },
      { status: 500 }
    );
  }
}

