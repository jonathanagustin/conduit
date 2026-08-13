package controlplane

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

type TerraformProductManifest struct {
	ManifestVersion string `yaml:"manifest-version" json:"manifestVersion"`
	ProductGroup    string `yaml:"product-group" json:"productGroup"`
	ProductName     string `yaml:"product-name" json:"productName"`
	ProductVersion  string `yaml:"product-version" json:"productVersion"`
	ProductType     string `yaml:"product-type" json:"productType"`
	Extensions      struct {
		TerraformRepositoryURL string `yaml:"terraform-repository-url" json:"terraformRepositoryUrl"`
	} `yaml:"extensions" json:"extensions"`
}

// DecodeTerraformProductManifest strictly decodes the public terraform.v1
// authoring document while preserving its documented kebab-case YAML shape.
func DecodeTerraformProductManifest(reader io.Reader) (TerraformProductManifest, error) {
	decoder := yaml.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.KnownFields(true)
	var manifest TerraformProductManifest
	if err := decoder.Decode(&manifest); err != nil {
		return TerraformProductManifest{}, fmt.Errorf("decode Terraform Product manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return TerraformProductManifest{}, errors.New("Terraform Product manifest must contain exactly one YAML document")
		}
		return TerraformProductManifest{}, fmt.Errorf("decode Terraform Product manifest: %w", err)
	}
	if err := ValidateTerraformProductManifest(manifest); err != nil {
		return TerraformProductManifest{}, err
	}
	return manifest, nil
}

func ValidateTerraformProductManifest(manifest TerraformProductManifest) error {
	if manifest.ManifestVersion != "1.0" || manifest.ProductType != "terraform.v1" {
		return errors.New("Terraform manifest requires manifest-version 1.0 and product-type terraform.v1")
	}
	if !coordinatePattern.MatchString(manifest.ProductGroup+":"+manifest.ProductName) ||
		!versionPattern.MatchString(manifest.ProductVersion) {
		return errors.New("Terraform manifest requires a valid Product coordinate and version")
	}
	if !strings.HasPrefix(manifest.Extensions.TerraformRepositoryURL, "oci://") {
		return errors.New("Terraform repository URL must use oci://")
	}
	if _, exact := artifactFromImmutableOCIReference(manifest.Extensions.TerraformRepositoryURL); !exact {
		return errors.New("Terraform repository URL must contain an explicit tag and @sha256 digest")
	}
	return nil
}
