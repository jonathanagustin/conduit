package controlplane

import (
	"context"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OperatorSignIn is the browser sign-in hint for one configured identity
// provider. It carries no client secret. An empty Issuer means sign-in is off.
type OperatorSignIn struct {
	Name                  string
	Issuer                string
	ClientID              string
	AuthorizationEndpoint string
}

// OperatorSignInFromIssuer reports the provider a browser can be sent to.
// Discovery failure leaves the authorization endpoint empty; the caller still
// learns that a provider is configured and must not collect a password itself.
func OperatorSignInFromIssuer(ctx context.Context, issuer, clientID string) OperatorSignIn {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return OperatorSignIn{}
	}
	signIn := OperatorSignIn{
		Name: "Operator identity", Issuer: issuer, ClientID: strings.TrimSpace(clientID),
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err == nil {
		signIn.AuthorizationEndpoint = provider.Endpoint().AuthURL
	}
	return signIn
}

func operatorIdentityDocument(signIn OperatorSignIn) map[string]any {
	if strings.TrimSpace(signIn.Issuer) == "" {
		return map[string]any{"enabled": false, "providers": []any{}}
	}
	name := strings.TrimSpace(signIn.Name)
	if name == "" {
		name = "Operator identity"
	}
	provider := map[string]any{"name": name, "issuer": signIn.Issuer, "clientId": signIn.ClientID}
	if endpoint := strings.TrimSpace(signIn.AuthorizationEndpoint); endpoint != "" {
		provider["authorizationEndpoint"] = endpoint
	}
	return map[string]any{"enabled": true, "providers": []any{provider}}
}
