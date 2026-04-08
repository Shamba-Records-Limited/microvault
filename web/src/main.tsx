/**
 * Vite entry point: initialises the wallet kit singleton and mounts the app.
 * @module main
 * @remarks `initWalletKit()` must run before the first render so any wallet
 * UI mounted from `<App />` finds the kit already configured.
 */
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./App.tsx";
import { initWalletKit } from "./lib/wallet-kit";

initWalletKit();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
