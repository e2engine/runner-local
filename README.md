# E2Engine Local Runner

Local test execution for E2Engine.

This module executes E2Engine test jobs locally using a configurable worker pool, manages runtime environment instances, and persists execution state through the E2Engine core execution interfaces.

## Installation

```bash
go get github.com/e2engine/runner-local
```

## Packages

**api**

Local runner service and configuration options for connecting test jobs, execution workers, runtime environment management, and execution state persistence.

**pkg/config**

Local runner configuration, including worker pool settings.

## Development

Run tests:

```bash
make test
```

Run the linter:

```bash
make lint
```

Run race detection:

```bash
make test-race
```

Run the complete verification suite:

```bash
make verify
```

## E2Engine

This repository is part of E2Engine.

- core — core domain model, execution logic, and public APIs
- repository — persistence implementations
- runner-local — local test execution
- cli — command-line interface
- tests — end-to-end tests for E2Engine
- demo — executable demonstration system and E2Engine examples

## License

Licensed under the Apache License, Version 2.0.