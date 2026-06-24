# mailer

[![Go Reference](https://pkg.go.dev/badge/github.com/slashdevops/mailer.svg)](https://pkg.go.dev/github.com/slashdevops/mailer)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/slashdevops/mailer?style=plastic)
[![Go Report Card](https://goreportcard.com/badge/github.com/slashdevops/mailer)](https://goreportcard.com/report/github.com/slashdevops/mailer)

This package provides a robust and concurrent email sending service for Go applications. It allows queueing emails and sending them asynchronously using a pool of workers via a configurable backend (e.g., SMTP).

## Features

* **Concurrent Sending:** Uses a worker pool to send emails concurrently.
* **Buffering:** Queues emails in a buffered channel, sized according to the worker count.
* **Graceful Shutdown:** Supports context cancellation for stopping workers and waits for them to finish processing enqueued items.
* **Pluggable Backend:** Uses a `MailerService` interface, allowing different sending mechanisms (e.g., SMTP, API-based services). An SMTP implementation (`MailerSMTP`) is included.
* **Content Validation:** Includes a builder (`MailContentBuilder`) for creating validated `MailContent` with checks for field lengths and allowed MIME types.
* **Context Propagation:** Leverages `context.Context` for cancellation and timeout propagation throughout the sending process.
* **Structured Logging:** Uses the standard `log/slog` package for informative logging.
* **Error Handling:** Provides specific error types (`MailerError`, `MailQueueError`) for better error management.
* **Customizable Worker Count:** Allows configuring the number of concurrent workers within defined limits.
* **MIME Type Support:** Supports `text/plain` and `text/html` MIME types.
* **Sender and Recipient Details:** Allows specifying sender and recipient names along with email addresses.

## Installation

To use this library in your project, install it using `go get`:

```sh
go get github.com/slashdevops/mailer@latest
````

## Components

* **`MailService`**: The main service that manages the email queue and worker pool.
* **`MailContent` / `MailContentBuilder`**: Struct and builder for defining email content (sender, recipient, subject, body, MIME type).
* **`MailerService`**: Interface for the actual email sending logic.
* **`MailerSMTP`**: An implementation of `MailerService` using standard SMTP.

## Configuration

### `MailService`

Configure the `MailService` using `MailServiceConfig`:

```go
type MailServiceConfig struct {
    Ctx         context.Context // Optional: Parent context for cancellation.
    WorkerCount int             // Number of concurrent sending workers (1-100).
    Timeout     time.Duration   // Optional: Timeout for operations (currently unused in core service logic but available).
    Mailer      MailerService   // The backend mailer implementation (e.g., MailerSMTP).
}
```

### `MailerSMTP`

Configure the `MailerSMTP` backend using `MailerSMTPConf`:

```go
type MailerSMTPConf struct {
    SMTPHost string // SMTP server hostname.
    SMTPPort int    // SMTP server port (e.g., 587, 465, 25).
    Username string // SMTP username for authentication.
    Password string // SMTP password for authentication.
}
```

## Usage Example

```go
package main

import (
  "context"
  "fmt"
  "log/slog"
  "os"
  "os/signal"
  "syscall"
  "time"

  "github.com/slashdevops/mailer" // Assuming this is the module path
)

func main() {
  // --- Configuration ---
  smtpConf := mailer.MailerSMTPConf{
    SMTPHost: "smtp.example.com", // Replace with your SMTP host
    SMTPPort: 587,                // Replace with your SMTP port
    Username: "user@example.com", // Replace with your SMTP username
    Password: "your_password",    // Replace with your SMTP password
  }

  smtpMailer, err := mailer.NewMailerSMTP(smtpConf)
  if err != nil {
    slog.Error("Failed to configure SMTP mailer", "error", err)
    os.Exit(1)
  }

  // Create a context that can be cancelled
  appCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
  defer cancel()

  mailServiceConf := &mailer.MailServiceConfig{
    Ctx:         appCtx,     // Use the cancellable context
    WorkerCount: 5,          // Number of concurrent workers
    Mailer:      smtpMailer, // Use the configured SMTP mailer
  }

  mailService, err := mailer.NewMailService(mailServiceConf)
  if err != nil {
    slog.Error("Failed to create mail service", "error", err)
    os.Exit(1)
  }

  // --- Start the Service ---
  // Start the service with the application context
  mailService.Start(appCtx)
  slog.Info("Mail service started. Press Ctrl+C to stop.")

  // --- Enqueue Emails ---
  go func() {
    // Example of enqueuing emails
    for i := 0; i < 10; i++ {
      subject := fmt.Sprintf("Test Email %d", i+1)
      body := fmt.Sprintf("This is the body of test email #%d.", i+1)

      content, err := (&mailer.MailContentBuilder{}).
        WithFromName("Sender Name").
        WithFromAddress("sender@example.com").
        WithToName("Recipient Name").
        WithToAddress("recipient@example.com"). // Replace with a valid recipient
        WithMimeType("text/plain").
        WithSubject(subject).
        WithBody(body).
        Build()

      if err != nil {
        slog.Error("Failed to build mail content", "error", err)
        continue // Skip this email
      }

      err = mailService.Enqueue(content)
      if err != nil {
        // This might happen if the context is cancelled while enqueuing
        slog.Error("Failed to enqueue email", "error", err)
        // If context is cancelled, we should probably stop trying to enqueue
        if appCtx.Err() != nil {
          break
        }
      } else {
        slog.Info("Email enqueued", "subject", subject)
      }
      time.Sleep(500 * time.Millisecond) // Simulate some delay between emails
    }
    slog.Info("Finished enqueuing sample emails.")
  }()

  // --- Wait for Shutdown Signal ---
  <-appCtx.Done() // Block until context is cancelled (Ctrl+C)

  slog.Info("Shutdown signal received.")

  // --- Stop the Service Gracefully ---
  // Stop accepting new emails and wait for workers to finish
  // Note: Stop() closes the channel. If context cancellation is the primary
  // shutdown mechanism, workers will stop based on <-ctx.Done().
  // Calling Stop() ensures the channel is closed if not already done by context cancellation propagation.
  // Depending on exact needs, you might just rely on context cancellation and use Wait().
  // Using Stop() here is generally safer for ensuring cleanup.
  mailService.Stop() // This also calls Wait() internally after closing the channel

  slog.Info("Mail service stopped gracefully.")
}
```

## Contributing

Contributions are welcome! Please feel free to submit pull requests or open issues.

## License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE) file for details.
