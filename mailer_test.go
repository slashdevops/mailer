package mailer

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMailContentBuilder_Build_Valid(t *testing.T) {
	content, err := NewMailContentBuilder().
		WithFromName("John Doe").
		WithFromAddress("john.doe@example.com").
		WithToName("Jane Doe").
		WithToAddress("jane.doe@example.com").
		WithMimeType(MimeTypeTextPlain).
		WithSubject("Test Subject").
		WithBody("Test Body").
		Build()
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	// Exercise the exported accessors that external transports rely on.
	if got := content.FromName(); got != "John Doe" {
		t.Errorf("FromName() = %q, want %q", got, "John Doe")
	}
	if got := content.FromAddress(); got != "john.doe@example.com" {
		t.Errorf("FromAddress() = %q, want %q", got, "john.doe@example.com")
	}
	if got := content.ToName(); got != "Jane Doe" {
		t.Errorf("ToName() = %q, want %q", got, "Jane Doe")
	}
	if got := content.ToAddress(); got != "jane.doe@example.com" {
		t.Errorf("ToAddress() = %q, want %q", got, "jane.doe@example.com")
	}
	if got := content.MimeType(); got != MimeTypeTextPlain {
		t.Errorf("MimeType() = %q, want %q", got, MimeTypeTextPlain)
	}
	if got := content.Subject(); got != "Test Subject" {
		t.Errorf("Subject() = %q, want %q", got, "Test Subject")
	}
	if got := content.Body(); got != "Test Body" {
		t.Errorf("Body() = %q, want %q", got, "Test Body")
	}
}

func TestMailContentBuilder_DefaultsToTextPlain(t *testing.T) {
	content, err := NewMailContentBuilder().
		WithFromName("John Doe").
		WithFromAddress("john.doe@example.com").
		WithToName("Jane Doe").
		WithToAddress("jane.doe@example.com").
		WithSubject("Test Subject").
		WithBody("Test Body").
		Build()
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}
	if content.MimeType() != MimeTypeTextPlain {
		t.Errorf("expected default MIME type text/plain, got %q", content.MimeType())
	}
}

func TestMailContentBuilder_Build_Invalid(t *testing.T) {
	validBuilder := func() *MailContentBuilder {
		return NewMailContentBuilder().
			WithFromName("John Doe").
			WithFromAddress("john.doe@example.com").
			WithToName("Jane Doe").
			WithToAddress("jane.doe@example.com").
			WithMimeType(MimeTypeTextPlain).
			WithSubject("Test Subject").
			WithBody("Test Body")
	}

	testCases := []struct {
		name          string
		modifier      func(*MailContentBuilder)
		expectedError string
	}{
		{
			name:          "FromName too short",
			modifier:      func(b *MailContentBuilder) { b.WithFromName("") },
			expectedError: fmt.Sprintf("fromName must be between %d and %d characters", ValidMinFromNameLength, ValidMaxFromNameLength),
		},
		{
			name:          "FromName too long",
			modifier:      func(b *MailContentBuilder) { b.WithFromName(strings.Repeat("a", ValidMaxFromNameLength+1)) },
			expectedError: fmt.Sprintf("fromName must be between %d and %d characters", ValidMinFromNameLength, ValidMaxFromNameLength),
		},
		{
			name:          "FromName with newline",
			modifier:      func(b *MailContentBuilder) { b.WithFromName("Evil\r\nBcc: victim@example.com") },
			expectedError: "fromName must not contain line breaks or null bytes",
		},
		{
			name:          "FromAddress too short",
			modifier:      func(b *MailContentBuilder) { b.WithFromAddress("a") },
			expectedError: fmt.Sprintf("fromAddress must be between %d and %d characters", ValidMinFromAddressLength, ValidMaxFromAddressLength),
		},
		{
			name:          "FromAddress invalid",
			modifier:      func(b *MailContentBuilder) { b.WithFromAddress("not-an-email") },
			expectedError: "fromAddress must be a valid email address",
		},
		{
			name:          "ToName too short",
			modifier:      func(b *MailContentBuilder) { b.WithToName("") },
			expectedError: fmt.Sprintf("toName must be between %d and %d characters", ValidMinToNameLength, ValidMaxToNameLength),
		},
		{
			name:          "ToName with newline",
			modifier:      func(b *MailContentBuilder) { b.WithToName("Bob\nSubject: hijacked") },
			expectedError: "toName must not contain line breaks or null bytes",
		},
		{
			name:          "ToAddress invalid",
			modifier:      func(b *MailContentBuilder) { b.WithToAddress("nope") },
			expectedError: "toAddress must be a valid email address",
		},
		{
			name:          "Invalid MimeType",
			modifier:      func(b *MailContentBuilder) { b.WithMimeTypeAsString("application/json") },
			expectedError: fmt.Sprintf("mimeType must be one of the following: %s", ValidMimeType),
		},
		{
			name:          "Subject too short",
			modifier:      func(b *MailContentBuilder) { b.WithSubject("") },
			expectedError: fmt.Sprintf("subject must be between %d and %d characters", ValidMinSubjectLength, ValidMaxSubjectLength),
		},
		{
			name:          "Subject too long",
			modifier:      func(b *MailContentBuilder) { b.WithSubject(strings.Repeat("a", ValidMaxSubjectLength+1)) },
			expectedError: fmt.Sprintf("subject must be between %d and %d characters", ValidMinSubjectLength, ValidMaxSubjectLength),
		},
		{
			name:          "Subject with newline",
			modifier:      func(b *MailContentBuilder) { b.WithSubject("Hi\r\nInjected: yes") },
			expectedError: "subject must not contain line breaks or null bytes",
		},
		{
			name:          "Body too short",
			modifier:      func(b *MailContentBuilder) { b.WithBody("") },
			expectedError: fmt.Sprintf("body must be between %d and %d characters", ValidMinBodyLength, ValidMaxBodyLength),
		},
		{
			name:          "Body too long",
			modifier:      func(b *MailContentBuilder) { b.WithBody(strings.Repeat("a", ValidMaxBodyLength+1)) },
			expectedError: fmt.Sprintf("body must be between %d and %d characters", ValidMinBodyLength, ValidMaxBodyLength),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := validBuilder()
			tc.modifier(builder)
			_, err := builder.Build()

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

func TestMailerError_Error(t *testing.T) {
	err := &MailerError{Message: "boom"}
	if err.Error() != "boom" {
		t.Errorf("Error() = %q, want %q", err.Error(), "boom")
	}

	wrapped := &MailerError{Message: "outer", Err: errors.New("inner")}
	if wrapped.Error() != "outer: inner" {
		t.Errorf("Error() = %q, want %q", wrapped.Error(), "outer: inner")
	}
	if !errors.Is(wrapped, wrapped.Err) {
		t.Error("expected errors.Is to unwrap to the inner error")
	}
}

func TestMimeType_IsValid(t *testing.T) {
	cases := map[MimeType]bool{
		MimeTypeTextPlain:    true,
		MimeTypeTextHTML:     true,
		MimeType("text/xml"): false,
		MimeType(""):         false,
	}
	for mt, want := range cases {
		if got := mt.IsValid(); got != want {
			t.Errorf("MimeType(%q).IsValid() = %v, want %v", mt, got, want)
		}
	}
	if MimeTypeTextHTML.String() != "text/html" {
		t.Errorf("String() = %q, want %q", MimeTypeTextHTML.String(), "text/html")
	}
}
