FROM golang:1.27-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /tmp/tickerlight \
    ./cmd/tickerlight

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /tmp/trading-decider \
    ./cmd/trading-decider

# allow: the image ships both binaries; the sidecar selects the decider by command
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

RUN addgroup -g 65532 app && adduser -D -u 65532 -G app app

COPY --from=builder /tmp/tickerlight /usr/local/bin/tickerlight
COPY --from=builder /tmp/trading-decider /usr/local/bin/trading-decider

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/tickerlight"]
CMD ["-addr", "0.0.0.0:8080"]
