package credhelper

import (
	"strings"
	"testing"
)

func TestRegistryHostBoundaryBeforeCredentials(t *testing.T) {
	// Recognized hosts must reach this deterministic environment error. Neither
	// branch can acquire credentials or make a token-exchange network request.
	for _, name := range []string{
		"AZURE_SUBSCRIPTION_ID", "AZURE_TENANT_ID", "AZURE_AUXILIARY_TENANT_IDS",
		"AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET", "AZURE_CERTIFICATE_PATH",
		"AZURE_CERTIFICATE_PASSWORD", "AZURE_USERNAME", "AZURE_PASSWORD",
		"AZURE_FEDERATED_TOKEN_FILE", "AZURE_FEDERATED_TOKEN", "AZURE_AUTH_LOCATION",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("AZURE_ENVIRONMENT", "ConduitFixtureMissingCloud")
	helper := NewACRCredentialsHelper()
	for _, mode := range []string{"true", "false"} {
		t.Run("azidentity_"+mode, func(t *testing.T) {
			t.Setenv("FF_DOCKER_ACR_AZIDENTITY", mode)
			for _, host := range []string{
				"registry.azurecr.io.attacker.example",
				"registry.azurecr.cn.attacker.example",
				"registry.azurecr.de.attacker.example",
				"registry.azurecr.us.attacker.example",
				"mcr.microsoft.com.attacker.example",
				"registry.azurecr.io@attacker.example",
				"attacker.example?registry=registry.azurecr.io",
				"registry.azurecr.io/unexpected-path",
				"azurecr.io", "attacker.example", "",
			} {
				t.Run("reject_"+host, func(t *testing.T) {
					username, password, err := helper.Get(host)
					if username != "" || password != "" || err == nil || err.Error() != "serverURL does not refer to Azure Container Registry" {
						t.Fatalf("untrusted host reached credential acquisition: username=%q credentialPresent=%v err=%v", username, password != "", err)
					}
				})
			}
			for _, host := range []string{
				"registry.azurecr.io", "registry.region.data.azurecr.io",
				"registry.azurecr.cn", "registry.azurecr.de", "registry.azurecr.us",
				"registry.azurecr.io:443", "mcr.microsoft.com",
			} {
				t.Run("recognize_"+host, func(t *testing.T) {
					username, password, err := helper.Get(host)
					if username != "" || password != "" || err == nil || !strings.Contains(strings.ToLower(err.Error()), "conduitfixturemissingcloud") {
						t.Fatalf("recognized host did not reach the controlled pre-network environment check: credentialPresent=%v err=%v", password != "", err)
					}
				})
			}
		})
	}

}
