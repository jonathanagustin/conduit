package conduitcli

import (
	"fmt"
	"os"
	"testing"
)

// os.UserConfigDir reads XDG_CONFIG_HOME, so pointing it at a temporary
// directory means nothing in this package can reach the developer's real
// conduit configuration.
//
// The tests already override the config path through Runtime.Getenv, which is
// enough while the code reads it. It is not enough when the code does not: a
// mutation testing run flipped the CONDUIT_CONFIG_FILE lookup in
// configPath, the override stopped being consulted, and a mutant wrote a
// profile document into the real ~/.config/conduit/config.yaml. Every
// subsequent run of this package then failed, because that document selects a
// profile carrying mTLS files and the tests pass --development-subject, which
// cannot be combined with one.
//
// A test that can write outside its temporary directory will eventually do so.
// This closes the path rather than relying on every caller to stub it.
func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "conduitcli-config-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "conduitcli tests: isolate config directory:", err)
		os.Exit(1)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", directory); err != nil {
		fmt.Fprintln(os.Stderr, "conduitcli tests: isolate config directory:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}
