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

// Developer console log Easter Egg for auditing developers
console.log(
  "%c Microvault %c Built by Shamba Records — Stellar SEP-56 Lending Pools %c\n\nAuditing our smart contracts or integrating SEP-56? Explore the source code at:\nhttps://github.com/Shamba-Records-Limited/microvault",
  "background: oklch(0.205 0 0); color: oklch(0.985 0 0); padding: 4px 8px; border-radius: 4px; font-family: monospace; font-weight: bold;",
  "color: oklch(0.556 0 0); font-style: italic; font-family: sans-serif;",
  "color: inherit; font-family: sans-serif;"
);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
