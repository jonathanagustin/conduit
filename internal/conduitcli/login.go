package conduitcli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// `conduit login` obtains a token for a person.
//
// The CLI is a public OAuth client: it holds no secret worth protecting, so it
// never asks for one. It uses the device authorization grant (RFC 8628) when
// the issuer advertises it, because that needs no browser on this machine and
// no listening socket; otherwise the authorization code grant with PKCE and a
// loopback redirect (RFC 8252). Either way the token is written to the bearer
// token file with mode 0600 and is never printed.
//
// What is deliberately absent: no refresh token is stored, so when the token
// expires the person logs in again; and no token is revoked at the issuer on
// logout, which only removes the local file.

const (
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	defaultLoginScope    = "openid profile email"
	defaultDeviceWait    = 5 * time.Second
	slowDownIncrement    = 5 * time.Second
	maximumDeviceWait    = 60 * time.Second
	defaultLoginDeadline = 5 * time.Minute
	maximumLoginDeadline = 30 * time.Minute
)

type oidcEndpoints struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	DeviceEndpoint        string `json:"device_authorization_endpoint"`
}

type loginRequest struct {
	flow      string
	tokenKind string
	deadline  time.Duration
}

type loginResult struct {
	TokenFile string `json:"tokenFile" yaml:"tokenFile"`
	Flow      string `json:"flow" yaml:"flow"`
	TokenKind string `json:"tokenKind" yaml:"tokenKind"`
}

func loginCommand(state *commandState) *cobra.Command {
	request := loginRequest{}
	command := &cobra.Command{
		Use:   "login",
		Short: "Sign in as a person and store the token in the bearer token file",
		Long: "Sign in at the OIDC issuer and store the resulting token, mode 0600, in the bearer token file.\n\n" +
			"The device authorization grant is used when the issuer advertises it; otherwise the authorization\n" +
			"code grant with PKCE and a loopback redirect on 127.0.0.1. The token is never printed. No refresh\n" +
			"token is stored: when the token expires, run login again.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := state.applySelectedProfile(command); err != nil {
				return err
			}
			result, err := state.login(command.Context(), request)
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, result)
		},
	}
	command.Flags().StringVar(&request.flow, "flow", "auto", "auto, device, or loopback")
	command.Flags().StringVar(&request.tokenKind, "token", "id",
		"which token to store: id (what the Hub's default policy verifies) or access")
	command.Flags().DurationVar(&request.deadline, "login-timeout", defaultLoginDeadline, "how long to wait for the sign-in to finish")
	return command
}

func logoutCommand(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored bearer token",
		Long: "Remove the bearer token file written by login. This signs the CLI out on this machine only;\n" +
			"the token is not revoked at the issuer and stays valid until it expires.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := state.applySelectedProfile(command); err != nil {
				return err
			}
			path, err := state.tokenFilePath()
			if err != nil {
				return err
			}
			removed, err := removeTokenFile(path)
			if err != nil {
				return err
			}
			return writeOutput(state.runtime.Stdout, state.output, map[string]any{"tokenFile": path, "removed": removed})
		},
	}
}

// tokenFilePath is where login writes and logout removes: the configured
// bearer token file, or a file beside the profile document when none is.
func (state *commandState) tokenFilePath() (string, error) {
	if configured := strings.TrimSpace(state.config.BearerTokenFile); configured != "" {
		return configured, nil
	}
	profiles, err := state.profilePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(profiles), "token"), nil
}

func (state *commandState) login(ctx context.Context, request loginRequest) (loginResult, error) {
	configuration := state.config
	issuer, clientID := strings.TrimSpace(configuration.OIDCIssuer), strings.TrimSpace(configuration.OIDCClientID)
	if issuer == "" || clientID == "" {
		return loginResult{}, errors.New("login requires --oidc-issuer and --oidc-client-id")
	}
	if strings.TrimSpace(configuration.OIDCClientSecretFile) != "" {
		return loginResult{}, errors.New("login is for people and uses a public client; --oidc-client-secret-file is for the client-credentials grant")
	}
	if request.tokenKind != "id" && request.tokenKind != "access" {
		return loginResult{}, errors.New("--token must be id or access")
	}
	if request.deadline <= 0 || request.deadline > maximumLoginDeadline {
		return loginResult{}, errors.New("--login-timeout must be greater than zero and at most 30m")
	}
	if err := requireHTTPSURL(issuer, "OIDC issuer"); err != nil {
		return loginResult{}, err
	}
	path, err := state.tokenFilePath()
	if err != nil {
		return loginResult{}, err
	}
	// Checked before anybody is asked to sign in, so a destination that cannot
	// be written is found out now rather than after the token is in hand.
	if err := checkTokenDestination(path); err != nil {
		return loginResult{}, err
	}
	if configuration.Timeout <= 0 {
		configuration.Timeout = defaultTimeout
	}
	client, err := oidcHTTPClient(configuration)
	if err != nil {
		return loginResult{}, err
	}
	defer client.CloseIdleConnections()
	endpoints, err := discoverOIDCEndpoints(ctx, client, issuer)
	if err != nil {
		return loginResult{}, err
	}
	scope := strings.TrimSpace(configuration.OIDCScope)
	if scope == "" {
		scope = defaultLoginScope
	}
	if !strings.Contains(" "+scope+" ", " openid ") {
		scope = "openid " + scope
	}
	flow := request.flow
	switch flow {
	case "auto":
		flow = "loopback"
		if endpoints.DeviceEndpoint != "" {
			flow = "device"
		}
	case "device":
		if endpoints.DeviceEndpoint == "" {
			return loginResult{}, errors.New("the issuer does not advertise a device authorization endpoint; use --flow loopback")
		}
	case "loopback":
	default:
		return loginResult{}, errors.New("--flow must be auto, device, or loopback")
	}
	ctx, cancel := context.WithTimeout(ctx, request.deadline)
	defer cancel()
	login := personLogin{
		client: client, endpoints: endpoints, clientID: clientID, scope: scope,
		tokenKind: request.tokenKind, prompt: state.runtime.Stderr, sleep: state.runtime.Sleep,
	}
	var token string
	if flow == "device" {
		token, err = login.device(ctx)
	} else {
		token, err = login.loopback(ctx)
	}
	if err != nil {
		return loginResult{}, err
	}
	if err := writeTokenFile(path, token); err != nil {
		return loginResult{}, err
	}
	return loginResult{TokenFile: path, Flow: flow, TokenKind: request.tokenKind}, nil
}

type personLogin struct {
	client    *http.Client
	endpoints oidcEndpoints
	clientID  string
	scope     string
	tokenKind string
	prompt    io.Writer
	sleep     func(context.Context, time.Duration) error
}

func discoverOIDCEndpoints(ctx context.Context, client *http.Client, issuer string) (oidcEndpoints, error) {
	trimmed := strings.TrimRight(issuer, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcEndpoints{}, fmt.Errorf("create OIDC discovery request: %w", err)
	}
	contents, status, err := doOIDC(client, request)
	if err != nil {
		return oidcEndpoints{}, err
	}
	if status != http.StatusOK {
		return oidcEndpoints{}, fmt.Errorf("OIDC discovery returned %d", status)
	}
	var endpoints oidcEndpoints
	if err := json.Unmarshal(contents, &endpoints); err != nil {
		return oidcEndpoints{}, fmt.Errorf("decode OIDC discovery document: %w", err)
	}
	if strings.TrimRight(endpoints.Issuer, "/") != trimmed {
		return oidcEndpoints{}, errors.New("OIDC discovery issuer does not match --oidc-issuer")
	}
	// Every endpoint a credential or a person is sent to must be https. The
	// discovery document is the issuer's word, and it is still checked.
	if err := requireHTTPSURL(endpoints.TokenEndpoint, "OIDC token endpoint"); err != nil {
		return oidcEndpoints{}, err
	}
	if endpoints.DeviceEndpoint != "" {
		if err := requireHTTPSURL(endpoints.DeviceEndpoint, "OIDC device authorization endpoint"); err != nil {
			return oidcEndpoints{}, err
		}
	}
	return endpoints, nil
}

// postOIDCForm posts a form and decodes the JSON answer whatever the status,
// because RFC 8628 carries "keep waiting" in a 400.
func (login personLogin) postOIDCForm(ctx context.Context, endpoint string, form url.Values, into any) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("create OIDC request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	contents, status, err := doOIDC(login.client, request)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(contents, into); err != nil {
		return status, fmt.Errorf("OIDC endpoint returned %d with a body that is not JSON", status)
	}
	return status, nil
}

type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
}

// usableToken picks the requested token out of a successful response. Nothing
// from the response is ever placed in an error.
func (login personLogin) usableToken(response tokenResponse) (string, error) {
	token := response.IDToken
	if login.tokenKind == "access" {
		token = response.AccessToken
		if kind := strings.ToLower(strings.TrimSpace(response.TokenType)); kind != "" && kind != "bearer" {
			return "", errors.New("the issuer returned an access token that is not a bearer token")
		}
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") || len(token) > maximumOIDCTokenBytes {
		return "", fmt.Errorf("the issuer returned no usable %s token", login.tokenKind)
	}
	return token, nil
}

// safeOAuthError returns an OAuth error code only when it looks like one, so a
// hostile response cannot put arbitrary text on the terminal.
func safeOAuthError(code string) string {
	if code == "" || len(code) > 64 {
		return ""
	}
	for _, character := range code {
		if (character < 'a' || character > 'z') && character != '_' {
			return ""
		}
	}
	return code
}

func (login personLogin) device(ctx context.Context) (string, error) {
	var started struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
	}
	status, err := login.postOIDCForm(ctx, login.endpoints.DeviceEndpoint,
		url.Values{"client_id": {login.clientID}, "scope": {login.scope}}, &started)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || started.DeviceCode == "" || started.UserCode == "" {
		if code := safeOAuthError(started.Error); code != "" {
			return "", fmt.Errorf("the issuer refused to start a device sign-in: %s", code)
		}
		return "", fmt.Errorf("the issuer refused to start a device sign-in (%d)", status)
	}
	// The address the person is sent to is shown only if it is https, and the
	// user code only if it is the short printable code the grant describes.
	if err := requireHTTPSURL(started.VerificationURI, "device verification address"); err != nil {
		return "", err
	}
	if len(started.UserCode) > 32 || strings.ContainsFunc(started.UserCode, func(character rune) bool {
		return character < 0x21 || character > 0x7e
	}) {
		return "", errors.New("the issuer returned a user code that is not printable")
	}
	fmt.Fprintf(login.prompt, "To sign in, open %s and enter the code %s\n", started.VerificationURI, started.UserCode)
	fmt.Fprintln(login.prompt, "Waiting for the sign-in to be approved...")

	wait := time.Duration(started.Interval) * time.Second
	if wait <= 0 {
		wait = defaultDeviceWait
	}
	deadline := time.Now().Add(time.Duration(started.ExpiresIn) * time.Second)
	for {
		if wait > maximumDeviceWait {
			wait = maximumDeviceWait
		}
		if err := login.pause(ctx, wait); err != nil {
			return "", errors.New("the sign-in was not approved before the login timeout")
		}
		if started.ExpiresIn > 0 && time.Now().After(deadline) {
			return "", errors.New("the device code expired before the sign-in was approved")
		}
		var response tokenResponse
		status, err := login.postOIDCForm(ctx, login.endpoints.TokenEndpoint, url.Values{
			"grant_type": {deviceGrantType}, "device_code": {started.DeviceCode}, "client_id": {login.clientID},
		}, &response)
		if err != nil {
			return "", err
		}
		if status == http.StatusOK && response.Error == "" {
			return login.usableToken(response)
		}
		switch response.Error {
		case "authorization_pending":
		case "slow_down":
			// RFC 8628 section 3.5: the interval grows by five seconds and stays grown.
			wait += slowDownIncrement
		case "access_denied":
			return "", errors.New("the sign-in was denied")
		case "expired_token":
			return "", errors.New("the device code expired before the sign-in was approved")
		default:
			if code := safeOAuthError(response.Error); code != "" {
				return "", fmt.Errorf("the issuer refused the device sign-in: %s", code)
			}
			return "", fmt.Errorf("the issuer refused the device sign-in (%d)", status)
		}
	}
}

func (login personLogin) pause(ctx context.Context, wait time.Duration) error {
	if login.sleep != nil {
		return login.sleep(ctx, wait)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func randomURLToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

type loopbackAnswer struct {
	code string
	err  error
}

func (login personLogin) loopback(ctx context.Context) (string, error) {
	if err := requireHTTPSURL(login.endpoints.AuthorizationEndpoint, "OIDC authorization endpoint"); err != nil {
		return "", err
	}
	state, stateErr := randomURLToken()
	nonce, nonceErr := randomURLToken()
	verifier, verifierErr := randomURLToken()
	if err := errors.Join(stateErr, nonceErr, verifierErr); err != nil {
		return "", errors.New("could not draw randomness for the sign-in")
	}
	// 127.0.0.1 and not "localhost": the name can resolve elsewhere, and an
	// ephemeral port so that no fixed port has to be free.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen for the sign-in redirect: %w", err)
	}
	redirectURI := "http://" + listener.Addr().String() + "/callback"
	answers := make(chan loopbackAnswer, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		// A request that does not carry this sign-in's state is not its answer.
		// It is refused and the wait goes on, so that another local process
		// cannot end the sign-in by guessing the port.
		if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 {
			http.Error(writer, "This is not the sign-in this command is waiting for.", http.StatusBadRequest)
			return
		}
		answer := loopbackAnswer{code: query.Get("code")}
		if query.Has("error") || answer.code == "" {
			answer = loopbackAnswer{err: errors.New("the issuer reported that the sign-in did not complete")}
			if code := safeOAuthError(query.Get("error")); code != "" {
				answer.err = fmt.Errorf("the issuer reported that the sign-in did not complete: %s", code)
			}
		}
		select {
		case answers <- answer:
			writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(writer, "Sign-in received. You can close this window and return to the terminal.\n")
		default:
			http.Error(writer, "This sign-in was already answered.", http.StatusConflict)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		// The callback publishes its answer before net/http finishes its reply.
		// On an issuer error the caller returns immediately, so Close could
		// tear down that active response and show an EOF in the browser.
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()

	destination, err := url.Parse(login.endpoints.AuthorizationEndpoint)
	if err != nil {
		return "", errors.New("the OIDC authorization endpoint is not a URL")
	}
	sum := sha256.Sum256([]byte(verifier))
	query := destination.Query()
	query.Set("response_type", "code")
	query.Set("client_id", login.clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", login.scope)
	query.Set("state", state)
	query.Set("nonce", nonce)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	query.Set("code_challenge_method", "S256")
	destination.RawQuery = query.Encode()
	fmt.Fprintf(login.prompt, "To sign in, open this address in a browser on this machine:\n\n  %s\n\n", destination.String())
	fmt.Fprintln(login.prompt, "Waiting for the sign-in to finish...")

	var answer loopbackAnswer
	select {
	case <-ctx.Done():
		return "", errors.New("the sign-in did not finish before the login timeout")
	case answer = <-answers:
	}
	if answer.err != nil {
		return "", answer.err
	}
	var response tokenResponse
	status, err := login.postOIDCForm(ctx, login.endpoints.TokenEndpoint, url.Values{
		"grant_type": {"authorization_code"}, "code": {answer.code}, "client_id": {login.clientID},
		"redirect_uri": {redirectURI}, "code_verifier": {verifier},
	}, &response)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || response.Error != "" {
		if code := safeOAuthError(response.Error); code != "" {
			return "", fmt.Errorf("the issuer refused the authorization code: %s", code)
		}
		return "", fmt.Errorf("the issuer refused the authorization code (%d)", status)
	}
	// The ID token must answer this request. It arrived over TLS from the token
	// endpoint, so its signature is the Hub's to check; the nonce is ours.
	if !idTokenCarriesNonce(response.IDToken, nonce) {
		return "", errors.New("the issuer returned an ID token that does not answer this sign-in")
	}
	return login.usableToken(response)
}

func idTokenCarriesNonce(token, nonce string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Nonce string `json:"nonce"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return false
	}
	return nonce != "" && subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) == 1
}

// checkTokenDestination refuses a destination login must not write through: a
// symbolic link, a directory, a device. A file that does not exist yet is fine.
func checkTokenDestination(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect bearer token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("bearer token file must be a regular file; login does not write through a link or a directory")
	}
	return nil
}

// writeTokenFile replaces the token file in one rename, so there is no moment
// at which it holds half a token or is readable by anybody else: the temporary
// file is created 0600 and only then written.
func writeTokenFile(path, token string) error {
	if err := checkTokenDestination(path); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create bearer token directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".token-*")
	if err != nil {
		return fmt.Errorf("create bearer token transaction: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect bearer token transaction: %w", err)
	}
	if _, err := temporary.WriteString(token + "\n"); err != nil {
		_ = temporary.Close()
		return errors.New("write bearer token transaction failed")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync bearer token transaction: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close bearer token transaction: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("commit bearer token transaction: %w", err)
	}
	return nil
}

// removeTokenFile deletes the token file and reports whether there was one.
func removeTokenFile(path string) (bool, error) {
	if err := checkTokenDestination(path); err != nil {
		return false, err
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remove bearer token file: %w", err)
	}
	return true, nil
}
