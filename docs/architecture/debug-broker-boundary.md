# Debug broker identity boundary

The next phase of #303 adds a separate outbound debug broker under ADR 0015.
The existing delivery Agent keeps its Plan/Helm identity and responsibility.
The native identity parser in `internal/identity/domain/debug_broker.go` accepts
exactly one URI SAN:

`spiffe://platform.delivery/environment/ENV/debug-broker/BROKER`

Environment and broker are exact DNS labels of at most 63 characters. Empty,
encoded, ambiguous, trailing-path, user-info, query and fragment variants fail.
A second URI SAN also fails, including another role or a duplicate identity.
Common names, headers and operator/delivery-Agent aliases confer no broker role.
A dedicated broker certificate is rejected by the existing operator and delivery
Agent parsers; those credentials are rejected by the broker parser.

This parser does not verify a certificate chain. A future transport must first
verify trust and possession, and an enrollment/authority adapter must bind the
identity to current operator-owned configuration. Identity alone grants no
namespace, image, credential issuance or execution authority. There is no
production broker transport, process, Pod action or enrollment API in this
primitive. Existing credentials are not repurposed and no new key is created.

The URI is a registered Conduit-native choice. Public terminal documentation
establishes no private broker credential protocol. Tests cover role separation,
ambiguous SANs, wrong trust domain, noncanonical paths and label bounds. Runtime
wiring must test the verified transport and enrollment boundary separately;
passing a constructed x509 object to this parser is not that evidence.
