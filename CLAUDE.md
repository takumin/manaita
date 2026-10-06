# Development Guide

## Build Commands

- `task` - Install the tools, then run generate, format, reviewdog and app
- `task aqua` - Install the tools pinned in `.aqua/`
- `task generate` - Run the code generators
- `task format` - Format code
- `task reviewdog` - Run all linters through reviewdog
- `task reviewdog:<linter>` - Run a single linter
- `task app` - Run mod, test and build
- `task app:mod` - Download, tidy and verify the Go modules
- `task app:test` - Run all tests with race detection and coverage
- `task app:build` - Build the binary for the host platform into `dist/`
- `task app:build:release` - Build the binaries for all release platforms
- `task app:clean` - Remove the coverage and build outputs
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
- `/internal/command` - CLI command implementation, one subpackage per subcommand
  - `apply` - Apply the recipes to a host (this machine when no host is given)
  - `plan` - Show the changes apply would make (mitamae dry run)
  - `show` - Show the node files, recipes and command of a host
  - `list` - List the hosts of the inventory
  - `complete` - Shell completion of the subcommands
- `/internal/config` - Configuration management
- `/internal/project` - Load a project: manifest, host inventory, node attribute files and recipes
- `/internal/deploy` - Run the recipes on hosts, locally or over ssh with the project copied by rsync
- `/internal/mitamae` - Fetch the mitamae release binaries and build their command lines
- `/internal/logging` - Build the logger of a command run and carry it in a context
- `/internal/metadata` - Application name, description and author
- `/internal/version` - Version information
- `/internal/testutil` - Project fixtures for the tests
- Separate packages by logical concerns

## Error Handling

- Return errors with context
- Use exit codes for command-line errors
- Test error paths with mocked functions

## Pull Requests

- Enable auto-merge right after creating a PR: `gh pr merge <number> --auto --merge`
- If auto-merge is refused because the PR is already mergeable, report it and ask before merging directly
