package domain

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderPodIsLeastPrivilege(t *testing.T) {
	documents, err := RenderPod(PodRequest{Name: "ws-1", Namespace: "workspaces"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(documents.Pod) + string(documents.NetworkPolicy))
	for _, want := range []string{`"runasnonroot":true`, `"readonlyrootfilesystem":true`, `"automountserviceaccounttoken":false`, `"cpu":"200m"`, `"memory":"512mi"`, `"drop":["all"]`} {
		if !strings.Contains(text, want) {
			t.Fatalf("pod missing %s\n%s", want, text)
		}
	}
	if bytes.Contains([]byte(text), []byte("cluster-admin")) {
		t.Fatal("pod grants cluster-admin")
	}
}

func TestConfineRejectsEscape(t *testing.T) {
	for _, path := range []string{"../etc/passwd", "/etc/passwd", `..\secret`, "foo/../../etc/passwd"} {
		if _, err := Confine(path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	got, err := Confine("src/main.txt")
	if err != nil || got != "src/main.txt" {
		t.Fatalf("confine = %s %v", got, err)
	}
}
