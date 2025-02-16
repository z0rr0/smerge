TARGET=smerge
TS=$(shell date -u +"%FT%T")
TAG=$(shell git tag | sort -V | tail -1)
VERSION=$(shell git rev-parse --short HEAD)
LDFLAGS=-X main.Version=$(TAG) -X main.Revision=git:$(VERSION) -X main.BuildDate=$(TS)
DOCKER_TAG=z0rr0/smerge

# coverage check example
# go test -cover -v -race -coverprofile=coverage.out -trace trace.out github.com/z0rr0/spts/client
# go tool cover -html=coverage.out

# linters
# go install github.com/securego/gosec/v2/cmd/gosec@latest
# go install honnef.co/go/tools/cmd/staticcheck@latest
# and https://golangci-lint.run/usage/install/#local-installation

all: test

build:
	go build -o $(PWD)/$(TARGET) -ldflags "$(LDFLAGS)"

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

docker: lint clean
	docker buildx build --platform linux/amd64 --build-arg LDFLAGS="$(LDFLAGS)" -t $(DOCKER_TAG) .

clean:
	rm -f $(PWD)/$(TARGET)
	find ./ -type f -name "*.out" -delete