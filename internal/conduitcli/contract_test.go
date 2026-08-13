package conduitcli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandTreeCoversPinnedOfficialInventory(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile("../../docs/apollo/cli-command-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Commands []struct {
			Command string `json:"command"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(contents, &contract); err != nil {
		t.Fatal(err)
	}
	root := NewRoot(Runtime{Getenv: func(string) string { return "" }})
	actual := map[string]struct{}{"conduit": {}}
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		for _, child := range command.Commands() {
			actual[child.CommandPath()] = struct{}{}
			visit(child)
		}
	}
	visit(root)
	missing := []string{}
	for _, item := range contract.Commands {
		expected := strings.Replace(item.Command, "apollo-cli", "conduit", 1)
		expected = strings.TrimSuffix(expected, " [Experimental]")
		expected = strings.TrimSuffix(expected, " [Deprecated]")
		if _, exists := actual[expected]; !exists {
			missing = append(missing, expected)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("Conduit command tree omits %d pinned commands: %s", len(missing), strings.Join(missing, ", "))
	}
}
