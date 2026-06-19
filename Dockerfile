# syntax=docker/dockerfile:1

# Multi-stage build for chess-to-music:
#   1. build the Svelte frontend with Bun         -> web/dist
#   2. compile the Go server (which embeds dist)  -> static binary
#   3. assemble a minimal runtime image with ffmpeg

# ---- Stage 1: build the Svelte frontend with Bun ----
FROM oven/bun:1-alpine AS frontend
WORKDIR /app/web
# Install dependencies first for better layer caching.
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
# Build the production assets into web/dist.
COPY web/ ./
RUN bun run build

# ---- Stage 2: build the Go server (embeds web/dist) ----
FROM golang:1.25-alpine AS backend
WORKDIR /src
# Download modules first for better layer caching.
COPY go.mod go.sum ./
RUN go mod download
# Copy the source, then drop in the freshly built frontend assets so the
# //go:embed directive in web/embed.go has something to embed.
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
# Build a small, static binary (pgx is pure Go, so CGO can stay off).
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server

# ---- Stage 3: minimal runtime with ffmpeg ----
FROM alpine:3.20
# ffmpeg is needed for MP3 output; ca-certificates for TLS database URLs.
RUN apk add --no-cache ffmpeg ca-certificates \
    && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=backend /out/server /app/server
USER app
EXPOSE 8080
# DATABASE_URL is optional: without it the game library is simply disabled.
ENTRYPOINT ["/app/server"]
CMD ["-addr", ":8080"]
