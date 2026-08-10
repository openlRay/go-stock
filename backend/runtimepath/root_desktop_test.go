//go:build !web

package runtimepath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootDirUsesExecutableDirectoryInDesktopMode(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	want := filepath.Clean(filepath.Dir(executable))
	if got := filepath.Clean(RootDir()); got != want {
		t.Fatalf("RootDir() = %q, want executable directory %q", got, want)
	}
}
