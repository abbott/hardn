package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestReadFileIfThere: a file's bytes; a file that is not there is ok false
// with no error; any other failure is the read's error.
func TestReadFileIfThere(t *testing.T) {
	ctx, r, dir := context.Background(), NewReal(Options{}), t.TempDir()
	name := filepath.Join(dir, "a.conf")
	if data, ok, err := ReadFileIfThere(ctx, r, name); data != nil || ok || err != nil {
		t.Fatalf("no file: %q, %v, %v", data, ok, err)
	}
	if err := os.WriteFile(name, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, ok, err := ReadFileIfThere(ctx, r, name); string(data) != "x\n" || !ok || err != nil {
		t.Fatalf("a file: %q, %v, %v", data, ok, err)
	}
	if data, ok, err := ReadFileIfThere(ctx, r, dir); data != nil || ok || err == nil {
		t.Fatalf("a directory: %q, %v, %v; want an error", data, ok, err)
	}
}
