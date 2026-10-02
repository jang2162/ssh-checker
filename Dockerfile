# syntax=docker/dockerfile:1
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ssl-checker ./cmd/ssl-checker

# distroless/static: 셸·curl 없음. CA 인증서는 포함돼 Cloudflare HTTPS 호출이 된다.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ssl-checker /usr/local/bin/ssl-checker
USER nonroot
CMD ["ssl-checker", "serve", "--listen", ":8080"]
