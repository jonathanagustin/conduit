package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestGeoServerDiscoveryFixture(t *testing.T) {
	// Synthetic acceptance fixture informed by official GeoServer docs; not a blind evaluation.
	text := "FROM docker.osgeo.org/geoserver:3.0.0\nEXPOSE 8080\nVOLUME [\"/opt/geoserver_data\"]\nENV INSTALL_EXTENSIONS=true ADMIN_PASSWORD=do-not-emit\nRUN curl https://example.invalid/plugin\n"
	a := DiscoverDockerfile("Dockerfile", text)
	b := DiscoverDockerfile("Dockerfile", text)
	if !reflect.DeepEqual(a, b) || a.Status != "needs-assessment" || len(a.Facts) != 3 {
		t.Fatalf("unexpected discovery: %#v", a)
	}
	if a.Facts[1].Value != "8080" || a.Facts[1].Line != 2 || a.Facts[2].Value != "/opt/geoserver_data" {
		t.Fatal("incorrect evidence")
	}
	for _, f := range a.Facts {
		if strings.Contains(f.Value, "do-not-emit") || strings.Contains(f.Value, "Postgres") || strings.Contains(f.Value, "healthz") {
			t.Fatal("invented or secret fact")
		}
	}
}
func TestDiscoveryDoesNotInventRuntimeGuarantees(t *testing.T) {
	d := DiscoverDockerfile("Dockerfile", "FROM ${BASE}\nEXPOSE 0 65536 ${PORT}\nVOLUME ${DATA}\nENV RUN_UNPRIVILEGED=true\n")
	if len(d.Facts) != 0 || len(d.Warnings) < 5 {
		t.Fatalf("unresolved values accepted: %#v", d)
	}
	d = DiscoverDockerfile("Dockerfile", "FROM base AS build\nEXPOSE 9000\nFROM runtime\nEXPOSE 8080\n")
	if d.Facts[1].Stage != 1 || d.Facts[3].Stage != 2 {
		t.Fatal("stages lost")
	}
}

func TestContinuationIsNotAnInstruction(t *testing.T) {
	d := DiscoverDockerfile("Dockerfile", "FROM base\nRUN echo \\\nEXPOSE 1234\nEXPOSE 8080\n")
	if len(d.Facts) != 2 || d.Facts[1].Value != "8080" {
		t.Fatalf("interpreted continuation: %#v", d)
	}
	for _, s := range []string{"# escape=`\nFROM base\nEXPOSE 8080", "FROM base\nRUN <<EOF\nEXPOSE 1234\nEOF"} {
		if len(DiscoverDockerfile("Dockerfile", s).Facts) != 0 {
			t.Fatal("interpreted unsupported syntax")
		}
	}
}

func TestContinuedFromPreservesStageAndEscapeVariantsFailClosed(t *testing.T) {
	d := DiscoverDockerfile("Dockerfile", "FROM base\nFROM runtime AS \\\n final\nEXPOSE 8080\n")
	if len(d.Facts) != 2 || d.Facts[1].Stage != 2 {
		t.Fatalf("wrong stage: %#v", d)
	}
	for _, directive := range []string{"#escape=`", "# escape = `", "# ESCAPE=`"} {
		if len(DiscoverDockerfile("Dockerfile", directive+"\nFROM base\nRUN echo `\nEXPOSE 1234").Facts) != 0 {
			t.Fatal("alternate escape accepted")
		}
	}
}
