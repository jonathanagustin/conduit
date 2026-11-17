package main

import "testing"

func TestGeneratedReferenceIgnoresOperatorConfiguration(t *testing.T) {
	t.Chdir("../..")
	for name, value := range map[string]string{
		"CONDUIT_SERVER":            "https://not-a-deployment.invalid",
		"CONDUIT_CLIENT_KEY_FILE":   "/not-a-secret/operator-private-key",
		"CONDUIT_BEARER_TOKEN_FILE": "/not-a-secret/operator-token",
		"CONDUIT_SPACE_ID":          "operator-private-space",
		"CONDUIT_PROFILE":           "operator-private-profile",
	} {
		t.Setenv(name, value)
	}
	if err := generate(false); err != nil {
		t.Fatalf("operator configuration changed the generated public reference: %v", err)
	}
}
