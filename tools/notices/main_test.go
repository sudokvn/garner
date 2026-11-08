package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestNativeNoticesMatchDistributedLibrary(t *testing.T) {
	library := []byte("native library fixture")
	sum := fmt.Sprintf("%x", sha256.Sum256(library))
	pins := "TURSOGO_VERSION=v0.0.1\nPLATFORM_LIBS_VERSION=v0.0.1\nTURSO_VERSION=v0.0.1\nTURSO_COMMIT=fixture\nRUST_VERSION=1.88.0\nLIBRARY=library.so\nLIBRARY_SHA256=" + sum + "\n"
	inventory := "Turso v0.0.1 (commit fixture) with Rust 1.88.0.\nReviewed library SHA256: " + sum + "\n"

	for _, tc := range []struct {
		name      string
		version   string
		pins      string
		library   []byte
		inventory string
		wantError bool
	}{
		{"matching", "v0.0.1", pins, library, inventory, false},
		{"changed module", "v0.0.2", pins, library, inventory, true},
		{"changed binary", "v0.0.1", pins, []byte("different native code"), inventory, true},
		{"stale pin", "v0.0.1", strings.ReplaceAll(pins, sum, "stale"), library, inventory, true},
		{"stale inventory", "v0.0.1", pins, library, strings.ReplaceAll(inventory, sum, "unreviewed"), true},
		{"wrong source", "v0.0.1", pins, library, strings.ReplaceAll(inventory, "commit fixture", "commit other"), true},
		{"wrong Rust version", "v0.0.1", pins, library, strings.ReplaceAll(inventory, "1.88.0.", "1.88.01."), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.MkdirAll("tools/turso", 0o755); err != nil {
				t.Fatal(err)
			}
			for path, content := range map[string][]byte{
				tursoPins: []byte(tc.pins), tursoNotices: []byte(tc.inventory), "library.so": tc.library,
			} {
				if err := os.WriteFile(path, content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := turso(map[string]*module{
				tursoGo:       {version: tc.version},
				tursoPlatform: {version: "v0.0.1", dir: "."},
			})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error: %v", err, tc.wantError)
			}
		})
	}
}
