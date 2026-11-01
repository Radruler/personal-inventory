# Dev container: Go + Node 22 + libvips/libheif, on the same Alpine release (3.22)
# as the runtime image so vips/HEIC behaviour matches production.
FROM golang:1.26-alpine3.22

RUN apk add --no-cache \
      bash git make curl jq sqlite tzdata ca-certificates \
      nodejs npm \
      vips-tools vips-heif libheif exiftool procps coreutils findutils

ENV GOFLAGS=-buildvcs=false \
    GOPATH=/go \
    GOCACHE=/go/cache/build \
    GOMODCACHE=/go/cache/mod \
    npm_config_cache=/go/cache/npm

WORKDIR /workspaces/personal-inventory
