package mailer

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// receivedMail captures what a fakeSMTPServer observed for a single delivery.
type receivedMail struct {
	from    string
	to      []string
	data    string
	authed  bool
	usedTLS bool
}

// fakeSMTPServer is a minimal, in-process SMTP server used to exercise the
// MailerSMTP transport end to end without reaching a real mail server.
type fakeSMTPServer struct {
	t         *testing.T
	ln        net.Listener
	tlsConfig *tls.Config
	offerTLS  bool // advertise STARTTLS
	offerAuth bool // advertise AUTH PLAIN
	implicit  bool // serve TLS immediately (SMTPS)

	received chan receivedMail
}

// newFakeSMTPServer starts a server on an ephemeral loopback port and returns it.
func newFakeSMTPServer(t *testing.T, opts fakeSMTPServer) *fakeSMTPServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s := &fakeSMTPServer{
		t:         t,
		ln:        ln,
		tlsConfig: testServerTLSConfig(t),
		offerTLS:  opts.offerTLS,
		offerAuth: opts.offerAuth,
		implicit:  opts.implicit,
		received:  make(chan receivedMail, 8),
	}

	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTPServer) host() string {
	h, _, _ := net.SplitHostPort(s.ln.Addr().String())
	return h
}

func (s *fakeSMTPServer) port() int {
	return s.ln.Addr().(*net.TCPAddr).Port
}

func (s *fakeSMTPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handle(conn)
	}
}

func (s *fakeSMTPServer) handle(conn net.Conn) {
	defer conn.Close()

	if s.implicit {
		tlsConn := tls.Server(conn, s.tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		conn = tlsConn
	}

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}

	_, usedTLS := conn.(*tls.Conn)
	var rec receivedMail
	rec.usedTLS = usedTLS

	write("220 fake ESMTP ready")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			write("250-fake greets you")
			if s.offerTLS && !usedTLS {
				write("250-STARTTLS")
			}
			if s.offerAuth {
				write("250-AUTH PLAIN")
			}
			write("250 SMTPUTF8")

		case strings.HasPrefix(cmd, "STARTTLS"):
			write("220 ready to start TLS")
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			usedTLS = true
			rec.usedTLS = true
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			write = func(line string) {
				_, _ = w.WriteString(line + "\r\n")
				_ = w.Flush()
			}

		case strings.HasPrefix(cmd, "AUTH"):
			rec.authed = true
			write("235 2.7.0 authentication succeeded")

		case strings.HasPrefix(cmd, "MAIL FROM:"):
			rec.from = strings.TrimPrefix(line[len("MAIL FROM:"):], " ")
			write("250 2.1.0 OK")

		case strings.HasPrefix(cmd, "RCPT TO:"):
			rec.to = append(rec.to, strings.TrimPrefix(line[len("RCPT TO:"):], " "))
			write("250 2.1.5 OK")

		case strings.HasPrefix(cmd, "DATA"):
			write("354 end data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if dl == ".\r\n" || dl == ".\n" {
					break
				}
				body.WriteString(dl)
			}
			rec.data = body.String()
			write("250 2.0.0 OK: queued")

		case strings.HasPrefix(cmd, "QUIT"):
			write("221 2.0.0 Bye")
			s.received <- rec
			return

		case strings.HasPrefix(cmd, "RSET"), strings.HasPrefix(cmd, "NOOP"):
			write("250 2.0.0 OK")

		default:
			write("500 5.5.1 command not recognized")
		}
	}
}

// testServerTLSConfig builds a throwaway self-signed certificate for the server.
func testServerTLSConfig(t *testing.T) *tls.Config {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: priv}},
	}
}
