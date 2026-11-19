package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestProblemsPreserveWireFieldsOrderAndErrorIdentity(t *testing.T) {
	problems := Problems{{Path: "entities[0].name", Message: "must not be empty"}, {Path: "notes", Message: `quoted "value"`}}
	if got := problems.Error(); got != `entities[0].name: must not be empty; notes: quoted "value"` {
		t.Fatalf("aggregate error %q", got)
	}
	encoded, err := json.Marshal(problems)
	if err != nil || string(encoded) != `[{"path":"entities[0].name","message":"must not be empty"},{"path":"notes","message":"quoted \"value\""}]` {
		t.Fatalf("wire error %s %v", encoded, err)
	}
	var recovered Problems
	if !errors.As(fmt.Errorf("validation failed: %w", problems), &recovered) || !reflect.DeepEqual(recovered, problems) {
		t.Fatalf("typed errors lost: %#v", recovered)
	}
	for _, tc := range []struct {
		problems Problems
		wire     string
	}{{nil, "null"}, {Problems{}, "[]"}} {
		encoded, err := json.Marshal(tc.problems)
		if err != nil || string(encoded) != tc.wire || tc.problems.Error() != "" {
			t.Fatalf("empty/nil semantics %s %v", encoded, err)
		}
	}
}

func TestExistingFieldGrammarConformance(t *testing.T) {
	for _, tc := range []struct {
		name, expression   string
		accepted, rejected []string
	}{
		{"coordinate", ProductCoordinateExpression, []string{"group:app", "Group_1:app.name-9"}, []string{"", "app", "group:app:extra", "group/path:app"}},
		// These expressions are lexical checks, not a new global DNS length policy.
		{"dns label", DNSLabelExpression, []string{"a", "9", "app-1", strings.Repeat("a", 64)}, []string{"", "A", "-app", "app-", "app_name"}},
		{"channel", ReleaseChannelExpression, []string{"STABLE", "CANDIDATE_1-A"}, []string{"", "stable", "1STABLE", "STABLE/extra"}},
		{"dependency group", DependencyGroupExpression, []string{"prod", "a.b_1-2", strings.Repeat("a", 64)}, []string{"", "/escape", "_prefix", strings.Repeat("a", 65)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pattern := regexp.MustCompile(tc.expression)
			for _, value := range tc.accepted {
				if !pattern.MatchString(value) {
					t.Errorf("existing valid value refused: %q", value)
				}
			}
			for _, value := range tc.rejected {
				if pattern.MatchString(value) {
					t.Errorf("invalid value accepted: %q", value)
				}
			}
		})
	}
}
