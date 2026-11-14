package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/packaging/adapters/source"
	"github.com/jonathanagustin/conduit/internal/packaging/domain"
	"io"
)

func discover(args []string, out io.Writer) error {
	if len(args) != 3 {
		return fmt.Errorf("discover requires ROOT COMMIT DOCKERFILE")
	}
	snapshot, err := source.ReadCommit(context.Background(), args[0], args[1], args[2:])
	if err != nil {
		return err
	}
	f := snapshot.Files[0]
	return json.NewEncoder(out).Encode(struct {
		Revision  string           `json:"revision"`
		Blob      string           `json:"blob"`
		SHA256    string           `json:"sha256"`
		Discovery domain.Discovery `json:"discovery"`
	}{snapshot.Revision, f.Blob, f.SHA256, domain.DiscoverDockerfile(f.Path, f.Text)})
}
