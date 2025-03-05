# SMerge Build & Code Style Guide

## Build Commands
- `make build` - Build the application
- `make test` - Run all tests with race detection and coverage
- `make lint` - Run linters and formatters
- `go test -race -cover ./...` - Run all tests with race detection and coverage
- `go test -v -race ./cfg` - Run tests for specific package
- `go test -v -run TestGroupValidate ./cfg` - Run specific test

## Style Guidelines
- **Imports**: Standard library first, then third-party, then local packages
- **Error Handling**: Use `errors.Join()` with sentinel errors for error wrapping
- **Formatting**: Run `gofmt` before commits (enforced by make lint)
- **Naming**: CamelCase for exported items, lower camelCase for unexported items
- **Types**: Use strong typing with custom types (e.g., Duration, SubPath)
- **Comments**: Add comments for exported functions and types
- **Code Structure**: Place interfaces near their implementations
- **Error Constants**: Define sentinel error variables at package level
- **Testing**: Table-driven tests with descriptive names and error checking
- **Logging**: Use structured logging with slog package