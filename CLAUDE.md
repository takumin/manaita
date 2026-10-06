# Development Guide

## Build Commands
- `task` - Run all tasks (generate, style, format, reviewdog, app)
- `task app:build` - Build the application
- `task app:test` - Run all tests with race detection and coverage
- `task format` - Format code (gofmt, shfmt, yamlfmt)
- `task reviewdog` - Run linters (golangci-lint, govet, gosec, etc.)
- Single test: `go test -race -cover ./path/to/package -run TestName`

## Code Style
- Follow Google Go Style Guide conventions ( gofmt )
- Use urfave/cli library for command structure
- Place tests in same directory as implementation files
- Document all public functions
- Use table-driven tests with clear error messages
- Handle errors with context ( fmt.Errorf )
- Use slog for structured logging
- Follow clean architecture with internal packages
- Use Functional Option Pattern for config packages

## Project Structure
- `/internal` - All internal code
- `/internal/command` - CLI command implementation
- `/internal/config` - Configuration management
- `/internal/version` - Version information
- Separate packages by logical concerns

## Error Handling
- Return errors with context
- Use exit codes for command-line errors
- Test error paths with mocked functions

## Pull Requests
- Enable auto-merge right after creating a PR: `gh pr merge <number> --auto --merge`
- If auto-merge is refused because the PR is already mergeable, report it and ask before merging directly
