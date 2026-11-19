package productdefinition

import (
	"github.com/jonathanagustin/conduit/internal/apollo/productmanifest"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
	"regexp"
	"sort"
)

var productCoordinatePattern = regexp.MustCompile(validationdomain.ProductCoordinateExpression)

// Validate retains paired-document checks, including optional manifest-version
// and native Product version classification. Packaging admission is separate.
func Validate(definition Input) []validationdomain.Problem {
	errs := make([]validationdomain.Problem, 0)
	add := func(path, message string) {
		errs = append(errs, validationdomain.Problem{Path: path, Message: message})
	}
	manifest := definition.Manifest
	// manifest-version is optional here, unlike in the packaging validator. The
	// Product Manifest page lists exactly four required fields and its minimal
	// example carries no manifest-version; apollo-cli emits one, so the execution
	// path can keep requiring it, but a hand-written conformant manifest must not
	// be rejected for omitting a field the specification never asked for.
	if manifest.ManifestVersion != "" && manifest.ManifestVersion != "1.0" {
		add("manifest.manifest-version", "must be 1.0 when present")
	}
	if !productCoordinatePattern.MatchString(manifest.ProductGroup + ":" + manifest.ProductName) {
		add("manifest.product", "product-group and product-name must form a group:name coordinate")
	}
	// A Product Release may declare a non-orderable version. Nothing may sort it
	// against another version, but refusing to publish it would be this validator
	// narrowing the specification rather than enforcing it.
	if !catalog.IsApolloProductVersion(manifest.ProductVersion) {
		add("manifest.product-version", "must be an Apollo orderable or non-orderable version")
	}
	switch manifest.ProductType {
	case productmanifest.ProductTypeHelmChart, productmanifest.ProductTypeTerraform,
		productmanifest.ProductTypeService, productmanifest.ProductTypeServiceV2, productmanifest.ProductTypeDaemonSet,
		productmanifest.ProductTypeAsset, productmanifest.ProductTypeHelm:
	default:
		add("manifest.product-type", "must be a known Apollo Product type")
	}
	manifestErrs := make([]validationdomain.Problem, 0)
	productmanifest.ValidateServiceExtensions(manifest.Extensions, &manifestErrs)
	for _, err := range manifestErrs {
		errs = append(errs, validationdomain.Problem{Path: "manifest." + err.Path, Message: err.Message})
	}
	errs = append(errs, ValidateConfiguration(definition)...)
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Path < errs[j].Path })
	return errs
}
