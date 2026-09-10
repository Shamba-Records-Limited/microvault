FROM node:24-alpine AS builder
RUN apk add --no-cache python3 make g++ linux-headers eudev-dev
RUN npm install -g npm@12.0.2
# npm 12 defaults to blocking URL-resolved deps (allow-remote=none).
# @creit-tech/stellar-wallets-kit is installed via the JSR proxy and pulls in
# @jsr/std__encoding the same way; both are tarball URLs, not registry
# versions, so they need this opt-in or `npm ci` fails with EALLOWREMOTE. Set
# here rather than via .npmrc, which .dockerignore excludes from the build
# context (it may hold a developer's local registry auth token).
RUN npm config set allow-remote all
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VITE_VAULT_CONTRACT_ID
ARG VITE_SOROBAN_RPC_URL
ARG VITE_NETWORK_PASSPHRASE
ARG VITE_STELLAR_EXPLORER_URL
ARG VITE_TIMELOCK_CONTRACT_ID
ARG VITE_TREASURY_ADDRESS
ARG VITE_HORIZON_URL
RUN npm run build

FROM nginx:1.31.2-alpine3.23-slim
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
CMD ["nginx", "-g", "daemon off;"]
