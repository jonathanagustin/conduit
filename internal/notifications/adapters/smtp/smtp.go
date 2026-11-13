// Package smtp submits native email notifications using verified TLS and the
// standard SMTP envelope (RFC 5321), message format (RFC 5322), STARTTLS
// (RFC 3207), and optional AUTH PLAIN over TLS (RFC 4954/RFC 4616).
package smtp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	netsmtp "net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

const DefaultTimeout = 10 * time.Second

type Config struct {
	Address   string
	From      string
	Username  string
	Password  string
	TLSMode   string
	TLSConfig *tls.Config
	Timeout   time.Duration
}

type Transport struct {
	config Config
	host   string
	sender *mail.Address
}

var _ ports.Transport = (*Transport)(nil)

// New validates a bounded, certificate-verifying SMTP submission configuration.
// Empty TLSMode means STARTTLS; implicit TLS is available as "tls". Plaintext
// delivery and disabling certificate verification are not supported.
func New(config Config) (*Transport, error) {
	host, port, err := net.SplitHostPort(config.Address)
	number, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || number < 1 || number > 65535 {
		return nil, errors.New("SMTP address must name a host and numeric port")
	}
	sender, err := mail.ParseAddress(config.From)
	if err != nil || !strings.Contains(sender.Address, "@") {
		return nil, errors.New("SMTP From must be a valid mailbox with a domain")
	}
	if config.TLSMode == "" {
		config.TLSMode = "starttls"
	}
	if config.TLSMode != "starttls" && config.TLSMode != "tls" {
		return nil, errors.New("SMTP TLS mode must be starttls or tls")
	}
	if (config.Username == "") != (config.Password == "") {
		return nil, errors.New("SMTP authentication requires both username and password")
	}
	if strings.ContainsRune(config.Username, 0) || strings.ContainsRune(config.Password, 0) {
		return nil, errors.New("SMTP credentials contain an invalid delimiter")
	}
	secured := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if config.TLSConfig != nil {
		secured = config.TLSConfig.Clone()
		if secured.InsecureSkipVerify {
			return nil, errors.New("SMTP certificate verification cannot be disabled")
		}
		if secured.MinVersion < tls.VersionTLS12 {
			secured.MinVersion = tls.VersionTLS12
		}
		if secured.MaxVersion != 0 && secured.MaxVersion < secured.MinVersion {
			return nil, errors.New("SMTP maximum TLS version is below its required minimum")
		}
		if secured.ServerName == "" {
			secured.ServerName = host
		}
		if secured.RootCAs != nil {
			secured.RootCAs = secured.RootCAs.Clone()
		}
	}
	config.TLSConfig = secured
	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}
	if config.Timeout < 0 {
		return nil, errors.New("SMTP timeout must be positive")
	}
	return &Transport{config: config, host: host, sender: sender}, nil
}

func (*Transport) Name() string   { return "smtp" }
func (*Transport) String() string { return "smtp notification transport" }

func (transport *Transport) Deliver(ctx context.Context, message domain.Message) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if message.Recipient.Channel != domain.ChannelEmail || message.Destination.Kind != domain.ContactEmail || !message.Destination.Named() {
		return fmt.Errorf("%w: SMTP delivery requires an email contact", ports.ErrPermanentDelivery)
	}
	recipient, err := mail.ParseAddress(message.Destination.Address)
	if err != nil || !strings.Contains(recipient.Address, "@") {
		return fmt.Errorf("%w: invalid notification email contact", ports.ErrPermanentDelivery)
	}
	attempt, cancel := context.WithTimeout(ctx, transport.config.Timeout)
	defer cancel()
	dialer := &net.Dialer{}
	var connection net.Conn
	if transport.config.TLSMode == "tls" {
		connection, err = (&tls.Dialer{NetDialer: dialer, Config: transport.config.TLSConfig}).DialContext(attempt, "tcp", transport.config.Address)
	} else {
		connection, err = dialer.DialContext(attempt, "tcp", transport.config.Address)
	}
	if err != nil {
		return deliveryError(attempt, err)
	}
	defer connection.Close()
	stop := context.AfterFunc(attempt, func() { _ = connection.Close() })
	defer stop()
	if deadline, ok := attempt.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	client, err := netsmtp.NewClient(connection, transport.host)
	if err != nil {
		return deliveryError(attempt, err)
	}
	defer client.Close()
	if transport.config.TLSMode == "starttls" {
		if supported, _ := client.Extension("STARTTLS"); !supported {
			return fmt.Errorf("%w: SMTP server does not offer required STARTTLS", ports.ErrPermanentDelivery)
		}
		if err := client.StartTLS(transport.config.TLSConfig); err != nil {
			return deliveryError(attempt, err)
		}
	}
	if transport.config.Username != "" {
		if supported, _ := client.Extension("AUTH"); !supported {
			return fmt.Errorf("%w: SMTP server does not offer authentication", ports.ErrPermanentDelivery)
		}
		auth := netsmtp.PlainAuth("", transport.config.Username, transport.config.Password, transport.host)
		if err := client.Auth(auth); err != nil {
			return deliveryError(attempt, err)
		}
	}
	if err := client.Mail(transport.sender.Address); err != nil {
		return deliveryError(attempt, err)
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return deliveryError(attempt, err)
	}
	writer, err := client.Data()
	if err != nil {
		return deliveryError(attempt, err)
	}
	if _, err := writer.Write(transport.encode(message, recipient)); err != nil {
		return deliveryError(attempt, err)
	}
	if err := writer.Close(); err != nil {
		return deliveryError(attempt, err)
	}
	// DATA's final affirmative reply is the submission acknowledgement. A later
	// QUIT failure cannot turn accepted mail into an unsent notification.
	_ = client.Quit()
	return nil
}

func (transport *Transport) encode(message domain.Message, recipient *mail.Address) []byte {
	var encoded bytes.Buffer
	identity := sha256.Sum256([]byte(message.EventID + "\x00" + string(message.Recipient.Owner.Kind) + "\x00" + message.Recipient.Owner.ID + "\x00" + string(message.Recipient.Channel)))
	_, senderDomain, _ := strings.Cut(transport.sender.Address, "@")
	fmt.Fprintf(&encoded, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: <%x@%s>\r\n", transport.sender.String(), recipient.String(), mime.QEncoding.Encode("utf-8", message.Subject), identity[:16], senderDomain)
	if !message.OccurredAt.IsZero() {
		fmt.Fprintf(&encoded, "Date: %s\r\n", message.OccurredAt.UTC().Format(time.RFC1123Z))
	}
	encoded.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	body := quotedprintable.NewWriter(&encoded)
	_, _ = body.Write([]byte(message.Body))
	_ = body.Close()
	return encoded.Bytes()
}

func deliveryError(ctx context.Context, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(cause, &unknown) || errors.As(cause, &hostname) || errors.As(cause, &invalid) {
		return fmt.Errorf("%w: SMTP TLS certificate verification failed", ports.ErrPermanentDelivery)
	}
	// SMTP replies may echo a mailbox, credential or message. Retain only the
	// numeric response class, never provider text or the recipient's address.
	var response *textproto.Error
	if errors.As(cause, &response) {
		if response.Code >= 500 {
			return fmt.Errorf("%w: SMTP response %d", ports.ErrPermanentDelivery, response.Code)
		}
		return fmt.Errorf("SMTP response %d", response.Code)
	}
	return errors.New("SMTP submission failed")
}
