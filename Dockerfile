# syntax=docker/dockerfile:1.7
ARG APP_VERSION=unknown
ARG EFFECTIVE_APP_VERSION=${APP_VERSION}
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=https://github.com/MisonL/new-api

FROM oven/bun:1.4.3@sha256:ec06c3b6cea04192ae6770c434f668ca41d343ad19fa6472216c7b48be39c598 AS builder

WORKDIR /build
COPY web/default/package.json .
COPY web/default/bun.lock .
RUN --mount=type=cache,id=new-api-bun-install,target=/root/.bun/install/cache,sharing=locked \
    bun install --frozen-lockfile
COPY ./web/default .
COPY ./web/scripts /scripts
COPY ./VERSION .
COPY ./scripts/write-frontend-release-metadata.sh /scripts/write-frontend-release-metadata.sh
COPY ./scripts/frontend-bundle-versions.mjs /scripts/frontend-bundle-versions.mjs
ARG APP_VERSION=unknown
ARG EFFECTIVE_APP_VERSION=${APP_VERSION}
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
RUN VERSION_VALUE="${EFFECTIVE_APP_VERSION}"; \
    if [ -z "$VERSION_VALUE" ] || [ "$VERSION_VALUE" = "unknown" ]; then VERSION_VALUE="${APP_VERSION}"; fi; \
    if [ -z "$VERSION_VALUE" ] || [ "$VERSION_VALUE" = "unknown" ]; then echo "EFFECTIVE_APP_VERSION or APP_VERSION build arg is required" >&2; exit 1; fi; \
    DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="$VERSION_VALUE" bun run build && \
    bun /scripts/frontend-bundle-versions.mjs --root /build --only web/default --dist-dir dist --modules-dir node_modules && \
    sh /scripts/write-frontend-release-metadata.sh default dist "$VERSION_VALUE" "$VCS_REF" "$BUILD_DATE"

FROM oven/bun:1.4.3@sha256:ec06c3b6cea04192ae6770c434f668ca41d343ad19fa6472216c7b48be39c598 AS builder-classic

WORKDIR /build
COPY web/classic/package.json .
COPY web/classic/bun.lock .
RUN --mount=type=cache,id=new-api-bun-install,target=/root/.bun/install/cache,sharing=locked \
    bun install --frozen-lockfile
COPY ./web/classic .
COPY ./web/scripts /scripts
COPY ./VERSION .
COPY ./scripts/write-frontend-release-metadata.sh /scripts/write-frontend-release-metadata.sh
COPY ./scripts/frontend-bundle-versions.mjs /scripts/frontend-bundle-versions.mjs
ARG APP_VERSION=unknown
ARG EFFECTIVE_APP_VERSION=${APP_VERSION}
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
RUN VERSION_VALUE="${EFFECTIVE_APP_VERSION}"; \
    if [ -z "$VERSION_VALUE" ] || [ "$VERSION_VALUE" = "unknown" ]; then VERSION_VALUE="${APP_VERSION}"; fi; \
    if [ -z "$VERSION_VALUE" ] || [ "$VERSION_VALUE" = "unknown" ]; then echo "EFFECTIVE_APP_VERSION or APP_VERSION build arg is required" >&2; exit 1; fi; \
    DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="$VERSION_VALUE" bun run build && \
    bun /scripts/frontend-bundle-versions.mjs --root /build --only web/classic --dist-dir dist --modules-dir node_modules && \
    sh /scripts/write-frontend-release-metadata.sh classic dist "$VERSION_VALUE" "$VCS_REF" "$BUILD_DATE"

FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS builder2
ENV GO111MODULE=on CGO_ENABLED=0

ARG TARGETOS
ARG TARGETARCH
ARG APP_VERSION=unknown
ARG EFFECTIVE_APP_VERSION=${APP_VERSION}
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=https://github.com/MisonL/new-api
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOPROXY=${GOPROXY}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build

COPY go.mod go.sum ./
RUN --mount=type=cache,id=new-api-go-mod,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY main.go VERSION ./
COPY common ./common
COPY constant ./constant
COPY controller ./controller
COPY dto ./dto
COPY i18n ./i18n
COPY logger ./logger
COPY middleware ./middleware
COPY model ./model
COPY oauth ./oauth
COPY pkg ./pkg
COPY relay ./relay
COPY router ./router
COPY service ./service
COPY setting ./setting
COPY types ./types
COPY --from=builder /build/dist ./web/default/dist
COPY --from=builder-classic /build/dist ./web/classic/dist
RUN --mount=type=cache,id=new-api-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=new-api-go-build,target=/root/.cache/go-build,sharing=locked \
    VERSION_VALUE="${EFFECTIVE_APP_VERSION}"; \
    if [ -z "$VERSION_VALUE" ] || [ "$VERSION_VALUE" = "unknown" ]; then VERSION_VALUE="${APP_VERSION}"; fi; \
    if [ -z "$VERSION_VALUE" ] || [ "$VERSION_VALUE" = "unknown" ]; then echo "EFFECTIVE_APP_VERSION or APP_VERSION build arg is required" >&2; exit 1; fi; \
    go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=${VERSION_VALUE}' -X 'github.com/QuantumNous/new-api/common.BuildCommit=${VCS_REF}' -X 'github.com/QuantumNous/new-api/common.BuildDate=${BUILD_DATE}' -X 'github.com/QuantumNous/new-api/common.BuildSource=${SOURCE_URL}'" -o new-api

FROM alpine:3.22.6@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8

ARG APP_VERSION=unknown
ARG EFFECTIVE_APP_VERSION=${APP_VERSION}
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=https://github.com/MisonL/new-api

LABEL org.opencontainers.image.title="new-api" \
      org.opencontainers.image.description="Unified AI gateway" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.version="${EFFECTIVE_APP_VERSION}"

RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates tzdata su-exec \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
# 运行时以非 root 运行；属主交接逻辑见 deploy/docker/entrypoint.sh。
# /data 是 WORKDIR（SQLite、pprof 默认落点），/app/logs 是 compose 的日志目录。
COPY deploy/docker/entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh \
    && addgroup -g 1000 newapi \
    && adduser -D -u 1000 -G newapi newapi \
    && mkdir -p /data /app/logs \
    && chown -R 1000:1000 /data /app/logs
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/entrypoint.sh"]
