TARGET=smerge
TS=$(shell date -u +"%FT%T")
TAG=$(shell git tag | sort -V | tail -1 | grep . || echo "v0.0.0")
VERSION=$(shell git rev-parse --short HEAD)

# GO_LDFLAGS, not LDFLAGS: the latter conventionally carries C linker flags and
# is commonly exported by the host shell (Homebrew sets it), which would leak
# into the build.
GO_LDFLAGS=-X main.Version=$(TAG) -X main.Revision=git:$(VERSION) -X main.BuildDate=$(TS)

IMAGE=z0rr0/smerge
# Docker Hub tags of this image carry no "v" prefix: 1.9.3, 1.9.2, ...
IMAGE_TAG=$(TAG:v%=%)
BUILDER=smerge-multiarch
DOCKER_PLATFORMS=linux/amd64,linux/arm64

# coverage check example
# go test -cover -v -race -coverprofile=coverage.out -trace trace.out github.com/z0rr0/spts/client
# go tool cover -html=coverage.out

# linters
# go install github.com/securego/gosec/v2/cmd/gosec@latest
# go install honnef.co/go/tools/cmd/staticcheck@latest
# and https://golangci-lint.run/usage/install/#local-installation

all: test

build:
	go build -o $(PWD)/$(TARGET) -ldflags "$(GO_LDFLAGS)"

fmt:
	gofmt -d .

check_fmt:
	@test -z "`gofmt -l .`" || { echo "ERROR: failed gofmt, for more details run - make fmt"; false; }
	@-echo "gofmt successful"

lint: check_fmt
	go vet $(PWD)/...
	-golangci-lint run $(PWD)/...
	-govulncheck ./...
	-staticcheck ./...
	-gosec ./...

test: build lint
	go test -race -cover $(PWD)/...

gh: build
	go test -race -cover $(PWD)/...

# Host-arch only: the default Docker Desktop builder uses the overlay2 image
# store, which cannot --load a multi-arch manifest list.
docker: lint clean
	docker buildx build \
		-t $(IMAGE):latest -t $(IMAGE):$(IMAGE_TAG) \
		--build-arg GO_LDFLAGS="$(GO_LDFLAGS)" \
		--load .

# Multi-arch goes straight to the registry: the `docker` driver cannot build a
# manifest list at all, so this needs its own docker-container builder, created
# on first use and reused afterwards. Not a dependency of `docker` — that would
# build the image twice.
docker-push: lint clean
	@test "$(TAG)" != "v0.0.0" || (echo "no git tag found, refusing to push $(IMAGE):v0.0.0" && exit 1)
	@docker buildx inspect $(BUILDER) >/dev/null 2>&1 || docker buildx create --name $(BUILDER) --driver docker-container
	docker buildx build --builder $(BUILDER) \
		--platform $(DOCKER_PLATFORMS) \
		-t $(IMAGE):latest -t $(IMAGE):$(IMAGE_TAG) \
		--build-arg GO_LDFLAGS="$(GO_LDFLAGS)" \
		--push .

clean:
	rm -f $(PWD)/$(TARGET)
	find ./ -type f -name "*.out" -delete