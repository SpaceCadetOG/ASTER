import { NextResponse } from "next/server";
import { buildTokenDetailData } from "@/lib/server-data";
import type { ScannerSide } from "@/lib/types";

export const dynamic = "force-dynamic";

export async function GET(
  request: Request,
  { params }: { params: { symbol: string } }
) {
  const { symbol } = params;
  const url = new URL(request.url);
  const side: ScannerSide = url.searchParams.get("side") === "short" ? "short" : "long";

  return NextResponse.json(await buildTokenDetailData(symbol, side));
}
