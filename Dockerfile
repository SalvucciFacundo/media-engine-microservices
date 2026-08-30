# syntax=docker/dockerfile:1

# Build Stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Install templ CLI
RUN go install github.com/a-h/templ/cmd/templ@v0.3.906

# Copy dependency manifests
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Generate templ templates
RUN templ generate

# Compile binaries
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/janitor ./cmd/janitor

# Gateway Target
FROM alpine:3.20 AS gateway
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /bin/gateway /app/gateway
EXPOSE 8080
ENTRYPOINT ["/app/gateway"]

# Worker Target
FROM alpine:3.20 AS worker
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /bin/worker /app/worker
ENTRYPOINT ["/app/worker"]

# Janitor Target
FROM alpine:3.20 AS janitor
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /bin/janitor /app/janitor
ENTRYPOINT ["/app/janitor"]
