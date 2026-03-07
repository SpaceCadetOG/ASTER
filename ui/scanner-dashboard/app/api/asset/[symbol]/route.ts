import { NextResponse } from "next/server";
import { buildAssetDetail } from "@/lib/server-data";

export const dynamic = "force-dynamic";

export async function GET(
  _req: Request,
  { params }: { params: { symbol: string } }
) {
  const data = await buildAssetDetail(params.symbol);
  return NextResponse.json(data);
}
