package notificationsadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	catalogports "github.com/jonathanagustin/conduit/internal/catalog/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// PromotionFailures raises a notification for a promotion run that ended
// without promoting.
type PromotionFailures struct {
	emitter notificationports.Emitter
}

var _ catalogports.PromotionFailures = (*PromotionFailures)(nil)

func NewPromotionFailures(emitter notificationports.Emitter) (*PromotionFailures, error) {
	if emitter == nil {
		return nil, errors.New("a notification emitter is required to announce a failed promotion")
	}
	return &PromotionFailures{emitter: emitter}, nil
}

// PromotionEvaluated announces an evaluation whose lifecycle is ABORTED and
// nothing else.
//
// BLOCKED is not the test, and that is the decision this method exists to
// hold. A staged promotion is evaluated repeatedly and is BLOCKED on every
// evaluation until its soak has elapsed; that lifecycle is RUNNING, and telling
// somebody each time would be telling them the stage is working. ABORTED is
// the lifecycle catalog.PromotionLifecycleFor assigns when the evaluation
// window timed out, a canary recall condition triggered, or an operator
// aborted the run: the run is over and the Release did not cross the boundary.
// CANCELLED is a person withdrawing the run, which is a decision rather than a
// failure, and the person who made it does not need telling.
//
// A record with no lifecycle predates runs and is skipped: nothing on it says
// whether the promotion is over.
// CONDUIT-INVENTED: catalog.PromotionFailureSelection
// Displaced-by: a capture of what Apollo counts as a failed promotion for PRODUCT_RELEASE_PROMOTION_FAILED and which resource identifier the event carries
func (failures *PromotionFailures) PromotionEvaluated(
	ctx context.Context, record catalog.PromotionRecord,
) error {
	if record.Lifecycle != catalog.PromotionAborted {
		return nil
	}
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.Product) == "" {
		return errors.New("announce failed promotion: the evaluation names no id or no Product")
	}
	return failures.emitter.Emit(ctx, notifications.Event{
		// Stable in the evaluation, which is itself a digest of its content, so
		// the same evaluation submitted twice is one event.
		ID:   promotionEventID(record.ID),
		Type: notifications.EventPromotionFailed,
		// The Product, under the identifier Governance already authorizes a
		// Product by. Apollo offers this type on a Product's settings page, and
		// a promotion crosses channels rather than Environments, so there is no
		// Environment to name. Conduit mints no ri.apollo.main identifier for a
		// Product; product/<coordinate> is the one a grant is written against,
		// and therefore the one a preference can be authorized on.
		ResourceRID: ProductRID(record.Product),
		Summary: fmt.Sprintf(
			"Promotion of Product %s version %s from channel %s to channel %s was aborted: %s",
			record.Product, record.Version, record.FromChannel, record.ToChannel,
			bounded(promotionReason(record)),
		),
		// No actor: an evaluation record names nobody. An operator abort and a
		// timeout are told apart by the reason, which is in the summary.
		OccurredAt: record.EvaluatedAt.UTC(),
	})
}

// ProductRID is the resource a promotion failure is about, exported so the
// composition test that proves the join names it the same way.
func ProductRID(product string) string { return "product/" + product }

func promotionReason(record catalog.PromotionRecord) string {
	if reason := strings.TrimSpace(record.LifecycleReason); reason != "" {
		return reason
	}
	return strings.Join(record.Reasons, "; ")
}

func promotionEventID(evaluationID string) string {
	digest := sha256.Sum256([]byte("promotion-aborted\x00" + evaluationID))
	return "nev_" + hex.EncodeToString(digest[:16])
}
