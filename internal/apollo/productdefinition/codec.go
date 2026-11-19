package productdefinition

import (
	"io"

	"github.com/jonathanagustin/conduit/internal/apollo/productconfiguration"
	"github.com/jonathanagustin/conduit/internal/apollo/productmanifest"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

// Definition is the compatibility manifest/configuration document pair and
// packaged template names. Native Release conversion remains a separate step.
type Definition struct {
	Manifest      productmanifest.Manifest           `json:"manifest"`
	Configuration productconfiguration.Configuration `json:"configuration"`
	// Templates are file names relative to deployment/templates. Nil means the
	// caller did not provide an inventory; a present empty inventory has no files.
	Templates []string `json:"templates,omitempty"`
}

// Decode retains the original strict document decoders and paired validator.
// Configuration is optional. Packaging validation is deliberately separate:
// conformant service definitions need not contain an executable Helm chart.
func Decode(manifest, configuration io.Reader, templates []string) (Definition, []validationdomain.Problem, error) {
	decoded, err := productmanifest.Decode(manifest)
	if err != nil {
		return Definition{}, nil, err
	}
	config, err := productconfiguration.Decode(configuration)
	if err != nil {
		return Definition{}, nil, err
	}
	definition := Definition{Manifest: decoded, Configuration: config, Templates: templates}
	return definition, Validate(Input{Manifest: decoded, Configuration: config, Templates: templates}), nil
}
