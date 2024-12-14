FROM rust:1.75.0-slim AS builder

WORKDIR /usr/src/menu
COPY Cargo.toml ./

# Create dummy main.rs and build dependencies
RUN mkdir src && \
    echo "fn main() {println!(\"dummy\");}" > src/main.rs && \
    cargo build --release && \
    rm -f target/release/deps/menu*

# Copy actual source and build
COPY src ./src
RUN cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /usr/local/bin
COPY --from=builder /usr/src/menu/target/release/menu .
EXPOSE 8080
CMD ["./menu"]