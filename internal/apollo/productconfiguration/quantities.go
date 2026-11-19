package productconfiguration

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

var memoryPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(Ki|K|Mi|M|Gi|G|T)?$`)
var cpuPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
var memoryScales = map[string]float64{
	"": 1, "K": 1e3, "M": 1e6, "G": 1e9, "T": 1e12,
	"Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30,
}

// ParseMemory converts a memory quantity to a comparable magnitude. The
// result is only used for ordering comparisons, never round-tripped back into a
// document, so float imprecision cannot corrupt a declared value.
func ParseMemory(value string) (float64, bool) {
	match := memoryPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, false
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	magnitude := number * memoryScales[match[2]]
	if math.IsInf(magnitude, 0) {
		return 0, false
	}
	return magnitude, true
}

func ValidateResourceQuantities(
	path string, quantities *ResourceQuantities, errs *[]validationdomain.Problem,
) (float64, float64) {
	if quantities == nil {
		return 0, 0
	}
	add := func(field, message string) {
		*errs = append(*errs, validationdomain.Problem{Path: path + field, Message: message})
	}
	var cpu, memory float64
	if trimmed := strings.TrimSpace(quantities.CPU); trimmed != "" {
		parsed, err := strconv.ParseFloat(trimmed, 64)
		switch {
		case !cpuPattern.MatchString(trimmed) || err != nil:
			add(".cpu", "must be a unit-less number of cores")
		case parsed <= 0:
			add(".cpu", "must be greater than zero")
		default:
			cpu = parsed
		}
	}
	if trimmed := strings.TrimSpace(quantities.Memory); trimmed != "" {
		parsed, ok := ParseMemory(trimmed)
		switch {
		case !ok:
			add(".memory", "must be a number optionally suffixed Ki, K, Mi, M, Gi, G, or T")
		case parsed <= 0:
			add(".memory", "must be greater than zero")
		default:
			memory = parsed
		}
	}
	return cpu, memory
}
