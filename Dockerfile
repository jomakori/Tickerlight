# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source
COPY . .

# Build binary with security/size flags
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /tmp/tickerlight \
    ./cmd/tickerlight

# Runtime stage
FROM alpine:3.21

# Install CA certificates for HTTPS support
RUN apk add --no-cache ca-certificates

# Create non-root user (uid 65532 is the standard nonroot in distroless)
RUN addgroup -g 65532 app && adduser -D -u 65532 -G app app

# Copy binary from builder
COPY --from=builder /tmp/tickerlight /usr/local/bin/tickerlight

# Switch to non-root user
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/tickerlight"]
CMD ["-addr", "0.0.0.0:8080"]
