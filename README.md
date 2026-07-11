# mailer

[![Go Reference](https://pkg.go.dev/badge/github.com/slashdevops/mailer.svg)](https://pkg.go.dev/github.com/slashdevops/mailer)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/slashdevops/mailer?style=plastic)
[![license](https://img.shields.io/github/license/slashdevops/mailer.svg)](https://github.com/slashdevops/mailer/blob/main/LICENSE)
[![Release](https://github.com/slashdevops/mailer/actions/workflows/release.yml/badge.svg)](https://github.com/slashdevops/mailer/actions/workflows/release.yml)
[![release](https://img.shields.io/github/release/slashdevops/mailer/all.svg)](https://github.com/slashdevops/mailer/releases)

`mailer` is a production-oriented Go library for **asynchronous, concurrent email delivery**. It combines a buffered queue, a worker-pool runtime, context-aware lifecycle control, validated message construction, and a pluggable transport (with a batteries-included, TLS-capable SMTP backend) so you can offload email sending from your request path without blocking.

```text
        Enqueue()                buffered queue              worker pool            MailerService
producer ─────────▶ ┌──────────────────────────────┐ ─────▶ [w1 w2 … wN] ─────▶ Send(ctx, MailContent)
                    └──────────────────────────────┘                                    │
                          back-pressure when full                                       ▼
                                                                                   SMTP / custom
```

## Features

- **Concurrent worker pool** — configurable number of workers drain a shared queue.
- **Back-pressure** — a bounded, buffered queue; `Enqueue` blocks (or fails on context cancellation) when full instead of growing unbounded.
- **Graceful shutdown** — `Stop()` closes the queue, drains in-flight work, and waits for workers. Context cancellation stops workers immediately.
- **Pluggable transports** — implement the small `MailerService` interface to send through any provider; `MailContent` exposes read accessors so external backends work.
- **TLS-capable SMTP** — implicit TLS (SMTPS, port 465) and opportunistic/required STARTTLS, PLAIN auth, configurable dial timeout and EHLO name.
- **Validated, injection-safe content** — a fluent `MailContentBuilder` validates addresses, MIME type, and lengths, and rejects CR/LF/NUL in header fields (SMTP header-injection protection).
- **Observability** — structured `log/slog` logging and typed, wrappable errors (`errors.Is`/`errors.As`).
- **Zero third-party dependencies** — standard library only.

## Requirements

- Go **1.25** or newer.

## Installation

```sh
go get github.com/slashdevops/mailer@latest
```

```go
import "github.com/slashdevops/mailer"
```

## Quick start

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/slashdevops/mailer"
)

func main() {
	// 1. Configure a transport (the built-in SMTP backend).
	smtpMailer, err := mailer.NewMailerSMTP(mailer.MailerSMTPConf{
		SMTPHost:   os.Getenv("SMTP_HOST"),
		SMTPPort:   587,
		Username:   os.Getenv("SMTP_USER"),
		Password:   os.Getenv("SMTP_PASS"),
		RequireTLS: true, // refuse to send over an unencrypted connection
	})
	if err != nil {
		slog.Error("invalid SMTP configuration", "error", err)
		os.Exit(1)
	}

	// 2. Bind the worker context to OS signals for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 3. Create and start the queue-backed service.
	service, err := mailer.NewMailService(&mailer.MailServiceConfig{
		Ctx:         ctx,
		WorkerCount: 4,
		QueueSize:   256,
		Mailer:      smtpMailer,
	})
	if err != nil {
		slog.Error("failed to create mail service", "error", err)
		os.Exit(1)
	}
	service.Start()

	// 4. Build a validated message and enqueue it.
	content, err := mailer.NewMailContentBuilder().
		WithFromName("Operations").
		WithFromAddress("ops@example.com").
		WithToName("Customer").
		WithToAddress("customer@example.com").
		WithMimeType(mailer.MimeTypeTextPlain).
		WithSubject("Hello from mailer").
		WithBody("This is a queued email.").
		Build()
	if err != nil {
		slog.Error("invalid email content", "error", err)
		os.Exit(1)
	}
	if err := service.Enqueue(content); err != nil {
		slog.Error("failed to enqueue email", "error", err)
	}

	// 5. Shut down gracefully: drain the queue and wait for workers.
	<-ctx.Done()
	service.Stop()
}
```

A complete, runnable program lives in [example/main.go](example/main.go).

## Concepts

| Type | Responsibility |
| ---- | -------------- |
| `MailContentBuilder` / `MailContent` | Build and validate an immutable message. |
| `MailService` | Queue messages and dispatch them through a worker pool. |
| `MailerService` (interface) | Transport contract: `Send(ctx, MailContent) error`. |
| `MailerSMTP` | Standard-library SMTP transport with TLS/STARTTLS and auth. |

### `MailServiceConfig`

| Field | Default | Description |
| ----- | ------- | ----------- |
| `Ctx` | `context.Background()` | Governs worker lifetime; cancel to stop workers. |
| `WorkerCount` | — (required, 1–100) | Number of concurrent workers. |
| `QueueSize` | `WorkerCount` | Buffered queue capacity (burst absorption). |
| `Timeout` | `0` (disabled) | Per-message deadline applied to each `Send`. |
| `Mailer` | — (required) | The transport implementation. |

### `MailerSMTPConf`

| Field | Default | Description |
| ----- | ------- | ----------- |
| `SMTPHost` / `SMTPPort` | — (required) | Server host and port (1–65535). |
| `Username` / `Password` | empty | PLAIN auth credentials; empty disables auth. |
| `ImplicitTLS` | `false` (implied on 465) | TLS from the first byte (SMTPS). |
| `RequireTLS` | `false` | Fail rather than send over an unencrypted link. |
| `TLSConfig` | verify against `SMTPHost`, TLS 1.2+ | Custom `*tls.Config`. |
| `DialTimeout` | `10s` | TCP connection timeout. |
| `LocalName` | `localhost` | Name announced in EHLO/HELO. |

## Lifecycle & shutdown semantics

- `Start()` is idempotent and launches `WorkerCount` goroutines.
- `Enqueue()` is safe for concurrent use. It blocks while the queue is full and returns `ErrServiceStopped` after `Stop()`, or the context error if the context is cancelled first.
- `Stop()` is idempotent and safe from multiple goroutines. It stops accepting work, closes the queue, and **drains queued messages** before returning.
- Cancelling `Ctx` stops workers promptly **without** draining; use it for hard shutdown and `Stop()` for graceful shutdown.

## Custom transports

`MailContent` exposes read accessors (`FromName()`, `FromAddress()`, `ToName()`, `ToAddress()`, `MimeType()`, `Subject()`, `Body()`), so any backend can implement `MailerService`:

```go
type ConsoleMailer struct{}

func (ConsoleMailer) Send(ctx context.Context, m mailer.MailContent) error {
	slog.Info("would send", "to", m.ToAddress(), "subject", m.Subject())
	return nil
}
```

See [docs/examples/custom-backend.md](docs/examples/custom-backend.md).

## Documentation

- [Docs overview](docs/README.md)
- [Production guide](docs/production-guide.md) — lifecycle, TLS, worker sizing, observability, testing.
- [Basic example](docs/examples/basic.md)
- [Custom backend example](docs/examples/custom-backend.md)
- [GoDoc reference](https://pkg.go.dev/github.com/slashdevops/mailer)

## Development

```sh
make test       # go test -race with coverage
make lint       # golangci-lint
make cover      # enforce the coverage threshold
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [DEVELOPMENT_GUIDELINES.md](DEVELOPMENT_GUIDELINES.md).

## Security

Report vulnerabilities per [SECURITY.md](SECURITY.md). The builder rejects header-injection attempts (CR/LF/NUL in header fields), and the SMTP backend supports `RequireTLS` to avoid sending credentials or content over unencrypted connections.

## License

Licensed under the Apache License 2.0. See [LICENSE](LICENSE).
