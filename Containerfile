# Node stage for building Svelte webfront
FROM docker.io/library/node:22-alpine AS webfront-builder

WORKDIR /app/webfront
COPY webfront/package*.json ./
RUN npm ci
COPY webfront/ ./
RUN npm run build

# Go builder stage
FROM docker.io/library/golang:1.25.5-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . ./
COPY --from=webfront-builder /app/webfront/dist ./webfront/dist
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bot .

# Final stage
FROM docker.io/library/alpine:3.23

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bot .

# Copy webfront static files
COPY --from=webfront-builder /app/webfront/dist ./webfront/dist

# Create non-root user and group
RUN addgroup -g 1001 absabot && \
    adduser -D -u 1001 -G absabot absabot && \
    chown -R absabot:absabot /app

# Switch to non-root user
USER 1001

# Expose ports
# 3001: Bot API server (optional, when API_ENABLED=true)
# 8080: Web frontend (optional, when WEBFRONT_ENABLED=true)
EXPOSE 3001 8080

# Set environment variables (replace at runtime)
ENV DISCORD_TOKEN=""
ENV CHANNEL_ID=""

CMD ["./bot"]
