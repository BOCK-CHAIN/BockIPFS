import type { Metadata } from "next";
import { Geist, Geist_Mono, Open_Sans } from "next/font/google";
import "./globals.css";
import logo from "./bockchain-logo.svg";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

const openSans = Open_Sans({
  subsets: ["latin"],
  weight: ["400", "600", "700"], // add more if needed
  variable: "--font-open-sans",
  display: "swap",
});

export const metadata: Metadata = {
  title: "Create Next App",
  description: "Generated by create next app",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en"  className={`${geistSans.variable} ${geistMono.variable} ${openSans.variable}`}>
      <body className="bg-[#050816]">
        <header className="flex  my-3.5 mx-3.5 gap-10 p-4">
          <img src={logo.src} alt="BOCKCHAIN Logo" className="h-20 w-auto animate-spin-slow" />
          <h3 className="font-bold flex justify-center items-center text-[#873A87] text-5xl ">Bock-IPFS</h3>
        </header>      
        {children}
      </body>
    </html>
  );
}
