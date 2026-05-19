import { NextResponse } from "next/server";
import { buildScannerData } from "@/lib/server-data";
import type { ScannerSide } from "@/lib/types";

export const dynamic = "force-dynamic";

export async function GET(
  _request: Request,
  { params }: { params: { side: string } }
) {
  const { side } = params;
  if (side !== "long" && side !== "short") {
    return NextResponse.json({ error: "invalid scanner side" }, { status: 400 });
  }

  return NextResponse.json(await buildScannerData(side as ScannerSide));
}
