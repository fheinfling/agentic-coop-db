# syntax=docker/dockerfile:1.7
#
# AIColDB server image — multi-stage, multi-arch (amd64 + arm64), distroless.
#
# Build:
#   docker buildx build --platform linux/amd64,linux/arm64 -t aicoldb-server:dev .
#
# The final image:
#   - runs as uid 65532 (nonroot, distroless convention)
#   - has no shell, no apt, no busybox
#   - read-only root filesystem friendly (no writes outside /tmp)
#   - ARG TARGETARCH-aware so buildx slices the right Go binary in
#
# Migrations are embedded in the binary via cmd/server's call to db.RunMigrations,
# which uses the migrations/ files baked into the image at /app/migrations.

ARG GO_VERSION=1.24
ARG ALPINE_VERSION=3.20

# ---- builder -----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.1.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

ENV CGO_ENABLED=0 \
    GO111MODULE=on \
    GOPROXY=https://proxy.golang.org,direct

WORKDIR /src

# Cache module downloads in their own layer
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w \
        -X github.com/fheinfling/aicoldb/internal/version.Version=${VERSION} \
        -X github.com/fheinfling/aicoldb/internal/version.Commit=${COMMIT} \
        -X github.com/fheinfling/aicoldb/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/aicoldb-server ./cmd/server && \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w" \
      -o /out/aicoldb-migrate ./cmd/migrate

# ---- runtime -----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="aicoldb-server" \
      org.opencontainers.image.source="https://github.com/fheinfling/aicoldb" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.description="Auth gateway for shared PostgreSQL"

WORKDIR /app

COPY --from=builder /out/aicoldb-server /app/aicoldb-server
COPY --from=builder /out/aicoldb-migrate /app/aicoldb-migrate
COPY migrations /app/migrations
COPY sql /app/sql

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/app/aicoldb-server"]
