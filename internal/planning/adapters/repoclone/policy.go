package repoclone

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTimeout bounds one clone from the first git process to the last.
	DefaultTimeout = 2 * time.Minute
	// DefaultMaxBytes bounds the checkout directory, objects and files together.
	DefaultMaxBytes int64 = 512 << 20
	// DefaultCacheEntries is how many distinct commits the cache keeps.
	DefaultCacheEntries = 8
)

var (
	// ErrHostRefused means the repository host is not on the operator's allowlist.
	ErrHostRefused = errors.New("source host is not allowed by the source-host policy")
	// ErrAddressRefused means the host is, or resolved to, an address the
	// policy does not permit for it.
	ErrAddressRefused = errors.New("source address is not allowed by the source-host policy")
	// ErrTooLarge means the checkout passed the size limit and was stopped.
	ErrTooLarge = errors.New("source checkout exceeds the size limit")
	// ErrTimeLimit means the clone did not finish inside the time limit.
	ErrTimeLimit = errors.New("source checkout exceeded the time limit")
)

// Environment variables PolicyFromEnv reads.
const (
	EnvAllowedHosts = "CONDUIT_SOURCE_ALLOWED_HOSTS"
	EnvConnections  = "CONDUIT_SOURCE_CONNECTIONS"
	EnvTimeout      = "CONDUIT_SOURCE_CLONE_TIMEOUT"
	EnvMaxBytes     = "CONDUIT_SOURCE_CLONE_MAX_BYTES"
	EnvCacheEntries = "CONDUIT_SOURCE_CACHE_ENTRIES"
)

// Connection is an operator-managed credential for one source host. It holds
// paths to mounted files and never a secret value.
type Connection struct {
	// ID names the connection in audit records and logs.
	ID string `json:"id"`
	// Host is one exact host, with an optional port. A pattern is not accepted.
	Host string `json:"host"`
	// TokenFile is a file holding the token. It is read at each clone, so a
	// rotated Secret takes effect without a restart.
	TokenFile string `json:"tokenFile,omitempty"`
	// CAFile is a PEM bundle git trusts for this host only.
	CAFile string `json:"caFile,omitempty"`
	// Scheme is "bearer" (the default) or "basic".
	Scheme string `json:"scheme,omitempty"`
	// Username is the basic-auth user. It defaults to "x-access-token".
	Username string `json:"username,omitempty"`
	// AllowHTTP lets the token travel over plain http. Without it a clone of
	// an http URL on this host is refused and the token is not sent.
	AllowHTTP bool `json:"allowHTTP,omitempty"`
}

// Event describes one finished clone for an audit log. It names the
// connection and never carries the credential.
type Event struct {
	Host         string
	Commit       string
	ConnectionID string
	Cached       bool
	Error        string
}

// Policy is the operator's source-host policy and limits.
type Policy struct {
	// AllowedHosts lists hosts a clone may name. An entry is an exact host
	// ("git.example.com"), an exact host and port ("10.0.0.5:3000"), or a
	// suffix (".example.com", also written "*.example.com"). Empty means any
	// host with a public address. Only an exact entry, or a Connection, lets a
	// host be reached on a loopback or private address.
	AllowedHosts []string
	Connections  []Connection
	// Timeout bounds one clone. Zero means DefaultTimeout.
	Timeout time.Duration
	// MaxBytes bounds the checkout directory. Zero or less means no limit.
	MaxBytes int64
	// CacheEntries is how many commits to keep for reuse. Zero disables reuse.
	CacheEntries int
	// OnCheckout is called once per clone, after it succeeds or fails.
	OnCheckout func(Event)
}

// Cloner clones under one Policy and reuses checkouts of the same commit.
type Cloner struct {
	policy      Policy
	hosts       []hostEntry
	connections []boundConnection
	cache       *cache
	// loopback permits loopback addresses for any name. Only the package
	// level Clone sets it.
	loopback bool
	observe  func(argv, env []string)
}

type hostEntry struct {
	name   string
	port   string
	suffix bool
}

type boundConnection struct {
	Connection
	entry hostEntry
}

var connectionID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// New validates policy and returns a Cloner. It does not read a token.
func New(policy Policy) (*Cloner, error) {
	cloner := &Cloner{policy: policy}
	for _, raw := range policy.AllowedHosts {
		entry, err := parseHostEntry(raw)
		if err != nil {
			return nil, err
		}
		cloner.hosts = append(cloner.hosts, entry)
	}
	seen := map[string]bool{}
	for _, connection := range policy.Connections {
		if !connectionID.MatchString(connection.ID) {
			return nil, fmt.Errorf("source connection id %q must be lowercase letters, digits, dot, dash or underscore", connection.ID)
		}
		if seen[connection.ID] {
			return nil, fmt.Errorf("source connection id %q is listed twice", connection.ID)
		}
		seen[connection.ID] = true
		entry, err := parseHostEntry(connection.Host)
		if err != nil {
			return nil, fmt.Errorf("source connection %s: %w", connection.ID, err)
		}
		if entry.suffix {
			return nil, fmt.Errorf("source connection %s: host must be one exact host, not a pattern", connection.ID)
		}
		for _, other := range cloner.connections {
			if other.entry == entry {
				return nil, fmt.Errorf("source connections %s and %s name the same host", other.ID, connection.ID)
			}
		}
		switch connection.Scheme {
		case "", "bearer", "basic":
		default:
			return nil, fmt.Errorf("source connection %s: scheme must be bearer or basic", connection.ID)
		}
		if connection.TokenFile == "" && connection.CAFile == "" {
			return nil, fmt.Errorf("source connection %s: tokenFile or caFile is required", connection.ID)
		}
		if strings.ContainsAny(connection.Username, ":\r\n") {
			return nil, fmt.Errorf("source connection %s: username is not valid", connection.ID)
		}
		cloner.connections = append(cloner.connections, boundConnection{Connection: connection, entry: entry})
	}
	if policy.CacheEntries > 0 {
		cloner.cache = newCache(policy.CacheEntries)
	}
	return cloner, nil
}

// Close removes every cached checkout.
func (cloner *Cloner) Close() {
	if cloner != nil && cloner.cache != nil {
		cloner.cache.close()
	}
}

// PolicyFromEnv reads the source-host policy through getenv. Unset variables
// give the defaults: any public host, no connection, DefaultTimeout,
// DefaultMaxBytes and DefaultCacheEntries.
func PolicyFromEnv(getenv func(string) string) (Policy, error) {
	policy := Policy{Timeout: DefaultTimeout, MaxBytes: DefaultMaxBytes, CacheEntries: DefaultCacheEntries}
	if getenv == nil {
		return policy, nil
	}
	for _, item := range strings.Split(getenv(EnvAllowedHosts), ",") {
		if item = strings.TrimSpace(item); item != "" {
			policy.AllowedHosts = append(policy.AllowedHosts, item)
		}
	}
	if raw := strings.TrimSpace(getenv(EnvConnections)); raw != "" {
		decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
		// An unknown field is refused so that an inline "token" is an error
		// and not a secret that sits in the environment unused.
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&policy.Connections); err != nil {
			return Policy{}, fmt.Errorf("%s must be a JSON array of {id, host, tokenFile, caFile, scheme, username, allowHTTP}; secrets are given as file paths only", EnvConnections)
		}
	}
	if raw := strings.TrimSpace(getenv(EnvTimeout)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return Policy{}, fmt.Errorf("%s must be a positive duration", EnvTimeout)
		}
		policy.Timeout = parsed
	}
	if raw := strings.TrimSpace(getenv(EnvMaxBytes)); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			return Policy{}, fmt.Errorf("%s must be a positive number of bytes", EnvMaxBytes)
		}
		policy.MaxBytes = parsed
	}
	if raw := strings.TrimSpace(getenv(EnvCacheEntries)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 256 {
			return Policy{}, fmt.Errorf("%s must be a number from 0 to 256", EnvCacheEntries)
		}
		policy.CacheEntries = parsed
	}
	for _, connection := range policy.Connections {
		for _, path := range []string{connection.TokenFile, connection.CAFile} {
			if path == "" {
				continue
			}
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				return Policy{}, fmt.Errorf("source connection %s: file %s is not readable", connection.ID, path)
			}
		}
	}
	return policy, nil
}

func parseHostEntry(raw string) (hostEntry, error) {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" || strings.ContainsAny(text, "/@ \t") {
		return hostEntry{}, fmt.Errorf("source host %q must be a host name, with an optional port, and no scheme or path", raw)
	}
	entry := hostEntry{}
	if strings.HasPrefix(text, "*.") {
		text = text[1:]
	}
	if strings.HasPrefix(text, ".") {
		entry.suffix = true
	}
	if name, port, err := net.SplitHostPort(text); err == nil {
		if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
			return hostEntry{}, fmt.Errorf("source host %q has a port that is not valid", raw)
		}
		text, entry.port = name, port
	}
	entry.name = strings.Trim(text, "[]")
	if strings.Trim(entry.name, ".") == "" || strings.Contains(entry.name, "*") {
		return hostEntry{}, fmt.Errorf("source host %q is not a host name or a suffix", raw)
	}
	return entry, nil
}

func (entry hostEntry) matches(name, port string) bool {
	if entry.port != "" && entry.port != port {
		return false
	}
	if entry.suffix {
		return strings.HasSuffix(name, entry.name)
	}
	return name == entry.name
}

// splitTarget returns the lowercase host name and the port of a URL host or
// a dial address. An absent port is reported as "".
func splitTarget(host string) (string, string) {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name, port = host, ""
	}
	return strings.ToLower(strings.Trim(name, "[]")), port
}

func urlTarget(repo *url.URL) (string, string) {
	name, port := splitTarget(repo.Host)
	if port == "" {
		port = "80"
		if repo.Scheme == "https" {
			port = "443"
		}
	}
	return name, port
}

// connectionFor is the connection whose host is exactly this one.
func (cloner *Cloner) connectionFor(name, port string) (boundConnection, bool) {
	for _, connection := range cloner.connections {
		if connection.entry.matches(name, port) {
			return connection, true
		}
	}
	return boundConnection{}, false
}

// exactly reports whether the operator named this host exactly, in the
// allowlist or as a connection. Only then may it sit on a private address.
func (cloner *Cloner) exactly(name, port string) bool {
	for _, entry := range cloner.hosts {
		if !entry.suffix && entry.matches(name, port) {
			return true
		}
	}
	_, ok := cloner.connectionFor(name, port)
	return ok
}

func (cloner *Cloner) named(address string) bool {
	name, port := splitTarget(address)
	return cloner.exactly(name, port)
}

// checkName applies the allowlist to the URL host and, when the host is an
// address already, the address rule.
func (cloner *Cloner) checkName(repo *url.URL) error {
	name, port := urlTarget(repo)
	exact := cloner.exactly(name, port)
	if len(cloner.hosts) > 0 && !exact {
		listed := false
		for _, entry := range cloner.hosts {
			if entry.matches(name, port) {
				listed = true
				break
			}
		}
		if !listed {
			return fmt.Errorf("clone %s: %w", repo.Host, ErrHostRefused)
		}
	}
	if address, err := netip.ParseAddr(name); err == nil {
		if err := cloner.checkAddress(address, exact); err != nil {
			return fmt.Errorf("clone %s: %w", repo.Host, err)
		}
	}
	return nil
}

func (cloner *Cloner) checkResolved(resolved string, exact bool) error {
	text, _, err := net.SplitHostPort(resolved)
	if err != nil {
		return ErrAddressRefused
	}
	address, err := netip.ParseAddr(text)
	if err != nil {
		return ErrAddressRefused
	}
	return cloner.checkAddress(address, exact)
}

// metadata lists instance-metadata addresses that are not link-local.
var metadata = []netip.Addr{
	netip.MustParseAddr("fd00:ec2::254"),
	netip.MustParseAddr("100.100.100.200"),
	netip.MustParseAddr("192.0.0.192"),
}

var carrierNAT = netip.MustParsePrefix("100.64.0.0/10")

// checkAddress is the address rule. Link-local, metadata, unspecified and
// multicast addresses are refused for every host. Loopback and private
// addresses are refused unless the operator named the host exactly.
func (cloner *Cloner) checkAddress(address netip.Addr, exact bool) error {
	address = address.Unmap().WithZone("")
	if !address.IsValid() || address.IsUnspecified() || address.IsMulticast() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsInterfaceLocalMulticast() {
		return ErrAddressRefused
	}
	for _, item := range metadata {
		if address == item {
			return ErrAddressRefused
		}
	}
	if address.IsLoopback() {
		if exact || cloner.loopback {
			return nil
		}
		return ErrAddressRefused
	}
	if address.IsPrivate() || carrierNAT.Contains(address) {
		if exact {
			return nil
		}
		return ErrAddressRefused
	}
	return nil
}

// credential is what one clone offers one host.
type credential struct {
	id     string
	origin string
	header string
	caFile string
	// secrets are the strings that must not reach an error or a log.
	secrets []string
}

// credentialFor loads the connection for the URL host. A host with no
// connection gets the zero credential and an anonymous fetch.
func (cloner *Cloner) credentialFor(repo *url.URL) (credential, error) {
	name, port := urlTarget(repo)
	connection, ok := cloner.connectionFor(name, port)
	if !ok {
		return credential{}, nil
	}
	out := credential{id: connection.ID, origin: repo.Scheme + "://" + repo.Host + "/", caFile: connection.CAFile}
	if connection.TokenFile == "" {
		return out, nil
	}
	if repo.Scheme != "https" && !connection.AllowHTTP {
		return credential{}, fmt.Errorf("source connection %s sends its token over https only; the repository URL is %s", connection.ID, repo.Scheme)
	}
	raw, err := os.ReadFile(connection.TokenFile)
	if err != nil {
		return credential{}, fmt.Errorf("source connection %s: token file is not readable", connection.ID)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return credential{}, fmt.Errorf("source connection %s: token file must hold one line", connection.ID)
	}
	out.secrets = []string{token}
	if connection.Scheme == "basic" {
		user := connection.Username
		if user == "" {
			user = "x-access-token"
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
		out.secrets = append(out.secrets, encoded)
		out.header = "Authorization: Basic " + encoded
		return out, nil
	}
	out.header = "Authorization: Bearer " + token
	return out, nil
}

// config is the git configuration that carries the credential. Every key is
// scoped to the connection's origin, so git applies it to no other URL.
func (item credential) config() [][2]string {
	var pairs [][2]string
	if item.header != "" {
		pairs = append(pairs, [2]string{"http." + item.origin + ".extraHeader", item.header})
	}
	if item.caFile != "" {
		pairs = append(pairs, [2]string{"http." + item.origin + ".sslCAInfo", item.caFile})
	}
	return pairs
}

func (item credential) redact(text string) string {
	for _, secret := range item.secrets {
		text = strings.ReplaceAll(text, secret, "[redacted]")
	}
	return text
}
