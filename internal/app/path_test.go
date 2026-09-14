package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootPathUsesAppRootEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APP_ROOT", root)
	if got := RootPath(); got != root {
		t.Fatalf("RootPath() = %q, want %q（APP_ROOT 优先级最高）", got, root)
	}
}

func TestRootFromExeDir(t *testing.T) {
	t.Run("with resources marker", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "resources"), 0755); err != nil {
			t.Fatal(err)
		}
		if got := rootFromExeDir(dir); got != dir {
			t.Fatalf("rootFromExeDir(%q) = %q, want %q", dir, got, dir)
		}
	})

	t.Run("without resources marker", func(t *testing.T) {
		dir := t.TempDir()
		if got := rootFromExeDir(dir); got != "" {
			t.Fatalf("rootFromExeDir(%q) = %q, want \"\"", dir, got)
		}
	})
}
