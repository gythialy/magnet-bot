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
    tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && goxx-go env \
    && make BINDIR=/out GO=goxx-go clean build

# Final stage
FROM ubuntu:oracular
WORKDIR /app

# Set environment variables
ENV TZ=Asia/Shanghai

# Create required directories
RUN mkdir -p /etc/magnet

# Copy binary and system files from builder
COPY --from=builder /out/magnet /app/magnet
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs /etc/ssl/certs

# Configure timezone with validation
RUN if [ -f "/usr/share/zoneinfo/$TZ" ]; then \
    ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone; \
    else \
    echo "Invalid timezone $TZ, using default Asia/Shanghai"; \
    ln -snf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && echo "Asia/Shanghai" > /etc/timezone; \
    fi

# Configure volume and entrypoint
VOLUME [ "/etc/magnet" ]
ENTRYPOINT [ "/app/magnet" ]
