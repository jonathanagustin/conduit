package uds

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"testing"
)

func gzipState(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestDeployedPackageGzipAndLegacyStateAgree(t *testing.T) {
	raw := []byte(`{"name":"fixture","digest":"sha256:fixture","generation":7,"data":{"metadata":{"version":"1.2.3"}},"deployedComponents":[{"name":"runtime","status":"Succeeded"}]}`)
	for _, data := range [][]byte{raw, gzipState(t, raw)} {
		got, err := parseDeployedPackage([]byte(base64.StdEncoding.EncodeToString(data)))
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "fixture" || got.Digest != "sha256:fixture" || got.Generation != 7 || got.Data.Metadata.Version != "1.2.3" || len(got.DeployedComponents) != 1 || got.DeployedComponents[0].Status != "Succeeded" {
			t.Fatalf("state identity changed: %#v", got)
		}
	}
}

func TestDeployedPackageRejectsCorruptAndOversizedGzip(t *testing.T) {
	good := gzipState(t, []byte(`{"name":"fixture"}`))
	corrupt := append([]byte(nil), good...)
	corrupt[len(corrupt)-5] ^= 1
	for name, data := range map[string][]byte{
		"truncated": good[:len(good)-3], "checksum": corrupt,
		"oversized":     gzipState(t, []byte(strings.Repeat(" ", (16<<20)+1))),
		"trailing-json": gzipState(t, []byte(`{"name":"fixture"} {"name":"other"}`)),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseDeployedPackage([]byte(base64.StdEncoding.EncodeToString(data)))
			if err == nil || got != nil {
				t.Fatalf("invalid state accepted: %#v %v", got, err)
			}
		})
	}
}
