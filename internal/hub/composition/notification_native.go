package composition

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/adapters/slack"
	notificationsmtp "github.com/jonathanagustin/conduit/internal/notifications/adapters/smtp"
	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

const (
	NotificationSMTPAddressVariable      = "NOTIFICATION_SMTP_ADDRESS"
	NotificationSMTPFromVariable         = "NOTIFICATION_SMTP_FROM"
	NotificationSMTPUsernameFileVariable = "NOTIFICATION_SMTP_USERNAME_FILE"
	NotificationSMTPPasswordFileVariable = "NOTIFICATION_SMTP_PASSWORD_FILE"
	NotificationSMTPTLSModeVariable      = "NOTIFICATION_SMTP_TLS_MODE"
	NotificationSMTPCAFileVariable       = "NOTIFICATION_SMTP_CA_FILE"
	NotificationSlackTokenFileVariable   = "NOTIFICATION_SLACK_TOKEN_FILE"
	NotificationNativeTimeoutVariable    = "NOTIFICATION_NATIVE_TIMEOUT"
)

var smtpVariables = []string{NotificationSMTPAddressVariable, NotificationSMTPFromVariable, NotificationSMTPUsernameFileVariable, NotificationSMTPPasswordFileVariable, NotificationSMTPTLSModeVariable, NotificationSMTPCAFileVariable}

func anyConfigured(getenv func(string) string, names []string) bool {
	for _, name := range names {
		if strings.TrimSpace(getenv(name)) != "" {
			return true
		}
	}
	return false
}

// NotificationDeliveryRequested reports explicit delivery configuration without
// reading credentials, so unsupported repository modes can refuse it early.
func NotificationDeliveryRequested(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	return nativeNotificationRequested(getenv) || strings.TrimSpace(getenv(NotificationWebhookURLVariable)) != "" || strings.TrimSpace(getenv(NotificationWebhookTimeoutVariable)) != "" || strings.TrimSpace(getenv(NotificationDeliveryLogVariable)) == "true"
}

func nativeNotificationRequested(getenv func(string) string) bool {
	return anyConfigured(getenv, smtpVariables) || strings.TrimSpace(getenv(NotificationSlackTokenFileVariable)) != "" || strings.TrimSpace(getenv(NotificationNativeTimeoutVariable)) != ""
}

func newNativeNotificationTransport(getenv func(string) string) (ports.Transport, error) {
	timeout := notificationsmtp.DefaultTimeout
	if value := strings.TrimSpace(getenv(NotificationNativeTimeoutVariable)); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 || parsed >= DefaultDispatcherConfig().LeaseDuration {
			return nil, fmt.Errorf("%s must be positive and shorter than the delivery lease", NotificationNativeTimeoutVariable)
		}
		timeout = parsed
	}
	channels := make(map[domain.Channel]ports.Transport)
	if anyConfigured(getenv, smtpVariables) {
		config := notificationsmtp.Config{Address: strings.TrimSpace(getenv(NotificationSMTPAddressVariable)), From: strings.TrimSpace(getenv(NotificationSMTPFromVariable)), TLSMode: strings.TrimSpace(getenv(NotificationSMTPTLSModeVariable)), Timeout: timeout}
		if config.Address == "" || config.From == "" {
			return nil, errors.New("native SMTP requires NOTIFICATION_SMTP_ADDRESS and NOTIFICATION_SMTP_FROM")
		}
		if path := strings.TrimSpace(getenv(NotificationSMTPUsernameFileVariable)); path != "" {
			secret, err := readNotificationSecret(path, NotificationSMTPUsernameFileVariable)
			if err != nil {
				return nil, err
			}
			config.Username = secret
		}
		if path := strings.TrimSpace(getenv(NotificationSMTPPasswordFileVariable)); path != "" {
			secret, err := readNotificationSecret(path, NotificationSMTPPasswordFileVariable)
			if err != nil {
				return nil, err
			}
			config.Password = secret
		}
		if path := strings.TrimSpace(getenv(NotificationSMTPCAFileVariable)); path != "" {
			contents, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("%s cannot be read", NotificationSMTPCAFileVariable)
			}
			roots, err := x509.SystemCertPool()
			if err != nil {
				roots = x509.NewCertPool()
			}
			if !roots.AppendCertsFromPEM(contents) {
				return nil, fmt.Errorf("%s contains no trusted certificates", NotificationSMTPCAFileVariable)
			}
			config.TLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		}
		transport, err := notificationsmtp.New(config)
		if err != nil {
			return nil, err
		}
		channels[domain.ChannelEmail] = transport
	}
	if path := strings.TrimSpace(getenv(NotificationSlackTokenFileVariable)); path != "" {
		secret, err := readNotificationSecret(path, NotificationSlackTokenFileVariable)
		if err != nil {
			return nil, err
		}
		transport, err := slack.New(secret, &http.Client{Timeout: timeout})
		if err != nil {
			return nil, err
		}
		channels[domain.ChannelSlack] = transport
	}
	return application.NewChannelTransport(channels)
}

func readNotificationSecret(path, variable string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%s cannot be read", variable)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(contents) > 64*1024 {
		return "", fmt.Errorf("%s must name a readable credential file smaller than 64 KiB", variable)
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(contents), "\n"), "\r")
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is empty", variable)
	}
	return value, nil
}

// NewNotificationDestinationResolver preserves generic relay resolution while
// native delivery resolves only the contact matching the selected channel.
func NewNotificationDestinationResolver(transport ports.Transport, store ports.Contacts, clock ports.Clock) (ports.DestinationResolver, error) {
	if _, native := transport.(*application.ChannelTransport); native {
		preferences, ok := store.(ports.PreferenceReader)
		if !ok {
			return nil, errors.New("native delivery requires a stored preference reader")
		}
		return application.NewNativeDestinations(store, preferences, clock)
	}
	return application.NewContacts(store, clock)
}
