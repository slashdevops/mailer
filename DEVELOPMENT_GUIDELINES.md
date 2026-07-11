# Development Guidelines

These guidelines keep `mailer` consistent, correct, and easy to maintain.

## Requirements

- Go **1.25+**.
- `golangci-lint` for linting (`make lint`).

## Project layout

The package is intentionally flat (a library, not an application):

| File | Responsibility |
| ---- | -------------- |
| `mailer.go` | `MailContent`, `MailContentBuilder`, validation, `MimeType`, `MailerError`. |
| `service.go` | `MailService` queue and worker pool, `MailQueueError`. |
| `smtp.go` | `MailerSMTP` transport (TLS/STARTTLS, auth, message rendering). |
| `doc.go` | Package-level documentation. |
| `*_test.go` | Unit and integration tests (including an in-process SMTP server). |
| `example/` | A runnable usage example (`package main`). |
| `docs/` | Guides and examples. |

## Coding standards

- Format with `gofmt` (`make fmt`); no unformatted code is merged.
- Keep the package **dependency-free** — standard library only.
- Every exported identifier has a doc comment starting with its name.
- Return typed errors (`*MailerError`, `*MailQueueError`) and wrap causes with
  the `Err` field so `errors.Is`/`errors.As` work.
- Log through `log/slog` at appropriate levels; never log credentials or message bodies.
- Public API changes are additive where possible. `MailContent` stays immutable —
  add accessors rather than exported fields.

## Concurrency rules

- `MailService` is safe for concurrent use. The `content` channel is closed
  exactly once, under the write lock, in `Stop`; `Enqueue` holds the read lock
  for the duration of a send so the channel can never be closed mid-send.
- Never perform a blocking channel send while holding an exclusive lock.
- All new concurrent behavior must be covered by tests that pass under `-race`.

## Testing

- Run `make test` (race detector + coverage) before opening a PR.
- Maintain the coverage threshold in `.testcoverage.yml` (`make cover`).
- Prefer the in-process `fakeSMTPServer` over network access for transport tests.
- Table-driven tests for validation and configuration.

## Commit and PR workflow

1. Branch from `main` (`feat/...`, `fix/...`, `docs/...`, `chore/...`).
2. Keep changes focused; add or update tests and docs.
3. Run `make check` locally.
4. Open a PR with a clear summary and rationale; call out breaking changes.

## Releases

Releases are tag-driven (`vX.Y.Z`). Pushing a matching tag triggers the release
workflow. Update `CHANGELOG.md` before tagging.
