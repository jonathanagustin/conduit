package apollo

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// The three error envelopes the pinned snapshot publishes, and the only three
// Conduit is entitled to emit.
//
// The snapshot declares ApiUsageDenied, ReleaseNotFound and NotEnvironmentOwner
// as Conjure envelopes -- {errorCode, errorName, errorInstanceId, parameters} --
// and openapi.gen.go generates Go types for all three. Nothing constructed
// them: every Apollo error path returned {"error": "..."}, so a client
// generated from the snapshot had typed handling for three cases and could
// deserialize none of them.
//
// This lives here rather than beside the transport because it is Apollo
// vocabulary, which is what this context owns. What Conduit does about a
// refusal is the control plane's business; what the wire shape of that refusal
// is, is the contract's.
//
// What is grounded and what is not, stated rather than blurred:
//
//   - errorCode is pinned. Each schema declares a single-value enum --
//     PERMISSION_DENIED, NOT_FOUND -- so these are read off the contract.
//   - parameters are pinned. ReleaseNotFound requires mavenCoordinate and
//     NotEnvironmentOwner requires environmentId; both are `required` in the
//     snapshot, so an envelope without them is invalid against the schema
//     Conduit claims to serve.
//   - errorName is NOT pinned. The snapshot types it as an unconstrained string
//     and no capture recovered a value, so Apollo's own spelling is unknown --
//     Conjure convention would be "Namespace:Name" and the namespace is exactly
//     the part no evidence establishes. The schema name is used because it is
//     the one value the pinned contract actually contains. That is a DOCUMENTED
//     choice, not an OBSERVED one, and it is the first field to correct if a
//     capture ever recovers a real Apollo error.
const (
	ErrorCodePermissionDenied = "PERMISSION_DENIED"
	ErrorCodeNotFound         = "NOT_FOUND"

	ErrorNameAPIUsageDenied      = "ApiUsageDenied"
	ErrorNameReleaseNotFound     = "ReleaseNotFound"
	ErrorNameNotEnvironmentOwner = "NotEnvironmentOwner"
)

// ErrorInstanceID mints the identifier that makes one error traceable across a
// support boundary, which is most of why a Conjure envelope carries one.
//
// A random UUIDv4 rather than a request id, because this transport has no
// request id to reuse -- there is no correlation middleware on the mux. It is
// formatted by hand rather than by taking a dependency: google/uuid is present
// only as an indirect requirement, and promoting it to a direct one to format
// sixteen bytes would be a supply-chain cost for no capability.
//
// A failed read from crypto/rand yields the nil UUID rather than an error. The
// caller is already writing an error response, and failing to produce one
// because the identifier could not be randomised would turn a 404 into a
// dropped connection; an all-zero id is visibly not a real one.
func ErrorInstanceID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	value[6] = (value[6] & 0x0f) | 0x40 // version 4
	value[8] = (value[8] & 0x3f) | 0x80 // RFC 4122 variant
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

// ErrorEnvelope builds one published envelope.
//
// parameters is written even when empty, because ApiUsageDenied declares the
// key as an object and a client generated from the snapshot may read it
// unconditionally.
//
// The human-readable "error" key is not part of the published schema and is
// carried anyway: none of the three schemas sets additionalProperties: false,
// so an extra key is contract-legal, the contract has nowhere else to put a
// message, and dropping it would break every existing reader for no gain to a
// typed client.
func ErrorEnvelope(code, name, message string, parameters map[string]string) map[string]any {
	rendered := map[string]any{}
	for key, value := range parameters {
		rendered[key] = value
	}
	return map[string]any{
		"errorCode":       code,
		"errorName":       name,
		"errorInstanceId": ErrorInstanceID(),
		"parameters":      rendered,
		"error":           message,
	}
}

// HelmChartEntityName recovers the Entity name from an Apollo Entity id.
//
// The id is Conduit's own construction -- "aeid:<environment>:helm-chart:<entity>"
// -- and its builder refuses any environment or entity name containing a colon,
// so the last segment is unambiguously the Entity. The inverse lives here, in
// the package that owns Apollo vocabulary, because the alternative is a caller
// re-deriving the shape from the string it happens to be looking at, and then
// the two drift the first time the shape changes.
//
// Returns "" for anything that is not an id of that shape, so a caller gets a
// value that matches nothing rather than a plausible-looking wrong one.
func HelmChartEntityName(entityID string) string {
	const prefix = "aeid:"
	const marker = ":helm-chart:"
	if !strings.HasPrefix(entityID, prefix) {
		return ""
	}
	marked := strings.Index(entityID, marker)
	if marked < 0 {
		return ""
	}
	return entityID[marked+len(marker):]
}

// ParseHelmChartEntityID translates the bounded Conduit AEID shape at the
// integration boundary. It does not infer private entity kinds or syntax.
func ParseHelmChartEntityID(id string) (environment, entity string, ok bool) {
	parts := strings.Split(id, ":")
	if len(parts) != 4 || parts[0] != "aeid" || parts[1] == "" || parts[2] != "helm-chart" || parts[3] == "" {
		return "", "", false
	}
	return parts[1], parts[3], true
}
