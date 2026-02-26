FROM node:20-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VITE_VAULT_CONTRACT_ID
ARG VITE_SOROBAN_RPC_URL
ARG VITE_NETWORK_PASSPHRASE
ARG VITE_STELLAR_EXPLORER_URL
RUN npm run build

FROM nginx:1.29.5-alpine3.23-slim
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
CMD ["nginx", "-g", "daemon off;"]
