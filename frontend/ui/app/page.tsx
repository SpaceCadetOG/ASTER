import { DashboardClient } from "@/components/DashboardClient";
import { buildDashboardData } from "@/lib/server-data";

export const dynamic = "force-dynamic";

export default async function HomePage() {
  const initialData = await buildDashboardData();

  return <DashboardClient initialData={initialData} />;
}
