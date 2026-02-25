# Microvault Web

Lightweight, read-only web interface for viewing Microvault SEP-56 lending pool metrics on Stellar.

## Stack

- **Vite + React 19 + TypeScript**
- **TanStack Router** - code-based routing (2 pages)
- **TanStack Query** - data fetching with caching and auto-refresh
- **@stellar/stellar-sdk** - direct Soroban RPC contract reads (no signing, no backend)
- **Tailwind CSS v4 + shadcn/ui** - CSS-first design system
- **oxlint** - linting

## Pages

- `/` — Pool dashboard with live TVL, total borrowed, utilization, and APR from the vault contract
- `/use-case` — Agricultural lending use case overview

## Setup

```bash
npm install
cp .env.example .env.local
npm run dev
```

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `VITE_VAULT_CONTRACT_ID` | Yes | — | Soroban vault contract address |
| `VITE_SOROBAN_RPC_URL` | No | `https://soroban-testnet.stellar.org` | Soroban RPC endpoint |
| `VITE_NETWORK_PASSPHRASE` | No | `Test SDF Network ; September 2015` | Stellar network passphrase |
| `VITE_STELLAR_EXPLORER_URL` | No | `https://stellar.expert/explorer/testnet` | Block explorer base URL |

## Scripts

```bash
npm run dev        # Start dev server on port 5173
npm run build      # Production build to dist/
npm run preview    # Preview production build
npm run typecheck  # TypeScript type checking
npm run lint       # Lint with oxlint
npm run lint:fix   # Lint and auto-fix
```

## Docker

**Development** (with file sync via `docker compose watch`):

```bash
docker compose up web
docker compose watch web
```

**Production** (multi-stage nginx build):

```bash
docker build -f Dockerfile \
  --build-arg VITE_VAULT_CONTRACT_ID=C... \
  -t microvault-web .
docker run -p 3000:3000 microvault-web
```
