/**
 * Root layout wrapping every route with the site header, footer, and a
 * themed Sonner toaster.
 * @module routes/__root
 */
import { Outlet } from "@tanstack/react-router";
import { Toaster } from "sonner";
import { Header } from "@/components/Header";
import { Footer } from "@/components/Footer";

/**
 * Persistent shell rendered around every routed page.
 * @remarks Toast colours are tuned inline here (rather than via the Sonner
 * theme prop) because we need variant-specific border tints and Tailwind
 * classNames to override Sonner's default dark styles.
 */
export default function RootLayout() {
  return (
    <div className="flex min-h-screen flex-col bg-background relative overflow-hidden">
      {/* Subtle dotted grid overlay for high-technology texture */}
      <div className="absolute inset-0 bg-[radial-gradient(oklch(0.922_0_0)_1px,transparent_1px)] dark:bg-[radial-gradient(oklch(1_0_0_/_10%)_1px,transparent_1px)] [background-size:24px_24px] pointer-events-none opacity-70 z-0" />
      
      <div className="relative z-10 flex flex-1 flex-col">
        <Header />
        <div className="flex-1">
          <Outlet />
        </div>
        <Footer />
      </div>
      
      <Toaster
        theme="dark"
        position="bottom-right"
        toastOptions={{
          style: {
            background: "oklch(0.205 0 0)",
            border: "1px solid oklch(1 0 0 / 10%)",
            color: "oklch(0.985 0 0)",
            fontFamily: "sans-serif",
            borderRadius: "0.625rem",
          },
          classNames: {
            success: "!border-green-500/30 !text-green-400",
            error: "!border-destructive/30 !text-red-400",
            warning: "!border-yellow-500/30 !text-yellow-400",
            info: "!border-blue-500/30 !text-blue-400",
          },
        }}
      />
    </div>
  );
}
