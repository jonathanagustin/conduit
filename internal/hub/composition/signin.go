package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/signinhttp"
)

// mountSignIn puts the browser sign-in routes in front of the Hub.
//
// They are mounted whether or not sign-in is configured. A nil handler answers
// each of them 404 with the reason, so a console that asks a Hub without
// sign-in to start one is told so, rather than falling through to the facade
// and being told nothing.
//
// The three routes are public in HubRouteAuthorization and therefore spend from
// the anonymous rate-limit bucket: starting a sign-in is the one write an
// unauthenticated caller can cause, and that bucket is what bounds it.
func mountSignIn(next http.Handler, signIn *signinhttp.Handler) http.Handler {
	mux := http.NewServeMux()
	signIn.Register(mux)
	mux.Handle("/", next)
	return mux
}
