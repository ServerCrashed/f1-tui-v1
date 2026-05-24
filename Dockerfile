# Stage 1: Build the Go binary
FROM golang:1.22-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o f1-tui main.go

# Stage 2: Final runtime image with ttyd
FROM debian:bookworm-slim
WORKDIR /app

# Install ttyd and CA certificates (necessary for secure HTTPS requests to api.openf1.org)
RUN apt-get update && apt-get install -y \
    ttyd \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy the compiled binary from the builder stage
COPY --from=builder /app/f1-tui .

# Expose ttyd default port
EXPOSE 7681

# Entrypoint configuration:
#  -i 0.0.0.0 : Binds to all network interfaces inside the container
#  -p 7681    : Port to listen on
#  -W         : Allow client write operations (critical for TUI keyboard inputs)
ENTRYPOINT ["ttyd", "-i", "0.0.0.0", "-p", "7681", "-W", "./f1-tui"]
