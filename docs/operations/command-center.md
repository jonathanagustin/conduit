# Recorded Command Center

The existing `s13-fleet-overview` entry opens the Command Center's live overview;
no second home or approval path is added. Geography, opportunities, profile
status and audit activity read the Hub's existing authorized views.

## Geography

Environment locations use two optional Conduit-native display labels from
`GET /v1/environments`. They follow the existing label-ID grammar and can be
registered through the existing label-definition process where required.

| Environment label | Value |
| --- | --- |
| `conduit.latitude` | Decimal degrees from -90 to 90 |
| `conduit.longitude` | Decimal degrees from -180 to 180 |

Both strings must be present and valid. A genuine zero is accepted; missing,
blank, nondecimal or out-of-range values remain unmapped. Nothing is geocoded
from a name, an IP address or the browser's location. Label changes retain the
existing Environment configuration, label authorization and review rules; this
view does not write them. These are declared coordinates, not GPS telemetry.

Every listed site comes from an authorized recorded Environment. Selecting an
unmapped site opens its recorded details without inventing a pin. The globe
shows only the front hemisphere; the site list can center any located record.
Borders, Labels and Globe toggle their layers. Links toggles the availability
notice: this Hub exposes no geographic link endpoints, so the view draws no
inferred network routes. Spoke state is the Hub's recorded classification and
is displayed separately from the declared location.

The static basemap is derived from Natural Earth's public-domain 1:110m country
geometry, pinned to commit `9380cca83db5f9aef52d5e762765100745f84b27`. Source,
license evidence, checksums and transform details are in
`web/src/console/operator/assets/world-borders-110m.source.json`. The layer is
local numeric data; it loads no remote tiles or executable map module. Rebuild
it with `python3 scripts/generate-world-borders.py SOURCE.geojson OUTPUT.json`;
the generator requires the reviewed source checksum before decoding it.

## Activity and review

- Governance opportunities use pending change requests plus reported version
  drift where both desired and observed versions are known. Missing versions
  are not drift. Ask adds a context draft; Review opens the existing authorized
  change-request or Environment view using the recorded identity.
- Active workspace profiles group visible `/v1/dev-workspaces` records marked
  starting or running by their actual profile IDs. These are recorded runtime
  states, not worker heartbeats or a complete profile catalog. Failed reads stay
  unavailable, and unknown metadata is disclosed as partial data.
- Recorded operations use loaded authorization-audit entries. The header counts
  loaded rows, not an inferred global total. This is a recorded audit view;
  live subscription transport remains the separate event-stream work.
- The existing release-control simulation is explicitly labeled Example at its
  entry point and by the owning screen. Its hardcoded validation/signature gates
  are not presented as live Plan evidence; #553 tracks that remaining wiring.

Ask, COA and Share in chat use the existing assistant rail. They append an
editable draft while preserving existing unsent text and the selected object
context. They do not submit a turn, run compute, approve, publish or deploy.
The operator chooses whether to send; subsequent tools retain the Hub's governed
action and separate-review rules. An oversized or invalid draft is not replaced.

## Verification

Unit cases cover explicit zero/missing/invalid coordinates, known drift,
profile availability and unknown states, escaped record text, draft preservation
and the pinned numeric basemap. Chromium exercises layer keyboard behavior,
object selection and focus, drafting, exact change-request navigation, and the
absence of automatic mutation or external requests in that flow. The existing
full local CI gate remains authoritative for the exact candidate commit.
