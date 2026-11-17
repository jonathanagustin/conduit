package localcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseArgvDoesNotUseAShell(t *testing.T) {
	argv, err := ParseArgv(`tool exec --flag "quoted value" -`)
	if err != nil || len(argv) != 5 || argv[3] != "quoted value" {
		t.Fatalf("argv = %#v %v", argv, err)
	}
	if _, err := ParseArgv(`tool $(echo hi)`); err == nil {
		t.Fatal("accepted shell syntax")
	}
	if _, err := ParseArgv(`tool "unterminated`); err == nil {
		t.Fatal("accepted an unclosed quote")
	}
}

func TestExecuteNeverStartsAHostCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "executed")
	for _, argv := range [][]string{{"/bin/sh", "-c", "touch \"$1\"", "fixture", marker}, nil, {"missing-model"}} {
		out, err := Execute(context.Background(), argv, time.Second, []byte("private prompt"))
		if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("host command executed before refusal: %v", statErr)
		}
		if !errors.Is(err, ErrLocalCommandUnsupported) || len(out) != 0 {
			t.Fatalf("out=%q err=%v", out, err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command ran: %v", err)
	}
}

func TestExecutorWrapperCompatibilityAndNilRefusal(t *testing.T) {
	var wrapped ExecFunc = func(context.Context, []string, time.Duration, []byte) ([]byte, error) { return []byte("wrapped"), nil }
	if out, _ := wrapped.Run(t.Context(), nil, time.Second, nil); string(out) != "wrapped" {
		t.Fatalf("wrapper unused: %q", out)
	}
	if _, err := ExecFunc(nil).Run(t.Context(), []string{"/bin/sh"}, time.Second, nil); !errors.Is(err, ErrLocalCommandUnsupported) {
		t.Fatalf("nil wrapper bypassed refusal: %v", err)
	}
}
