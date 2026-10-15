package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins/download"
)

// testPolicy es la política de las pruebas de camino feliz: los servidores de
// httptest escuchan en 127.0.0.1 y sirven http://, así que se habilitan loopback
// y http EXPLÍCITAMENTE. La política por defecto (sin excepciones) se prueba
// aparte en los casos de SSRF y de TLS.
func testPolicy() download.Policy {
	pol := download.DefaultPolicy()
	pol.AllowHTTP = true
	pol.AllowLoopback = true
	return pol
}

// testInstaller prepara un instalador y una instancia paper sobre un data dir temporal.
func testInstaller(t *testing.T) (*Installer, *instance.Spec) {
	t.Helper()
	inst := &Installer{DataDir: t.TempDir()}
	return inst, &instance.Spec{Slug: "demo", Engine: "paper"}
}

// jarMinimo construye un JAR válido mínimo en memoria.
func jarMinimo(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct {
		name string
		data string
	}{
		{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n"},
		{"plugin.yml", "name: Demo\nversion: 1.0\n"},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("create %s: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			t.Fatalf("write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// serveJar arranca un servidor que sirve el JAR indicado.
func serveJar(t *testing.T, jar []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jar)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// assertNoTemps verifica que no queda ningún archivo temporal o parcial.
func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".badomens-") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("quedó un archivo temporal/parcial: %s", e.Name())
		}
	}
}

// install es un atajo para instalar con la política de pruebas.
func install(t *testing.T, inst *Installer, spec *instance.Spec, source string, opts InstallOptions) (*Installed, error) {
	t.Helper()
	if opts.Download.MaxBytes == 0 && opts.Download.MaxRedirects == 0 {
		opts.Download = testPolicy()
	}
	return inst.Install(context.Background(), spec, source, opts)
}

// TestInstallCorrecto: instalación completa con archivo en destino, temporal
// eliminado y metadatos coherentes.
func TestInstallCorrecto(t *testing.T) {
	inst, spec := testInstaller(t)
	jar := jarMinimo(t)
	srv := serveJar(t, jar)
	sum := sha256.Sum256(jar)

	res, err := install(t, inst, spec, srv.URL+"/demo-1.0.jar", InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Name != "demo-1.0.jar" {
		t.Errorf("Name = %q", res.Name)
	}
	if res.Path != filepath.Join(inst.Dir(spec), "demo-1.0.jar") {
		t.Errorf("Path = %q", res.Path)
	}
	if res.Bytes != int64(len(jar)) {
		t.Errorf("Bytes = %d; se esperaban %d", res.Bytes, len(jar))
	}
	if res.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("SHA256 = %s", res.SHA256)
	}
	if res.Archive.Entries != 2 || !res.Archive.HasManifest || res.Archive.Descriptor != "plugin.yml" {
		t.Errorf("informe de archivo inesperado: %+v", res.Archive)
	}
	if res.ChecksumVerified {
		t.Error("ChecksumVerified = true sin checksum indicado")
	}

	raw, err := os.ReadFile(res.Path)
	if err != nil || !bytes.Equal(raw, jar) {
		t.Errorf("el archivo instalado no coincide con el origen (%v)", err)
	}
	list, err := inst.List(spec)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0] != "demo-1.0.jar" {
		t.Errorf("List = %v", list)
	}
	assertNoTemps(t, inst.Dir(spec))
}

// TestInstallAddCompatibilidad: Add sigue devolviendo el nombre final.
func TestInstallAddCompatibilidad(t *testing.T) {
	inst, spec := testInstaller(t)
	srv := serveJar(t, jarMinimo(t))
	// Add usa la política por defecto: http:// y loopback están bloqueados, así
	// que se instala desde un archivo local (vía avanzada) para no depender de red.
	src := filepath.Join(t.TempDir(), "local.jar")
	if err := os.WriteFile(src, jarMinimo(t), 0o644); err != nil {
		t.Fatal(err)
	}
	name, err := inst.Add(context.Background(), spec, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if name != "local.jar" {
		t.Errorf("Add devolvió %q", name)
	}
	// La política por defecto rechaza http:// también en Add.
	if _, err := inst.Add(context.Background(), spec, srv.URL+"/demo.jar"); err == nil {
		t.Error("Add debería rechazar http:// con la política por defecto")
	}
}

// TestInstallErroresHTTP: 404 y 500 dejan error tipado, sin archivo y sin resto.
func TestInstallErroresHTTP(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		inst, spec := testInstaller(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		t.Cleanup(srv.Close)

		_, err := install(t, inst, spec, srv.URL+"/demo.jar", InstallOptions{})
		if err == nil {
			t.Fatalf("HTTP %d: se esperaba error", code)
		}
		if !errx.Is(err, errx.CodeUnavailable) {
			t.Errorf("HTTP %d: se esperaba error tipado, hubo %v", code, err)
		}
		if errx.Details(err)["http_status"] != code {
			t.Errorf("HTTP %d: http_status = %v", code, errx.Details(err)["http_status"])
		}
		if _, serr := os.Stat(filepath.Join(inst.Dir(spec), "demo.jar")); serr == nil {
			t.Errorf("HTTP %d: no debería haber archivo final", code)
		}
		assertNoTemps(t, inst.Dir(spec))
	}
}

// TestInstallLimiteExcedido: se aborta y se limpia el temporal.
func TestInstallLimiteExcedido(t *testing.T) {
	inst, spec := testInstaller(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("A"), 1<<20))
	}))
	t.Cleanup(srv.Close)

	opts := InstallOptions{Download: testPolicy()}
	opts.Download.MaxBytes = 2048
	_, err := install(t, inst, spec, srv.URL+"/grande.jar", opts)
	if err == nil {
		t.Fatal("se esperaba error por tamaño")
	}
	if !errx.Is(err, errx.CodeCapacity) {
		t.Errorf("se esperaba error_capacity_exceeded, hubo %v", err)
	}
	if _, serr := os.Stat(filepath.Join(inst.Dir(spec), "grande.jar")); serr == nil {
		t.Error("el destino no debería existir")
	}
	assertNoTemps(t, inst.Dir(spec))
}

// TestInstallNombresInvalidos: el nombre final se valida (no se "sanea").
func TestInstallNombresInvalidos(t *testing.T) {
	inst, spec := testInstaller(t)
	srv := serveJar(t, jarMinimo(t))

	_, err := install(t, inst, spec, srv.URL+"/x.jar", InstallOptions{Name: "../evil.jar"})
	if err == nil {
		t.Error("se esperaba rechazo de ../evil.jar")
	} else if errx.Details(err)["reason"] != "nombre_invalido" {
		t.Errorf("reason = %v (%v)", errx.Details(err)["reason"], err)
	}
	for _, raw := range []string{srv.URL + "/a%2fb.jar", srv.URL + "/", srv.URL + "/plugin.txt"} {
		if _, err := install(t, inst, spec, raw, InstallOptions{}); err == nil {
			t.Errorf("Install(%q): se esperaba rechazo del nombre", raw)
		}
	}
	assertNoTemps(t, inst.Dir(spec))
}

// TestInstallJarInvalido: artefacto que no es un JAR/ZIP válido.
func TestInstallJarInvalido(t *testing.T) {
	inst, spec := testInstaller(t)
	basura := serveJar(t, bytes.Repeat([]byte("no soy un jar"), 32))
	if _, err := install(t, inst, spec, basura.URL+"/falso.jar", InstallOptions{}); err == nil {
		t.Error("se esperaba rechazo de un archivo que no es JAR/ZIP")
	} else if errx.Details(err)["reason"] != "artefacto_invalido" {
		t.Errorf("reason = %v (%v)", errx.Details(err)["reason"], err)
	}

	// JAR truncado (se corta el directorio central).
	jar := jarMinimo(t)
	trunc := serveJar(t, jar[:len(jar)-30])
	if _, err := install(t, inst, spec, trunc.URL+"/roto.jar", InstallOptions{}); err == nil {
		t.Error("se esperaba rechazo de un JAR truncado")
	}
	if _, serr := os.Stat(filepath.Join(inst.Dir(spec), "falso.jar")); serr == nil {
		t.Error("no debería haber destino final")
	}
	if _, serr := os.Stat(filepath.Join(inst.Dir(spec), "roto.jar")); serr == nil {
		t.Error("no debería haber destino final para el JAR truncado")
	}
	assertNoTemps(t, inst.Dir(spec))
}

// TestInstallChecksum: checksum incorrecto rechaza; correcto acepta.
func TestInstallChecksum(t *testing.T) {
	inst, spec := testInstaller(t)
	jar := jarMinimo(t)
	srv := serveJar(t, jar)
	sum := sha256.Sum256(jar)

	_, err := install(t, inst, spec, srv.URL+"/demo.jar", InstallOptions{SHA256: strings.Repeat("0", 64)})
	if err == nil {
		t.Fatal("se esperaba rechazo por checksum incorrecto")
	}
	if errx.Details(err)["reason"] != "checksum_no_coincide" {
		t.Errorf("reason = %v (%v)", errx.Details(err)["reason"], err)
	}
	if _, serr := os.Stat(filepath.Join(inst.Dir(spec), "demo.jar")); serr == nil {
		t.Error("no debería haber archivo final")
	}
	assertNoTemps(t, inst.Dir(spec))

	res, err := install(t, inst, spec, srv.URL+"/demo.jar", InstallOptions{SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatalf("con el checksum correcto se esperaba éxito: %v", err)
	}
	if !res.ChecksumVerified {
		t.Error("ChecksumVerified = false con checksum correcto")
	}
}

// TestInstallRechazaSSRF: la política por defecto bloquea destinos locales.
func TestInstallRechazaSSRF(t *testing.T) {
	inst, spec := testInstaller(t)
	urls := []string{
		"https://localhost/plugin.jar",
		"https://127.0.0.1/plugin.jar",
		"https://10.0.0.1/plugin.jar",
		"https://169.254.169.254/latest/meta-data/plugin.jar",
		"https://192.168.1.20/plugin.jar",
	}
	for _, raw := range urls {
		_, err := inst.Install(context.Background(), spec, raw, InstallOptions{})
		if err == nil {
			t.Errorf("Install(%q): se esperaba rechazo por SSRF", raw)
			continue
		}
		if errx.Details(err)["reason"] != "destino_no_permitido" {
			t.Errorf("Install(%q): reason = %v (%v)", raw, errx.Details(err)["reason"], err)
		}
	}
	entries, err := os.ReadDir(inst.Dir(spec))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("no debería haberse creado nada: %v", entries)
	}
}

// TestInstallRedireccionAPrivada: se valida cada salto de la redirección.
func TestInstallRedireccionAPrivada(t *testing.T) {
	inst, spec := testInstaller(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1:9/plugin.jar", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	_, err := install(t, inst, spec, srv.URL+"/plugin.jar", InstallOptions{})
	if err == nil {
		t.Fatal("se esperaba rechazo de la redirección a una IP privada")
	}
	if errx.Details(err)["reason"] != "destino_no_permitido" {
		t.Errorf("reason = %v (%v)", errx.Details(err)["reason"], err)
	}
	assertNoTemps(t, inst.Dir(spec))
}

// TestInstallHTTPRequierePolitica: http:// se rechaza por defecto.
func TestInstallHTTPRequierePolitica(t *testing.T) {
	inst, spec := testInstaller(t)
	srv := serveJar(t, jarMinimo(t))

	pol := download.DefaultPolicy()
	pol.AllowLoopback = true // loopback sí; lo que se prueba es el esquema
	_, err := inst.Install(context.Background(), spec, srv.URL+"/demo.jar", InstallOptions{Download: pol})
	if err == nil {
		t.Fatal("se esperaba rechazo de http://")
	}
	if errx.Details(err)["reason"] != "tls_requerido" {
		t.Errorf("reason = %v (%v)", errx.Details(err)["reason"], err)
	}

	if _, err := install(t, inst, spec, srv.URL+"/demo.jar", InstallOptions{}); err != nil {
		t.Fatalf("con política explícita se esperaba éxito: %v", err)
	}
}

// TestInstallConflictos: no se sobrescribe en silencio.
func TestInstallConflictos(t *testing.T) {
	inst, spec := testInstaller(t)
	srv := serveJar(t, jarMinimo(t))
	dir := inst.Dir(spec)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo.jar"), []byte("antiguo"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := install(t, inst, spec, srv.URL+"/demo.jar", InstallOptions{})
	if err == nil {
		t.Fatal("se esperaba error por archivo existente")
	}
	if errx.Details(err)["reason"] != "destino_existente" {
		t.Errorf("reason = %v (%v)", errx.Details(err)["reason"], err)
	}

	opts := InstallOptions{}
	opts.Download = testPolicy()
	opts.Download.Conflict = download.ConflictRename
	res, err := install(t, inst, spec, srv.URL+"/demo.jar", opts)
	if err != nil {
		t.Fatalf("con rename se esperaba éxito: %v", err)
	}
	if res.Name != "demo-2.jar" || res.RenamedFrom != "demo.jar" {
		t.Errorf("resultado inesperado: %+v", res)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "demo.jar")); err != nil || string(raw) != "antiguo" { // #nosec G304 -- ruta de prueba dentro de t.TempDir()
		t.Errorf("el archivo original se alteró (%v)", err)
	}

	opts2 := InstallOptions{}
	opts2.Download = testPolicy()
	opts2.Download.Conflict = download.ConflictReplace
	res2, err := install(t, inst, spec, srv.URL+"/demo.jar", opts2)
	if err != nil {
		t.Fatalf("con replace se esperaba éxito: %v", err)
	}
	if !res2.Replaced {
		t.Errorf("se esperaba Replaced=true: %+v", res2)
	}
	assertNoTemps(t, dir)
}

// TestInstallArchivoLocal cubre la vía local (válida, inexistente y con
// extensión no permitida).
func TestInstallArchivoLocal(t *testing.T) {
	inst, spec := testInstaller(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "mod-local.jar")
	if err := os.WriteFile(src, jarMinimo(t), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := inst.Install(context.Background(), spec, src, InstallOptions{})
	if err != nil {
		t.Fatalf("Install local: %v", err)
	}
	if res.Name != "mod-local.jar" {
		t.Errorf("Name = %q", res.Name)
	}

	if _, err := inst.Install(context.Background(), spec, filepath.Join(dir, "no-existe.jar"), InstallOptions{}); err == nil {
		t.Error("se esperaba error por archivo inexistente")
	} else if !errx.Is(err, errx.CodeNotFound) {
		t.Errorf("se esperaba error_not_found, hubo %v", err)
	}

	txt := filepath.Join(dir, "notas.txt")
	if err := os.WriteFile(txt, []byte("hola"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.Install(context.Background(), spec, txt, InstallOptions{}); err == nil {
		t.Error("se esperaba rechazo de .txt")
	}
	assertNoTemps(t, inst.Dir(spec))
}

// TestInstallRechazaVanillaYEsquemas: motor sin soporte y esquemas no admitidos.
func TestInstallRechazaVanillaYEsquemas(t *testing.T) {
	inst := &Installer{DataDir: t.TempDir()}
	vanilla := &instance.Spec{Slug: "demo", Engine: "vanilla"}
	if _, err := inst.Install(context.Background(), vanilla, "https://ejemplo.com/x.jar", InstallOptions{}); err == nil {
		t.Error("se esperaba rechazo con motor vanilla")
	} else if !errx.Is(err, errx.CodeUsage) {
		t.Errorf("se esperaba error_usage, hubo %v", err)
	}
	paper := &instance.Spec{Slug: "demo", Engine: "paper"}
	if _, err := inst.Install(context.Background(), paper, "ftp://ejemplo.com/x.jar", InstallOptions{}); err == nil {
		t.Error("se esperaba rechazo de ftp://")
	}
	if _, err := inst.Install(context.Background(), nil, "https://ejemplo.com/x.jar", InstallOptions{}); err == nil {
		t.Error("se esperaba error sin instancia")
	}
}

// TestThirdPartyWarning: el aviso existe y no promete garantías.
func TestThirdPartyWarning(t *testing.T) {
	w := ThirdPartyWarning()
	if !strings.Contains(w, "terceros") || !strings.Contains(w, "NO implica que sea seguro") {
		t.Errorf("aviso inesperado: %q", w)
	}
}

// TestListRemoveWarnFor cubre el resto del catálogo (sin tests antes).
func TestListRemoveWarnFor(t *testing.T) {
	inst, spec := testInstaller(t)
	dir := inst.Dir(spec)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"worldguard.jar", "a.jar"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err := inst.List(spec)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Strings(list)
	if len(list) != 2 || list[0] != "a.jar" || list[1] != "worldguard.jar" {
		t.Errorf("List = %v", list)
	}
	if w := WarnFor(list); !strings.Contains(w, "worldguard.jar") {
		t.Errorf("WarnFor = %q", w)
	}
	if err := inst.Remove(spec, "../evil.jar"); err == nil {
		t.Error("Remove debería rechazar nombres con ..")
	}
	if err := inst.Remove(spec, "a.jar"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := inst.Remove(spec, "a.jar"); err == nil {
		t.Error("se esperaba error al borrar dos veces")
	} else if !errx.Is(err, errx.CodeNotFound) {
		t.Errorf("se esperaba error_not_found, hubo %v", err)
	}
	if _, err := inst.List(&instance.Spec{Slug: "otra", Engine: "vanilla"}); err != nil {
		t.Errorf("List en vanilla: %v", err)
	}
}

// TestInstallLimpiaTemporalesHuerfanos: un temporal abandonado por una ejecución
// interrumpida no sobrevive a la siguiente instalación ni aparece en el listado.
func TestInstallLimpiaTemporalesHuerfanos(t *testing.T) {
	inst, spec := testInstaller(t)
	dir := inst.Dir(spec)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	huerfano := filepath.Join(dir, ".badomens-download-huerfano.tmp")
	if err := os.WriteFile(huerfano, []byte("resto"), 0o644); err != nil {
		t.Fatal(err)
	}
	viejo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(huerfano, viejo, viejo); err != nil {
		t.Fatal(err)
	}
	// Un temporal reciente (descarga en curso en otro proceso) no se toca.
	reciente := filepath.Join(dir, ".badomens-download-reciente.tmp")
	if err := os.WriteFile(reciente, []byte("en curso"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := serveJar(t, jarMinimo(t))
	if _, err := install(t, inst, spec, srv.URL+"/ok.jar", InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, serr := os.Stat(huerfano); !errors.Is(serr, os.ErrNotExist) {
		t.Error("el temporal huérfano debería haberse limpiado")
	}
	if _, serr := os.Stat(reciente); serr != nil {
		t.Errorf("no debería borrarse un temporal reciente: %v", serr)
	}
	list, err := inst.List(spec)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0] != "ok.jar" {
		t.Errorf("List = %v; los temporales no deben listarse", list)
	}
}
