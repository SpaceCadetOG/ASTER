import { NextResponse } from "next/server";
import { buildDashboardData } from "@/lib/server-data";

export const dynamic = "force-dynamic";

export async function GET() {
  return NextResponse.json(await buildDashboardData());
}
