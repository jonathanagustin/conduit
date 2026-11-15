# Release dependency constraints and upgrade lag

The existing Releases area has a Dependencies tab. It reads at most the first
32 distinct visible product/release coordinates from the already-loaded catalog.
Four concurrent workers fetch the native
`/v1/products/{product}/dependencies/{version}` route, with a 15-second read timeout
and at most 64 dependency rows per response. Missing/invalid reads remain
unavailable. A bounded subset is explicitly labeled; it is not a complete
inventory assertion. No new dependency, runtime SDK or write endpoint is added.

The graph shows the selected release's direct product/version constraints.
Dashed edges are optional. These constraints are not resolved installation or
version-to-version edges. Potential dependent products are transitive product
reachability across successfully loaded metadata, including optional edges and
terminating safely on cycles. Version ranges are not resolved. This potential
blast radius is not deployed impact, and no match within the subset does not
prove no impact. The native empty response does not distinguish no dependencies
from absent captured metadata; the UI states that limitation.

The same view reads `/v1/fleet/upgrades`, displaying recorded environment,
installation, product, observed/desired version and reason. Missing reads do not
mean no lag, and a generic reason does not establish blackout or disconnected
causality. At most 512 returned lag rows are displayed with a truncation notice.
Version lag is distinct from digest/configuration convergence.

These are Conduit extensions over native Hub records. All record text is escaped,
including SVG labels and select values. Selection remains bound to a visible
catalog coordinate. Detached/superseded mounts cannot apply late results. The
view uses GET reads only and makes no scheduling, approval, execution or private
protocol compatibility claim. Remaining scheduling work stays in #553.
