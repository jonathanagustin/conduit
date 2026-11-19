package productmanifest

// Existing Product type spellings accepted by paired-definition validation.
// The Helm execution adapter continues to admit only its existing subset.
const (
	ProductTypeHelmChart = "helm-chart.v1"
	ProductTypeTerraform = "terraform.v1"
	ProductTypeService   = "service.v1"
	ProductTypeServiceV2 = "service.v2"
	ProductTypeDaemonSet = "daemon-set.v1"
	ProductTypeAsset     = "asset.v1"
	ProductTypeHelm      = "helm.v1"
)
