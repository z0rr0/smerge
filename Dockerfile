ARG GOLANG_VERSION="1.23.5"

FROM golang:${GOLANG_VERSION}-alpine AS builder
ARG LDFLAGS
WORKDIR /go/src/github.com/z0rr0/smerge
COPY . .
RUN echo "LDFLAGS = $LDFLAGS"
RUN GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o ./smerge

FROM alpine:3.21
RUN apk --no-cache add ca-certificates
LABEL org.opencontainers.image.authors="me@axv.email" \
        org.opencontainers.image.url="https://hub.docker.com/r/z0rr0/smerge" \
        org.opencontainers.image.documentation="https://github.com/z0rr0/smerge" \
        org.opencontainers.image.source="https://github.com/z0rr0/smerge" \
        org.opencontainers.image.licenses="MIT" \
        org.opencontainers.image.title="SMerge" \
        org.opencontainers.image.description="Subscriptions merge tool"

COPY --from=builder /go/src/github.com/z0rr0/smerge/smerge /bin/smerge
RUN chmod 0755 /bin/smerge

VOLUME ["/data/"]
EXPOSE 43210
ENTRYPOINT ["/bin/smerge"]
CMD ["-config", "/data/config.json"]