package mailer

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
)

// Validation bounds for MailContent fields. They guard against obviously
// malformed input and keep messages within sane limits before they reach a
// transport.
const (
	ValidMinFromNameLength    = 1
	ValidMaxFromNameLength    = 100
	ValidMinFromAddressLength = 3
	ValidMaxFromAddressLength = 254
	ValidMinToNameLength      = 1
	ValidMaxToNameLength      = 100
	ValidMinToAddressLength   = 3
	ValidMaxToAddressLength   = 254
	ValidMinSubjectLength     = 1
	ValidMaxSubjectLength     = 255
	ValidMinBodyLength        = 1
	ValidMaxBodyLength        = 1 << 18 // 256 KiB, large enough for rich HTML bodies.
)

// ValidMimeType lists the accepted MIME types separated by "|".
const ValidMimeType = "text/plain|text/html"

// MailerService is implemented by any transport capable of delivering a message.
// Implementations must honour the provided context (cancellation/deadline) and
// read the message through the exported MailContent accessors.
type MailerService interface {
	Send(ctx context.Context, content MailContent) error
}

// MimeType is a custom type for the supported MIME types.
type MimeType string

const (
	// MimeTypeTextPlain is the MIME type for plain text emails.
	MimeTypeTextPlain MimeType = "text/plain"

	// MimeTypeTextHTML is the MIME type for HTML emails.
	MimeTypeTextHTML MimeType = "text/html"
)

// String returns the string representation of the MimeType.
func (m MimeType) String() string { return string(m) }

// IsValid reports whether the MimeType is one of the supported values.
func (m MimeType) IsValid() bool {
	return m == MimeTypeTextPlain || m == MimeTypeTextHTML
}

// MailerError is a validation or transport error produced by the package.
type MailerError struct {
	Message string
	// Err is the wrapped underlying error, if any. It enables errors.Is/As.
	Err error
}

// Error implements the error interface for MailerError.
func (e *MailerError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

// Unwrap exposes the wrapped error for errors.Is/errors.As.
func (e *MailerError) Unwrap() error { return e.Err }

// MailContent is an immutable, validated email message. Instances are created
// exclusively through MailContentBuilder so that every field is validated before
// it can be dispatched. The fields are unexported to keep the value immutable;
// transports read them through the accessor methods.
type MailContent struct {
	fromName    string
	fromAddress string
	toName      string
	toAddress   string
	mimeType    MimeType
	subject     string
	body        string
}

// FromName returns the sender display name.
func (c MailContent) FromName() string { return c.fromName }

// FromAddress returns the sender email address.
func (c MailContent) FromAddress() string { return c.fromAddress }

// ToName returns the recipient display name.
func (c MailContent) ToName() string { return c.toName }

// ToAddress returns the recipient email address.
func (c MailContent) ToAddress() string { return c.toAddress }

// MimeType returns the message MIME type.
func (c MailContent) MimeType() MimeType { return c.mimeType }

// Subject returns the message subject.
func (c MailContent) Subject() string { return c.subject }

// Body returns the message body.
func (c MailContent) Body() string { return c.body }

// MailContentBuilder builds and validates MailContent using a fluent API.
type MailContentBuilder struct {
	mailContent MailContent
}

// NewMailContentBuilder returns a builder pre-configured with a text/plain MIME type.
func NewMailContentBuilder() *MailContentBuilder {
	return &MailContentBuilder{mailContent: MailContent{mimeType: MimeTypeTextPlain}}
}

// WithFromName sets the sender display name.
func (b *MailContentBuilder) WithFromName(name string) *MailContentBuilder {
	b.mailContent.fromName = name
	return b
}

// WithFromAddress sets the sender email address.
func (b *MailContentBuilder) WithFromAddress(address string) *MailContentBuilder {
	b.mailContent.fromAddress = address
	return b
}

// WithToName sets the recipient display name.
func (b *MailContentBuilder) WithToName(name string) *MailContentBuilder {
	b.mailContent.toName = name
	return b
}

// WithToAddress sets the recipient email address.
func (b *MailContentBuilder) WithToAddress(address string) *MailContentBuilder {
	b.mailContent.toAddress = address
	return b
}

// WithMimeType sets the MIME type.
func (b *MailContentBuilder) WithMimeType(mimeType MimeType) *MailContentBuilder {
	b.mailContent.mimeType = mimeType
	return b
}

// WithMimeTypeAsString sets the MIME type from a raw string.
func (b *MailContentBuilder) WithMimeTypeAsString(mimeType string) *MailContentBuilder {
	return b.WithMimeType(MimeType(mimeType))
}

// WithSubject sets the subject line.
func (b *MailContentBuilder) WithSubject(subject string) *MailContentBuilder {
	b.mailContent.subject = subject
	return b
}

// WithBody sets the message body.
func (b *MailContentBuilder) WithBody(body string) *MailContentBuilder {
	b.mailContent.body = body
	return b
}

// Build validates every field and returns an immutable MailContent, or a
// *MailerError describing the first validation failure.
func (b *MailContentBuilder) Build() (MailContent, error) {
	c := b.mailContent

	if err := validateLength("fromName", c.fromName, ValidMinFromNameLength, ValidMaxFromNameLength); err != nil {
		return MailContent{}, err
	}
	if err := validateHeaderSafe("fromName", c.fromName); err != nil {
		return MailContent{}, err
	}

	if err := validateLength("fromAddress", c.fromAddress, ValidMinFromAddressLength, ValidMaxFromAddressLength); err != nil {
		return MailContent{}, err
	}
	if _, err := mail.ParseAddress(c.fromAddress); err != nil {
		return MailContent{}, &MailerError{Message: "fromAddress must be a valid email address", Err: err}
	}

	if err := validateLength("toName", c.toName, ValidMinToNameLength, ValidMaxToNameLength); err != nil {
		return MailContent{}, err
	}
	if err := validateHeaderSafe("toName", c.toName); err != nil {
		return MailContent{}, err
	}

	if err := validateLength("toAddress", c.toAddress, ValidMinToAddressLength, ValidMaxToAddressLength); err != nil {
		return MailContent{}, err
	}
	if _, err := mail.ParseAddress(c.toAddress); err != nil {
		return MailContent{}, &MailerError{Message: "toAddress must be a valid email address", Err: err}
	}

	if !c.mimeType.IsValid() {
		return MailContent{}, &MailerError{Message: fmt.Sprintf("mimeType must be one of the following: %s", ValidMimeType)}
	}

	if err := validateLength("subject", c.subject, ValidMinSubjectLength, ValidMaxSubjectLength); err != nil {
		return MailContent{}, err
	}
	if err := validateHeaderSafe("subject", c.subject); err != nil {
		return MailContent{}, err
	}

	if err := validateLength("body", c.body, ValidMinBodyLength, ValidMaxBodyLength); err != nil {
		return MailContent{}, err
	}

	return c, nil
}

func validateLength(field, value string, minLen, maxLen int) error {
	if len(value) < minLen || len(value) > maxLen {
		return &MailerError{Message: fmt.Sprintf("%s must be between %d and %d characters", field, minLen, maxLen)}
	}
	return nil
}

// validateHeaderSafe rejects CR/LF (and NUL) in fields that become email
// headers, preventing SMTP header/command injection.
func validateHeaderSafe(field, value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return &MailerError{Message: fmt.Sprintf("%s must not contain line breaks or null bytes", field)}
	}
	return nil
}
