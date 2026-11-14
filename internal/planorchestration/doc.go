// Package planorchestration sequences an approved Plan with Temporal.
//
// Activities call the delivery kernel. The workflow does not approve, does
// not write reported state, and does not apply cluster changes. The Spoke
// reconciler still does that work. With no worker, this package is not in
// the request path.
package planorchestration
