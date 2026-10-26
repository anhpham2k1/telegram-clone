# ── Build stage ─────────────────────────────────────────────────────────────
FROM golang:latest AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o clone_tin_tuc .

# ── Runtime stage ────────────────────────────────────────────────────────────
FROM alpine:latest

# ca-certificates needed for TLS (Telegram uses HTTPS/MTProto over TLS)
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/clone_tin_tuc .

EXPOSE 8080

CMD ["./clone_tin_tuc"]
