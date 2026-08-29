# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `MailerMailgun`, a second built-in transport delivering through Mailgun's
  messages HTTP API over HTTPS. It reaches providers from environments that
  block outbound SMTP ports and reports delivery failures synchronously rather
  than by bounce. Accepts an optional `*http.Client` so a caller's existing
  timeout, retry policy and connection pool are inherited rather than
  duplicated; refuses a non-`https` URL, because the API key travels as a
  basic-auth header on every request.
- Exported read accessors on `MailContent` (`FromName`, `FromAddress`, `ToName`,
  `ToAddress`, `MimeType`, `Subject`, `Body`) so external `MailerService`
  implementations can read message fields.
- TLS support in the SMTP transport: implicit TLS/SMTPS (`ImplicitTLS`, implied
  on port 465) and opportunistic/required STARTTLS (`RequireTLS`).
- Configurable SMTP `DialTimeout`, `LocalName` (EHLO), and custom `TLSConfig`.
- `MailServiceConfig.QueueSize` to size the buffered queue independently of the
  worker count.
- Per-message delivery deadline via `MailServiceConfig.Timeout`.
- Header-injection protection: the builder rejects CR/LF/NUL in header fields.
- Error wrapping via `MailerError.Err`/`Unwrap` for `errors.Is`/`errors.As`.
- In-process SMTP test server covering plain, STARTTLS, and implicit TLS paths;
  concurrency tests run under `-race`.
- Project docs (`docs/`), `Makefile`, `.golangci.yaml`, `DEVELOPMENT_GUIDELINES.md`.

### Changed

- **Fixed a deadlock**: `Enqueue` no longer holds an exclusive lock during a
  blocking channel send, so a full queue can no longer block `Start`/`Stop`.
  Concurrency now uses an `RWMutex` plus an atomic `started` flag.
- SMTP messages are rendered with CRLF line endings and a UTF-8 `Content-Type`.
- `SMTPPort` accepts the full valid range (1–65535) instead of a fixed allowlist.
- Address length validation raised to RFC-compatible bounds (up to 254 chars);
  body limit raised to 256 KiB.
- SMTP transport errors now wrap their underlying cause.

### Security

- STARTTLS/implicit TLS support with `RequireTLS` prevents sending credentials
  or content over unencrypted connections.
- SMTP header/command injection is prevented at message-build time.
