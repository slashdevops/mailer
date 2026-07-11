/*
Package mailer provides a production-oriented, concurrent email sending service
for Go applications.

It features a queue-based dispatcher (MailService) backed by a pool of worker
goroutines, so applications can enqueue validated messages quickly without
blocking on delivery. Delivery is performed by any implementation of the
MailerService interface; a standard-library SMTP transport (MailerSMTP) with
TLS/STARTTLS and authentication is included.

Key features:

  - Concurrent delivery via a configurable worker pool.
  - Bounded, buffered queue with back-pressure.
  - Graceful shutdown (queue drain) and context-driven hard shutdown.
  - Pluggable transports via the MailerService interface; MailContent exposes
    read accessors so external backends can read every field.
  - SMTP transport with implicit TLS (SMTPS), opportunistic/required STARTTLS,
    PLAIN auth, and a configurable dial timeout and EHLO name.
  - Validated, injection-safe message construction via MailContentBuilder
    (CR/LF/NUL are rejected in header fields).
  - Structured logging (log/slog) and typed, wrappable errors.

Typical usage:

 1. Configure a transport (for example, NewMailerSMTP).
 2. Configure and create the MailService with a worker count and the transport.
 3. Start the service.
 4. Build validated MailContent with NewMailContentBuilder and Enqueue it.
 5. Stop the service for a graceful, queue-draining shutdown, or cancel the
    context for an immediate stop.

See ExampleMailService_Enqueue for a runnable demonstration, and the docs
directory for the production guide and additional examples.
*/
package mailer
