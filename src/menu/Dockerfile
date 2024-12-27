FROM rust:latest AS builder

# Install build dependencies including OpenSSL and pkg-config
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    build-essential \
    pkg-config \
    libssl-dev \
    openssl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /usr/src/menu
COPY . .
RUN cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    libssl3 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /usr/local/bin
COPY --from=builder /usr/src/menu/target/release/menu .
EXPOSE 8080
CMD ["./menu"]
