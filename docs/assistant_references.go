// Package productdocs embeds selected native operator references. Private
// reference captures, evidence and arbitrary repository files are not included.
package productdocs

import (
	"embed"
	"io/fs"
)

//go:embed operations/operator-sign-in.md
//go:embed operations/ai-usage-and-transcripts.md
//go:embed operations/model-http.md
//go:embed operations/route-authorization.md
//go:embed architecture/agent-runs.md
var assistantReferences embed.FS

// AssistantReferences returns the immutable, explicitly selected corpus.
func AssistantReferences() fs.FS { return assistantReferences }
