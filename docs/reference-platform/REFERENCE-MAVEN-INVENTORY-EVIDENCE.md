# Bounded Apollo Maven inventory evidence

Reviewed 2026-08-02. This note records observations from an authorized
dependency-inventory capture; it is separate from the public-documentation
coverage ledger and is not a compatibility fixture.

## Source

- Repository path:
  `jonathanagustin/factory@develop/packages/foundry/system/evidence/foundry-maven-dependency-graph-2026-07-17.json`
- Git blob: `a447482327e1c78b23fef913cd1faa2f7c952152`
- File SHA-256:
  `c0b26c84bc64d406e1828f56880c6079f2014a9af556fb0d8bc45313f002840f`
- Capture metadata: 2026-07-17, 756 coordinates, 70 fetched POMs, breadth-first
  discovery from four pinned Foundry API seeds.

No source body, bearer token, POM, or artifact is committed here.

The copy supplied inside the generated `@factory/foundry-system` package has
the same SHA-256. It is not a second capture and therefore does not increase the
evidence count. The package's generated interface inventory is reviewed
separately in [`ROLE-INTERFACE-INVENTORY.md`](ROLE-INTERFACE-INVENTORY.md).

## Bounded observations

The flattened coordinate inventory contains independently versioned Apollo
families for commons/artifact registry/entity/Maven (`2.911.0`), auth broker and
scoped tokens (`0.197.0`), bundling (`1.197.0`), catalog (`3.2119.0`),
deployment state (`4.3632.0`), and label primitives (`0.287.0`). It also contains
Rubix network, credential, compute-attribution, and core library names.

An authorized recheck used the documented Artifacts flow: grant `MAVEN/READ`,
mint a repository-scoped token, and use the repository RID as the Basic-auth
username against `contents/release/maven`. The four graph seed POMs downloaded
successfully through Eddie, proving the credential and resolver path:

| Seed POM | SHA-256 |
| --- | --- |
| `object-set-service-api-dialogue:1.2549.2` | `61d06e7be9c6db6a9a162fb6729dc5c330b0ada211c2c3c93f43ffe0ac84483a` |
| `pg-api-dialogue:105.501.1` | `ad7eb772214a448e4e83ef84e651919ae0caed47eb14515fd4b6a8e2b3725d13` |
| `phonograph2-api-objects:1.995.0` | `d8c4ed2b68f3cfa79f8c86880aeb7e1f599b3720f5236676433328dbe9c80267` |
| `scheduler-api-objects:0.1597.0` | `04e9f0731ef38a722ca95192a0bb7a92592f8b82149ce43ea0bcbc637c93d035` |

The dependency trace found 37 POMs containing Apollo coordinates. Every Apollo
occurrence was under Maven `dependencyManagement`; zero was a direct
dependency. The captured versions come from the `com.palantir.artifacts`
`0.1777.0` build's version matrix. A resolvable `0.1801.0` matrix had already
advanced the same families to auth/scoped-token `0.201.0`, bundling `1.200.0`,
catalog `3.2157.0`, deployment state `4.3691.0`, labels `0.290.0`, commons
`2.917.0`, and Maven API `2.918.0`. This is evidence of BOM constraints, not
that those modules link or invoke every listed API.

Neither version set was downloadable. Fifty-six exact POM/JAR probes per
repository, through both Eddie and the documented Code Repository virtual
resolver, all returned HTTP 404. Global Maven package-name and artifact-version
queries returned zero Apollo values. No Apollo bytecode was therefore available
to decompile, and the flattened coordinate list cannot reveal type fields,
endpoint paths, authorization behavior, executable topology, Helm release
boundaries, private Hub/Agent transport, or conformance behavior.

## Consequence

Platform does not import, imitate, or claim compatibility with these private
libraries. Family names remain vocabulary hints only; the dependency trace
supersedes any stronger inference from the flattened inventory. The evidence
does not determine Palantir's chart count. Platform's own shipped-process and
trust boundaries determine two charts: a durable Hub and an independently
placed outbound Spoke. The Helm verification gate proves that separation
without introducing a private Apollo or Rubix API claim.
