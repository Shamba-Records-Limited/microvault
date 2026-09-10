#!/usr/bin/env node
// Renders a themed Redoc static HTML page from a swagger.json, so the API
// reference matches web/'s design tokens (web/src/index.css) instead of
// Redoc's stock look.
//
// This deliberately does not use `@redocly/cli build-docs`'s --theme flags:
// they serialize into the page's client-side hydration state but are not
// consulted by the prerendering pass, so the rendered page never actually
// picks up the colors or fonts. Redoc's own runtime `Redoc.init(spec,
// {theme}, el)` API — the same mechanism the pre-Redocly-CLI version of this
// page already used — does apply a theme, verified by rendering both and
// diffing screenshots. See the docs README (if one exists) or ask before
// switching this back to build-docs.
//
// Colors are hex, not the oklch() web/ actually uses: Redoc's theme engine
// does color math (hover/derived shades) with a library that does not
// resolve oklch(), so the oklch chroma-0 (grayscale) tokens were converted
// to hex once, by hand, via the standard OKLCH->linear-sRGB->sRGB formula.
// If web/'s palette changes, re-derive these rather than eyeballing new hex.

const fs = require("fs");
const path = require("path");

const [, , specPath, outPath, titleArg] = process.argv;
if (!specPath || !outPath) {
  console.error("Usage: render-redoc.js <swagger.json> <output.html> [title]");
  process.exit(1);
}

const spec = JSON.parse(fs.readFileSync(specPath, "utf8"));

// Inlined as a data URI so the docs page stays a single self-contained file
// with no extra static-asset route for the server to serve.
const logoPath = path.join(__dirname, "assets", "microvault-logo.svg");
const logoDataUri =
  "data:image/svg+xml;base64," + fs.readFileSync(logoPath).toString("base64");

spec.info = spec.info || {};
spec.info["x-logo"] = {
  url: logoDataUri,
  backgroundColor: "#FFFFFF",
  altText: "Microvault",
};

// web/src/index.css, oklch(L 0 0) grayscale tokens converted to hex:
//   --primary (light) L=0.205 -> #171717   --foreground L=0.145      -> #0a0a0a
//   --muted-foreground L=0.556 -> #737373  --sidebar L=0.985         -> #fafafa
//   --border L=0.922 -> #e5e5e5            dark-mode --background L=0.145 -> #0a0a0a
const theme = {
  colors: {
    primary: { main: "#171717" },
    text: { primary: "#0a0a0a", secondary: "#737373" },
    border: { dark: "#e5e5e5", light: "#f5f5f5" },
  },
  typography: {
    fontFamily:
      '"Geist", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    fontSize: "14px",
    headings: {
      fontFamily:
        '"Geist", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    },
    code: {
      fontFamily:
        '"Geist Mono", ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
    },
    links: { color: "#171717" },
  },
  sidebar: {
    backgroundColor: "#fafafa",
    textColor: "#0a0a0a",
    activeTextColor: "#171717",
  },
  // Kept dark deliberately: a light main content area with a dark code panel
  // is the near-universal API-reference convention (Stripe, YellowCard,
  // Twilio), and web/'s own dark-mode --background (oklch(0.145 0 0), the
  // same #0a0a0a) already exists for exactly this tone, so this borrows it
  // rather than inventing a new color.
  rightPanel: {
    backgroundColor: "#0a0a0a",
    textColor: "#fafafa",
  },
};

const title = titleArg || spec.info.title || "API Reference";

const html = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf8" />
  <title>${title}</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700&family=Geist+Mono:wght@400;500&display=swap" rel="stylesheet">
  <style>
    body { margin: 0; padding: 0; }
    .portal-login {
      position: fixed;
      top: 16px;
      right: 24px;
      z-index: 10000;
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 8px 16px;
      border-radius: 6px;
      background: #171717;
      color: #fafafa;
      font-family: "Geist", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
      font-size: 13px;
      font-weight: 500;
      text-decoration: none;
      box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15);
    }
    .portal-login:hover { background: #000000; }
  </style>
</head>
<body>
  <a class="portal-login" href="https://portal.microvault.shambarecords.com/login" target="_blank" rel="noopener">Portal Login</a>
  <div id="redoc-container"></div>
  <script src="https://cdn.redocly.com/redoc/v2.5.3/bundles/redoc.standalone.js" integrity="sha384-xiEssMQFSpSfLbzRZCGfxxIM5QDb2DTrU6vyoZdp2sV1L6pmOMy6MpTtUoLbpC96" crossorigin="anonymous"></script>
  <script>
    Redoc.init(${JSON.stringify(spec)}, { theme: ${JSON.stringify(theme)} }, document.getElementById('redoc-container'));
  </script>
</body>
</html>
`;

fs.writeFileSync(outPath, html);
console.log(
  "Rendered themed Redoc page to",
  outPath,
  "(" + Math.round(html.length / 1024) + " KiB)"
);
