# syntax=docker/dockerfile:1

# Build stage
FROM --platform=$BUILDPLATFORM golang:1.25.2 AS builder

# Set build arguments and environment variables
ARG TARGET_GOOS
ARG TARGET_GOARCH

ENV GO111MODULE=auto \
    CGO_ENABLED=0 \
    TARGET_GOOS=${TARGET_GOOS} \
    TARGET_GOARCH=${TARGET_GOARCH}

WORKDIR /src

COPY . .

RUN apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && make BINDIR=/out clean build \
    && mkdir -p /etc/magnet

# Final stage
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=builder --chown=nonroot:nonroot /out/magnet /app/magnet
COPY --from=builder --chown=nonroot:nonroot /etc/magnet /etc/magnet
COPY --from=builder /etc/ssl/certs /etc/ssl/certs

ENV TZ=Asia/Shanghai

USER nonroot

VOLUME ["/etc/magnet"]

ENTRYPOINT ["/app/magnet"]
