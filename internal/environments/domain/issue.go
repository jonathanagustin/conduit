package domain

// Issue is one thing wrong with an Environment's declared state: where it is
// and what is wrong with it.
//
// It is deliberately not the facade's ValidationError. These rules are decided
// here; which error class they belong to, and what HTTP status that renders as,
// is the caller's vocabulary. Returning the finding rather than the verdict is
// what lets the caller keep its own.
type Issue struct {
	Path    string
	Message string
}
