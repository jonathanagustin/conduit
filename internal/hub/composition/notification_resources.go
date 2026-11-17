package composition

import (
	"context"
	"fmt"
	"strings"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// notificationResourceResolver translates only authorization coordinates. The
// notification's resource, identity and subscription joins remain unchanged.
//
// CONDUIT-INVENTED: notifications.EnvironmentAuthorizationAlias
// Displaced-by: an authoritative notification resource-to-authorization mapping.
type notificationResourceResolver struct{ repository controlplane.Repository }

func (resolver notificationResourceResolver) resolve(ctx context.Context, resource string) (string, bool, error) {
	if !strings.HasPrefix(resource, "ri.apollo.main.environment.") {
		return resource, true, nil
	}
	catalog, err := controlplane.ListEnvironmentCatalog(ctx, resolver.repository)
	if err != nil {
		return "", false, fmt.Errorf("read notification Environment identities: %w", err)
	}
	// Compare the existing forward mapping with trusted inventory. The locator
	// is opaque, so neither decoding it nor trusting Event.Environment is valid.
	matched := ""
	for _, record := range catalog {
		id := record.Environment.ID
		if apolloapplication.EnvironmentRID(id) != resource {
			continue
		}
		if matched != "" && matched != id {
			return "", false, nil
		}
		matched = id
	}
	// The repository accepts report writes below the authenticated Agent
	// boundary. Do not let corrupt or legacy report IDs create policy scopes.
	if !identity.DNSLabelPattern.MatchString(matched) {
		return "", false, nil
	}
	return "environment/" + matched, true, nil
}

type notificationRecipientResourceAuthorization struct {
	resources notificationResourceResolver
	next      notificationports.RecipientEligibility
}

func (authorization notificationRecipientResourceAuthorization) Check(ctx context.Context, owner domain.Owner, resource string) (bool, error) {
	scope, known, err := authorization.resources.resolve(ctx, resource)
	if err != nil || !known {
		return false, err
	}
	return authorization.next.Check(ctx, owner, scope)
}
