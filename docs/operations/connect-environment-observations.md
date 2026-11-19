# Native Environment reported-state read

`conduit.environments.v1.EnvironmentObservationService/GetEnvironmentReportedState`
is a bounded Connect POST alongside `GET
/v1/environments/{environmentID}/reported-state`. It returns the full reported
observation rather than the summary index: Agent/Plan identity, revision, entity
coordinates and observed digests/status, nested health details, and secret
provider references, Kubernetes names and key names. The native Secrets
collection contains references rather than secret values; arbitrary health
diagnostics remain authorized reported data. All these fields remain private
Environment inventory.

The request authenticates the original operator carrier before protobuf
decoding. `ReadAuthorizedEnvironmentReport` shares the REST resource decision:
ReadResource on that exact Environment, including existing administrator and
delegation rules, before touching its report. The method adds no Space selector;
the REST reported-state endpoint does not apply Space selection either. A caller
must not treat a Space grouping as access authority.

Absent timestamps mean native zero/unknown observations, never the current time.
Present timestamps preserve nanoseconds. Health details have message presence,
so an absent detail differs from an observed empty detail. Status strings remain
open native vocabulary; transport adds no inferred health or convergence.
Revisions use uint64. Health-check `params_json` contains native JSON bytes
(including JSON null for a nil map), avoiding protobuf Struct/double conversion
of arbitrary numbers. Consumers must decode those bytes using their native JSON
number policy. Repeated fields are native collections, not evidence that an
absent health detail was observed.

The pinned Connect JSON codec ignores unknown request fields for forward
compatibility; only the contracted Environment field selects the resource.
Malformed bodies and empty identifiers are refused. Private store diagnostics,
invalid observation timestamps and unencodable diagnostic parameters produce a
sanitized error, without a partial report. An existing report is returned as
observed, with no new freshness or desired-state policy.

Only the registered read method is served. There is no report write, automatic
retry, REST fallback, CLI/console transport cutover or REST deprecation. See
[#391](https://github.com/jonathanagustin/conduit/issues/391),
[#392](https://github.com/jonathanagustin/conduit/issues/392) and
[#394](https://github.com/jonathanagustin/conduit/issues/394) for remaining work.

## Explicit CLI selection

`conduit environment reported-state ENVIRONMENT_ID --read-transport connect`
selects this method. Omitting the flag, or selecting rest, retains the existing
REST request. TLS, bearer/mTLS identity, timeout, cancellation, redirect refusal
and response-size bounds use the same configured HTTP client. A Connect refusal,
missing method or malformed response is an error; the CLI never replays it over
REST. Full Environment get/list and all writes retain their existing paths.

The client binds the returned Environment to the requested identity and refuses
invalid timestamps, missing messages or malformed diagnostic JSON before
printing a result. Diagnostic JSON numbers remain exact in JSON and YAML output.
As standard protobuf repeated fields carry no nil-versus-empty distinction,
empty repeated collections map to native zero slices; REST keeps its existing
wire behavior. Health-detail message presence and unknown observation times
remain explicit. The reported-state read adds no Space-selection semantics.
