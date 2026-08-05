package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserPathResolverPrefersConfiguredPath(t *testing.T) {
	t.Setenv("CHROME_BIN", "")
	detectCalls := 0
	resolver := newBrowserPathResolver(func() (string, bool) {
		detectCalls++
		return "/detected/browser", true
	})

	got := resolver.resolve(" /configured/browser ")
	if got != "/configured/browser" {
		t.Fatalf("resolve() = %q, want configured browser path", got)
	}
	if detectCalls != 0 {
		t.Fatalf("detect calls = %d, want 0", detectCalls)
	}
}

func TestBrowserPathResolverPrefersChromeBin(t *testing.T) {
	browserPath := filepath.Join(t.TempDir(), "chromium")
	if err := os.WriteFile(browserPath, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHROME_BIN", browserPath)
	detectCalls := 0
	resolver := newBrowserPathResolver(func() (string, bool) {
		detectCalls++
		return "/detected/browser", true
	})

	for i := 0; i < 2; i++ {
		if got := resolver.resolve(""); got != browserPath {
			t.Fatalf("resolve() = %q, want CHROME_BIN %q", got, browserPath)
		}
	}
	if detectCalls != 0 {
		t.Fatalf("detect calls = %d, want 0", detectCalls)
	}
}

func TestBrowserPathResolverCachesSystemDetection(t *testing.T) {
	t.Setenv("CHROME_BIN", "")
	detectCalls := 0
	resolver := newBrowserPathResolver(func() (string, bool) {
		detectCalls++
		return "/detected/browser", true
	})

	for i := 0; i < 2; i++ {
		if got := resolver.resolve(""); got != "/detected/browser" {
			t.Fatalf("resolve() = %q, want detected browser path", got)
		}
	}
	if detectCalls != 1 {
		t.Fatalf("detect calls = %d, want 1", detectCalls)
	}
}
