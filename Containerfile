FROM python:3.12-slim

WORKDIR /app

# Install uv
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

# Install dependencies
COPY pyproject.toml uv.lock ./
RUN uv venv && . .venv/bin/activate && uv sync

# Copy application
COPY bot.py ./

# Create non-root user and group
RUN groupadd -g 1001 botuser && \
    useradd -r -u 1001 -g botuser -s /sbin/nologin -c "Discord Bot User" botuser

# Change ownership of /app to botuser
RUN chown -R botuser:botuser /app

# Switch to non-root user (use numeric UID for Kubernetes compatibility)
USER 1001

# Set environment variables (replace at runtime)
ENV DISCORD_TOKEN=""
ENV CHANNEL_ID=""
ENV SERVER_IP=""

CMD [".venv/bin/python", "bot.py"]
