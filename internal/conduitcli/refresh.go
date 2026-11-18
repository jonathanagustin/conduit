package conduitcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// One atomic file contains both credentials so refresh rotation cannot commit
// a new bearer token while leaving the previous refresh token beside it.
type refreshGrant struct {
	Version      int    `json:"conduitCredentialVersion"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	TokenKind    string `json:"tokenKind"`
	Subject      string `json:"subject"`
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
}

func validCredential(value string) bool {
	return value != "" && len(value) <= maximumOIDCTokenBytes && !strings.ContainsAny(value, " \t\r\n,")
}

func decodeRefreshGrant(contents string) (refreshGrant, error) {
	var grant refreshGrant
	decoder := json.NewDecoder(strings.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&grant); err != nil {
		return refreshGrant{}, errors.New("invalid stored refresh grant")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return refreshGrant{}, errors.New("invalid stored refresh grant")
	}
	if grant.Version != 1 || requireHTTPSURL(grant.Issuer, "stored issuer") != nil || grant.ClientID == "" || grant.Subject == "" ||
		(grant.TokenKind != "id" && grant.TokenKind != "access") || !validCredential(grant.Token) || !validCredential(grant.RefreshToken) {
		return refreshGrant{}, errors.New("invalid stored refresh grant")
	}
	return grant, nil
}

func encodeRefreshGrant(grant refreshGrant) (string, error) {
	contents, err := json.Marshal(grant)
	if err != nil || len(contents) > maximumBearerTokenBytes {
		return "", errors.New("refresh grant exceeds credential file bounds")
	}
	if _, err := decodeRefreshGrant(string(contents)); err != nil {
		return "", err
	}
	return string(contents), nil
}

func readRefreshGrant(path string) (refreshGrant, error) {
	contents, err := readPrivateContents(path, privateFileLabels{
		inspect: "inspect refresh credential file", open: "open refresh credential file", read: "read refresh credential file",
		regular: "refresh credential file must be regular", perms: "refresh credential file must not grant group or other access", tooLong: "refresh credential file exceeds %d bytes",
	})
	if err != nil {
		return refreshGrant{}, err
	}
	if !strings.HasPrefix(contents, "{") {
		return refreshGrant{}, errors.New("no refresh grant stored; sign in with --store-refresh-token")
	}
	return decodeRefreshGrant(contents)
}

// The token endpoint's authenticated TLS response is the source here. These
// checks bind its ID token to the original identity/client. The Hub still owns
// signature, expiry and authorization verification before accepting the bearer.
func responseSubject(token, issuer, clientID string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("refresh retention requires an ID token bound to the configured issuer and client")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("invalid ID token in refresh response")
	}
	var claims struct {
		Issuer          string          `json:"iss"`
		Subject         string          `json:"sub"`
		Audience        json.RawMessage `json:"aud"`
		AuthorizedParty string          `json:"azp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Issuer != issuer || claims.Subject == "" {
		return "", errors.New("ID token does not match the refresh identity")
	}
	var one string
	var audiences []string
	if json.Unmarshal(claims.Audience, &one) == nil {
		audiences = []string{one}
	} else if json.Unmarshal(claims.Audience, &audiences) != nil {
		return "", errors.New("invalid ID token audience")
	}
	found := false
	for _, audience := range audiences {
		if audience == clientID {
			found = true
		}
	}
	if !found || (claims.AuthorizedParty != "" && claims.AuthorizedParty != clientID) || (len(audiences) > 1 && claims.AuthorizedParty != clientID) {
		return "", errors.New("ID token does not match the refresh client")
	}
	return claims.Subject, nil
}

func newRefreshGrant(issuer, clientID, kind, token string, response tokenResponse) (refreshGrant, error) {
	if !validCredential(response.RefreshToken) {
		return refreshGrant{}, errors.New("the issuer returned no usable refresh token; no credential was replaced")
	}
	subject, err := responseSubject(response.IDToken, issuer, clientID)
	if err != nil {
		return refreshGrant{}, err
	}
	return refreshGrant{Version: 1, Issuer: issuer, ClientID: clientID, TokenKind: kind, Subject: subject, Token: token, RefreshToken: response.RefreshToken}, nil
}

func refreshStoredGrant(ctx context.Context, client *http.Client, endpoints oidcEndpoints, grant refreshGrant, path string, deadline time.Duration) (loginResult, error) {
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	login := personLogin{client: client, endpoints: endpoints, clientID: grant.ClientID, tokenKind: grant.TokenKind}
	var response tokenResponse
	status, err := login.postOIDCForm(ctx, endpoints.TokenEndpoint, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {grant.RefreshToken}, "client_id": {grant.ClientID},
	}, &response)
	if err != nil {
		return loginResult{}, err
	}
	if status != http.StatusOK || response.Error != "" {
		if code := safeOAuthError(response.Error); code != "" {
			return loginResult{}, fmt.Errorf("the issuer refused refresh: %s; sign in again if the grant expired or was revoked", code)
		}
		return loginResult{}, fmt.Errorf("the issuer refused refresh (%d)", status)
	}
	token, err := login.usableToken(response)
	if err != nil {
		return loginResult{}, err
	}
	// RFC 6749 permits a refresh response to omit refresh_token: keep the prior
	// grant in that case. An explicitly malformed replacement is refused.
	if response.RefreshToken == "" {
		response.RefreshToken = grant.RefreshToken
	}
	updated, err := newRefreshGrant(grant.Issuer, grant.ClientID, grant.TokenKind, token, response)
	if err != nil {
		return loginResult{}, err
	}
	if updated.Subject != grant.Subject {
		return loginResult{}, errors.New("refreshed ID token changed the signed-in subject; no credential was replaced")
	}
	encoded, err := encodeRefreshGrant(updated)
	if err != nil {
		return loginResult{}, err
	}
	if err := writeTokenFile(path, encoded); err != nil {
		return loginResult{}, err
	}
	return loginResult{TokenFile: path, Flow: "refresh", TokenKind: grant.TokenKind}, nil
}

// Serialize writers across processes, including interactive login and logout.
// No stale lock is guessed safe to steal; a killed writer requires inspection.
func lockCredentialFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create credential directory: %w", err)
	}
	lockPath := path + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.New("credential file is locked or its lock cannot be created; inspect any interrupted login before removing its .lock file")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(lockPath)
		return nil, errors.New("close credential lock failed")
	}
	return func() { _ = os.Remove(lockPath) }, nil
}
