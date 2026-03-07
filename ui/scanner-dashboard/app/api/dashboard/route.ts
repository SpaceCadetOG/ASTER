import { NextResponse } from "next/server";
import { buildDashboardData } from "@/lib/server-data";

export const dynamic = "force-dynamic";

export async function GET() {
  const data = await buildDashboardData();
  return NextResponse.json(data);
}
