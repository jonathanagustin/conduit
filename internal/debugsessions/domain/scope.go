package domain

// Scope is the immutable Environment and namespace of a non-executing request.
type Scope struct {
	Environment string `json:"environment"`
	Namespace   string `json:"namespace"`
}

func (s Scope) Validate() error {
	if !coordinate.MatchString(s.Environment) || !namespace.MatchString(s.Namespace) {
		return ErrInvalid
	}
	return nil
}
func (t Target) Scope() Scope { return Scope{Environment: t.Environment, Namespace: t.Namespace} }
