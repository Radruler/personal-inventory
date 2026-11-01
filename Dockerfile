# syntax=docker/dockerfile:1.7
# Multi-stage: web build → go build (cross-compiled on the build platform) →
# alpine + libvips/libheif runtime. One process, two volumes.

FROM --platform=$BUILDPLATFORM node:22-alpine3.22 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci
COPY api /src/api
COPY web ./
# svelte.config.js writes the static SPA to ../internal/webui/dist
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine3.22 AS go
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/inv ./cmd/inv

FROM alpine:3.22
RUN apk add --no-cache vips-tools vips-heif ca-certificates tzdata \
 && addgroup -g 568 apps \
 && adduser -D -H -u 568 -G apps apps \
 && mkdir -p /data /media \
 && chown 568:568 /data /media
COPY --from=go /out/inv /usr/local/bin/inv
# 568:568 is TrueNAS's "apps" user; host datasets should be owned by it.
USER 568:568
ENV INV_ADDR=:8480 \
    INV_DATA_DIR=/data \
    INV_MEDIA_DIR=/media \
    GOMEMLIMIT=64MiB
VOLUME ["/data", "/media"]
EXPOSE 8480
# No HEALTHCHECK: probe results are state the Docker daemon may persist, and
# the app aims for zero idle disk writes. Probe /api/health externally if needed.
ENTRYPOINT ["/usr/local/bin/inv"]
CMD ["serve"]
