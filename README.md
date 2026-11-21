# E2Engine Local Runner

Local test execution for E2Engine.

Part of [E2Engine](https://e2engine.dev), an open-source platform for declarative end-to-end testing.

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

## Links

- [E2Engine website](https://e2engine.dev)
- [E2Engine documentation](https://e2engine.dev/docs)
- [E2Engine on GitHub](https://github.com/e2engine)

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).