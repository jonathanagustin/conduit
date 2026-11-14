package sandbox

import "testing"

func TestBridgeFailureFallsBackOnlyForBridgeErrors(t *testing.T) {
	if !bridgeUnavailable("failed to create bridge network") {
		t.Fatal("bridge error was not recognized")
	}
	if !bridgeUnavailable("iptables: No chain/target/match") {
		t.Fatal("iptables error was not recognized")
	}
	if bridgeUnavailable("compile failed") {
		t.Fatal("a compile failure looked like a bridge failure")
	}
}

func TestMemoryFlagUsesMebibytes(t *testing.T) {
	if memoryFlag("64Mi") != "64m" || memoryFlag("1Gi") != "1024m" {
		t.Fatalf("64Mi=%s 1Gi=%s", memoryFlag("64Mi"), memoryFlag("1Gi"))
	}
}

func TestHostPortParser(t *testing.T) {
	port, ok := hostPortOf("127.0.0.1:34567\n")
	if !ok || port != 34567 {
		t.Fatalf("port = %d %v", port, ok)
	}
	if _, ok := hostPortOf("not a port"); ok {
		t.Fatal("accepted a bad port line")
	}
}

func TestHostPortFromPortsJSON(t *testing.T) {
	port, ok := hostPortFromPortsJSON([]byte(`{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"34567"}]}`), 8080)
	if !ok || port != 34567 {
		t.Fatalf("port = %d %v", port, ok)
	}
	if _, ok := hostPortFromPortsJSON([]byte(`{}`), 8080); ok {
		t.Fatal("empty Ports looked published")
	}
	if _, ok := hostPortFromPortsJSON([]byte("null"), 8080); ok {
		t.Fatal("null Ports looked published")
	}
}

func TestContainerIPFromInspect(t *testing.T) {
	if ip := containerIPFromInspect([]byte(`{"IPAddress":"172.17.0.2","Networks":{}}`)); ip != "172.17.0.2" {
		t.Fatalf("IPAddress = %q", ip)
	}
	if ip := containerIPFromInspect([]byte(`{"IPAddress":"","Networks":{"bridge":{"IPAddress":"10.0.0.8"}}}`)); ip != "10.0.0.8" {
		t.Fatalf("network IP = %q", ip)
	}
	empty := containerIPFromInspect([]byte(`{"IPAddress":"","Networks":{"bridge":{"IPAddress":"","NetworkID":""}}}`))
	if empty != "" {
		t.Fatalf("empty inspect produced %q", empty)
	}
}
