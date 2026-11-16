package devserver

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReservationLeavesTimeWaitBeforeLaunch(t *testing.T) {
	listeners, err := reservePorts(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer closePorts(listeners)
	addresses := []string{listeners[0].Addr().String(), listeners[1].Addr().String()}
	closePorts(listeners)
	for _, address := range addresses {
		var port int
		if _, err := fmt.Sscanf(address, "127.0.0.1:%d", &port); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("0100007F:%04X", port)
		deadline := time.Now().Add(time.Second)
		for {
			data, err := os.ReadFile("/proc/net/tcp")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) > 3 && fields[1] == want && fields[3] == "06" {
					found = true
					break
				}
			}
			if found {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("reserved server port %s has no TIME_WAIT protection", address)
			}
			time.Sleep(time.Millisecond)
		}
	}
}
