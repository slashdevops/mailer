// filepath: example/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slashdevops/mailer" // Import the library
)

func main() {
	// --- Configuration ---
	// Configure the SMTP backend
	smtpConf := mailer.MailerSMTPConf{
		SMTPHost: os.Getenv("SMTP_HOST"), // Use environment variables for sensitive data
		SMTPPort: 587,                    // Or get from env
		Username: os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASS"),
	}

	smtpMailer, err := mailer.NewMailerSMTP(smtpConf)
	if err != nil {
		slog.Error("Failed to configure SMTP mailer", "error", err)
		os.Exit(1)
	}

	// Create a context that handles OS interrupt signals for graceful shutdown
	appCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel() // Ensure cancellation propagates

	// Configure the mail service
	mailServiceConf := &mailer.MailServiceConfig{
		Ctx:         appCtx,     // Pass the application context
		WorkerCount: 5,          // Set desired number of concurrent workers
		Mailer:      smtpMailer, // Provide the configured SMTP mailer backend
	}

	mailService, err := mailer.NewMailService(mailServiceConf)
	if err != nil {
		slog.Error("Failed to create mail service", "error", err)
		os.Exit(1)
	}

	// --- Start the Service ---
	// Start the mail service workers. They will listen on the internal channel.
	mailService.Start() // Pass the context again for workers
	slog.Info("Mail service started. Press Ctrl+C to stop.")

	// --- Enqueue Emails (Example in a Goroutine) ---
	go func() {
		// Example loop to enqueue emails
		for i := 0; i < 10; i++ {
			select {
			case <-appCtx.Done(): // Check if shutdown signal received before enqueuing
				slog.Info("Enqueue loop: Shutdown signal received, stopping.")
				return
			default:
				// Proceed to build and enqueue
			}

			subject := fmt.Sprintf("Test Email %d", i+1)
			body := fmt.Sprintf("This is the body of test email #%d.", i+1)

			// Use the builder to create and validate email content
			content, err := (&mailer.MailContentBuilder{}).
				WithFromName("Awesome Sender").
				WithFromAddress("sender@example.com"). // Use your actual sender address
				WithToName("Valued Recipient").
				WithToAddress("recipient@example.com"). // Replace with a valid recipient
				WithMimeType(mailer.MimeTypeTextPlain). // Use constants if available, otherwise "text/plain"
				WithSubject(subject).
				WithBody(body).
				Build()
			if err != nil {
				slog.Error("Failed to build mail content", "email_index", i, "error", err)
				continue // Skip this email if content is invalid
			}

			// Enqueue the validated content. This might block if the queue is full.
			// It also respects the context cancellation.
			err = mailService.Enqueue(content)
			if err != nil {
				// Error could be due to context cancellation during enqueue attempt
				slog.Error("Failed to enqueue email", "subject", subject, "error", err)
				// If context is cancelled, stop trying to enqueue more
				if appCtx.Err() != nil {
					slog.Warn("Enqueue loop: Context cancelled, stopping enqueue attempts.")
					return
				}
			} else {
				slog.Info("Email enqueued successfully", "subject", subject)
			}
			// Simulate some delay or work between enqueuing emails
			time.Sleep(500 * time.Millisecond)
		}
		slog.Info("Finished enqueuing sample emails.")
	}()

	// --- Wait for Shutdown Signal ---
	// Block main goroutine until the context is cancelled (e.g., by Ctrl+C)
	<-appCtx.Done()

	slog.Info("Shutdown signal received. Stopping mail service...")

	// --- Stop the Service Gracefully ---
	// Stop accepting new emails by closing the channel and wait for workers
	// to finish processing any remaining emails in the queue.
	mailService.Stop() // This calls Wait() internally

	slog.Info("Mail service stopped gracefully.")
}
