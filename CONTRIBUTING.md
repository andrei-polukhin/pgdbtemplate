# Contributing to pgdbtemplate

We welcome contributions to pgdbtemplate! This document provides guidelines for contributing to the project.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally
3. Create a new branch for your feature or bugfix
4. Make your changes
5. Add or update tests as necessary
6. Ensure all tests pass
7. Submit a pull request

## Development Environment

### Prerequisites

- Go 1.21 or later
- PostgreSQL 9.5 or later (for testing)
- Git

### Setup

```bash
# Clone your fork
git clone https://github.com/yourusername/pgdbtemplate.git
cd pgdbtemplate

# Install dependencies
go mod tidy

# Run tests
go test -v ./...

# Run tests with race detection
go test -race -v ./...

# Check code formatting
go fmt ./...

# Run linter
go vet ./...
```

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Add comments for all exported functions and types
- End all comment sentences with full stops
- Use meaningful variable and function names
- Keep functions focused and small

### Comment Guidelines

```go
// Good: DatabaseConnection represents any PostgreSQL database connection.
type DatabaseConnection interface { ... }

// Bad: DatabaseConnection represents any PostgreSQL database connection
type DatabaseConnection interface { ... }
```

## Testing

### Test Requirements

- All new features must include tests
- Tests should use real PostgreSQL connections when possible
- Use the `pgdbtemplate_test` package for external API testing
- Use `frankban/quicktest` for test assertions
- Maintain or improve test coverage

### Running Tests

```bash
# Run all tests
go test -v ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run tests against PostgreSQL container
docker run -d --name postgres-test -p 5432:5432 \
  -e POSTGRES_PASSWORD=password postgres:15
export POSTGRES_CONNECTION_STRING="postgres://postgres:password@localhost:5432/postgres?sslmode=disable"
go test -v ./...
docker rm -f postgres-test
```

### GitHub Actions

All pull requests are automatically tested using GitHub Actions with:
- PostgreSQL 15 service container
- Go 1.21
- Race detection
- Coverage reporting

## Submitting Changes

### Pull Request Process

1. **Create a feature branch** from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes** with clear, atomic commits:
   ```bash
   git commit -m "Add feature: description of what you added"
   ```

3. **Update documentation** if necessary

4. **Add tests** for new functionality

5. **Ensure all tests pass**:
   ```bash
   go test -v ./...
   go test -race -v ./...
   ```

6. **Push to your fork** and create a pull request

### Commit Messages

Use clear, descriptive commit messages:

```
Good:
- Add support for custom admin database names
- Fix race condition in template initialization
- Update documentation for testcontainers integration

Bad:
- Fix bug
- Update code
- Changes
```

### Pull Request Guidelines

- **Title**: Clear, descriptive title explaining what the PR does
- **Description**: Explain the motivation and changes made
- **Tests**: Include tests for new features or bug fixes
- **Documentation**: Update README.md if the API changes
- **Breaking changes**: Clearly mark any breaking changes

## Code Architecture

### Core Components

- **TemplateManager**: Main orchestrator for template database management
- **ConnectionProvider**: Abstraction for database connections
- **MigrationRunner**: Interface for running database migrations
- **DatabaseConnection**: Wrapper around database/sql connections

### Design Principles

- **PostgreSQL-specific**: Optimized for PostgreSQL features
- **Thread-safe**: Safe for concurrent usage
- **Interface-based**: Pluggable components via interfaces
- **Context-aware**: All operations accept context.Context
- **Error-transparent**: Clear error messages with context

## Issue Reporting

### Bug Reports

Include the following information:
- Go version
- PostgreSQL version
- Operating system
- Code example that reproduces the issue
- Expected vs actual behavior
- Error messages and stack traces

### Feature Requests

- Describe the use case and motivation
- Provide examples of how the feature would be used
- Consider backward compatibility
- Discuss implementation approaches if you have ideas

## Code Review

### Review Criteria

Pull requests are reviewed for:
- **Correctness**: Does the code work as intended?
- **Tests**: Are there adequate tests?
- **Performance**: Does it maintain or improve performance?
- **API design**: Is the API consistent and usable?
- **Documentation**: Are changes properly documented?
- **Style**: Does it follow Go conventions?

### Response Time

- We aim to review pull requests within 48 hours
- Complex changes may take longer
- Feel free to ping maintainers if a PR has been waiting more than a week

## Release Process

Releases follow semantic versioning (semver):
- **Major** (1.0.0): Breaking changes
- **Minor** (0.1.0): New features, backward compatible
- **Patch** (0.0.1): Bug fixes, backward compatible

## Getting Help

- **GitHub Issues**: For bugs, feature requests, and questions
- **Discussions**: For general questions and community interaction
- **Code review**: For feedback on implementation approaches

## Recognition

Contributors are recognized in:
- GitHub contributors list
- Release notes for significant contributions
- Special thanks for first-time contributors

## License

By contributing to pgdbtemplate, you agree that your contributions will be licensed under the MIT License.

## Community Guidelines

- Be respectful and inclusive
- Help newcomers get started
- Provide constructive feedback
- Focus on the code and ideas, not the person
- Assume good intentions

Thank you for contributing to pgdbtemplate! 🚀
