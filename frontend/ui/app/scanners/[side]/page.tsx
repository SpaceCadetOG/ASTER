import { notFound } from "next/navigation";
import { ScannerClient } from "@/components/ScannerClient";
import { buildScannerData } from "@/lib/server-data";
import type { ScannerSide } from "@/lib/types";

export const dynamic = "force-dynamic";

export default async function ScannerPage({
  params
}: {
  params: { side: string };
}) {
  const { side } = params;
  if (side !== "long" && side !== "short") {
    notFound();
  }

  const initialData = await buildScannerData(side as ScannerSide);
  return <ScannerClient side={side as ScannerSide} initialData={initialData} />;
}
