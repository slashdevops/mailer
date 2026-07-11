package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTP validation bounds and defaults.
const (
	ValidMinSMTPHostLength = 1
	ValidMaxSMTPHostLength = 255
	ValidMinSMTPPort       = 1
	ValidMaxSMTPPort       = 65535
	ValidMinUsernameLength = 0
	ValidMaxUsernameLength = 255
	ValidMinPasswordLength = 0
	ValidMaxPasswordLength = 255

	// DefaultSMTPDialTimeout is used when MailerSMTPConf.DialTimeout is zero.
	DefaultSMTPDialTimeout = 10 * time.Second
	// DefaultSMTPLocalName is the HELO/EHLO name used when none is configured.
	DefaultSMTPLocalName = "localhost"
	// implicitTLSPort is the well-known port that expects TLS from the first byte.
	implicitTLSPort = 465
)

// MailerSMTPConf configures the SMTP transport.
type MailerSMTPConf struct {
	// SMTPHost is the mail server host name. It is also used as the TLS server
	// name for certificate verification.
	SMTPHost string
	// SMTPPort is the mail server port (1-65535). Port 465 uses implicit TLS;
	// other ports negotiate STARTTLS opportunistically (see RequireTLS).
	SMTPPort int
	// ImplicitTLS forces a TLS handshake immediately after connecting, before any
	// SMTP command (SMTPS). It is implied for port 465.
	ImplicitTLS bool
	// Username and Password are the credentials for PLAIN authentication. Leave
	// both empty to send without authentication.
	Username string
	Password string

	// DialTimeout bounds the TCP connection setup. Defaults to DefaultSMTPDialTimeout.
	DialTimeout time.Duration
	// LocalName is the name announced in the EHLO/HELO command. Defaults to
	// DefaultSMTPLocalName.
	LocalName string
	// RequireTLS makes delivery fail if the connection cannot be secured with
	// TLS (implicit TLS or STARTTLS). Recommended for production.
	RequireTLS bool
	// TLSConfig is an optional custom TLS configuration. When nil a default
	// config verifying the server against SMTPHost is used.
	TLSConfig *tls.Config
}

// MailerSMTP is an SMTP implementation of MailerService.
type MailerSMTP struct {
	smtpHost    string
	smtpPort    int
	username    string
	password    string
	dialTimeout time.Duration
	localName   string
	requireTLS  bool
	implicitTLS bool
	tlsConfig   *tls.Config
}

// NewMailerSMTP validates the configuration and returns an SMTP transport.
func NewMailerSMTP(conf MailerSMTPConf) (*MailerSMTP, error) {
	if l := len(conf.SMTPHost); l < ValidMinSMTPHostLength || l > ValidMaxSMTPHostLength {
		return nil, &MailerError{Message: fmt.Sprintf("SMTPHost must be between %d and %d characters", ValidMinSMTPHostLength, ValidMaxSMTPHostLength)}
	}

	if conf.SMTPPort < ValidMinSMTPPort || conf.SMTPPort > ValidMaxSMTPPort {
		return nil, &MailerError{Message: fmt.Sprintf("SMTPPort must be between %d and %d", ValidMinSMTPPort, ValidMaxSMTPPort)}
	}

	if l := len(conf.Username); l > ValidMaxUsernameLength {
		return nil, &MailerError{Message: fmt.Sprintf("Username must be at most %d characters", ValidMaxUsernameLength)}
	}

	if l := len(conf.Password); l > ValidMaxPasswordLength {
		return nil, &MailerError{Message: fmt.Sprintf("Password must be at most %d characters", ValidMaxPasswordLength)}
	}

	dialTimeout := conf.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = DefaultSMTPDialTimeout
	}

	localName := conf.LocalName
	if localName == "" {
		localName = DefaultSMTPLocalName
	}

	return &MailerSMTP{
		smtpHost:    conf.SMTPHost,
		smtpPort:    conf.SMTPPort,
		username:    conf.Username,
		password:    conf.Password,
		dialTimeout: dialTimeout,
		localName:   localName,
		requireTLS:  conf.RequireTLS,
		implicitTLS: conf.ImplicitTLS || conf.SMTPPort == implicitTLSPort,
		tlsConfig:   conf.TLSConfig,
	}, nil
}

// Send delivers a single message. It honours the context for connection setup
// and returns a *MailerError wrapping the underlying failure.
func (m *MailerSMTP) Send(ctx context.Context, content MailContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	client, err := m.dial(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Hello(m.localName); err != nil {
		return &MailerError{Message: "failed to send SMTP EHLO", Err: err}
	}

	if err := m.maybeStartTLS(client); err != nil {
		return err
	}

	if m.username != "" || m.password != "" {
		auth := smtp.PlainAuth("", m.username, m.password, m.smtpHost)
		if err := client.Auth(auth); err != nil {
			return &MailerError{Message: "failed to authenticate with SMTP server", Err: err}
		}
	}

	if err := client.Mail(content.fromAddress); err != nil {
		return &MailerError{Message: "failed to set SMTP sender", Err: err}
	}
	if err := client.Rcpt(content.toAddress); err != nil {
		return &MailerError{Message: "failed to set SMTP recipient", Err: err}
	}

	writer, err := client.Data()
	if err != nil {
		return &MailerError{Message: "failed to start SMTP data transfer", Err: err}
	}
	if _, err := writer.Write(buildMessage(content)); err != nil {
		return &MailerError{Message: "failed to write SMTP message", Err: err}
	}
	if err := writer.Close(); err != nil {
		return &MailerError{Message: "failed to finish SMTP data transfer", Err: err}
	}
	if err := client.Quit(); err != nil {
		return &MailerError{Message: "failed to close SMTP session", Err: err}
	}

	return nil
}

// dial establishes the transport connection and wraps it in an smtp.Client,
// using implicit TLS on port 465 and a plain TCP connection otherwise.
func (m *MailerSMTP) dial(ctx context.Context) (*smtp.Client, error) {
	addr := fmt.Sprintf("%s:%d", m.smtpHost, m.smtpPort)
	dialer := &net.Dialer{Timeout: m.dialTimeout}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, &MailerError{Message: "failed to connect to SMTP server", Err: err}
	}

	if m.implicitTLS {
		tlsConn := tls.Client(conn, m.tlsClientConfig())
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, &MailerError{Message: "failed TLS handshake with SMTP server", Err: err}
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, m.smtpHost)
	if err != nil {
		conn.Close()
		return nil, &MailerError{Message: "failed to initialize SMTP client", Err: err}
	}
	return client, nil
}

// maybeStartTLS upgrades the connection with STARTTLS when the server advertises
// it. When RequireTLS is set and the connection is not (or cannot be) secured,
// it returns an error rather than sending credentials in the clear.
func (m *MailerSMTP) maybeStartTLS(client *smtp.Client) error {
	if _, isTLS := client.TLSConnectionState(); isTLS {
		return nil // already secured (implicit TLS on 465).
	}

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(m.tlsClientConfig()); err != nil {
			return &MailerError{Message: "failed to start TLS with SMTP server", Err: err}
		}
		return nil
	}

	if m.requireTLS {
		return &MailerError{Message: "SMTP server does not support STARTTLS but RequireTLS is set"}
	}
	return nil
}

func (m *MailerSMTP) tlsClientConfig() *tls.Config {
	if m.tlsConfig != nil {
		return m.tlsConfig.Clone()
	}
	return &tls.Config{ServerName: m.smtpHost, MinVersion: tls.VersionTLS12}
}

// buildMessage renders RFC 5322 headers and body using CRLF line endings.
func buildMessage(content MailContent) []byte {
	var b strings.Builder
	writeHeader(&b, "From", fmt.Sprintf("%s <%s>", content.fromName, content.fromAddress))
	writeHeader(&b, "To", fmt.Sprintf("%s <%s>", content.toName, content.toAddress))
	writeHeader(&b, "Subject", content.subject)
	writeHeader(&b, "MIME-Version", "1.0")
	writeHeader(&b, "Content-Type", content.mimeType.String()+"; charset=UTF-8")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(content.body, "\n", "\r\n"))
	return []byte(b.String())
}

func writeHeader(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}
