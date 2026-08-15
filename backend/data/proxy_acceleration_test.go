package data

import "testing"

func TestProxySpeedRejectsNilContext(t *testing.T) {
	speed, ok := TestProxySpeed(nil, "https://github.com/example/project/releases/download/v1/app", "")
	if ok || speed != 0 {
		t.Fatalf("TestProxySpeed(nil) = (%v, %v), want (0, false)", speed, ok)
	}

	proxy, selectedSpeed := SelectFastestProxy(nil, "https://github.com/example/project/releases/download/v1/app")
	if proxy != "" || selectedSpeed != 0 {
		t.Fatalf("SelectFastestProxy(nil) = (%q, %v), want empty fallback", proxy, selectedSpeed)
	}
}
