package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testMailContent(t *testing.T) MailContent {
	t.Helper()
	content, err := NewMailContentBuilder().
		WithFromName("Test Sender").
		WithFromAddress("sender@example.com").
		WithToName("Test Recipient").
		WithToAddress("recipient@example.com").
		WithMimeType(MimeTypeTextPlain).
		WithSubject("Basic Send Test").
		WithBody("This is a basic test.").
		Build()
	if err != nil {
		t.Fatalf("failed to build mail content: %v", err)
	}
	return content
}

func TestNewMailerSMTP_Valid(t *testing.T) {
	validConf := MailerSMTPConf{
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
		Username: "user@example.com",
		Password: "password123",
	}

	m, err := NewMailerSMTP(validConf)
	if err != nil {
		t.Fatalf("Expected no error for valid config, but got: %v", err)
	}
	if m == nil {
		t.Fatal("Expected mailer instance, but got nil")
	}

	if m.smtpHost != validConf.SMTPHost {
		t.Errorf("Expected smtpHost %q, got %q", validConf.SMTPHost, m.smtpHost)
	}
	if m.smtpPort != validConf.SMTPPort {
		t.Errorf("Expected smtpPort %d, got %d", validConf.SMTPPort, m.smtpPort)
	}
	if m.dialTimeout != DefaultSMTPDialTimeout {
		t.Errorf("Expected default dial timeout %v, got %v", DefaultSMTPDialTimeout, m.dialTimeout)
	}
	if m.localName != DefaultSMTPLocalName {
		t.Errorf("Expected default local name %q, got %q", DefaultSMTPLocalName, m.localName)
	}
}

func TestNewMailerSMTP_DefaultsOverridable(t *testing.T) {
	m, err := NewMailerSMTP(MailerSMTPConf{
		SMTPHost:    "smtp.example.com",
		SMTPPort:    465,
		DialTimeout: 3 * time.Second,
		LocalName:   "mail.example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.dialTimeout != 3*time.Second {
		t.Errorf("expected dial timeout 3s, got %v", m.dialTimeout)
	}
	if m.localName != "mail.example.com" {
		t.Errorf("expected custom local name, got %q", m.localName)
	}
	if !m.implicitTLS {
		t.Error("expected implicit TLS to be enabled for port 465")
	}
}

func TestNewMailerSMTP_Invalid(t *testing.T) {
	validConf := func() MailerSMTPConf {
		return MailerSMTPConf{
			SMTPHost: "smtp.example.com",
			SMTPPort: 587,
			Username: "user@example.com",
			Password: "password123",
		}
	}

	testCases := []struct {
		name          string
		modifier      func(*MailerSMTPConf)
		expectedError string
	}{
		{
			name:          "SMTPHost empty",
			modifier:      func(c *MailerSMTPConf) { c.SMTPHost = "" },
			expectedError: fmt.Sprintf("SMTPHost must be between %d and %d characters", ValidMinSMTPHostLength, ValidMaxSMTPHostLength),
		},
		{
			name:          "SMTPHost too long",
			modifier:      func(c *MailerSMTPConf) { c.SMTPHost = strings.Repeat("a", ValidMaxSMTPHostLength+1) },
			expectedError: fmt.Sprintf("SMTPHost must be between %d and %d characters", ValidMinSMTPHostLength, ValidMaxSMTPHostLength),
		},
		{
			name:          "SMTPPort too low",
			modifier:      func(c *MailerSMTPConf) { c.SMTPPort = 0 },
			expectedError: fmt.Sprintf("SMTPPort must be between %d and %d", ValidMinSMTPPort, ValidMaxSMTPPort),
		},
		{
			name:          "SMTPPort too high",
			modifier:      func(c *MailerSMTPConf) { c.SMTPPort = 70000 },
			expectedError: fmt.Sprintf("SMTPPort must be between %d and %d", ValidMinSMTPPort, ValidMaxSMTPPort),
		},
		{
			name:          "Username too long",
			modifier:      func(c *MailerSMTPConf) { c.Username = strings.Repeat("a", ValidMaxUsernameLength+1) },
			expectedError: fmt.Sprintf("Username must be at most %d characters", ValidMaxUsernameLength),
		},
		{
			name:          "Password too long",
			modifier:      func(c *MailerSMTPConf) { c.Password = strings.Repeat("a", ValidMaxPasswordLength+1) },
			expectedError: fmt.Sprintf("Password must be at most %d characters", ValidMaxPasswordLength),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			conf := validConf()
			tc.modifier(&conf)
			_, err := NewMailerSMTP(conf)

			if err == nil {
				t.Fatalf("Expected error %q, but got nil", tc.expectedError)
			}
			var mailerErr *MailerError
			if !errors.As(err, &mailerErr) {
				t.Fatalf("Expected *MailerError, but got %T", err)
			}
			if mailerErr.Message != tc.expectedError {
				t.Errorf("Expected error message %q, but got %q", tc.expectedError, mailerErr.Message)
			}
		})
	}
}

func TestMailerSMTP_Send_NoAuth(t *testing.T) {
	server := newFakeSMTPServer(t, fakeSMTPServer{})

	m, err := NewMailerSMTP(MailerSMTPConf{SMTPHost: server.host(), SMTPPort: server.port()})
	if err != nil {
		t.Fatalf("failed to create mailer: %v", err)
	}

	if err := m.Send(context.Background(), testMailContent(t)); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	rec := waitForMail(t, server)
	if rec.usedTLS {
		t.Error("did not expect TLS on a plain server")
	}
	if !strings.Contains(rec.from, "sender@example.com") {
		t.Errorf("unexpected MAIL FROM: %q", rec.from)
	}
	if len(rec.to) != 1 || !strings.Contains(rec.to[0], "recipient@example.com") {
		t.Errorf("unexpected RCPT TO: %v", rec.to)
	}
	if !strings.Contains(rec.data, "Subject: Basic Send Test") {
		t.Errorf("message missing subject header:\n%s", rec.data)
	}
	if !strings.Contains(rec.data, "\r\n") {
		t.Error("message should use CRLF line endings")
	}
}

func TestMailerSMTP_Send_STARTTLSAndAuth(t *testing.T) {
	server := newFakeSMTPServer(t, fakeSMTPServer{offerTLS: true, offerAuth: true})

	m, err := NewMailerSMTP(MailerSMTPConf{
		SMTPHost:   server.host(),
		SMTPPort:   server.port(),
		Username:   "user",
		Password:   "secret",
		RequireTLS: true,
		TLSConfig:  &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		t.Fatalf("failed to create mailer: %v", err)
	}

	if err := m.Send(context.Background(), testMailContent(t)); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	rec := waitForMail(t, server)
	if !rec.usedTLS {
		t.Error("expected STARTTLS to secure the connection")
	}
	if !rec.authed {
		t.Error("expected authentication to occur")
	}
}

func TestMailerSMTP_Send_ImplicitTLS(t *testing.T) {
	server := newFakeSMTPServer(t, fakeSMTPServer{implicit: true, offerAuth: true})

	m, err := NewMailerSMTP(MailerSMTPConf{
		SMTPHost:    server.host(),
		SMTPPort:    server.port(),
		Username:    "user",
		Password:    "secret",
		ImplicitTLS: true,
		TLSConfig:   &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		t.Fatalf("failed to create mailer: %v", err)
	}

	if err := m.Send(context.Background(), testMailContent(t)); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	rec := waitForMail(t, server)
	if !rec.usedTLS {
		t.Error("expected implicit TLS connection")
	}
}

func TestMailerSMTP_Send_RequireTLSFailsWithoutSupport(t *testing.T) {
	server := newFakeSMTPServer(t, fakeSMTPServer{offerTLS: false})

	m, err := NewMailerSMTP(MailerSMTPConf{
		SMTPHost:   server.host(),
		SMTPPort:   server.port(),
		RequireTLS: true,
	})
	if err != nil {
		t.Fatalf("failed to create mailer: %v", err)
	}

	err = m.Send(context.Background(), testMailContent(t))
	if err == nil {
		t.Fatal("expected an error when TLS is required but unsupported")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("expected STARTTLS-related error, got %v", err)
	}
}

func TestMailerSMTP_Send_ContextCancelled(t *testing.T) {
	server := newFakeSMTPServer(t, fakeSMTPServer{})
	m, err := NewMailerSMTP(MailerSMTPConf{SMTPHost: server.host(), SMTPPort: server.port()})
	if err != nil {
		t.Fatalf("failed to create mailer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = m.Send(ctx, testMailContent(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMailerSMTP_Send_ConnectionFailure(t *testing.T) {
	m, err := NewMailerSMTP(MailerSMTPConf{
		SMTPHost:    "127.0.0.1",
		SMTPPort:    1, // nothing is listening here
		DialTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create mailer: %v", err)
	}

	err = m.Send(context.Background(), testMailContent(t))
	if err == nil {
		t.Fatal("expected a connection error")
	}
	var mailerErr *MailerError
	if !errors.As(err, &mailerErr) {
		t.Fatalf("expected *MailerError, got %T", err)
	}
	if !strings.Contains(mailerErr.Message, "failed to connect") {
		t.Errorf("expected connect failure message, got %q", mailerErr.Message)
	}
}

func TestBuildMessage(t *testing.T) {
	content := testMailContent(t)
	msg := string(buildMessage(content))

	for _, want := range []string{
		"From: Test Sender <sender@example.com>\r\n",
		"To: Test Recipient <recipient@example.com>\r\n",
		"Subject: Basic Send Test\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q in:\n%s", want, msg)
		}
	}
}

func waitForMail(t *testing.T, s *fakeSMTPServer) receivedMail {
	t.Helper()
	select {
	case rec := <-s.received:
		return rec
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the server to receive mail")
		return receivedMail{}
	}
}
