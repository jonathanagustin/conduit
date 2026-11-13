// Package pipeline is the Temporal workflow for one packaging run.
// Activities perform every read and write. The workflow itself only decides
// which step is next and whether a review message matches a Conduit approval.
package pipeline
