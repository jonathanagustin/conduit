package productconfiguration

// Existing document enums and bounds shared with cross-document validation.
const (
	ManagedFileTypeJSON     = "json"
	ManagedFileTypeYAML     = "yaml"
	ManagedFileTypeYML      = "yml"
	ManagedFileTypeTemplate = "tmpl"
	ReloadTypeRuntime       = "runtime"
	SecretTypeRandomString  = "random_string"
	SecretTypeRandomBytes   = "random_bytes"
	SecretTypeRSAPKCS8      = "rsa_pkcs8"
	SecretTypeECPKCS8       = "ec_pkcs8"
	SecretTypeSTIGPassword  = "stig_password"
	MinimumEndpointPort     = 1024
	MaximumEndpointPort     = 19999
	TemplateDirectory       = "deployment/templates"
)
