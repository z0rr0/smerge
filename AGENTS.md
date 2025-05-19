# AGENTS: AI Assistants Guide for SMerge

This document provides information for AI agents working on the **SMerge** codebase, including:

- Project overview
- Directory structure
- Build and development workflow
- Code style conventions
- Testing style guidelines

## Project Overview

SMerge is a Go-based web service that merges multiple stealth proxy subscription feeds into a single HTTP endpoint. It can decode and encode data in base64 format and supports configurable group refresh periods.

## Repository Layout

```
.
├── cfg/           # configuration types and validation
├── crawler/       # subscription fetching and parsing logic
├── limiter/       # rate-limiting implementation
├── server/        # HTTP server and request handling
├── smerge.go      # main entrypoint
├── smerge_test.go # tests for the main package
├── config.json    # example runtime configuration
├── Makefile       # build, lint, and test commands
├── README.md      # project overview and usage
└── AGENTS.md      # this guide
```

## Build & Development Workflow

Use the provided Makefile for common tasks:

```bash
make build    # compile the application
make lint     # run format checks, linters, and static analysis
make test     # run tests with race detection and coverage
```

Alternatively, individual commands:

- `go fmt ./...`
- `go vet ./...`
- `golangci-lint run ./...`
- `go test -race -cover ./...`
- `go test -v -race ./cfg` - Run tests for specific package
- `go test -v -run TestGroupValidate ./cfg` - Run specific test

## Code Style Conventions

- **Imports**: Standard library first, then third-party, then local packages
- **Error Handling**: Use `errors.Join()` with sentinel errors for error wrapping
- **Formatting**: Run `gofmt` before commits (enforced by `make lint`)
- **Naming**: CamelCase for exported items, lower camelCase for unexported items
- **Types**: Use strong typing with custom types (e.g., Duration, SubPath)
- **Comments**: Add comments for exported functions and types
- **Code Structure**: Place interfaces near their implementations
- **Error Constants**: Define sentinel error variables at package level
- **Logging**: Use structured logging with slog package

## Testing Style Guidelines

- Write table-driven tests with descriptive test names
- Check returned errors explicitly and compare against expected sentinel errors
- Place test files alongside the code they cover, using the `_test.go` suffix
- Run tests with `go test -race -cover` to ensure coverage and race-condition detection

## Additional Agent Guidelines

- Focus on fixing issues at the root cause; avoid superficial patches
- Keep changes minimal and consistent with existing patterns
- Update documentation (e.g., README.md, CONFIG.md) when introducing new functionality or behavioral changes