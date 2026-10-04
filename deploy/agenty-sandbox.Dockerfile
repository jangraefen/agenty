# Image for the sandbox runner agenty-sandbox (ARCHITECTURE §9, §14). Build from the repository root:
#   task build:images
# Base images are pinned by digest; the Go version matches go.work (a unit test keeps them in sync).

FROM golang:1.27.1-trixie@sha256:3b77fc618ec235a1ab412de7737f120dd507c57e8d87de4cbb7994fb94275ed5 AS build
# Build the module on its own (server and sandbox do not import each other), with the image's toolchain.
ENV GOWORK=off GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY sandbox/go.mod ./
RUN go mod download
COPY sandbox/ ./
ARG VERSION=dev
RUN go build -trimpath \
    -ldflags "-s -w -X github.com/jangraefen/agenty/sandbox/internal/buildinfo.Version=${VERSION}" \
    -o /out/agenty-sandbox ./cmd/agenty-sandbox

# Static, shell-less runtime with CA certificates, tzdata, and a non-root user (65532, "nonroot").
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG VERSION=dev
LABEL org.opencontainers.image.title="agenty-sandbox" \
      org.opencontainers.image.description="Agenty sandbox runner" \
      org.opencontainers.image.source="https://github.com/jangraefen/agenty" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/agenty-sandbox /usr/local/bin/agenty-sandbox
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/agenty-sandbox"]
