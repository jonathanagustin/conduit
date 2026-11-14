package sandbox

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

func TestDockerBuildAndHealthOfTheRecordedApp(t *testing.T) {
	if os.Getenv("CONDUIT_APPGEN_DOCKER") != "1" {
		t.Skip("set CONDUIT_APPGEN_DOCKER=1 to build the recorded application")
	}
	spec, _, err := (recorded.Adapter{}).Generate(context.Background(), domain.Request{Name: "hello-http", Description: "a small page"})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := domain.Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	input := application.BuildRequest{Spec: spec, Dockerfile: rendered.Dockerfile, Sources: map[string][]byte{"main.go": []byte(spec.Files[0].Content)}}
	built, err := Docker{}.Build(ctx, input)
	if err != nil || !built.Passed {
		t.Fatalf("build = %#v %v", built, err)
	}
	health, err := Docker{}.Check(ctx, built, input)
	if err != nil || !health.Passed {
		t.Fatalf("health = %#v %v", health, err)
	}
}
