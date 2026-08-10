//go:build web

package runtimepath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootDirUsesWorkingDirectoryInWebMode(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve working directory: %v", err)
	}
	want := filepath.Clean(wd)
	if got := filepath.Clean(RootDir()); got != want {
		t.Fatalf("RootDir() = %q, want working directory %q", got, want)
	}
}
