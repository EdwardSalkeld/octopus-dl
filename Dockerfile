# Stage 1: Base image with dependencies
FROM golang:1.26-trixie AS base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Stage 2: Test the application
FROM base AS test
RUN go test ./...

# Stage 3: Build the application statically
FROM base AS builder
RUN CGO_ENABLED=0 GOOS=linux go build -o /octopus-dl .

# Stage 4: Minimal runtime image
FROM debian:trixie-slim
WORKDIR /app
COPY --from=builder /octopus-dl .
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

ARG UID=1000
ARG GID=1000
RUN groupadd -g $GID -o appgroup && useradd --no-log-init -u $UID -g $GID -o -m appuser
USER appuser

CMD ["/app/octopus-dl"]
