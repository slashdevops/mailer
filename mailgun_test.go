package mailer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// captured is what the fake Mailgun endpoint saw.
type captured struct {
	method      string
	path        string
	contentType string
	authUser    string
	authPass    string
	authOK      bool
	form        url.Values
}

// mailgunServer stands in for Mailgun's messages endpoint, recording the
// request and answering with the status the test asks for.
//
// It asserts the REQUEST this package builds against Mailgun's documented wire
// format. It deliberately does not model a response body this package parses --
// there is none to parse, only a status code -- so there is no risk of the test
// agreeing with the code about a shape neither shares with the real provider.
func mailgunServer(t *testing.T, status int, body string) (*httptest.Server, *captured) {
	t.Helper()

	got := &captured{}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		got.contentType = r.Header.Get("Content-Type")
		got.authUser, got.authPass, got.authOK = r.BasicAuth()

		raw, _ := io.ReadAll(r.Body)
		got.form, _ = url.ParseQuery(string(raw))

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv, got
}

func newTestMailgun(t *testing.T, srv *httptest.Server, key string) *MailerMailgun {
	t.Helper()

	m, err := NewMailerMailgun(MailerMailgunConf{
		APIURL:     srv.URL + "/v3/mg.example.com/messages",
		APIKey:     key,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewMailerMailgun: %v", err)
	}

	return m
}

// The documented Mailgun contract, asserted field by field: a form-encoded POST
// with basic auth under the literal username "api".
func TestMailerMailgunSendsTheDocumentedRequest(t *testing.T) {
	srv, got := mailgunServer(t, http.StatusOK, `{"id":"<20260829@mg.example.com>","message":"Queued. Thank you."}`)

	if err := newTestMailgun(t, srv, "key-secret").Send(t.Context(), testMailContent(t)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}

	if got.path != "/v3/mg.example.com/messages" {
		t.Errorf("path = %q; the sending domain is part of the URL and must be preserved", got.path)
	}

	if got.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q; Mailgun takes form encoding, not JSON", got.contentType)
	}

	if !got.authOK || got.authUser != "api" || got.authPass != "key-secret" {
		t.Errorf("basic auth = (%q, %q, ok=%v); Mailgun expects the literal user \"api\" and the key as the password",
			got.authUser, got.authPass, got.authOK)
	}

	for field, want := range map[string]string{
		"from":    "Test Sender <sender@example.com>",
		"to":      "Test Recipient <recipient@example.com>",
		"subject": "Basic Send Test",
		"text":    "This is a basic test.",
	} {
		if g := got.form.Get(field); g != want {
			t.Errorf("form[%s] = %q, want %q", field, g, want)
		}
	}

	if got.form.Has("html") {
		t.Error("a text/plain message must not be sent as html")
	}
}

// The MIME type chooses the field. Getting this wrong delivers markup as
// visible source, which no status code would reveal.
func TestMailerMailgunUsesTheHTMLFieldForHTMLContent(t *testing.T) {
	srv, got := mailgunServer(t, http.StatusOK, "{}")

	content, err := NewMailContentBuilder().
		WithFromName("Sender").
		WithFromAddress("sender@example.com").
		WithToName("Recipient").
		WithToAddress("recipient@example.com").
		WithMimeType(MimeTypeTextHTML).
		WithSubject("HTML Test").
		WithBody("<p>hello</p>").
		Build()
	if err != nil {
		t.Fatalf("build content: %v", err)
	}

	if err := newTestMailgun(t, srv, "key").Send(t.Context(), content); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if g := got.form.Get("html"); g != "<p>hello</p>" {
		t.Errorf("form[html] = %q, want the body", g)
	}

	if got.form.Has("text") {
		t.Error("an html message must not also be sent as text")
	}
}

// A display name that is only whitespace must not produce a stray "<>" prefix.
//
// The builder validates the name with len() BEFORE any trimming, so "   " is a
// perfectly valid three-character name as far as it is concerned -- which is
// what makes this reachable rather than defensive.
func TestMailerMailgunOmitsAWhitespaceOnlyDisplayName(t *testing.T) {
	srv, got := mailgunServer(t, http.StatusOK, "{}")

	content, err := NewMailContentBuilder().
		WithFromName("   ").
		WithFromAddress("sender@example.com").
		WithToName("Recipient").
		WithToAddress("recipient@example.com").
		WithSubject("s").
		WithBody("b").
		Build()
	if err != nil {
		t.Fatalf("build content: %v", err)
	}

	if err := newTestMailgun(t, srv, "key").Send(t.Context(), content); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if g := got.form.Get("from"); g != "sender@example.com" {
		t.Errorf("form[from] = %q, want the bare address", g)
	}
}

// A comma in a display name would split one recipient into two if it were not
// quoted -- so the message would silently go somewhere else as well.
func TestMailerMailgunQuotesADisplayNameWithSpecialCharacters(t *testing.T) {
	srv, got := mailgunServer(t, http.StatusOK, "{}")

	content, err := NewMailContentBuilder().
		WithFromName(`Doe, John`).
		WithFromAddress("sender@example.com").
		WithToName("Recipient").
		WithToAddress("recipient@example.com").
		WithSubject("s").
		WithBody("b").
		Build()
	if err != nil {
		t.Fatalf("build content: %v", err)
	}

	if err := newTestMailgun(t, srv, "key").Send(t.Context(), content); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if g := got.form.Get("from"); g != `"Doe, John" <sender@example.com>` {
		t.Errorf("form[from] = %q; a name containing a comma must be quoted", g)
	}
}

// A non-2xx is an error, and it carries the provider's own explanation --
// "Domain not found" is the whole diagnosis, and dropping it leaves an operator
// with a bare 400.
func TestMailerMailgunReportsAnAPIFailure(t *testing.T) {
	srv, _ := mailgunServer(t, http.StatusBadRequest, `{"message":"Domain not found: mg.example.com"}`)

	err := newTestMailgun(t, srv, "key").Send(t.Context(), testMailContent(t))
	if err == nil {
		t.Fatal("a 400 must be an error")
	}

	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "Domain not found") {
		t.Errorf("error = %q; it must carry the status and the provider's message", err)
	}
}

// The API key must never reach an error string: an error is the one place
// guaranteed to be written to a log.
func TestMailerMailgunNeverPutsTheKeyInAnError(t *testing.T) {
	const key = "key-super-secret-value"

	srv, _ := mailgunServer(t, http.StatusUnauthorized, `{"message":"Invalid private key"}`)

	err := newTestMailgun(t, srv, key).Send(t.Context(), testMailContent(t))
	if err == nil {
		t.Fatal("a 401 must be an error")
	}

	if strings.Contains(err.Error(), key) {
		t.Errorf("the API key leaked into an error: %q", err)
	}
}

func TestNewMailerMailgunRejectsBadConfiguration(t *testing.T) {
	for name, conf := range map[string]MailerMailgunConf{
		"empty url":    {APIURL: "", APIKey: "k"},
		"relative url": {APIURL: "/v3/mg.example.com/messages", APIKey: "k"},
		"not https":    {APIURL: "http://api.mailgun.net/v3/d/messages", APIKey: "k"},
		"empty key":    {APIURL: "https://api.mailgun.net/v3/d/messages", APIKey: ""},
		"unparsable":   {APIURL: "https://api.mailgun.net/%zz", APIKey: "k"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewMailerMailgun(conf); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

// http rather than https is refused, and the reason is worth naming: the key is
// a basic-auth header on every request.
func TestNewMailerMailgunRefusesPlainHTTP(t *testing.T) {
	_, err := NewMailerMailgun(MailerMailgunConf{
		APIURL: "http://api.mailgun.net/v3/mg.example.com/messages",
		APIKey: "key",
	})
	if err == nil {
		t.Fatal("plain http must be refused; the API key travels on every request")
	}

	if !strings.Contains(err.Error(), "https") {
		t.Errorf("error = %q; it should say what is wrong", err)
	}
}

// With no client supplied the transport builds its own, so the zero-config case
// works rather than panicking on a nil client.
func TestNewMailerMailgunDefaultsItsHTTPClient(t *testing.T) {
	m, err := NewMailerMailgun(MailerMailgunConf{
		APIURL: "https://api.mailgun.net/v3/mg.example.com/messages",
		APIKey: "key",
	})
	if err != nil {
		t.Fatalf("NewMailerMailgun: %v", err)
	}

	if m.client == nil {
		t.Fatal("no HTTP client was built")
	}

	if m.client.Timeout != DefaultMailgunTimeout {
		t.Errorf("timeout = %v, want %v", m.client.Timeout, DefaultMailgunTimeout)
	}
}

// A cancelled context is refused before the request is built, so a caller that
// has already given up does not cost a connection.
func TestMailerMailgunHonoursACancelledContext(t *testing.T) {
	srv, got := mailgunServer(t, http.StatusOK, "{}")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := newTestMailgun(t, srv, "key").Send(ctx, testMailContent(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	if got.method != "" {
		t.Error("a request was sent despite the context already being cancelled")
	}
}
