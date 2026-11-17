package ports

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// SessionStore remembers signed-in browsers and sign-in attempts in flight.
//
// It is keyed by hashes throughout: no method takes or returns a cookie value,
// so an implementation cannot store one by accident and a dump of the store is
// not a set of credentials.
//
// Every read takes the caller's clock and treats an expired record as absent.
// Expiry is therefore enforced on the read path and Prune is housekeeping: a
// store that was never pruned is larger, never more permissive.
//
// An implementation reports domain.ErrSessionNotFound for anything absent,
// expired or already consumed. Any other error means the store could not be
// consulted, and a caller must refuse rather than read it as "signed out".
type SessionStore interface {
	// SaveLoginAttempt records an attempt. A second attempt under the same
	// state hash is an error, never an overwrite.
	SaveLoginAttempt(ctx context.Context, attempt domain.LoginAttempt, now time.Time) error
	// TakeLoginAttempt returns the attempt and deletes it in one step, so a
	// state value can be redeemed once however many replicas are asked.
	TakeLoginAttempt(ctx context.Context, stateHash string, now time.Time) (domain.LoginAttempt, error)

	SaveSession(ctx context.Context, session domain.Session) error
	Session(ctx context.Context, idHash string, now time.Time) (domain.Session, error)
	// DeleteSession is idempotent: signing out twice is not an error.
	DeleteSession(ctx context.Context, idHash string) error

	// Prune removes what has expired.
	Prune(ctx context.Context, now time.Time) error
}

// AuthorizationCodeProvider is the identity provider's side of a browser
// sign-in: where to send the browser, and how to redeem what it brings back.
//
// It returns the ID token as an opaque string. Whether that token is authentic
// is the TokenVerifier's answer and what it must say is the policy's; a
// provider adapter that vouched for its own response would be trusting the
// network.
type AuthorizationCodeProvider interface {
	AuthorizationEndpoint() string
	// EndSessionEndpoint is empty when the provider advertises none.
	EndSessionEndpoint() string
	// Exchange redeems one authorization code with its PKCE verifier.
	Exchange(ctx context.Context, code, codeVerifier, redirectURI string) (idToken string, err error)
}
