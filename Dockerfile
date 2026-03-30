FROM golang:1.25 AS build

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/porkbun-dns ./cmd/porkbun-dns

FROM tailscale/tailscale:latest

COPY --from=build /out/porkbun-dns /usr/local/bin/porkbun-dns
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
COPY docker/tailscale-local.sh /usr/local/bin/tailscale-local

RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/tailscale-local

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
