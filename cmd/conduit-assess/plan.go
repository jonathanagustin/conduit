package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/packaging/adapters/source"
	"github.com/jonathanagustin/conduit/internal/packaging/domain"
	"io"
)

func plan(args []string, in io.Reader, out io.Writer) error {
	if len(args) < 3 {
		return fmt.Errorf("plan requires ROOT COMMIT DOCKERFILE [SUPPORTING_FILE...]")
	}
	b, err := io.ReadAll(io.LimitReader(in, maxInput+1))
	if err != nil {
		return err
	}
	if len(b) > maxInput {
		return fmt.Errorf("plan input exceeds limit")
	}
	if len(bytes.TrimSpace(b)) == 0 {
		b = []byte("{}")
	}
	if err := rejectDuplicateKeys(b, "scaffold", "review", "evidenceSHA256", "decisions", "topic", "disposition", "rationale", "profile", "name", "version", "sourceRevision", "sourceSHA256", "image", "port", "uid", "cpuMilli", "memoryMiB", "zarfVersion", "udsCommit", "existingClaim", "mountPath", "existingSecretFile", "key", "env"); err != nil {
		return err
	}
	var input domain.PlanInput
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		return err
	}
	snapshot, err := source.ReadCommit(context.Background(), args[0], args[1], args[2:])
	if err != nil {
		return err
	}
	docs := make([]domain.SourceDocument, 0, len(snapshot.Files))
	for _, f := range snapshot.Files {
		docs = append(docs, domain.SourceDocument{Path: f.Path, Blob: f.Blob, Text: f.Text})
	}
	result, err := domain.PlanPackaging(snapshot.Revision, args[2], docs, input)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
