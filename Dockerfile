# syntax=docker/dockerfile:1

# --- frontend: Svelte → static assets -------------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- backend: static Go binary with the frontend embedded -----------------------
FROM golang:1.27-alpine AS backend
ARG VERSION=docker
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY db/ db/
COPY web/embed.go web/embed.go
COPY --from=frontend /src/web/dist web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/trackstar ./cmd/trackstar \
 && mkdir -p /out/data

# --- runtime: just the binary (no shell, no Node, no package manager) ------------
FROM gcr.io/distroless/static-debian12
COPY --from=backend /out/trackstar /usr/local/bin/trackstar
# Owned by the distroless "nonroot" user so a fresh named volume is writable.
COPY --from=backend --chown=65532:65532 /out/data /var/lib/trackstar

ENV TRACKSTAR_ADDR=0.0.0.0:3000 \
    TRACKSTAR_DATA_DIR=/var/lib/trackstar \
    TRACKSTAR_DATABASE_DRIVER=sqlite \
    TRACKSTAR_DATABASE_URL=/var/lib/trackstar/trackstar.db
EXPOSE 3000
VOLUME /var/lib/trackstar
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/trackstar", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/trackstar"]
