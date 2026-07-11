# Basic example

This example shows how to wire `mailer` into a small service, step by step.

## Step 1: Create the SMTP backend

`RequireTLS` makes delivery fail rather than send credentials over an unencrypted
connection. On port 587 the client upgrades with STARTTLS; use port 465 (or
`ImplicitTLS: true`) for implicit TLS.

```go
backend, err := mailer.NewMailerSMTP(mailer.MailerSMTPConf{
	SMTPHost:   "smtp.example.com",
	SMTPPort:   587,
	Username:   os.Getenv("SMTP_USER"),
	Password:   os.Getenv("SMTP_PASS"),
	RequireTLS: true,
})
if err != nil {
	log.Fatal(err)
}
```

## Step 2: Create and start the mail service

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

service, err := mailer.NewMailService(&mailer.MailServiceConfig{
	Ctx:         ctx,
	WorkerCount: 4,
	QueueSize:   64,
	Mailer:      backend,
})
if err != nil {
	log.Fatal(err)
}

service.Start()
```

## Step 3: Build a validated message

```go
content, err := mailer.NewMailContentBuilder().
	WithFromName("Operations").
	WithFromAddress("ops@example.com").
	WithToName("Customer").
	WithToAddress("customer@example.com").
	WithMimeType(mailer.MimeTypeTextHTML).
	WithSubject("Welcome").
	WithBody("<p>Thanks for signing up.</p>").
	Build()
if err != nil {
	log.Fatal(err) // validation error (*mailer.MailerError)
}
```

## Step 4: Enqueue the message

```go
if err := service.Enqueue(content); err != nil {
	// ErrServiceStopped, or a context error if the context was cancelled.
	log.Print(err)
}
```

## Step 5: Stop gracefully

`Stop` closes the queue, drains any messages still queued, and waits for the
workers to finish before returning.

```go
service.Stop()
```

This pattern suits CLI tools, HTTP handlers, and background jobs that need to
offload email sending to a worker pool. See the runnable
[example/main.go](../../example/main.go) for a complete program.
