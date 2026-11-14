package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleCatalog() Catalog {
	return Catalog{
		Objects: []Object{
			{Type: "Environment", PrimaryKey: "east", Properties: map[string]string{"id": "east", "generation": "1"}},
			{Type: "Environment", PrimaryKey: "west", Properties: map[string]string{"id": "west", "generation": "2"}},
			{Type: "Plan", PrimaryKey: "plan-east", Properties: map[string]string{"id": "plan-east", "environment": "east", "status": "RECORDED"}},
			{Type: "Plan", PrimaryKey: "plan-west", Properties: map[string]string{"id": "plan-west", "environment": "west", "status": "RECORDED"}},
			{Type: "Artifact", PrimaryKey: "sha256:abc", Properties: map[string]string{"id": "sha256:abc", "digest": "sha256:abc"}},
		},
		Links: []Link{
			{APIName: "plans", FromType: "Environment", FromKey: "east", ToType: "Plan", ToKey: "plan-east"},
			{APIName: "plans", FromType: "Environment", FromKey: "west", ToType: "Plan", ToKey: "plan-west"},
			{APIName: "environment", FromType: "Plan", FromKey: "plan-east", ToType: "Environment", ToKey: "east"},
			{APIName: "artifacts", FromType: "Plan", FromKey: "plan-east", ToType: "Artifact", ToKey: "sha256:abc"},
		},
	}
}

func TestResolveBaseFilterAndSearchAround(t *testing.T) {
	t.Parallel()
	catalog := sampleCatalog()
	base := ObjectSet{Type: "base", ObjectType: "Environment"}
	cases := []struct {
		name    string
		set     ObjectSet
		want    []string
		wantErr error
	}{
		{name: "base", set: base, want: []string{"east", "west"}},
		{name: "eq", set: ObjectSet{Type: "filter", Inner: &base, Where: &Where{Type: "eq", Field: "id", Value: "west"}}, want: []string{"west"}},
		{name: "and", set: ObjectSet{Type: "filter", Inner: &base, Where: &Where{Type: "and", Clauses: []Where{
			{Type: "eq", Field: "id", Value: "east"},
			{Type: "eq", Field: "generation", Value: "1"},
		}}}, want: []string{"east"}},
		{name: "or", set: ObjectSet{Type: "filter", Inner: &base, Where: &Where{Type: "or", Clauses: []Where{
			{Type: "eq", Field: "id", Value: "east"},
			{Type: "eq", Field: "id", Value: "missing"},
		}}}, want: []string{"east"}},
		{name: "not", set: ObjectSet{Type: "filter", Inner: &base, Where: &Where{Type: "not", Clauses: []Where{
			{Type: "eq", Field: "id", Value: "east"},
		}}}, want: []string{"west"}},
		{name: "search around", set: ObjectSet{Type: "searchAround", Link: "plans", Inner: &ObjectSet{
			Type: "filter", Inner: &base, Where: &Where{Type: "eq", Field: "id", Value: "east"},
		}}, want: []string{"plan-east"}},
		{name: "unknown type", set: ObjectSet{Type: "base", ObjectType: "Missing"}, wantErr: ErrUnknownType},
		{name: "unknown link", set: ObjectSet{Type: "searchAround", Link: "missing", Inner: &base}, wantErr: ErrBadQuery},
		{name: "unknown where", set: ObjectSet{Type: "filter", Inner: &base, Where: &Where{Type: "lt", Field: "id", Value: "z"}}, wantErr: ErrBadQuery},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(catalog, tc.set)
			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("keys = %v, want %v", keys(got), tc.want)
			}
			for i := range tc.want {
				if got[i].PrimaryKey != tc.want[i] {
					t.Fatalf("keys = %v, want %v", keys(got), tc.want)
				}
			}
		})
	}
}

func TestPageOfHonorsTokenAndSize(t *testing.T) {
	t.Parallel()
	objects := []Object{{PrimaryKey: "a"}, {PrimaryKey: "b"}, {PrimaryKey: "c"}}
	first, err := PageOf(objects, 2, "")
	if err != nil || first.TotalCount != 3 || first.NextPageToken != "2" || len(first.Objects) != 2 {
		t.Fatalf("first = %#v err=%v", first, err)
	}
	second, err := PageOf(objects, 2, first.NextPageToken)
	if err != nil || second.NextPageToken != "" || len(second.Objects) != 1 || second.Objects[0].PrimaryKey != "c" {
		t.Fatalf("second = %#v err=%v", second, err)
	}
	if _, err := PageOf(objects, 0, "nope"); err != ErrBadQuery {
		t.Fatalf("bad token error = %v", err)
	}
	if _, err := PageOf(objects, 501, ""); err != ErrBadQuery {
		t.Fatalf("huge page error = %v", err)
	}
}

func TestMetadataCarriesPublishedFieldNames(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(Metadata())
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, key := range []string{
		`"ontology"`, `"objectTypes"`, `"actionTypes"`, `"queryTypes"`, `"interfaceTypes"`,
		`"sharedPropertyTypes"`, `"valueTypes"`, `"apiName"`, `"displayName"`, `"pluralDisplayName"`,
		`"primaryKey"`, `"titleProperty"`, `"linkTypes"`, `"objectTypeApiName"`, `"linkTypeRid"`,
		`"cardinality"`, `"implementsInterfaces"`, `"implementsInterfaces2"`, `"sharedPropertyTypeMapping"`,
		`"createChangeRequest"`, `"parameters"`, `"operations"`, `"rid"`,
	} {
		if !strings.Contains(body, key) {
			t.Errorf("metadata missing %s", key)
		}
	}
	if strings.Contains(body, `"aliases"`) || strings.Contains(body, `"datasources"`) {
		t.Fatal("empty aliases and datasources must be omitted")
	}
	archived := Metadata().ObjectTypes["ReleaseChannel"].ObjectType.Properties["archived"]
	if archived.DataType.Type != "boolean" || archived.RID == "" || archived.Description != "" {
		t.Fatalf("archived property = %#v", archived)
	}
	if Metadata().ObjectTypes["Environment"].ObjectType.Properties["id"].DataType.Type != "string" {
		t.Fatal("identifier properties stay strings")
	}
	if !strings.Contains(body, `"interfaceTypes":{}`) {
		t.Fatalf("interface types = %s", body)
	}
	for _, spec := range Types() {
		if _, ok := Metadata().ObjectTypes[spec.APIName]; !ok {
			t.Errorf("missing object type %s", spec.APIName)
		}
	}
	environment := Metadata().ObjectTypes["Environment"]
	if len(environment.LinkTypes) == 0 || environment.LinkTypes[0].ObjectTypeAPIName != "Environment" {
		t.Fatalf("environment links = %#v", environment.LinkTypes)
	}
	paired := false
	for _, side := range Metadata().ObjectTypes["Spoke"].LinkTypes {
		if side.LinkTypeRID == "ri.conduit.main.link-type.environment-spoke" {
			paired = true
		}
	}
	if !paired {
		t.Fatal("spoke is not the other side of the environment link")
	}
}

func keys(objects []Object) []string {
	out := make([]string, len(objects))
	for i, object := range objects {
		out[i] = object.PrimaryKey
	}
	return out
}
