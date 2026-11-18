package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"

	"connectrpc.com/connect/v2"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
	"google.golang.org/protobuf/proto"
)

type detailReader struct {
	row   domain.RunStatus
	err   error
	reads atomic.Int32
}

func (r *detailReader) Read(_ context.Context, id string) (domain.RunStatus, error) {
	r.reads.Add(1)
	if id != r.row.RunID && r.err == nil {
		return domain.RunStatus{}, packaginghttp.ErrNotFound
	}
	return r.row, r.err
}
func (*detailReader) List(context.Context) ([]domain.RunStatus, error)           { return nil, nil }
func (*detailReader) Events(context.Context, string) ([]domain.StepEvent, error) { return nil, nil }

func detailCommand(ctx context.Context, server, mode, id string, extra ...string) (string, error) {
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: &out, Getenv: func(string) string { return "" }})
	args := []string{"--server", server, "--development-subject", "developer", "run", "get", id}
	if mode != "" {
		args = append(args, "--read-transport", mode)
	}
	root.SetArgs(append(args, extra...))
	root.SetContext(ctx)
	err := root.Execute()
	return out.String(), err
}
func registerDetail(mux *http.ServeMux, cfg packaginghttp.Config) {
	packaginghttp.Register(mux, cfg)
	packaginghttp.RegisterConnect(mux, cfg)
}
func detailFixture() domain.RunStatus {
	return domain.RunStatus{Tier: "CONDUIT_NATIVE", Configured: true, Authority: "packaging-run", PlanAuthority: "conduit-plan", ApprovalAuthority: "conduit-governance", RunID: "run/odd ?%", Phase: "future-phase", Outcome: "future-outcome", Reason: "recorded reason", SourceDigest: "sha256:" + strings.Repeat("a", 64), ArtifactDigest: "sha256:" + strings.Repeat("b", 64), ProposalRevision: "reviewed-r2", TargetPlanRef: "east/app", PlanID: "pln_native", HandoffReconciled: true, LastReview: "rejected", DuplicateReviews: 3, TemplateVersion: "v2", ToolVersions: "zarf=1.0.0", AttachmentSHA256: []string{strings.Repeat("c", 64)}}
}

func TestRunGetMatchesNativeRESTAndGeneratedConnect(t *testing.T) {
	for _, mode := range []string{"", "rest", "connect"} {
		t.Run("mode-"+mode, func(t *testing.T) {
			reader := &detailReader{row: detailFixture()}
			mux := http.NewServeMux()
			registerDetail(mux, packaginghttp.Config{Reader: reader, Authorize: func(r *http.Request) error {
				if r.Header.Get("X-Platform-Subject") != "developer" {
					return errors.New("missing identity")
				}
				return nil
			}})
			var methods []string
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				methods = append(methods, r.Method+" "+r.URL.EscapedPath())
				mu.Unlock()
				mux.ServeHTTP(w, r)
			}))
			defer server.Close()
			out, err := detailCommand(t.Context(), server.URL, mode, reader.row.RunID)
			if err != nil {
				t.Fatalf("read: %v %s", err, out)
			}
			var got domain.RunStatus
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, reader.row) {
				t.Fatalf("lost native evidence: got=%+v expected=%+v", got, reader.row)
			}
			want := "GET /v1/packaging-runs/run%2Fodd%20%3F%25"
			if mode == "connect" {
				want = "POST " + packagingv1connect.PackagingRunServiceGetPackagingRunProcedure
			}
			mu.Lock()
			defer mu.Unlock()
			if len(methods) != 1 || methods[0] != want || reader.reads.Load() != 1 {
				t.Fatalf("transport/replay changed: %v reads=%d", methods, reader.reads.Load())
			}
		})
	}
}

func TestRunGetPreservesNativeWorkerUnconfiguredResponse(t *testing.T) {
	for _, mode := range []string{"rest", "connect"} {
		t.Run(mode, func(t *testing.T) {
			mux := http.NewServeMux()
			registerDetail(mux, packaginghttp.Config{Authorize: func(*http.Request) error { return nil }})
			server := httptest.NewServer(mux)
			defer server.Close()
			out, err := detailCommand(t.Context(), server.URL, mode, "unconfigured")
			if err != nil {
				t.Fatal(err, out)
			}
			var got domain.RunStatus
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			expected := domain.RunStatus{Tier: "CONDUIT_NATIVE", Authority: "packaging-run", PlanAuthority: "conduit-plan", ApprovalAuthority: "conduit-governance"}
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("invented unconfigured state: %+v", got)
			}
		})
	}
}

func TestRunGetNativeRefusalsAndMissingRunNeverFallback(t *testing.T) {
	for _, mode := range []string{"rest", "connect"} {
		for _, status := range []int{401, 403, 404, 502} {
			t.Run(mode+http.StatusText(status), func(t *testing.T) {
				reader := &detailReader{row: detailFixture()}
				cfg := packaginghttp.Config{Reader: reader, Authorize: func(*http.Request) error { return nil }}
				if status == 401 || status == 403 {
					cfg.Authorize = func(*http.Request) error {
						return packaginghttp.StatusError{Status: status, Message: "fixture refusal"}
					}
				} else if status == 404 {
					reader.err = packaginghttp.ErrNotFound
				} else {
					reader.err = errors.New("reader unavailable")
				}
				mux := http.NewServeMux()
				registerDetail(mux, cfg)
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); mux.ServeHTTP(w, r) }))
				defer server.Close()
				out, err := detailCommand(t.Context(), server.URL, mode, reader.row.RunID)
				if err == nil || out != "" || calls.Load() != 1 {
					t.Fatalf("refusal was accepted/replayed: err=%v output=%s calls=%d", err, out, calls.Load())
				}
				if mode == "connect" {
					expected := map[int]connect.Code{401: connect.CodeUnauthenticated, 403: connect.CodePermissionDenied, 404: connect.CodeNotFound, 502: connect.CodeUnavailable}[status]
					if connect.CodeOf(err) != expected {
						t.Fatalf("wrong Connect refusal: %v", err)
					}
				} else if !strings.Contains(err.Error(), strconv.Itoa(status)) {
					t.Fatalf("wrong REST refusal: %v", err)
				}
				if status >= 404 && reader.reads.Load() != 1 {
					t.Fatal("request did not reach native reader")
				}
				if (status == 401 || status == 403) && reader.reads.Load() != 0 {
					t.Fatal("refused caller reached reader")
				}
			})
		}
	}
}

func TestRunGetRefusesMismatchedOrMalformedResponses(t *testing.T) {
	for _, mode := range []string{"rest", "connect"} {
		for _, fault := range []string{"id", "negative-reviews", "authority", "oversize", "malformed"} {
			t.Run(mode+fault, func(t *testing.T) {
				row := detailFixture()
				wire := &packagingv1.GetPackagingRunResponse{Tier: row.Tier, Configured: true, Authority: row.Authority, PlanAuthority: row.PlanAuthority, ApprovalAuthority: row.ApprovalAuthority, RunId: row.RunID}
				switch fault {
				case "id":
					row.RunID = "other"
					wire.RunId = "other"
				case "negative-reviews":
					row.DuplicateReviews = -1
					wire.DuplicateReviews = -1
				case "authority":
					row.ApprovalAuthority = "packaging-run"
					wire.ApprovalAuthority = "packaging-run"
				case "oversize":
					row.Reason = strings.Repeat("x", (1<<20)+1)
					wire.Reason = row.Reason
				}
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if mode == "connect" {
						w.Header().Set("Content-Type", "application/proto")
						if fault == "malformed" {
							_, _ = w.Write([]byte{0xff})
							return
						}
						b, _ := proto.Marshal(wire)
						_, _ = w.Write(b)
					} else {
						w.Header().Set("Content-Type", "application/json")
						if fault == "malformed" {
							_, _ = io.WriteString(w, "{")
							return
						}
						_ = json.NewEncoder(w).Encode(row)
					}
				}))
				defer server.Close()
				out, err := detailCommand(t.Context(), server.URL, mode, detailFixture().RunID)
				if err == nil || out != "" || calls.Load() != 1 {
					t.Fatalf("invalid response accepted/replayed: err=%v output-length=%d calls=%d", err, len(out), calls.Load())
				}
			})
		}
	}
}

func TestRunGetValidatesTransportAndIDBeforeRequest(t *testing.T) {
	for _, tc := range []struct{ mode, id string }{{"unknown", "run-a"}, {"rest", ""}, {"connect", " padded "}} {
		t.Run(tc.mode+tc.id, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer server.Close()
			out, err := detailCommand(t.Context(), server.URL, tc.mode, tc.id)
			if err == nil || out != "" || calls.Load() != 0 {
				t.Fatalf("invalid input reached network: %v %q %d", err, out, calls.Load())
			}
		})
	}
}

func TestRunGetCancellationDoesNotReplay(t *testing.T) {
	for _, mode := range []string{"rest", "connect"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				cancel()
				<-r.Context().Done()
			}))
			defer server.Close()
			out, err := detailCommand(ctx, server.URL, mode, "run-a")
			if err == nil || out != "" || calls.Load() != 1 {
				t.Fatalf("cancelled read accepted/replayed: %v %q %d", err, out, calls.Load())
			}
		})
	}
}

func TestRunGetUsesConfiguredTLSTrustAndBearerIdentity(t *testing.T) {
	for _, mode := range []string{"rest", "connect"} {
		t.Run(mode, func(t *testing.T) {
			reader := &detailReader{row: detailFixture()}
			mux := http.NewServeMux()
			registerDetail(mux, packaginghttp.Config{Reader: reader, Authorize: func(r *http.Request) error {
				if r.Header.Get("Authorization") != "Bearer fixture-credential" || r.Header.Get("X-Platform-Subject") != "" {
					return errors.New("configured identity not preserved")
				}
				return nil
			}})
			server := httptest.NewTLSServer(mux)
			defer server.Close()
			token := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(token, []byte("fixture-credential"), 0600); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
			root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "hub", server), "--bearer-token-file", token, "run", "get", reader.row.RunID, "--read-transport", mode})
			if err := root.Execute(); err != nil {
				t.Fatal(err, output.String())
			}
			var got domain.RunStatus
			if err := json.Unmarshal(output.Bytes(), &got); err != nil || !reflect.DeepEqual(got, reader.row) {
				t.Fatal("TLS read lost native status", err, output.String())
			}
		})
	}
}

func TestRunGetConfiguredTimeoutDoesNotReplay(t *testing.T) {
	for _, mode := range []string{"rest", "connect"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				<-r.Context().Done()
			}))
			defer server.Close()
			out, err := detailCommand(t.Context(), server.URL, mode, "run-a", "--timeout", "1s")
			if err == nil || out != "" || calls.Load() != 1 {
				t.Fatalf("timeout accepted/replayed: %v %q %d", err, out, calls.Load())
			}
		})
	}
}
