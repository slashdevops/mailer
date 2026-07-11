// Command example demonstrates how to wire the mailer package into a service:
// configure an SMTP transport, run a worker pool, enqueue validated messages,
// and shut down gracefully on SIGINT/SIGTERM.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slashdevops/mailer"
)

func main() {
	// Configure the SMTP transport. Credentials come from the environment so
	// they never end up in source control. RequireTLS refuses to send unless the
	// connection is secured (implicit TLS on 465 or STARTTLS otherwise).
	smtpMailer, err := mailer.NewMailerSMTP(mailer.MailerSMTPConf{
		SMTPHost:   os.Getenv("SMTP_HOST"),
		SMTPPort:   587,
		Username:   os.Getenv("SMTP_USER"),
		Password:   os.Getenv("SMTP_PASS"),
		RequireTLS: true,
	})
	if err != nil {
		slog.Error("failed to configure SMTP mailer", "error", err)
		os.Exit(1)
	}

	// Cancel the worker context when an interrupt signal arrives.
	appCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mailService, err := mailer.NewMailService(&mailer.MailServiceConfig{
		Ctx:         appCtx,
		WorkerCount: 5,
		QueueSize:   256,
		Timeout:     30 * time.Second,
		Mailer:      smtpMailer,
	})
	if err != nil {
		slog.Error("failed to create mail service", "error", err)
		os.Exit(1)
	}

	mailService.Start()
	slog.Info("mail service started, press Ctrl+C to stop")

	// Enqueue a few sample messages from a background goroutine.
	go func() {
		for i := range 10 {
			content, err := mailer.NewMailContentBuilder().
				WithFromName("Awesome Sender").
				WithFromAddress("sender@example.com").
				WithToName("Valued Recipient").
				WithToAddress("recipient@example.com").
				WithMimeType(mailer.MimeTypeTextPlain).
				WithSubject(fmt.Sprintf("Test Email %d", i+1)).
				WithBody(fmt.Sprintf("This is the body of test email #%d.", i+1)).
				Build()
			if err != nil {
				slog.Error("failed to build mail content", "index", i, "error", err)
				continue
			}

			if err := mailService.Enqueue(content); err != nil {
				slog.Error("failed to enqueue email", "index", i, "error", err)
				return
			}
			slog.Info("email enqueued", "index", i)
		}
	}()

	<-appCtx.Done()
	slog.Info("shutdown signal received, stopping mail service")

	// Stop stops accepting new work, drains the queue, and waits for workers.
	mailService.Stop()
	slog.Info("mail service stopped gracefully")
}
