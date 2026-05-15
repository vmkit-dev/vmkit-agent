# Supabyoi Agent

A Go-based deployment agent that runs on VMs to handle Supabase instance deployments.

## Overview

The Supabyoi Agent is a self-contained CLI binary that eliminates shell escaping issues and permission problems associated with Python SSH command execution. It provides idempotent operations with structured JSON output.

## Architecture

```
agent/
├── cmd/vmkit-agent/    # CLI entry point
├── internal/              # Core packages
│   ├── config/           # Configuration loading
│   ├── deploy/           # Deployment orchestration
│   ├── docker/           # Docker installation & management
│   ├── nginx/            # Nginx installation & configuration
│   └── tls/              # TLS certificate provisioning
└── pkg/types/            # Shared types
```

## Building

```bash
# Build for current OS
make build

# Build for Linux (production)
make build-linux

# Run tests
make test

# Check test coverage
make coverage
```

## Testing

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Test Coverage

Current coverage: **45.8%** (target: >70%)

By package:
- internal/config: 100%
- internal/tls: 100%
- internal/deploy: 76.7%
- cmd/vmkit-agent: 50.0%
- internal/docker: 37.9%
- internal/nginx: 33.3%

### CI Integration

Tests run automatically on GitHub Actions (`.github/workflows/agent-tests.yml`):
- Unit tests on every push
- Coverage reporting
- Integration tests with Docker/Nginx
- Binary build verification
