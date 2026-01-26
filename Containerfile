# Builder stage
FROM docker.io/library/golang:1.25.5-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bot .

# Final stage
FROM docker.io/library/alpine:3.23

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bot .
COPY --chown=1001:1001 static ./static

# Create non-root user and group
RUN addgroup -g 1001 absabot && \
    adduser -D -u 1001 -G absabot absabot && \
    chown -R absabot:absabot /app


# Switch to non-root user
USER 1001

# Expose ports
# 3001: Bot API server (optional, when API_ENABLED=true)
# 8080: Proxy server (optional, when PROXY_ENABLED=true)
EXPOSE 3001 8080

# Set environment variables (replace at runtime)
ENV DISCORD_TOKEN=""
ENV CHANNEL_ID=""

CMD ["./bot"]
