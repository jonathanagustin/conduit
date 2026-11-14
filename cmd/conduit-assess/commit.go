package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/packaging/adapters/source"
	"io"
)

func committedEvidence(args []string, out io.Writer) error {
	if len(args) < 3 {
		return fmt.Errorf("commit-evidence requires ROOT COMMIT FILE...")
	}
	snapshot, err := source.ReadCommit(context.Background(), args[0], args[1], args[2:])
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(snapshot)
}
