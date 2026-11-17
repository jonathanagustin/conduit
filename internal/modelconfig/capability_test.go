package modelconfig

import "testing"

func TestCapabilitiesKeepTheirOwnModelAndCommand(t *testing.T) {
	values := map[string]string{
		"CONDUIT_MODEL_NAME": "shared", "CONDUIT_MODEL_BASE_URL": "https://models.example/v1",
		"CONDUIT_MODEL_API_KEY_FILE": "/mounted/key", "CONDUIT_MODEL_TIMEOUT": "45s",
		"CONDUIT_ASSISTANT_MODEL_NAME": "assistant-model", "CONDUIT_PLANNING_MODEL_NAME": "planning-model",
		"CONDUIT_PACKAGING_MODEL_CMD": "operator-cli --model package-model", "CONDUIT_PACKAGING_MODEL_NAME": "package-model",
		"CONDUIT_PACKAGING_MODEL_TIMEOUT": "2m",
	}
	getenv := func(key string) string { return values[key] }
	for _, tc := range []struct{ capability, model, command, base, timeout string }{
		{Assistant, "assistant-model", "", values["CONDUIT_MODEL_BASE_URL"], "45s"},
		{Planning, "planning-model", "", values["CONDUIT_MODEL_BASE_URL"], "45s"},
		{Packaging, "package-model", values["CONDUIT_PACKAGING_MODEL_CMD"], "", "2m"},
		{Appgen, "shared", "", values["CONDUIT_MODEL_BASE_URL"], "45s"},
	} {
		t.Run(tc.capability, func(t *testing.T) {
			get := ForCapability(getenv, tc.capability)
			for key, want := range map[string]string{
				"CONDUIT_MODEL_NAME": tc.model, "CONDUIT_MODEL_CMD": tc.command,
				"CONDUIT_MODEL_BASE_URL": tc.base, "CONDUIT_MODEL_TIMEOUT": tc.timeout,
				"CONDUIT_MODEL_API_KEY_FILE": "/mounted/key",
			} {
				if got := get(key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
		})
	}
	if got := ForCapability(nil, Assistant)("CONDUIT_MODEL_NAME"); got != "" {
		t.Fatal(got)
	}
}
