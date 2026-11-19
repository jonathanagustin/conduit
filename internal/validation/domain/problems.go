// Package domain defines native validation problem values and immutable field
// grammar expressions without depending on a transport or control-plane facade.
package domain

import "strings"

// Problem is one existing path/message diagnostic; it grants no authority.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Problems retains diagnostic order and the existing aggregate error format.
type Problems []Problem

func (errs Problems) Error() string {
	parts := make([]string, len(errs))
	for index, err := range errs {
		parts[index] = err.Path + ": " + err.Message
	}
	return strings.Join(parts, "; ")
}

// Field grammar expressions are lexical constraints only. Callers retain any
// additional field-specific length, scope and authority checks.
const (
	ProductCoordinateExpression = `^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+$`
	DNSLabelExpression          = `^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`
	ReleaseChannelExpression    = `^[A-Z][A-Z0-9_-]*$`
	DependencyGroupExpression   = `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`
)
