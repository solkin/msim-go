# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o msim-server .

# Runtime stage
FROM alpine:latest

# Install SQLite, CA certificates and netcat for healthcheck
RUN apk --no-cache add ca-certificates sqlite netcat-openbsd

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/msim-server .

# Create directory for database
RUN mkdir -p /app/data

# Expose port
EXPOSE 3215

# Run the application
CMD ["./msim-server"]

