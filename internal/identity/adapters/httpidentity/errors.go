package httpidentity

import "errors"

var (
	errClientCertificateRequired = errors.New(
		"a verified per-Environment client certificate is required")
	errInvalidDevelopmentHeaders = errors.New(
		"development identity headers require valid Environment and Agent IDs")
)
