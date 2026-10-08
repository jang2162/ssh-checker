# syntax=docker/dockerfile:1
# 1) Build the dashboard (web/). The image is only built if the type check and logic tests pass.
FROM node:24 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# vite.config.ts writes the output to ../internal/webui/dist.
RUN npm test && npm run build

# 2) Build the Go binary with the dashboard output embedded.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ssl-checker ./cmd/ssl-checker

# distroless/static: no shell, no curl. CA certificates are included, so HTTPS calls to Cloudflare work.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ssl-checker /usr/local/bin/ssl-checker
USER nonroot
CMD ["ssl-checker", "serve", "--listen", ":8080"]
