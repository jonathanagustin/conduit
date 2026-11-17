package domain

import "fmt"

const (
	ChartVersion = "0.1.0"
	// PythonRuntimeImage is the digest-pinned CPython 3.12 interpreter used
	// when the only source file is main.py. The sandbox pulls this digest
	// before the network-free image build.
	PythonRuntimeImage = "python:3.12-alpine@sha256:a61cbd76a882645de7f91423329fe43a67eed60d7da24f9e5331414bb3618b20"
	// AllowedSourceShapes is the complete set a generator may return.
	AllowedSourceShapes = "allowed source shapes are exactly one of main.go (language go), main.py (language python), or index.html (language html)"

	imageValueUnset = "image: \"\"\n"
	imagePinUnset   = "    # image-pin-unset\n"
)

// Shape is the one source file the image is built from.
type Shape string

const (
	ShapeGo     Shape = "go"
	ShapePython Shape = "python"
	ShapeHTML   Shape = "html"
)

// ShapeOf returns the single entrypoint after the allowlist passes.
func ShapeOf(spec Spec) (Shape, string, bool) {
	reason, ok := CheckAllowlist(spec)
	if !ok {
		return "", reason, false
	}
	paths := make([]string, 0, len(spec.Files))
	for _, file := range spec.Files {
		paths = append(paths, file.Path)
	}
	return shapeFromPaths(paths)
}

// ShapeFromPaths reports the entrypoint when the caller has names only.
func ShapeFromPaths(paths []string) (Shape, string, bool) {
	return shapeFromPaths(paths)
}

func shapeFromPaths(paths []string) (Shape, string, bool) {
	var shape Shape
	count := 0
	for _, path := range paths {
		switch path {
		case "main.go":
			shape = ShapeGo
			count++
		case "main.py":
			shape = ShapePython
			count++
		case "index.html":
			shape = ShapeHTML
			count++
		default:
			return "", fmt.Sprintf("path %s is not on the allowlist; %s", path, AllowedSourceShapes), false
		}
	}
	switch count {
	case 1:
		return shape, "source paths and languages are on the allowlist", true
	case 0:
		return "", AllowedSourceShapes, false
	default:
		return "", "source has more than one entrypoint; " + AllowedSourceShapes, false
	}
}
