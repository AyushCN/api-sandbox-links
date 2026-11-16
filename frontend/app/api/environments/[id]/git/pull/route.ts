import { NextRequest, NextResponse } from "next/server";

export async function POST() {
  return NextResponse.json({
    success: true,
    message: "Working directory is already up to date.",
  });
}

