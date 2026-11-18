// Package application builds one environment's timeline from the existing
// authorization audit chain. It does not create another chain.
package application

import (
	"context"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// Entry is one timeline row. Sequence and Digest are set when the row is an
// authorization decision. Verified is true only when the digest matches the chain.
type Entry struct {
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"`
	Summary  string    `json:"summary"`
	Sequence uint64    `json:"sequence,omitempty"`
	Digest   string    `json:"digest,omitempty"`
	Verified bool      `json:"verified"`
}

// Build returns newest first. Events for another environment are omitted.
// A digest that is not the chain digest is not verified.
func Build(ctx context.Context, repository controlplane.Repository, environment string) ([]Entry, error) {
	events, err := repository.ListAuthorizationAudit(ctx, "")
	if err != nil {
		return nil, err
	}
	chain, err := controlplane.ChainAuthorizationAudit(events)
	if err != nil {
		return nil, err
	}
	byID := map[string]controlplane.AuthorizationAuditChainEntry{}
	for _, entry := range chain {
		byID[entry.ID] = entry
	}
	var rows []Entry
	for _, event := range events {
		if !forEnvironment(event.ResourceRID, environment) {
			continue
		}
		linked := byID[event.ID]
		verified := linked.Digest != "" && linked.Digest == eventDigest(chain, event.ID)
		rows = append(rows, Entry{
			At: event.CreatedAt, Kind: "audit", Summary: event.Action + " " + event.Decision,
			Sequence: linked.Sequence, Digest: linked.Digest, Verified: verified,
		})
	}
	plans, err := repository.ListPlans(ctx, environment)
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if plan.Plan.Environment != environment {
			continue
		}
		rows = append(rows, Entry{At: plan.CreatedAt, Kind: "plan", Summary: "plan " + plan.ID + " " + string(plan.Status)})
	}
	sortNewest(rows)
	if rows == nil {
		rows = []Entry{}
	}
	return rows, nil
}

func forEnvironment(resource, environment string) bool {
	anchor := "environment/" + environment
	return resource == anchor || strings.HasPrefix(resource, anchor+"/")
}

func eventDigest(chain []controlplane.AuthorizationAuditChainEntry, id string) string {
	for _, entry := range chain {
		if entry.ID == id {
			return entry.Digest
		}
	}
	return ""
}

func sortNewest(rows []Entry) {
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].At.After(rows[i].At) || (rows[j].At.Equal(rows[i].At) && rows[j].Sequence > rows[i].Sequence) {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
}
