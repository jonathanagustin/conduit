package composition

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/adapters/logging"
	"github.com/jonathanagustin/conduit/internal/notifications/adapters/webhook"
	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// Notification delivery is off unless configured, exactly as METRICS_ADDRESS
// leaves the metrics listener off. An existing deployment gains no outbound
// behaviour by upgrading: with no delivery settings, no transport is built, no
// dispatcher is constructed, no loop runs and no request leaves the process.
// Sending mail nobody asked for is worse than not sending it.
const (
	// NotificationWebhookURLVariable names the endpoint deliveries are POSTed
	// to. Setting it is the whole opt-in.
	NotificationWebhookURLVariable = "NOTIFICATION_WEBHOOK_URL"
	// NotificationWebhookTimeoutVariable bounds one attempt. Optional.
	NotificationWebhookTimeoutVariable = "NOTIFICATION_WEBHOOK_TIMEOUT"
	// NotificationDeliveryLogVariable turns on the structured-log transport for
	// a deployment whose logs somebody already watches.
	NotificationDeliveryLogVariable = "NOTIFICATION_DELIVERY_LOG"
)

// DefaultDispatcherConfig is the retry budget a Hub runs with.
//
// The numbers are the reconciliation worker's, deliberately: ten attempts from
// a two-second base doubling to an hour's ceiling. An operator watching both
// queues during an incident should not have to hold two models of when the next
// attempt lands.
func DefaultDispatcherConfig() application.DispatcherConfig {
	return application.DispatcherConfig{
		LeaseDuration:   30 * time.Second,
		MaximumAttempts: 10,
		BaseRetryDelay:  2 * time.Second,
		PollInterval:    time.Second,
	}
}

// NewNotificationTransport builds the transport an operator asked for, or
// reports that none was asked for.
//
// A nil transport with a nil error is the unconfigured case and is the normal
// one. A misconfiguration is an error rather than a silent fallback, on the
// same reasoning the Gitless scheduler uses in cmd/platform-delivery: a Hub
// that started cleanly and silently never told anyone anything is worse than
// one that refused to start.
func NewNotificationTransport(
	getenv func(string) string, logger *slog.Logger,
) (ports.Transport, error) {
	if getenv == nil {
		return nil, errors.New("an environment reader is required to configure notification delivery")
	}
	endpoint := strings.TrimSpace(getenv(NotificationWebhookURLVariable))
	timeoutValue := strings.TrimSpace(getenv(NotificationWebhookTimeoutVariable))
	useLog := strings.TrimSpace(getenv(NotificationDeliveryLogVariable)) == "true"
	if nativeNotificationRequested(getenv) {
		if endpoint != "" || timeoutValue != "" || useLog {
			return nil, errors.New("native notification settings cannot be combined with webhook or log delivery")
		}
		return newNativeNotificationTransport(getenv)
	}
	if endpoint == "" && timeoutValue != "" {
		return nil, fmt.Errorf("%s requires %s",
			NotificationWebhookTimeoutVariable, NotificationWebhookURLVariable)
	}
	if endpoint != "" && useLog {
		// Refused rather than resolved by precedence. Both are destinations, and
		// a Hub that quietly picked one would deliver everything to the loser's
		// operator exactly never, while its configuration says otherwise.
		return nil, fmt.Errorf("%s and %s are both set; choose one notification transport",
			NotificationWebhookURLVariable, NotificationDeliveryLogVariable)
	}
	switch {
	case endpoint != "":
		client, err := webhookClient(timeoutValue)
		if err != nil {
			return nil, err
		}
		return webhook.New(endpoint, client)
	case useLog:
		return logging.New(logger), nil
	default:
		return nil, nil
	}
}

// NewNotificationDispatcher assembles the dispatcher, or reports that delivery
// is off.
//
// Returns a nil dispatcher and a nil error when no transport is configured,
// which the caller reads as "start no loop". It does not substitute a
// do-nothing transport: a dispatcher that leased entries and marked them
// delivered without sending them would record silence as success.
func NewNotificationDispatcher(
	queue ports.DeliveryQueue, transport ports.Transport,
	clock ports.Clock, config application.DispatcherConfig,
) (*application.Dispatcher, error) {
	if transport == nil {
		return nil, nil
	}
	if queue == nil {
		return nil, errors.New("notification delivery is configured but no durable queue is available")
	}
	return application.NewDispatcher(queue, transport, clock, config)
}

// webhookClient returns nil when no timeout was configured, which the webhook
// adapter reads as "use the default one" rather than as "use no timeout".
func webhookClient(timeoutValue string) (*http.Client, error) {
	if timeoutValue == "" {
		return nil, nil
	}
	timeout, err := time.ParseDuration(timeoutValue)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", NotificationWebhookTimeoutVariable, err)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("%s must be positive", NotificationWebhookTimeoutVariable)
	}
	return &http.Client{Timeout: timeout}, nil
}
