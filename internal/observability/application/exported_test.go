package application_test

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/application"
)

const exportedMetricsManifest = "../../../deploy/observability/exported-metrics.txt"

// exportedFamilies renders everything this package can publish, with every
// recorder exercised so that no series is absent for want of an event, and
// returns "name type" for each family.
func exportedFamilies(t *testing.T) []string {
	t.Helper()
	gather := application.Gather("1.4.2", func(context.Context) (application.ReconciliationStats, error) {
		return stats(), nil
	})
	gather = application.WithRateLimits(gather, func() []application.RateLimitClass {
		return []application.RateLimitClass{{Class: "operator", Allowed: 1, Limited: 1, Callers: 1}}
	})
	requests := application.NewHTTPRecorder([]string{"/v1/products"})
	requests.Begin()
	requests.Finish("/v1/products", "GET", 200, time.Millisecond)
	gather = application.WithHTTPRequests(gather, requests)
	deliveries := application.NewDeliveryRecorder()
	deliveries.Record(application.TransportWebhook, application.DeliveryAttempted)
	gather = application.WithNotificationDeliveries(gather, deliveries,
		func(context.Context) ([]application.DeliveryBacklog, error) {
			return []application.DeliveryBacklog{{Transport: application.TransportWebhook, Pending: 1}}, nil
		})
	registry, err := gather(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatalf("the complete registry would be refused at publish time: %v", err)
	}
	families := []string{}
	for _, line := range strings.Split(registry.Render(), "\n") {
		if declaration, found := strings.CutPrefix(line, "# TYPE "); found {
			families = append(families, declaration)
		}
	}
	sort.Strings(families)
	return families
}

// The alert rules and the dashboard are checked against a manifest of metric
// names (scripts/verify-observability-assets.rb), because a script cannot run
// the Hub. This is the other half: the manifest is what the code renders, name
// for name and type for type. Renaming a series, or adding one, fails here
// until the manifest says so -- and then the script fails until every rule and
// panel that used the old name is fixed.
func TestTheExportedMetricsManifestIsWhatTheCodeRenders(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(exportedMetricsManifest)
	if err != nil {
		t.Fatal(err)
	}
	recorded := []string{}
	for _, line := range strings.Split(string(contents), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			recorded = append(recorded, line)
		}
	}
	sort.Strings(recorded)
	rendered := exportedFamilies(t)
	if strings.Join(recorded, "\n") != strings.Join(rendered, "\n") {
		t.Fatalf("deploy/observability/exported-metrics.txt does not match what the code renders.\n"+
			"manifest:\n  %s\nrendered:\n  %s\n"+
			"Update the manifest, then run ruby scripts/verify-observability-assets.rb to find the rules and panels that used the old names.",
			strings.Join(recorded, "\n  "), strings.Join(rendered, "\n  "))
	}
}
