FROM python:3.12-slim

WORKDIR /app

# Install uv
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

# Install dependencies
COPY pyproject.toml ./
RUN uv venv && . .venv/bin/activate && uv sync

# Copy application
COPY bot.py ./

# Set environment variables (replace at runtime)
ENV DISCORD_TOKEN=""
ENV CHANNEL_ID=""
ENV SERVER_IP=""

CMD [".venv/bin/python", "bot.py"]
