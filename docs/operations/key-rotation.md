# Key rotation: the Agent CA and the Bundle signing key

Issue #847. Two procedures, each with the Go test that rehearses it. Both
rotate by **overlap**: trust the successor alongside the predecessor, move
everything over, then remove the predecessor.

"Rehearsed" here means a Go test drives the real code through each state of
the procedure. Neither procedure was run against a deployed Hub; the list of
what was not exercised is at the end.

## Agent CA

The Hub trusts every root in the PEM file `AGENT_CLIENT_CA_FILE` names
(`AgentServerTLSConfig`, `internal/controlplane/server_tls.go`). The file can
hold more than one certificate. That is the whole mechanism: a bundle with two
roots admits Agents from either.

**The Hub reads the file once, at start.** Every change to the bundle below
takes effect only when the Hub restarts. With the Hub chart the bundle is the
ConfigMap `tls.externalCAConfigMapName`, key `tls.externalCAKey`; editing the
ConfigMap does not restart the Pods.

### Procedure

1. **Create the new CA.** Keep its key wherever the old one's was kept.
2. **Overlap.** Write a bundle holding the old root and the new root. Roll
   the Hub. Both old and new Agent certificates are now admitted.
3. **Move the Agents.** Issue each Agent a certificate from the new CA and
   restart it. The Agent reads its certificate once, at start
   (see [agent-enrolment.md](agent-enrolment.md), "Renewal needs a restart").
   With cert-manager this is: point the `Certificate`'s `issuerRef` at an
   `Issuer` over the new CA, wait for the Secret to change, restart the Agent.
4. **Check nobody is left.** `conduit agent-credential list <environment>`
   shows the serial each Agent last presented and when. An Agent still
   presenting an old-CA serial has not moved. The Hub does not record which
   root signed a certificate, so compare serials against what the new CA
   issued.
5. **Retire.** Write a bundle holding only the new root. Roll the Hub. Every
   certificate from the old CA is refused at the TLS handshake from then on,
   whatever its own expiry says.

If the old CA's key is compromised, do not wait for step 3 to finish. Revoke
the Environment first (`conduit agent-credential revoke <environment>
--all-agents --reason …`), which refuses every certificate issued up to now,
from either root. Certificates issued from the new CA afterwards are admitted.
An attacker holding the old CA key can also issue certificates afterwards, so
finish step 5 quickly: only removing the root ends that.

### Rehearsal

`TestAgentCARotationWithOverlapAdmitsBothRootsAndRetiringTheOldRootRefusesItsCertificates`
in `internal/controlplane/agent_ca_rotation_test.go`. It starts a Hub with
`AgentServerTLSConfig` over a CA file, as `cmd/platform-delivery` does, and
makes real TLS connections with client certificates carrying the Agent URI SAN:

| Bundle | Old-root Agent | New-root Agent |
| --- | --- | --- |
| old only | admitted | refused at the handshake |
| old + new | admitted | admitted |
| new only | refused at the handshake | admitted |

Each row starts a new server, because that is how the Hub picks up a bundle.

```sh
go test -count=1 -run TestAgentCARotation ./internal/controlplane
```

## Bundle signing key

A Hub signs every Bundle it builds with the Ed25519 key in
`BUNDLE_SIGNING_KEY_FILE`. A Hub that imports a Bundle verifies the signature
against:

- its own signing key, always; and
- every key in the file `BUNDLE_TRUSTED_KEYS_FILE` names, if set.

So rotation only involves a trust file when the signer and the verifier are
**different Hubs**. A Hub with no trust file trusts only itself.

### The trust file

A JSON object. Each name is a key identifier; each value is that Ed25519
public key, base64-encoded (padded or not).

```json
{
  "key_90ba5bdd82d606a4": "3dL0ayPPrj73ltpfcxhdkdn4ajePpZ4P0daly+a/Hk8="
}
```

The identifier is derived from the key: `key_` followed by the first 16
hexadecimal digits of the SHA-256 of the 32 raw public-key bytes
(`SigningKeyID`, `internal/artifacts/domain/envelope.go`). An entry filed
under any other name stops the Hub starting, with
`invalid trusted Bundle signing key`. The file is read once, at start.

To get both values from a private key:

```sh
openssl pkey -in bundle-signing-key.pem -pubout -outform DER | tail -c 32 > public.raw
echo "value: $(base64 -w0 public.raw)"
echo "name:  key_$(sha256sum public.raw | cut -c1-16)"
```

The value is also on the signing Hub's startup log line as `bundleSigner`, and
the name is on every Bundle it signs as `signingKeyId`.

### Procedure

"Source" is the Hub that signs. "Consumer" is each Hub that imports its
Bundles.

1. **Generate the successor** key (`openssl genpkey -algorithm Ed25519`).
   Do not mount it on the source yet.
2. **Overlap.** On every consumer, add the successor's public key to the trust
   file next to the predecessor's. Restart each consumer. Both signers verify.
3. **Switch the source.** Replace the Secret behind `BUNDLE_SIGNING_KEY_FILE`
   (the Hub chart's `bundleSigningSecretName`) with the successor and restart
   the source. Check `bundleSigner` on its startup line is the new public key.
4. **Drain.** Bundles the predecessor signed that are still in transit verify
   only while the predecessor is in the trust file. Import them, or decide
   they will not be.
5. **Retire.** Remove the predecessor from every consumer's trust file and
   restart. Anything the predecessor signed is refused from then on, including
   a Bundle built before the switch and imported late.

Order matters at step 2 and 3. A source that signs with a key no consumer
trusts yet produces Bundles every consumer refuses.

The source itself needs a trust file only if it must still verify Bundles its
own predecessor key signed: after step 3 it trusts its new key and no longer
its old one.

### The Hub chart does not set the trust file

`deploy/helm/platform-hub` renders `BUNDLE_SIGNING_KEY_FILE` and has no value
for `BUNDLE_TRUSTED_KEYS_FILE`; the chart has no free-form environment. A
consumer installed from the chart therefore trusts only its own key, and this
procedure cannot be carried out on it without a deployment that sets the
variable and mounts the file some other way. That is a gap in the chart, not
something this page works around.

### Rehearsal

`TestBundleSigningKeyRotationWithOverlapAndRetirement` in
`internal/controlplane/bundle_signing_rotation_test.go`. It builds and signs a
real Bundle with each of two keys, renders the trust file in its own format,
reads it with the parser the Hub uses (`ParseTrustedKeys`), and checks a
consumer in each state:

| Consumer's trust file | Bundle from old key | Bundle from new key |
| --- | --- | --- |
| old | verifies | refused |
| old + new | verifies | verifies |
| new | refused | verifies |
| none | refused | refused |

`TestATrustListEntryFiledUnderTheWrongIdentifierIsRefusedAtStart` covers the
mislabelled entry.

```sh
go test -count=1 -run 'TestBundleSigningKeyRotation|TestATrustListEntry' ./internal/controlplane
```

## Not exercised

- Either procedure against a deployed Hub, including the restarts.
- Editing the CA ConfigMap and rolling the Hub chart's Deployment.
- A consumer Hub started with `BUNDLE_TRUSTED_KEYS_FILE` from a mounted file.
  The parser and the verification are tested; the mount is not, and the chart
  cannot produce it.
- cert-manager reissuing Agent certificates from a second `Issuer`.
- The Hub's SPIFFE Workload API mode, which refreshes its trust bundle on each
  handshake and does not use `AGENT_CLIENT_CA_FILE`.
