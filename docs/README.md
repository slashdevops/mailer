# Documentation

This directory contains deeper implementation guidance and runnable examples for the `mailer` package.

## Guides

- [Production guide](production-guide.md) — architecture, lifecycle, TLS, worker sizing, observability, security, and deployment recommendations.
- [Basic example](examples/basic.md) — a minimal end-to-end setup for a mail queue.
- [Custom backend example](examples/custom-backend.md) — implement the `MailerService` interface for a non-SMTP provider.

## Recommended reading order

1. Start with the [production guide](production-guide.md) to understand the package model.
2. Review the [basic example](examples/basic.md) to see a real integration path.
3. Read the [custom backend example](examples/custom-backend.md) if you deliver through an API (SES, SendGrid, etc.) instead of SMTP.
4. Use [../example/main.go](../example/main.go) as a template for your own service.

## API reference

The full, authoritative API reference is published on
[pkg.go.dev/github.com/slashdevops/mailer](https://pkg.go.dev/github.com/slashdevops/mailer).
