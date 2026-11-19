package productconfiguration

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

func TestMemoryScaleOverflowIsRejected(t *testing.T) {
	maximum := strconv.FormatFloat(math.MaxFloat64, 'f', -1, 64)
	if got, ok := ParseMemory(maximum); !ok || got != math.MaxFloat64 {
		t.Fatalf("finite numeric part was rejected: %v %v", got, ok)
	}
	for _, unit := range []string{"K", "Ki", "M", "Mi", "G", "Gi", "T"} {
		if got, ok := ParseMemory(maximum + unit); ok || got != 0 {
			t.Fatalf("scaled overflow %s accepted as %v", unit, got)
		}
	}
	// This is a representation-safety check, not a physical capacity bound.
	if got, ok := ParseMemory("1" + strings.Repeat("0", 296) + "T"); !ok || math.IsInf(got, 0) || got <= 0 {
		t.Fatalf("large finite scaled quantity rejected: %v %v", got, ok)
	}
}

func TestMemoryFiniteScalesAndGrammar(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  float64
	}{{"1", 1}, {"1K", 1e3}, {"1M", 1e6}, {"1G", 1e9}, {"1T", 1e12}, {"1Ki", 1024}, {"1Mi", 1 << 20}, {"1Gi", 1 << 30}, {"1.5Gi", 1.5 * (1 << 30)}, {" 0.5M ", 5e5}, {"0Gi", 0}} {
		if got, ok := ParseMemory(tt.value); !ok || got != tt.want {
			t.Fatalf("%q: %v %v want %v", tt.value, got, ok, tt.want)
		}
	}
	for _, s := range []string{"", "abc", "1Mib", "1 Gi", "-1", "1.2.3", "Gi", "1gi", "NaN", "Inf", strings.Repeat("9", 400)} {
		if got, ok := ParseMemory(s); ok {
			t.Fatalf("invalid quantity %q accepted as %v", s, got)
		}
	}
}

func TestResourceQuantityDiagnosticsAndOverflow(t *testing.T) {
	quantities := &ResourceQuantities{CPU: "1m", Memory: "unknown"}
	errs := []validationdomain.Problem{{Path: "previous", Message: "retained"}}
	cpu, memory := ValidateResourceQuantities("requests", quantities, &errs)
	want := []validationdomain.Problem{{Path: "previous", Message: "retained"}, {Path: "requests.cpu", Message: "must be a unit-less number of cores"}, {Path: "requests.memory", Message: "must be a number optionally suffixed Ki, K, Mi, M, Gi, G, or T"}}
	if cpu != 0 || memory != 0 || !reflect.DeepEqual(errs, want) || quantities.CPU != "1m" || quantities.Memory != "unknown" {
		t.Fatalf("diagnostics changed: %v %v %#v", cpu, memory, errs)
	}
	errs = nil
	cpu, memory = ValidateResourceQuantities("requests", nil, &errs)
	if cpu != 0 || memory != 0 || errs != nil {
		t.Fatal("nil quantities changed")
	}
	errs = nil
	ValidateResourceQuantities("limits", &ResourceQuantities{CPU: "0", Memory: "0"}, &errs)
	if !reflect.DeepEqual(errs, []validationdomain.Problem{{Path: "limits.cpu", Message: "must be greater than zero"}, {Path: "limits.memory", Message: "must be greater than zero"}}) {
		t.Fatalf("zero diagnostics: %#v", errs)
	}
	errs = nil
	for _, path := range []string{"requests", "limits"} {
		_, m := ValidateResourceQuantities(path, &ResourceQuantities{Memory: strings.Repeat("9", 300) + "T"}, &errs)
		if m != 0 {
			t.Fatalf("non-finite diagnostic returned magnitude %v", m)
		}
	}
	if len(errs) != 2 || errs[0].Path != "requests.memory" || errs[1].Path != "limits.memory" {
		t.Fatalf("overflow paths missing: %#v", errs)
	}
	errs = nil
	cpu, memory = ValidateResourceQuantities("valid", &ResourceQuantities{CPU: " 1.25 ", Memory: "2Gi"}, &errs)
	if cpu != 1.25 || memory != 2*(1<<30) || len(errs) != 0 {
		t.Fatalf("finite quantities changed: %v %v %#v", cpu, memory, errs)
	}
}
