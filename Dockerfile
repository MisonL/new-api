# syntax=docker/dockerfile:1.7
ARG APP_VERSION=unknown
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=https://github.com/MisonL/new-api

FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder

WORKDIR /build
COPY web/default/package.json .
COPY web/default/bun.lock .
RUN --mount=type=cache,id=new-api-bun-install,target=/root/.bun/install/cache,sharing=locked \
    bun install
COPY ./web/default .
COPY ./VERSION .
RUN DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat VERSION) bun run build

FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder-classic

WORKDIR /build
COPY web/classic/package.json .
COPY web/classic/bun.lock .
RUN --mount=type=cache,id=new-api-bun-install,target=/root/.bun/install/cache,sharing=locked \
    bun install
COPY ./web/classic .
COPY ./VERSION .
RUN VITE_REACT_APP_VERSION=$(cat VERSION) bun run build

FROM golang:1.26.2-alpine@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS builder2
ENV GO111MODULE=on CGO_ENABLED=0

ARG TARGETOS
ARG TARGETARCH
ARG APP_VERSION=unknown
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
    VERSION_VALUE="${APP_VERSION}"; \
    if [ "$VERSION_VALUE" = "unknown" ]; then VERSION_VALUE="$(cat VERSION)"; fi; \
    go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=${VERSION_VALUE}' -X 'github.com/QuantumNous/new-api/common.BuildCommit=${VCS_REF}' -X 'github.com/QuantumNous/new-api/common.BuildDate=${BUILD_DATE}' -X 'github.com/QuantumNous/new-api/common.BuildSource=${SOURCE_URL}'" -o new-api

FROM alpine:3.22.2@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1

ARG APP_VERSION=unknown
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=https://github.com/MisonL/new-api

LABEL org.opencontainers.image.title="new-api" \
      org.opencontainers.image.description="Unified AI gateway" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.version="${APP_VERSION}"

RUN apk add --no-cache ca-certificates tzdata \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-api"]
