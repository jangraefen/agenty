import { connection, NextResponse } from "next/server";
import { checkHealth } from "@/server/health";

export async function GET() {
  // Always run at request time; never prerender at build (no database there).
  await connection();
  const { httpStatus, body } = await checkHealth();
  return NextResponse.json(body, { status: httpStatus, headers: { "cache-control": "no-store" } });
}
