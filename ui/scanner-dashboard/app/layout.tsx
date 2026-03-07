import "./globals.css";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "ASTER Scanner Dashboard",
  description: "Tabbed scanner UI for long/short/in-play/confluence views"
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
