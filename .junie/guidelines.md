# Project Guidelines for Junie

## Project Overview

SMerge (Subscriptions Merge) is a web service that joins data from multiple stealth proxy subscriptions and provides 
it in a single endpoint. It can decode and encode data in base64 format and supports group update periods.

### Key Features
- Merges multiple subscription sources into unified endpoints
- Supports both remote and local subscription sources
- Handles base64 encoding/decoding
- Configurable refresh periods for different subscription groups
- Rate limiting and concurrent request handling
- Filtering capabilities based on prefixes

## Project Structure

The project is organized into several Go packages:

- `cfg/`: Configuration handling and validation
- `crawler/`: Subscription data fetching and processing
- `limiter/`: Rate limiting functionality
- `server/`: HTTP server and API endpoints
- Root package: Main application entry point

## Building the Project

Junie should build the project before submitting any changes to ensure they compile correctly:

```bash
make build
```

For Docker-based builds:

```bash
make docker
```

## Testing

Junie should run tests to verify the correctness of any changes:

```bash
make test
```

This will run all tests with race detection and coverage reporting. For specific tests:

- Run tests for a specific package: `go test -v -race ./package_name`
- Run a specific test: `go test -v -run TestName ./package_name`

## Code Style Guidelines

When making changes, Junie should follow these style guidelines:

1. **Imports**: Standard library first, then third-party, then local packages
2. **Error Handling**: Use `errors.Join()` with sentinel errors for error wrapping
3. **Formatting**: Run `gofmt` before commits (enforced by `make lint`)
4. **Naming**: CamelCase for exported items, lower camelCase for unexported items
5. **Types**: Use strong typing with custom types (e.g., Duration, SubPath)
6. **Comments**: Add comments for exported functions and types
7. **Code Structure**: Place interfaces near their implementations
8. **Error Constants**: Define sentinel error variables at package level
9. **Testing**: Table-driven tests with descriptive names and error checking
10. **Logging**: Use structured logging with slog package

## Configuration

The application uses a JSON configuration file (default: `config.json`). 
Any changes to configuration handling should maintain backward compatibility and follow the existing structure.

Key configuration sections include:
- Main server settings (host, port, timeouts)
- Rate limiting options
- Subscription groups and their sources

## Making Changes

When implementing changes, Junie should:

1. Understand the existing code structure and patterns
2. Make minimal changes necessary to address the requirements
3. Ensure all tests pass after changes
4. Follow the established code style guidelines
5. Update documentation if necessary
6. Verify changes work with the example configuration
