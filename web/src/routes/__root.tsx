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
    <div className="flex min-h-screen flex-col bg-background">
      <Header />
      <div className="flex-1">
        <Outlet />
      </div>
      <Footer />
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
