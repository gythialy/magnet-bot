# syntax=docker/dockerfile:1

# Build stage
FROM --platform=$BUILDPLATFORM crazymax/goxx:1.23 AS builder
WORKDIR /src

# Set build arguments and environment variables
ARG TARGETPLATFORM
ENV GO111MODULE=auto \
    CGO_ENABLED=1

# Install build dependencies and copy source code
COPY ./ /src
RUN goxx-apt-get install -y --no-install-recommends \
    binutils \
    gcc \
    g++ \
    pkg-config \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && goxx-go env \
    && make BINDIR=/out GO=goxx-go clean build \
    && mkdir -p /etc/magnet

# Final stage
FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app

COPY --from=builder --chown=nonroot:nonroot /out/magnet /app/magnet
COPY --from=builder /etc/ssl/certs /etc/ssl/certs
COPY --from=builder --chown=nonroot:nonroot /etc/magnet /etc/magnet

ENV TZ=Asia/Shanghai

VOLUME ["/etc/magnet"]

ENTRYPOINT ["/app/magnet"]
