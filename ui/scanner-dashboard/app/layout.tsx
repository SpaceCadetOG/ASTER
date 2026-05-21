import "./globals.css";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "ASTER Unified Operator Portal",
  description: "Unified ASTER operator dashboard for scanners, runtime, paper, and asset detail"
};

export default function RootLayout({
  children
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
