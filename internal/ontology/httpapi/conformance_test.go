package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/ontology/domain"
)

// TestLiveReferenceShapes compares JSON keys and types, not values, against a
// reference server. It stays skipped unless ONTOLOGY_CONFORMANCE_URL is set,
// so CI never runs it and this tree never vendors a reference payload.
func TestLiveReferenceShapes(t *testing.T) {
	reference := strings.TrimRight(os.Getenv("ONTOLOGY_CONFORMANCE_URL"), "/")
	if reference == "" {
		t.Skip("ONTOLOGY_CONFORMANCE_URL is unset")
	}
	local := httptest.NewServer(mux(Config{
		Source:    &fakeSource{catalog: conformanceCatalog()},
		Authorize: allowAll,
	}))
	t.Cleanup(local.Close)
	client := &http.Client{Timeout: 15 * time.Second}
	referenceName := firstString(t, client, reference, "/api/v2/ontologies", "apiName")
	referenceType := firstString(t, client, reference, "/api/v2/ontologies/"+referenceName+"/objectTypes", "apiName")
	referenceAction := firstString(t, client, reference, "/api/v2/ontologies/"+referenceName+"/actionTypes", "apiName")
	cases := []struct {
		method, referencePath, localPath, body string
	}{
		{http.MethodGet, "/api/v2/ontologies", "/api/v2/ontologies", ""},
		{http.MethodGet, "/api/v2/ontologies/" + referenceName + "/fullMetadata", "/api/v2/ontologies/conduit/fullMetadata", ""},
		{http.MethodGet, "/api/v2/ontologies/" + referenceName + "/objectTypes", "/api/v2/ontologies/conduit/objectTypes", ""},
		{http.MethodGet, "/api/v2/ontologies/" + referenceName + "/objectTypes/" + referenceType, "/api/v2/ontologies/conduit/objectTypes/Environment", ""},
		{http.MethodGet, "/api/v2/ontologies/" + referenceName + "/objectTypes/" + referenceType + "/fullMetadata", "/api/v2/ontologies/conduit/objectTypes/Environment/fullMetadata", ""},
		{http.MethodGet, "/api/v2/ontologies/" + referenceName + "/interfaceTypes", "/api/v2/ontologies/conduit/interfaceTypes", ""},
		{http.MethodGet, "/api/v2/ontologies/" + referenceName + "/actionTypes", "/api/v2/ontologies/conduit/actionTypes", ""},
		{http.MethodPost, "/api/v2/ontologies/" + referenceName + "/objectSets/loadObjects", "/api/v2/ontologies/conduit/objectSets/loadObjects",
			`{"objectSet":{"type":"base","objectType":"` + referenceType + `"}}`},
	}
	if referenceAction != "" {
		cases = append(cases, struct {
			method, referencePath, localPath, body string
		}{http.MethodGet, "/api/v2/ontologies/" + referenceName + "/actionTypes/" + referenceAction, "/api/v2/ontologies/conduit/actionTypes/createChangeRequest", ""})
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.localPath, func(t *testing.T) {
			left := fetchJSON(t, client, tc.method, reference+tc.referencePath, tc.body)
			body := tc.body
			if tc.method == http.MethodPost {
				body = `{"objectSet":{"type":"base","objectType":"Environment"}}`
			}
			right := fetchJSON(t, client, tc.method, local.URL+tc.localPath, body)
			if err := compatible(left, right, "$", false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func conformanceCatalog() domain.Catalog {
	return domain.Catalog{Objects: []domain.Object{{
		Type: "Environment", PrimaryKey: "east", Properties: map[string]string{"id": "east"},
	}}}
}

func firstString(t *testing.T, client *http.Client, base, path, field string) string {
	t.Helper()
	body := fetchJSON(t, client, http.MethodGet, base+path, "")
	document, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("%s: %T", path, body)
	}
	rows, _ := document["data"].([]any)
	if len(rows) == 0 {
		return ""
	}
	row, _ := rows[0].(map[string]any)
	value, _ := row[field].(string)
	if value == "" {
		t.Fatalf("%s has no %s", path, field)
	}
	return value
}

func fetchJSON(t *testing.T, client *http.Client, method, url, body string) any {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, url, response.StatusCode, payload)
	}
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// dictionaryFields are maps keyed by caller-defined names. The names are
// values, so only the value shapes are compared.
var dictionaryFields = map[string]bool{
	"objectTypes": true, "actionTypes": true, "queryTypes": true, "interfaceTypes": true,
	"sharedPropertyTypes": true, "valueTypes": true, "properties": true, "parameters": true,
	"implementsInterfaces2": true, "sharedPropertyTypeMapping": true,
}

func compatible(left, right any, path string, valuesOnly bool) error {
	if valuesOnly {
		leftMap, leftOK := left.(map[string]any)
		rightMap, rightOK := right.(map[string]any)
		if !leftOK || !rightOK {
			return fmt.Errorf("%s: expected objects, got %T and %T", path, left, right)
		}
		return compatibleValues(leftMap, rightMap, path)
	}
	switch typed := left.(type) {
	case map[string]any:
		other, ok := right.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: object vs %T", path, right)
		}
		if len(typed) != len(other) {
			return fmt.Errorf("%s: keys %s vs %s", path, mapKeys(typed), mapKeys(other))
		}
		for key, value := range typed {
			peer, ok := other[key]
			if !ok {
				return fmt.Errorf("%s: missing %s", path, key)
			}
			if err := compatible(value, peer, path+"."+key, dictionaryFields[key]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		other, ok := right.([]any)
		if !ok {
			return fmt.Errorf("%s: array vs %T", path, right)
		}
		if len(typed) == 0 || len(other) == 0 {
			return nil
		}
		return compatible(typed[0], other[0], path+"[]", false)
	default:
		if jsonKind(left) != jsonKind(right) {
			return fmt.Errorf("%s: %s vs %s", path, jsonKind(left), jsonKind(right))
		}
		return nil
	}
}

func compatibleValues(left, right map[string]any, path string) error {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	var leftProto, rightProto any
	for _, value := range left {
		leftProto = value
		break
	}
	for _, value := range right {
		rightProto = value
		break
	}
	for key, value := range left {
		if err := compatible(value, rightProto, path+"."+key, false); err != nil {
			return err
		}
	}
	for key, value := range right {
		if err := compatible(leftProto, value, path+"."+key, false); err != nil {
			return err
		}
	}
	return nil
}

func jsonKind(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func mapKeys(value map[string]any) string {
	encoded, _ := json.Marshal(value)
	if len(encoded) > 180 {
		encoded = append(encoded[:180], []byte("...")...)
	}
	return string(bytes.TrimSpace(encoded))
}
