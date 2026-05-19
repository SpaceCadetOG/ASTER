import { TokenClient } from "@/components/TokenClient";
import { buildTokenDetailData } from "@/lib/server-data";
import type { ScannerSide } from "@/lib/types";

export const dynamic = "force-dynamic";

export default async function TokenPage({
  params,
  searchParams
}: {
  params: { symbol: string };
  searchParams?: { side?: string };
}) {
  const { symbol } = params;
  const query = searchParams || {};
  const side: ScannerSide = query.side === "short" ? "short" : "long";
  const initialData = await buildTokenDetailData(symbol, side);

  return <TokenClient initialData={initialData} />;
}
