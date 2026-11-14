package credhelper

import (
	"strings"
	"testing"
)

func TestRegistryHostBoundaryBeforeCredentials(t *testing.T) {
	// Recognized hosts must reach this deterministic environment error. Neither
	// branch can acquire credentials or make a token-exchange network request.
	t.Setenv("FF_DOCKER_ACR_AZIDENTITY", "false")
	t.Setenv("AZURE_ENVIRONMENT", "ConduitFixtureMissingCloud")
	helper := NewACRCredentialsHelper()
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
			if username != "" || password != "" || err == nil || !strings.Contains(err.Error(), "failed to get auth settings from environment") {
				t.Fatalf("recognized host did not reach the controlled pre-network environment check: credentialPresent=%v err=%v", password != "", err)
			}
		})
	}
}
