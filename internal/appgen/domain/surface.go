package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Surface is the part of a spec that a repair may not change when the
// generation implements an accepted plan: which source shape it is, the port
// it listens on, and the routes it serves.
//
// An accepted plan is prose, so the Hub cannot check that a reply implements
// it. It can check that a repaired reply is still the same application as
// the reply it corrects. Without that, a reply that swaps the language or
// drops a route to get past a failing gate would be accepted because it
// builds.
type Surface struct {
	Shape  Shape
	Port   int
	Routes []string
}

// SurfaceOf returns the surface of a spec whose source shape is one of the
// allowed ones. It reports false for a spec that fails the allowlist, which
// has no shape to compare.
func SurfaceOf(spec Spec) (Surface, bool) {
	shape, _, ok := ShapeOf(spec)
	if !ok {
		return Surface{}, false
	}
	routes := make([]string, 0, len(spec.Routes))
	for _, route := range spec.Routes {
		routes = append(routes, strings.ToUpper(strings.TrimSpace(route.Method))+" "+strings.TrimSpace(route.Path))
	}
	sort.Strings(routes)
	return Surface{Shape: shape, Port: spec.Port, Routes: routes}, true
}

// Drift says how next differs from the surface, or "" when it does not.
func (surface Surface) Drift(next Surface) string {
	if surface.Shape != next.Shape {
		return fmt.Sprintf("source shape changed from %s to %s", surface.Shape, next.Shape)
	}
	if surface.Port != next.Port {
		return fmt.Sprintf("port changed from %d to %d", surface.Port, next.Port)
	}
	if strings.Join(surface.Routes, "\n") != strings.Join(next.Routes, "\n") {
		return fmt.Sprintf("routes changed from [%s] to [%s]", strings.Join(surface.Routes, ", "), strings.Join(next.Routes, ", "))
	}
	return ""
}
