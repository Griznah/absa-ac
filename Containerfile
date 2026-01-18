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

# Volume mount for config.json - host can edit configuration without container rebuild
# Mount options:
# 1) Single file: podman run -v /path/to/config.json:/data/config.json:ro ...
# 2) Directory: podman run -v /path/to/config:/data:ro ... (contains config.json)
# NOTE: VOLUME must be declared BEFORE chown, else ownership changes are discarded
VOLUME /data

# Create non-root user and group
RUN addgroup -g 1001 absabot && \
    adduser -D -u 1001 -G absabot absabot && \
    chown -R absabot:absabot /app /data


# Switch to non-root user
USER 1001

# Set environment variables (replace at runtime)
ENV DISCORD_TOKEN=""
ENV CHANNEL_ID=""

CMD ["./bot"]
