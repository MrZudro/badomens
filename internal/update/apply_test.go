package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type cleanupFunc func()

// srvRelease monta un servidor que sirve la release y el paquete. Si
// checksumOverride != "" se sirve ese hash en lugar del correcto.
func srvRelease(t *testing.T, pkg []byte, asset, checksumOverride string) (*httptest.Server, cleanupFunc) {
	t.Helper()
	sum := checksumOverride
	if sum == "" {
		sum = sha256hex(pkg)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/releases", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]release{{
			TagName: "v1.2.3",
			Assets: []releaseAsset{
				{Name: asset, URL: srv.URL + "/" + asset},
				{Name: "checksums.txt", URL: srv.URL + "/checksums.txt"},
			},
			HTMLURL: srv.URL + "/releases",
		}})
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", sum, asset)
	})
	mux.HandleFunc("/"+asset, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(pkg) })
	restore := func() {
		releaseURL = func(repo, version, a string) string {
			return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", repo, version, a)
		}
		releaseAPI = func(repo string) string {
			return fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=1", repo)
		}
		currentExecutable = os.Executable
		srv.Close()
	}
	return srv, restore
}

func testApplyServer(t *testing.T, pkg []byte, asset, checksumOverride string, exe string) cleanupFunc {
	srv, cleanup := srvRelease(t, pkg, asset, checksumOverride)
	releaseURL = func(_, _, a string) string { return srv.URL + "/" + a }
	releaseAPI = func(_ string) string { return srv.URL + "/releases" }
	currentExecutable = func() (string, error) { return exe, nil }
	return cleanup
}

func packageForThisOS(t *testing.T) (asset string, pkg []byte) {
	t.Helper()
	asset = fmt.Sprintf("badomens_1.2.3_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".zip"
		pkg = zipPkg(t, "badomens.exe", "NEWBIN")
	} else {
		asset += ".tar.gz"
		pkg = tgzPkg(t, "badomens", "NEWBIN")
	}
	return asset, pkg
}

func TestApplyVerificaChecksumYReemplaza(t *testing.T) {
	asset, pkg := packageForThisOS(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "badomens")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}

	cleanup := testApplyServer(t, pkg, asset, "", exe)
	defer cleanup()

	res, err := Apply(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if res.To != "1.2.3" {
		t.Fatalf("destino: quiero 1.2.3, tengo %q", res.To)
	}
	got, err := os.ReadFile(exe) // #nosec G304 -- ruta de prueba dentro de TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEWBIN" {
		t.Fatalf("el binario no se reemplazó: %q", got)
	}
}

func TestApplyRechazaChecksumIncorrecto(t *testing.T) {
	asset, pkg := packageForThisOS(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "badomens")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}

	cleanup := testApplyServer(t, pkg, asset, "0000000000000000000000000000000000000000000000000000000000000000", exe)
	defer cleanup()

	res, err := Apply(context.Background(), "owner/repo")
	if err == nil {
		t.Fatalf("un checksum erróneo debe abortar, res=%+v", res)
	}
	got, _ := os.ReadFile(exe) // #nosec G304 -- ruta de prueba dentro de TempDir
	if string(got) != "OLD" {
		t.Fatalf("el binario no debe tocarse: %q", got)
	}
}

func TestExtractBinaryConceptos(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := extractBinary(zipPkg(t, "badomens.exe", "HOLA"), "x.zip"); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := extractBinary(tgzPkg(t, "badomens", "HOLA"), "x.tar.gz"); err != nil {
			t.Fatal(err)
		}
	}
}

func zipPkg(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(content))
	_ = zw.Close()
	return buf.Bytes()
}

func tgzPkg(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))})
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestCacheDue(t *testing.T) {
	now := time.Now()
	c := Cache{}
	if !c.Due(now, CheckTTL) {
		t.Fatal("sin caché debe ser debida")
	}
	c.LastCheck = now.Add(-CheckTTL)
	if !c.Due(now, CheckTTL) {
		t.Fatal("tras el TTL debe ser debida")
	}
	c.LastCheck = now
	if c.Due(now, CheckTTL) {
		t.Fatal("dentro del TTL no debe ser debida")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := Cache{LastCheck: time.Now(), Latest: "1.2.3", Skipped: "1.2.2"}
	if err := SaveCache(dir, c); err != nil {
		t.Fatal(err)
	}
	got := LoadCache(dir)
	if got.Latest != "1.2.3" || got.Skipped != "1.2.2" {
		t.Fatalf("caché ida y vuelta: %+v", got)
	}
}
