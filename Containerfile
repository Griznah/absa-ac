# Builder stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bot .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/bot .

# Create non-root user and group
RUN addgroup -g 1001 botuser && \
    adduser -D -u 1001 -G botuser botuser && \
    chown -R botuser:botuser /root

# Switch to non-root user
USER 1001

# Set environment variables (replace at runtime)
ENV DISCORD_TOKEN=""
ENV CHANNEL_ID=""
ENV SERVER_IP=""

CMD ["./bot"]
