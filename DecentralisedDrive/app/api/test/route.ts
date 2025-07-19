// app/api/debug/route.ts
import { NextResponse } from "next/server";

export async function POST() {
  return NextResponse.json({ ok: "no auth applied" });
}
