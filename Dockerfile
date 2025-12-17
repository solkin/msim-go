# Build stage
FROM golang:1.21 AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o msim-server .

# Runtime stage - use Debian for glibc compatibility
FROM debian:bookworm-slim

# Install SQLite, CA certificates and netcat for healthcheck
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    libsqlite3-0 \
    netcat-openbsd && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/msim-server .

# Create directory for database
RUN mkdir -p /app/data

# Expose port
EXPOSE 3215

# Run the application
CMD ["./msim-server"]

