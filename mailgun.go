package mailer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Mailgun validation bounds and defaults.
const (
	ValidMinMailgunAPIKeyLength = 1
	ValidMaxMailgunAPIKeyLength = 512

	// DefaultMailgunTimeout bounds one request when MailerMailgunConf.HTTPClient
	// is nil and the package builds its own client.
	DefaultMailgunTimeout = 30 * time.Second

	// mailgunBasicAuthUser is the literal username Mailgun expects; the API key
	// is the password. It is not the account's email address.
	mailgunBasicAuthUser = "api"

	// maxMailgunErrorBody bounds how much of a failed response is read into an
	// error. A provider having a bad day can answer with a full HTML page, and
	// none of it belongs in a log line.
	maxMailgunErrorBody = 4 << 10
)

// MailerMailgunConf configures the Mailgun transport.
type MailerMailgunConf struct {
	// APIURL is the messages endpoint, in full, including the sending domain:
	//
	//	https://api.mailgun.net/v3/mg.example.com/messages
	//
	// The domain is part of the path rather than a separate field because
	// Mailgun's EU region uses a different host entirely
	// (api.eu.mailgun.net), and a host+domain pair would not express that.
	APIURL string

	// APIKey is the private API key, sent as the HTTP basic-auth password.
	APIKey string

	// HTTPClient sends the request. Optional: when nil a client with
	// DefaultMailgunTimeout is built.
	//
	// Supplying one is the norm for a service that already has a configured
	// client -- it makes this transport inherit that client's timeout, retry
	// policy and connection pool instead of quietly keeping its own.
	HTTPClient *http.Client
}

// MailerMailgun is a Mailgun HTTP API implementation of MailerService.
//
// # Why an API transport at all
//
// SMTP requires an outbound connection on a port many hosting environments
// block, and it reports delivery failures asynchronously by bouncing. A
// provider API answers synchronously over HTTPS on 443, which is both reachable
// from more places and easier to surface as an error the caller can act on.
//
// # The wire format is form-encoded, not JSON
//
// Mailgun's messages endpoint takes application/x-www-form-urlencoded. The
// fields are from, to, subject, and exactly one of text or html -- chosen from
// the MailContent MIME type, so an HTML template does not arrive as visible
// source. Authentication is HTTP basic with the literal username "api".
type MailerMailgun struct {
	apiURL string
	apiKey string
	client *http.Client
}

// Ensure MailerMailgun satisfies the transport interface.
var _ MailerService = (*MailerMailgun)(nil)

// NewMailerMailgun validates the configuration and returns a Mailgun transport.
//
// Configuration errors are returned here rather than on the first Send. The
// first message a service sends is usually a password reset or an account
// verification, so a misconfiguration discovered there is discovered as a user
// who cannot get in.
func NewMailerMailgun(conf MailerMailgunConf) (*MailerMailgun, error) {
	if strings.TrimSpace(conf.APIURL) == "" {
		return nil, &MailerError{Message: "APIURL is required"}
	}

	parsed, err := url.Parse(conf.APIURL)
	if err != nil {
		return nil, &MailerError{Message: fmt.Sprintf("APIURL %q is not a valid URL", conf.APIURL), Err: err}
	}

	if parsed.Host == "" {
		return nil, &MailerError{Message: fmt.Sprintf("APIURL %q must be absolute, with a scheme and host", conf.APIURL)}
	}

	if parsed.Scheme != "https" {
		// The API key travels as a basic-auth header on every send, so plain
		// HTTP puts a credential on the wire. A mail provider is on the public
		// internet by definition; there is no private-network case to allow for.
		return nil, &MailerError{Message: fmt.Sprintf("APIURL must use https, got %q", parsed.Scheme)}
	}

	if l := len(conf.APIKey); l < ValidMinMailgunAPIKeyLength || l > ValidMaxMailgunAPIKeyLength {
		return nil, &MailerError{Message: fmt.Sprintf("APIKey must be between %d and %d characters", ValidMinMailgunAPIKeyLength, ValidMaxMailgunAPIKeyLength)}
	}

	client := conf.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultMailgunTimeout}
	}

	return &MailerMailgun{apiURL: conf.APIURL, apiKey: conf.APIKey, client: client}, nil
}

// Send delivers a single message through the Mailgun API. It honours the
// context and returns a *MailerError wrapping the underlying failure.
func (m *MailerMailgun) Send(ctx context.Context, content MailContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	form := url.Values{}
	form.Set("from", formatMailgunAddress(content.FromName(), content.FromAddress()))
	form.Set("to", formatMailgunAddress(content.ToName(), content.ToAddress()))
	form.Set("subject", content.Subject())

	// text and html are DIFFERENT fields at Mailgun, and putting the body under
	// the wrong one delivers the markup as visible source to the recipient.
	if content.MimeType() == MimeTypeTextHTML {
		form.Set("html", content.Body())
	} else {
		form.Set("text", content.Body())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return &MailerError{Message: "failed to build the Mailgun request", Err: err}
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(mailgunBasicAuthUser, m.apiKey)

	resp, err := m.client.Do(req)
	if err != nil {
		// The URL is configuration and safe to name. The KEY never appears in an
		// error: an error message is the one place guaranteed to reach a log.
		return &MailerError{Message: fmt.Sprintf("failed to send the message to %s", m.apiURL), Err: err}
	}

	defer func() { _ = resp.Body.Close() }()

	// Read before deciding, so the body is available for the error below and so
	// the connection can be reused on the success path.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxMailgunErrorBody))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &MailerError{Message: fmt.Sprintf(
			"Mailgun returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)),
		)}
	}

	return nil
}

// formatMailgunAddress renders a recipient as RFC 5322 wants it.
//
// An unquoted display name containing a comma would split one recipient into
// two, so any name carrying a special character is quoted. An empty name yields
// the bare address rather than a stray "<>" prefix.
func formatMailgunAddress(name, address string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return address
	}

	if strings.ContainsAny(name, `",;:<>@[]\`) {
		name = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}

	return name + " <" + address + ">"
}
