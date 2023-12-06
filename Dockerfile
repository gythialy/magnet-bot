# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM crazymax/goxx:1.21 AS base
ENV GO111MODULE=auto
ENV CGO_ENABLED=1
WORKDIR /src

FROM base AS build
ARG TARGETPLATFORM
RUN --mount=type=cache,sharing=private,target=/var/cache/apt \
    --mount=type=cache,sharing=private,target=/var/lib/apt/lists \
    goxx-apt-get install -y binutils gcc g++ pkg-config
RUN --mount=type=bind,source=.,rw \
    --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod <<EOT
VERSION=`git describe --tags || echo "develop"`
BUILDTIME=`date -u +%Y-%m-%dT%H:%M:%SZ`

LDFLAGS="-s -w -buildid= -X github.com/gythialy/magnet/pkg/constant.Version=${VERSION} -X github.com/gythialy/magnet/pkg/constant.BuildTime=${BUILDTIME}"

if [ "$(. goxx-env && echo $GOOS)" = "linux" ]; then
  LDFLAGS="$LDFLAGS -extldflags -static"
fi
goxx-go env
goxx-go build -v -o /out/magnet -trimpath -ldflags "$LDFLAGS" .
EOT

FROM debian:stable-slim
RUN apt-get update && apt-get install -y ca-certificates \
    && apt-get clean \
    && mkdir -p /etc/magnet
COPY --from=build /out/magnet /app/magnet
WORKDIR /app
VOLUME [ "/etc/magnet" ]
ENTRYPOINT [ "/app/magnet" ]
