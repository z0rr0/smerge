ARG GOLANG_VERSION=1.27.1
ARG ALPINE_VERSION=3.24

# The builder runs natively on the host arch ($BUILDPLATFORM) and cross-compiles:
# with CGO_ENABLED=0 that is free, while an emulated builder would not be.
FROM --platform=$BUILDPLATFORM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS builder
WORKDIR /build

COPY . .
ARG GO_LDFLAGS=""
# TARGETOS/TARGETARCH come from BuildKit's --platform: `make docker` builds for
# the host arch, `make docker-push` for linux/amd64 and linux/arm64. Not named
# LDFLAGS: that one conventionally holds C linker flags and is often already
# exported by the host shell (Homebrew sets it).
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "$GO_LDFLAGS" -o /app/smerge .

# No --platform here: BuildKit resolves this to the target platform, so the
# binary above and this base image always match.
FROM alpine:${ALPINE_VERSION}
# ca-certificates for HTTPS subscriptions
RUN apk --no-cache add ca-certificates
RUN adduser -D -u 1000 appuser
LABEL org.opencontainers.image.authors="me@axv.email" \
    org.opencontainers.image.url="https://hub.docker.com/r/z0rr0/smerge" \
    org.opencontainers.image.documentation="https://github.com/z0rr0/smerge" \
    org.opencontainers.image.source="https://github.com/z0rr0/smerge" \
    org.opencontainers.image.licenses="MIT" \
    org.opencontainers.image.title="SMerge" \
    org.opencontainers.image.description="Subscriptions merge tool"

COPY --from=builder /app/smerge /bin/smerge
USER appuser
VOLUME ["/data/"]
EXPOSE 43210
ENTRYPOINT ["/bin/smerge"]
CMD ["-config", "/data/config.json"]
