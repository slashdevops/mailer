# Contributing

Thank you for helping improve `mailer`.

## Development workflow

1. Fork the repository and create a topic branch (`feat/...`, `fix/...`, `docs/...`).
2. Make focused changes and add or update tests and documentation.
3. Run the local verification suite:

   ```sh
   make check   # gofmt + go vet + go test -race + golangci-lint
   ```

   Or individually: `make fmt`, `make vet`, `make test`, `make lint`, `make cover`.
4. Open a pull request with a clear summary and rationale.

## Pull request expectations

- Include tests for any behavior change; new concurrent code must pass under `-race`.
- Keep the coverage threshold in `.testcoverage.yml` satisfied (`make cover`).
- Keep changes focused and update `CHANGELOG.md`.
- Mention any breaking changes clearly.

See [DEVELOPMENT_GUIDELINES.md](DEVELOPMENT_GUIDELINES.md) for coding, concurrency,
and testing standards.
