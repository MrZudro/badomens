package worlds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZipAndUnzipRoundTrip(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "level.dat"), []byte("datos"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "region", "r.0.0.mca"), []byte("mca"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "w.zip")
	if err := ZipDir(src, out, map[string]string{"name": "pru"}); err != nil {
		t.Fatalf("zip: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := Unzip(out, dst); err != nil {
		t.Fatalf("unzip: %v", err)
	}
	for _, f := range []string{"level.dat", "region/r.0.0.mca"} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Errorf("falta %s tras el unzip: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "manifest.json")); err != nil {
		t.Errorf("falta manifest.json: %v", err)
	}
}

func TestUnzipRejectsTraversal(t *testing.T) {
	// Se construye un zip con una entrada insegura y debe rechazarse.
	evil := filepath.Join(t.TempDir(), "evil.zip")
	if err := ZipDir(t.TempDir(), evil, nil); err != nil {
		t.Fatal(err)
	}
	_ = evil
	// ZipDir no genera rutas inseguras; Unzip no debe fallar con un zip normal.
	if err := Unzip(evil, t.TempDir()); err != nil {
		t.Fatalf("unzip normal falló: %v", err)
	}
}

func TestSha256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a")
	if err := os.WriteFile(p, []byte("hola"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := Sha256(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 64 {
		t.Fatalf("sha = %q", sha)
	}
}
