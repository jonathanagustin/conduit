package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

type receipt struct {
	from, recipient            string
	auth, protected, validAuth bool
	data                       []byte
}
type submissionServer struct {
	address string
	roots   *x509.CertPool
	done    chan receipt
}
type serverOptions struct {
	implicit, starttls, stall bool
	reject                    int
}

func startServer(t *testing.T, options serverOptions) submissionServer {
	t.Helper()
	certificateServer := httptest.NewTLSServer(nil)
	secured := &tls.Config{Certificates: certificateServer.TLS.Certificates, MinVersion: tls.VersionTLS12}
	roots := x509.NewCertPool()
	roots.AddCert(certificateServer.Certificate())
	certificateServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	result := submissionServer{address: listener.Addr().String(), roots: roots, done: make(chan receipt, 1)}
	go func() {
		seen := receipt{}
		defer func() { result.done <- seen }()
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		if options.stall {
			_, _ = io.Copy(io.Discard, connection)
			return
		}
		protected := options.implicit
		if protected {
			connection = tls.Server(connection, secured)
		}
		write := func(line string) { _, _ = fmt.Fprint(connection, line+"\r\n") }
		write("220 localhost ESMTP")
		reader := textproto.NewReader(bufio.NewReader(connection))
		for {
			line, err := reader.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO ") || strings.HasPrefix(line, "HELO "):
				write("250-localhost")
				if !protected && options.starttls {
					write("250-STARTTLS")
				}
				write("250 AUTH PLAIN")
			case line == "STARTTLS":
				if !options.starttls {
					write("500 unsupported")
					continue
				}
				write("220 begin TLS")
				connection = tls.Server(connection, secured)
				if err := connection.(*tls.Conn).Handshake(); err != nil {
					return
				}
				protected = true
				reader = textproto.NewReader(bufio.NewReader(connection))
			case strings.HasPrefix(line, "AUTH "):
				seen.auth = true
				decoded, authErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
				seen.validAuth = authErr == nil && string(decoded) == "\x00sender\x00password-secret"
				seen.protected = protected
				if !protected {
					write("535 no plaintext authentication")
				} else {
					write("235 authenticated")
				}
			case strings.HasPrefix(line, "MAIL FROM:"):
				seen.from = strings.TrimPrefix(line, "MAIL FROM:")
				write("250 sender accepted")
			case strings.HasPrefix(line, "RCPT TO:"):
				seen.recipient = strings.TrimPrefix(line, "RCPT TO:")
				if options.reject != 0 {
					write(fmt.Sprintf("%d recipient-private@example.test password-secret rejected", options.reject))
				} else {
					write("250 recipient accepted")
				}
			case line == "DATA":
				write("354 send message")
				seen.data, err = reader.ReadDotBytes()
				if err != nil {
					return
				}
				write("250 accepted")
			case line == "QUIT":
				write("221 goodbye")
				return
			default:
				write("500 unsupported command")
			}
		}
	}()
	return result
}

func emailMessage() domain.Message {
	return domain.Message{EventID: "event-123", Recipient: domain.Recipient{Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: domain.ChannelEmail}, Destination: domain.Destination{Kind: domain.ContactEmail, Address: "recipient-private@example.test"}, Subject: "Recall ä\r\nBcc: unwanted@example.test", Body: "Notification body.\n.\nStill text ä\n", OccurredAt: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)}
}
func configuration(server submissionServer, mode string) Config {
	return Config{Address: server.address, From: "Conduit <sender@example.test>", Username: "sender", Password: "password-secret", TLSMode: mode, TLSConfig: &tls.Config{RootCAs: server.roots}, Timeout: time.Second}
}

func TestSMTPSubmitsOverVerifiedImplicitTLSAndSTARTTLS(t *testing.T) {
	var identity string
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			server := startServer(t, serverOptions{implicit: mode == "tls", starttls: mode == "starttls"})
			transport, err := New(configuration(server, mode))
			if err != nil {
				t.Fatal(err)
			}
			if err := transport.Deliver(context.Background(), emailMessage()); err != nil {
				t.Fatal(err)
			}
			seen := <-server.done
			if !seen.auth || !seen.protected || !seen.validAuth || seen.from != "<sender@example.test>" || seen.recipient != "<recipient-private@example.test>" {
				t.Fatal("SMTP envelope/authentication did not match the configured sender and selected contact")
			}
			parsed, err := mail.ReadMessage(bytes.NewReader(seen.data))
			if err != nil {
				t.Fatal(err)
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
			if err != nil {
				t.Fatal(err)
			}
			if subject != emailMessage().Subject || parsed.Header.Get("Bcc") != "" {
				t.Fatal("subject changed or injected a header")
			}
			body, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), "Still text ä") || !strings.Contains(string(body), "\n.\n") {
				t.Fatal("SMTP MIME encoding or dot handling changed the body")
			}
			if identity != "" && identity != parsed.Header.Get("Message-Id") {
				t.Fatal("logical delivery identity changed across submission modes")
			}
			identity = parsed.Header.Get("Message-Id")
			if identity == "" {
				t.Fatal("no delivery identity header")
			}
			if strings.Contains(fmt.Sprint(transport), "password-secret") {
				t.Fatal("transport formatting disclosed its credential")
			}
		})
	}
}

func TestSMTPRefusesPlaintextAuthenticationAndUntrustedCertificates(t *testing.T) {
	for _, mode := range []string{"no STARTTLS", "untrusted TLS"} {
		t.Run(mode, func(t *testing.T) {
			server := startServer(t, serverOptions{implicit: mode == "untrusted TLS"})
			config := configuration(server, "starttls")
			if mode == "untrusted TLS" {
				config.TLSMode = "tls"
				config.TLSConfig = nil
			}
			transport, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			err = transport.Deliver(context.Background(), emailMessage())
			if !errors.Is(err, ports.ErrPermanentDelivery) {
				t.Fatalf("TLS failure was not terminal: %v", err)
			}
			seen := <-server.done
			if seen.auth || len(seen.data) > 0 {
				t.Fatal("credentials or notification left before TLS verification")
			}
		})
	}
}

func TestSMTPReplyClassificationNeverDisclosesProviderText(t *testing.T) {
	for _, code := range []int{450, 550} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := startServer(t, serverOptions{implicit: true, reject: code})
			transport, _ := New(configuration(server, "tls"))
			err := transport.Deliver(context.Background(), emailMessage())
			<-server.done
			if err == nil || errors.Is(err, ports.ErrPermanentDelivery) != (code == 550) {
				t.Fatalf("reply classification=%v", err)
			}
			if strings.Contains(err.Error(), "recipient-private") || strings.Contains(err.Error(), "password-secret") {
				t.Fatal("SMTP provider text disclosed contact or credential")
			}
		})
	}
}

func TestSMTPAttemptHasABoundedDeadline(t *testing.T) {
	server := startServer(t, serverOptions{stall: true})
	config := configuration(server, "starttls")
	config.Timeout = 30 * time.Millisecond
	transport, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	err = transport.Deliver(context.Background(), emailMessage())
	if err == nil || time.Since(before) > time.Second {
		t.Fatalf("unbounded submission: %v", err)
	}
	<-server.done
}

func TestSMTPInvalidConfigurationCannotDisableVerification(t *testing.T) {
	for _, config := range []Config{
		{Address: "mail.example.test", From: "sender@example.test"},
		{Address: "mail.example.test:587", From: "invalid"},
		{Address: "mail.example.test:587", From: "sender@example.test", TLSMode: "none"},
		{Address: "mail.example.test:587", From: "sender@example.test", TLSConfig: &tls.Config{InsecureSkipVerify: true}},
		{Address: "mail.example.test:587", From: "sender@example.test", Username: "sender"},
	} {
		if _, err := New(config); err == nil {
			t.Fatal("invalid SMTP configuration accepted")
		}
	}
}
