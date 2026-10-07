# syntax=docker/dockerfile:1
# 1) 대시보드(web/)를 빌드한다. 타입 검사와 로직 테스트가 통과해야 이미지가 만들어진다.
FROM node:24 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# vite.config.ts가 결과를 ../internal/webui/dist로 내보낸다.
RUN npm test && npm run build

# 2) 빌드 결과를 내장해 Go 바이너리를 만든다.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ssl-checker ./cmd/ssl-checker

# distroless/static: 셸·curl 없음. CA 인증서는 포함돼 Cloudflare HTTPS 호출이 된다.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ssl-checker /usr/local/bin/ssl-checker
USER nonroot
CMD ["ssl-checker", "serve", "--listen", ":8080"]
