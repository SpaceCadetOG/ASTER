import type { Metadata } from "next";
import { AppChrome } from "@/components/AppChrome";
import "./globals.css";

export const metadata: Metadata = {
  title: "ASTER Scanner UI",
  description: "Standalone frontend for ASTER long/short scanners and token drilldown."
};

export default function RootLayout({
  children
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body>
        <div className="page-shell">
          <AppChrome />
          {children}
        </div>
      </body>
    </html>
  );
}
