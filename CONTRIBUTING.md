# Contributing

Issues and PRs welcome.

## Development

```bash
go build ./...
go test ./...
gofmt -l .   # must be empty
go vet ./...
```

Dashboard tests (`internal/dashboard`) need a Postgres instance to run against real data instead of skipping:

```bash
docker run -d --name skillci-test-pg -p 5433:5432 -e POSTGRES_PASSWORD=test -e POSTGRES_DB=skillci_test postgres:16
export SKILLCI_TEST_DATABASE_URL="postgres://postgres:test@localhost:5433/skillci_test?sslmode=disable"
```

Without that env var set, those tests skip cleanly — the rest of the suite runs with no external dependencies.

## Guidelines

- New behavior needs a test that would fail without it.
- Keep `gofmt`/`go vet` clean before opening a PR.
- Small, focused PRs over large ones.
