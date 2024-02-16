# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM crazymax/goxx:1.22 AS builder
ENV GO111MODULE=auto
ENV CGO_ENABLED=1
WORKDIR /src

ARG TARGETPLATFORM
COPY ./ /src
RUN \
    goxx-apt-get install -y binutils gcc g++ pkg-config \
    && goxx-go env \
    && make BINDIR=/out GO=goxx-go clean build

FROM ubuntu:jammy
ENV TZ=Asia/Shanghai
RUN set -eux; \
    apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /etc/magnet
COPY --from=builder /out/magnet /app/magnet
WORKDIR /app
VOLUME [ "/etc/magnet" ]
ENTRYPOINT [ "/app/magnet" ]