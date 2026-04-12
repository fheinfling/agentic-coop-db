# syntax=docker/dockerfile:1.7
#
# Agentic Coop DB server image — multi-stage, multi-arch (amd64 + arm64), distroless.
#
# Build:
#   docker buildx build --platform linux/amd64,linux/arm64 -t agentic-coop-db-server:dev .
#
# The final image:
#   - runs as uid 65532 (nonroot, distroless convention)
#   - has no shell, no apt, no busybox
#   - read-only root filesystem friendly (no writes outside /tmp)
#   - ARG TARGETARCH-aware so buildx slices the right Go binary in
#
# Migrations are embedded in the binary via cmd/server's call to db.RunMigrations,
# which uses the migrations/ files baked into the image at /app/migrations.

# GO_VERSION must be >= the `go` directive in go.mod (currently 1.26.2).
# Pinned to match CI (.github/workflows/ci.yml). Bump in lockstep.
ARG GO_VERSION=1.26.2
ARG ALPINE_VERSION=3.22

# ---- builder -----------------------------------------------------------------
#
# pg_query_go embeds the PostgreSQL C parser and REQUIRES cgo. We therefore:
#   - install gcc + musl-dev so cgo can compile the C sources
#   - set CGO_ENABLED=1
#   - link statically (-extldflags "-static") so the resulting binary still
#     runs on distroless/static (no glibc, no musl loader at runtime)
#
# We deliberately do NOT use --platform=$BUILDPLATFORM here. Cgo cross-
# compilation needs a cross-toolchain (xx, gcc-aarch64-linux-musl-cross, ...);
# letting buildx run the builder under QEMU for each TARGETPLATFORM is slower
# but keeps the Dockerfile simple. CI's `buildx` job uses
# `docker/setup-qemu-action` which provides the binfmt handlers.

# Digest must be updated when GO_VERSION or ALPINE_VERSION change.
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION}@sha256:c259ff7ffa06f1fd161a6abfa026573cf00f64cfd959c6d2a9d43e3ff63e8729 AS builder

ARG VERSION=0.1.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

ENV CGO_ENABLED=1 \
    GO111MODULE=on \
    GOPROXY=https://proxy.golang.org,direct

RUN apk add --no-cache gcc musl-dev

WORKDIR /src

# Cache module downloads in their own layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath \
      -ldflags "-s -w -extldflags '-static' \
        -X github.com/fheinfling/agentic-coop-db/internal/version.Version=${VERSION} \
        -X github.com/fheinfling/agentic-coop-db/internal/version.Commit=${COMMIT} \
        -X github.com/fheinfling/agentic-coop-db/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/agentic-coop-db-server ./cmd/server && \
    go build -trimpath \
      -ldflags "-s -w -extldflags '-static'" \
      -o /out/agentic-coop-db-migrate ./cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w \
        -X github.com/fheinfling/agentic-coop-db/internal/version.Version=${VERSION} \
        -X github.com/fheinfling/agentic-coop-db/internal/version.Commit=${COMMIT} \
        -X github.com/fheinfling/agentic-coop-db/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/agentic-coop-db-mcp ./cmd/mcp

# ---- runtime -----------------------------------------------------------------
# Alpine instead of distroless so operators can `docker exec` into the
# container for admin tasks (e.g. mint-key, migrate force). The static
# Go binary doesn't need musl, but the shell + coreutils cost ~3 MB and
# save a lot of operational pain on platforms like Coolify that rely on
# exec for one-off commands.
# Digest must be updated when ALPINE_VERSION changes.
FROM alpine:${ALPINE_VERSION}@sha256:55ae5d250caebc548793f321534bc6a8ef1d116f334f18f4ada1b2daad3251b2

RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

LABEL org.opencontainers.image.title="agentic-coop-db-server" \
      org.opencontainers.image.source="https://github.com/fheinfling/agentic-coop-db" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.description="Auth gateway for shared PostgreSQL"

WORKDIR /app

COPY --from=builder /out/agentic-coop-db-server /app/agentic-coop-db-server
COPY --from=builder /out/agentic-coop-db-migrate /app/agentic-coop-db-migrate
COPY --from=builder /out/agentic-coop-db-mcp /app/agentic-coop-db-mcp
COPY migrations /app/migrations
COPY sql /app/sql

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/app/agentic-coop-db-server"]
