# Stage 1: Build the Go binary
FROM golang:latest AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o f1-tui main.go

# Stage 2: Final runtime image with ttyd
FROM debian:bookworm-slim
WORKDIR /app

# Install curl and CA certificates (necessary for secure HTTPS requests to api.openf1.org)
RUN apt-get update && apt-get install -y \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Dynamically download the pre-compiled ttyd standalone binary based on CPU architecture (Intel vs ARM64)
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "x86_64" ]; then TTYD_ARCH="x86_64"; \
    elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then TTYD_ARCH="aarch64"; \
    else TTYD_ARCH="x86_64"; fi && \
    curl -L -o /usr/local/bin/ttyd "https://github.com/tsl0922/ttyd/releases/download/1.7.7/ttyd.${TTYD_ARCH}" && \
    chmod +x /usr/local/bin/ttyd

# Copy the compiled binary from the builder stage
COPY --from=builder /app/f1-tui .

# Expose ttyd default port
EXPOSE 7681

# Entrypoint configuration:
#  -i 0.0.0.0 : Binds to all network interfaces inside the container
#  -p 7681    : Port to listen on
#  -W         : Allow client write operations (critical for TUI keyboard inputs)
ENTRYPOINT ["ttyd", "-i", "0.0.0.0", "-p", "7681", "-W", "./f1-tui"]
