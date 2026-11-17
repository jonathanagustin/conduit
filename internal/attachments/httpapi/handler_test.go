package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/attachments/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/attachments/application"
	"github.com/jonathanagustin/conduit/internal/attachments/domain"
	"github.com/jonathanagustin/conduit/internal/attachments/httpapi"
)

func server(allow bool) *http.ServeMux {
	mux := http.NewServeMux()
	httpapi.Register(mux, httpapi.Config{
		Service: application.Service{Store: recorded.New()},
		Authorize: func(*http.Request) error {
			if allow {
				return nil
			}
			return errors.New("no")
		},
		Operator: func(*http.Request) (string, error) { return "ada", nil },
	})
	return mux
}

func call(mux http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func upload(t *testing.T, mux http.Handler, name, content string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"filename": name, "content": content})
	return call(mux, "POST", "/v1/intake-attachments", string(raw))
}

func TestAnonymousCallersAreRefused(t *testing.T) {
	mux := server(false)
	for _, row := range [][2]string{
		{"POST", "/v1/intake-attachments"}, {"GET", "/v1/intake-attachments"},
		{"GET", "/v1/intake-attachments/" + strings.Repeat("a", 64)},
	} {
		if got := call(mux, row[0], row[1], `{"filename":"a.txt","content":"x"}`).Code; got != http.StatusUnauthorized {
			t.Fatalf("%v: %d", row, got)
		}
	}
	nobody := http.NewServeMux()
	httpapi.Register(nobody, httpapi.Config{})
	if got := call(nobody, "GET", "/v1/intake-attachments", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("nil authorizer: %d", got)
	}
}

func TestUploadListAndReadBack(t *testing.T) {
	mux := server(true)
	created := upload(t, mux, "values.yaml", "memory: 512Mi\n")
	if created.Code != http.StatusCreated {
		t.Fatalf("%d %s", created.Code, created.Body)
	}
	var item domain.Attachment
	_ = json.Unmarshal(created.Body.Bytes(), &item)
	if item.SHA256 != domain.Digest([]byte("memory: 512Mi\n")) || item.Name != "values.yaml" || item.Kind != domain.KindValues {
		t.Fatalf("item %+v", item)
	}
	list := call(mux, "GET", "/v1/intake-attachments", "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), item.SHA256) || !strings.Contains(list.Body.String(), "maxFileBytes") {
		t.Fatalf("%d %s", list.Code, list.Body)
	}
	if strings.Contains(list.Body.String(), "memory: 512Mi") {
		t.Fatal("the list must not carry content")
	}
	one := call(mux, "GET", "/v1/intake-attachments/"+item.SHA256, "")
	if one.Code != 200 || !strings.Contains(one.Body.String(), "memory: 512Mi") {
		t.Fatalf("%d %s", one.Code, one.Body)
	}
	if call(mux, "GET", "/v1/intake-attachments/"+strings.Repeat("0", 64), "").Code != http.StatusNotFound {
		t.Fatal("unknown digest must be 404")
	}
	if call(mux, "GET", "/v1/intake-attachments/NOT-A-DIGEST", "").Code != http.StatusNotFound {
		t.Fatal("malformed digest must be 404")
	}
}

func TestAnotherOperatorCannotFetchTheFile(t *testing.T) {
	mux := http.NewServeMux()
	httpapi.Register(mux, httpapi.Config{
		Service:   application.Service{Store: recorded.New()},
		Authorize: func(*http.Request) error { return nil },
		Operator: func(request *http.Request) (string, error) {
			name := request.Header.Get("X-Test-User")
			if name == "" {
				return "", errors.New("no")
			}
			return name, nil
		},
	})
	raw, _ := json.Marshal(map[string]string{"filename": "notes.txt", "content": "private note\n"})
	created := callAs(mux, "ada", "POST", "/v1/intake-attachments", string(raw))
	if created.Code != http.StatusCreated {
		t.Fatalf("create %d %s", created.Code, created.Body)
	}
	var item domain.Attachment
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil || item.SHA256 == "" {
		t.Fatal(created.Body.String())
	}
	ada := callAs(mux, "ada", "GET", "/v1/intake-attachments/"+item.SHA256, "")
	if ada.Code != http.StatusOK || !strings.Contains(ada.Body.String(), "private note") {
		t.Fatalf("ada %d %s", ada.Code, ada.Body)
	}
	foreign := callAs(mux, "bea", "GET", "/v1/intake-attachments/"+item.SHA256, "")
	missing := callAs(mux, "bea", "GET", "/v1/intake-attachments/"+strings.Repeat("0", 64), "")
	if foreign.Code != http.StatusNotFound || missing.Code != http.StatusNotFound || foreign.Body.String() != missing.Body.String() {
		t.Fatalf("foreign %d %s missing %d %s", foreign.Code, foreign.Body, missing.Code, missing.Body)
	}
	list := callAs(mux, "bea", "GET", "/v1/intake-attachments", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), item.SHA256) || strings.Contains(list.Body.String(), "private note") {
		t.Fatalf("bea list %s", list.Body)
	}
	adaList := callAs(mux, "ada", "GET", "/v1/intake-attachments", "")
	if adaList.Code != http.StatusOK || !strings.Contains(adaList.Body.String(), item.SHA256) {
		t.Fatalf("ada list %s", adaList.Body)
	}
}

func callAs(mux http.Handler, user, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("X-Test-User", user)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func TestRejectedUploadsReturnAReasonAndAreNotStored(t *testing.T) {
	mux := server(true)
	cases := []struct {
		name, content string
		status        int
		reason        string
	}{
		{"shot.png", "x", http.StatusUnprocessableEntity, "not an allowed type"},
		{"bundle.tar.zst", "x", http.StatusUnprocessableEntity, "not an allowed type"},
		{"values.yaml", "api_key: abc\n", http.StatusUnprocessableEntity, "credential assignment"},
		{"key.txt", "-----BEGIN PRIVATE KEY-----\n", http.StatusUnprocessableEntity, "key or certificate"},
		{"big.txt", strings.Repeat("a", domain.MaxFileBytes+1), http.StatusRequestEntityTooLarge, "limit"},
	}
	for _, row := range cases {
		got := upload(t, mux, row.name, row.content)
		if got.Code != row.status || !strings.Contains(got.Body.String(), row.reason) {
			t.Fatalf("%s: %d %s", row.name, got.Code, got.Body)
		}
	}
	list := call(mux, "GET", "/v1/intake-attachments", "")
	if !strings.Contains(list.Body.String(), `"items":[]`) {
		t.Fatalf("rejected uploads were stored: %s", list.Body)
	}
}

func TestOversizedEnvelopeAndBadBodyAreRefused(t *testing.T) {
	mux := server(true)
	huge := `{"filename":"a.txt","content":"` + strings.Repeat("a", 5*domain.MaxFileBytes) + `"}`
	if got := call(mux, "POST", "/v1/intake-attachments", huge).Code; got != http.StatusRequestEntityTooLarge {
		t.Fatalf("envelope: %d", got)
	}
	for _, body := range []string{"", "[]", `{"filename":"a.txt","extra":1}`} {
		if got := call(mux, "POST", "/v1/intake-attachments", body).Code; got != http.StatusBadRequest {
			t.Fatalf("%q: %d", body, got)
		}
	}
}
