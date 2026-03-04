import {
  StellarWalletsKit,
  Networks,
  KitEventType,
  type SwkAppTheme,
} from "@creit-tech/stellar-wallets-kit";
import { defaultModules } from "@creit-tech/stellar-wallets-kit/modules/utils";

const microvaultDarkTheme: SwkAppTheme = {
  background: "oklch(0.145 0 0)",
  "background-secondary": "oklch(0.205 0 0)",
  "foreground-strong": "#fff",
  foreground: "oklch(0.985 0 0)",
  "foreground-secondary": "oklch(0.708 0 0)",
  primary: "oklch(0.922 0 0)",
  "primary-foreground": "oklch(0.205 0 0)",
  transparent: "rgba(0, 0, 0, 0)",
  lighter: "#fcfcfc",
  light: "#f8f8f8",
  "light-gray": "oklch(0.800 0.006 286.033)",
  gray: "oklch(0.600 0.006 286.033)",
  danger: "oklch(0.704 0.191 22.216)",
  border: "oklch(1 0 0 / 10%)",
  shadow:
    "0 10px 15px -3px rgba(255, 255, 255, 0.1), 0 4px 6px -4px rgba(255, 255, 255, 0.1)",
  "border-radius": "0.625rem",
  "font-family": "sans-serif",
};

export function initWalletKit() {
  StellarWalletsKit.init({
    modules: defaultModules(),
    network: Networks.TESTNET,
    theme: microvaultDarkTheme,
    authModal: {
      hideUnsupportedWallets: true,
    },
  });
}

export { StellarWalletsKit, KitEventType };
