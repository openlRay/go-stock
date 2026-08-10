package runtimepath

import (
	"os"
	"path/filepath"
)

func workingDirectory() (string, bool) {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "", false
	}
	return wd, true
}

func executableDirectory() (string, bool) {
	executable, err := os.Executable()
	if err != nil || executable == "" {
		return "", false
	}
	return filepath.Dir(executable), true
}
