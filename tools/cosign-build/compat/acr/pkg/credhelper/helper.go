// Package credhelper adapts Cosign's legacy Azure helper import to the
// maintained implementation. No legacy token-exchange implementation is kept.
package credhelper

import (
	"github.com/docker/docker-credential-helpers/credentials"
	maintained "github.com/osscontainertools/docker-credential-acr/pkg/credhelper"
)

// NewACRCredentialsHelper preserves the constructor consumed by Cosign.
func NewACRCredentialsHelper() credentials.Helper {
	return maintained.NewACRCredentialsHelper()
}
