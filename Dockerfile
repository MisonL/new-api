ARG APP_VERSION=unknown
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=https://github.com/MisonL/new-api

FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder

WORKDIR /build
COPY web/package.json .
COPY web/bun.lock .
RUN bun install
COPY ./web .
COPY ./VERSION .
RUN DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat VERSION) bun run build

FROM golang:1.26.2-alpine@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS builder2
ENV GO111MODULE=on CGO_ENABLED=0

ARG TARGETOS
ARG TARGETARCH
ARG APP_VERSION=unknown
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=https://github.com/MisonL/new-api
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build

ADD go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=builder /build/dist ./web/dist
RUN VERSION_VALUE="${APP_VERSION}"; \
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
