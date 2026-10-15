package download

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
)

// testPolicy es la polÃƒÂ­tica de las pruebas de camino feliz: httptest siempre
// escucha en 127.0.0.1 y sirve http://, asÃƒÂ­ que se habilita loopback y http
// EXPLÃƒÂCITAMENTE (la polÃƒÂ­tica por defecto los rechaza, y eso tambiÃƒÂ©n se prueba).
func testPolicy() Policy {
	pol := DefaultPolicy()
	pol.AllowHTTP = true
	pol.AllowLoopback = true
	return pol
}

// newServer arranca un servidor de prueba con el handler dado.
func newServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// serveJar sirve un JAR mÃƒÂ­nimo vÃƒÂ¡lido.
func serveJar(t *testing.T, name string) *httptest.Server {
	t.Helper()
	return newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if name != "" && !strings.HasSuffix(r.URL.Path, name) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/java-archive")
		_, _ = w.Write(minimalJar(t))
	})
}

// minimalJar construye un ZIP/JAR mÃƒÂ­nimo en memoria.
func minimalJar(t *testing.T) []byte {
	t.Helper()
	return zipBytes(t, map[string][]byte{
		"META-INF/MANIFEST.MF": []byte("Manifest-Version: 1.0\n"),
		"plugin.yml":           []byte("name: Demo\nversion: 1.0\n"),
	})
}

// assertNoTemps verifica que no ha quedado ningÃƒÂºn archivo temporal o parcial.
func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("no se pudo leer %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".badomens-") {
			t.Errorf("quedÃƒÂ³ un archivo temporal/parcial: %s", e.Name())
		}
	}
}

// assertNotExists comprueba que la ruta no existe.
func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("el archivo %s no deberÃƒÂ­a existir", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat(%s): %v", path, err)
	}
}

// TestNormalizeName cubre el rechazo de nombres inseguros: se rechazan, no se
// "sanean" (un nombre ambiguo es un error, no una adivinanza).
func TestNormalizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
		bad  bool
	}{
		{in: "plugin.jar", want: "plugin.jar"},
		{in: "mi-plugin-1.2.3.jar", want: "mi-plugin-1.2.3.jar"},
		{in: "datos.ZIP", want: "datos.ZIP"},
		{in: "../evil.jar", bad: true},
		{in: "a/b.jar", bad: true},
		{in: `a\b.jar`, bad: true},
		{in: "..", bad: true},
		{in: ".", bad: true},
		{in: "", bad: true},
		{in: "   ", bad: true},
		{in: ".hidden.jar", bad: true},
		{in: "plugin.txt", bad: true},
		{in: "plugin", bad: true},
		{in: "CON.jar", bad: true},
		{in: "lpt9.jar", bad: true},
		{in: "plugin.jar.", bad: true},
		{in: " plugin.jar", bad: true},
		{in: "plugin.jar ", bad: true},
		{in: "evil\x00.jar", bad: true},
		{in: "evil\x1b.jar", bad: true},
		{in: strings.Repeat("a", 200) + ".jar", bad: true},
	}
	for _, tc := range cases {
		got, err := NormalizeName(tc.in, nil)
		if tc.bad {
			if err == nil {
				t.Errorf("NormalizeName(%q) = %q; se esperaba rechazo", tc.in, got)
				continue
			}
			if !errx.Is(err, errx.CodeUsage) {
				t.Errorf("NormalizeName(%q): se esperaba error de uso, hubo %v", tc.in, err)
			}
			if errx.Details(err)["reason"] != "nombre_invalido" {
				t.Errorf("NormalizeName(%q): falta reason=nombre_invalido (%v)", tc.in, errx.Details(err))
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeName(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeName(%q) = %q; se esperaba %q", tc.in, got, tc.want)
		}
	}
}

// TestStageURLRechazaDestinosSSRF comprueba que la polÃƒÂ­tica por defecto rechaza
// loopback, redes privadas, link-local y metadatos de nube sin tocar la red.
func TestStageURLRechazaDestinosSSRF(t *testing.T) {
	dest := t.TempDir()
	urls := []string{
		"https://localhost/x.jar",
		"https://localhost./x.jar",
		"https://127.0.0.1/x.jar",
		"https://127.0.0.2:8080/x.jar",
		"https://10.0.0.1/x.jar",
		"https://172.16.5.5/x.jar",
		"https://192.168.1.10/x.jar",
		"https://169.254.169.254/latest/meta-data/x.jar",
		"https://0.0.0.0/x.jar",
		"https://[::1]/x.jar",
		"https://[fd00::1]/x.jar",
		"https://[fe80::1]/x.jar",
		"https://metadata.google.internal/computeMetadata/v1/x.jar",
		"https://maquina.internal/x.jar",
		"https://ruta.local/x.jar",
	}
	for _, raw := range urls {
		st, err := StageURL(context.Background(), Request{URL: raw, DestDir: dest, Policy: DefaultPolicy()})
		if err == nil {
			_ = st.Discard()
			t.Errorf("StageURL(%q): se esperaba rechazo por SSRF", raw)
			continue
		}
		if !errx.Is(err, errx.CodeUsage) {
			t.Errorf("StageURL(%q): se esperaba error de uso, hubo %v", raw, err)
		}
		details := errx.Details(err)
		if details["reason"] != "destino_no_permitido" {
			t.Errorf("StageURL(%q): falta reason=destino_no_permitido (%v)", raw, details)
		}
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("no deberÃƒÂ­a haberse creado nada en el destino: %v", entries)
	}
}

// TestControlBloqueaIPResuelta cubre la barrera frente a DNS rebinding: se
// valida la IP realmente resuelta (la que ve el dialer), no solo el hostname.
func TestControlBloqueaIPResuelta(t *testing.T) {
	blocked := []string{"10.0.0.1:443", "127.0.0.1:80", "169.254.169.254:80", "[::1]:80", "[fc00::1]:443", "192.168.0.1:443"}
	pol := DefaultPolicy()
	for _, addr := range blocked {
		if err := pol.control(context.Background(), "tcp", addr, nil); err == nil {
			t.Errorf("control(%s) deberÃƒÂ­a bloquearse con la polÃƒÂ­tica por defecto", addr)
		}
	}
	if err := pol.control(context.Background(), "tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("control(IP pÃƒÂºblica) = %v; se esperaba permitido", err)
	}
	loop := DefaultPolicy()
	loop.AllowLoopback = true
	if err := loop.control(context.Background(), "tcp", "127.0.0.1:80", nil); err != nil {
		t.Errorf("control(127.0.0.1) con AllowLoopback = %v; se esperaba permitido", err)
	}
	if err := loop.control(context.Background(), "tcp", "10.0.0.1:80", nil); err == nil {
		t.Error("AllowLoopback no debe permitir redes privadas")
	}
	priv := DefaultPolicy()
	priv.AllowPrivate = true
	for _, addr := range blocked {
		if err := priv.control(context.Background(), "tcp", addr, nil); err != nil {
			t.Errorf("control(%s) con AllowPrivate = %v; se esperaba permitido", addr, err)
		}
	}
}

// TestStageURLRequiereHTTPS: http:// se rechaza salvo polÃƒÂ­tica explÃƒÂ­cita.
func TestStageURLRequiereHTTPS(t *testing.T) {
	srv := serveJar(t, "plugin.jar")
	dest := t.TempDir()
	pol := DefaultPolicy()
	pol.AllowLoopback = true // loopback permitido: lo que se prueba es el esquema

	_, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest, Policy: pol})
	if err == nil {
		t.Fatal("se esperaba rechazo de http:// sin polÃƒÂ­tica explÃƒÂ­cita")
	}
	if errx.Details(err)["reason"] != "tls_requerido" {
		t.Errorf("reason = %v; se esperaba tls_requerido (%v)", errx.Details(err)["reason"], err)
	}

	pol.AllowHTTP = true
	st, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest, Policy: pol})
	if err != nil {
		t.Fatalf("con AllowHTTP se esperaba ÃƒÂ©xito: %v", err)
	}
	defer func() { _ = st.Discard() }()
	if _, err := st.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "plugin.jar")); err != nil {
		t.Fatalf("el archivo no estÃƒÂ¡ en el destino: %v", err)
	}
	assertNoTemps(t, dest)
}

// TestStageURLErroresHTTP: 404 y 500 producen error tipado y no dejan nada.
func TestStageURLErroresHTTP(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusForbidden} {
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		})
		dest := t.TempDir()
		_, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest, Policy: testPolicy()})
		if err == nil {
			t.Fatalf("HTTP %d: se esperaba error", code)
		}
		if !errx.Is(err, errx.CodeUnavailable) {
			t.Errorf("HTTP %d: se esperaba error tipado de servicio no disponible, hubo %v", code, err)
		}
		if got := errx.Details(err)["http_status"]; got != code {
			t.Errorf("HTTP %d: http_status = %v", code, got)
		}
		assertNotExists(t, filepath.Join(dest, "plugin.jar"))
		assertNoTemps(t, dest)
	}
}

// TestStageURLAbortaSiSuperaElLimite: el lÃƒÂ­mite se aplica DURANTE la descarga y
// el temporal se limpia.
func TestStageURLAbortaSiSuperaElLimite(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("A"), 512*1024))
	})
	dest := t.TempDir()
	pol := testPolicy()
	pol.MaxBytes = 4096

	_, err := StageURL(context.Background(), Request{URL: srv.URL + "/grande.jar", DestDir: dest, Policy: pol})
	if err == nil {
		t.Fatal("se esperaba error por superar el lÃƒÂ­mite")
	}
	if !errx.Is(err, errx.CodeCapacity) {
		t.Errorf("se esperaba error_capacity_exceeded, hubo %v", err)
	}
	if errx.Details(err)["reason"] != "limite_excedido" {
		t.Errorf("reason = %v", errx.Details(err)["reason"])
	}
	if errx.Details(err)["limit_bytes"] != int64(4096) {
		t.Errorf("limit_bytes = %v", errx.Details(err)["limit_bytes"])
	}
	assertNotExists(t, filepath.Join(dest, "grande.jar"))
	assertNoTemps(t, dest)
}

// TestStageURLContentLengthExcesivo: si el origen declara un tamaÃƒÂ±o mayor que
// el lÃƒÂ­mite se aborta antes de transferir.
func TestStageURLContentLengthExcesivo(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		_, _ = w.Write(bytes.Repeat([]byte("A"), 1024))
	})
	dest := t.TempDir()
	pol := testPolicy()
	pol.MaxBytes = 1024
	_, err := StageURL(context.Background(), Request{URL: srv.URL + "/grande.jar", DestDir: dest, Policy: pol})
	if err == nil || !errx.Is(err, errx.CodeCapacity) {
		t.Fatalf("se esperaba error de capacidad, hubo %v", err)
	}
	assertNoTemps(t, dest)
}

// TestStageURLRedireccionAPrivada: se valida CADA salto; una redirecciÃƒÂ³n a una
// IP privada se rechaza.
func TestStageURLRedireccionAPrivada(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1:9/plugin.jar", http.StatusFound)
	})
	dest := t.TempDir()
	_, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest, Policy: testPolicy()})
	if err == nil {
		t.Fatal("se esperaba rechazo de la redirecciÃƒÂ³n a una IP privada")
	}
	if errx.Details(err)["reason"] != "destino_no_permitido" {
		t.Errorf("reason = %v; se esperaba destino_no_permitido (%v)", errx.Details(err)["reason"], err)
	}
	assertNoTemps(t, dest)
}

// TestStageURLRedireccionAHTTPSDeshabilitado: una bajada de https a http sÃƒÂ­ se
// permite en las pruebas de loopback, pero el nÃƒÂºmero de saltos estÃƒÂ¡ acotado.
func TestStageURLLimiteDeRedirecciones(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/otra.jar", http.StatusFound)
	})
	dest := t.TempDir()
	pol := testPolicy()
	pol.MaxRedirects = 2
	_, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest, Policy: pol})
	if err == nil {
		t.Fatal("se esperaba error por exceso de redirecciones")
	}
	if errx.Details(err)["reason"] != "redirecciones_excesivas" {
		t.Errorf("reason = %v (%v)", errx.Details(err)["reason"], err)
	}
	assertNoTemps(t, dest)
}

// TestStageURLNombreInvalido: los nombres derivados de la URL tambiÃƒÂ©n se validan.
func TestStageURLNombreInvalido(t *testing.T) {
	srv := serveJar(t, "")
	dest := t.TempDir()
	urls := []string{
		srv.URL + "/%2e%2e%2fevil.jar", // ../evil.jar codificado
		srv.URL + "/a%2fb.jar",         // a/b.jar codificado
		srv.URL + "/",                  // sin nombre
		srv.URL + "/plugin.txt",        // extensiÃƒÂ³n no permitida
	}
	for _, raw := range urls {
		_, err := StageURL(context.Background(), Request{URL: raw, DestDir: dest, Policy: testPolicy()})
		if err == nil {
			t.Errorf("StageURL(%q): se esperaba rechazo del nombre", raw)
			continue
		}
		if !errx.Is(err, errx.CodeUsage) {
			t.Errorf("StageURL(%q): se esperaba error de uso, hubo %v", raw, err)
		}
	}
	_, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest, Name: "../evil.jar", Policy: testPolicy()})
	if err == nil {
		t.Error("se esperaba rechazo del nombre explÃƒÂ­cito ../evil.jar")
	}
	assertNoTemps(t, dest)
}

// TestStageURLChecksum: un checksum incorrecto rechaza la instalaciÃƒÂ³n; el
// correcto se acepta.
func TestStageURLChecksum(t *testing.T) {
	jar := minimalJar(t)
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jar)
	})
	sum := sha256.Sum256(jar)
	dest := t.TempDir()

	_, err := StageURL(context.Background(), Request{
		URL: srv.URL + "/plugin.jar", DestDir: dest, SHA256: strings.Repeat("0", 64), Policy: testPolicy(),
	})
	if err == nil {
		t.Fatal("se esperaba rechazo por checksum incorrecto")
	}
	if errx.Details(err)["reason"] != "checksum_no_coincide" {
		t.Errorf("reason = %v (%v)", errx.Details(err)["reason"], err)
	}
	assertNotExists(t, filepath.Join(dest, "plugin.jar"))
	assertNoTemps(t, dest)

	st, err := StageURL(context.Background(), Request{
		URL: srv.URL + "/plugin.jar", DestDir: dest, SHA256: strings.ToUpper(hex.EncodeToString(sum[:])), Policy: testPolicy(),
	})
	if err != nil {
		t.Fatalf("con el checksum correcto se esperaba ÃƒÂ©xito: %v", err)
	}
	if st.SHA256() != hex.EncodeToString(sum[:]) {
		t.Errorf("SHA256() = %s", st.SHA256())
	}
	res, err := st.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if res.Name != "plugin.jar" || res.Bytes != int64(len(jar)) {
		t.Errorf("resultado inesperado: %+v", res)
	}
	if res.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("SHA256 del resultado = %s", res.SHA256)
	}
}

// TestCommitNoSobrescribe: nunca se sobrescribe en silencio.
func TestCommitNoSobrescribe(t *testing.T) {
	jar := minimalJar(t)
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jar)
	})
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "plugin.jar"), []byte("antiguo"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest, Policy: testPolicy()})
	if err != nil {
		t.Fatalf("StageURL: %v", err)
	}
	if _, err := st.Commit(); err == nil {
		t.Error("se esperaba error por archivo existente")
	} else if errx.Details(err)["reason"] != "destino_existente" {
		t.Errorf("reason = %v", errx.Details(err)["reason"])
	}
	if err := st.Discard(); err != nil {
		t.Errorf("Discard: %v", err)
	}
	// El original sigue intacto.
	raw, err := os.ReadFile(filepath.Join(dest, "plugin.jar"))
	if err != nil || string(raw) != "antiguo" {
		t.Errorf("el archivo original se modificÃƒÂ³: %q (%v)", raw, err)
	}
	assertNoTemps(t, dest)
}

// TestCommitRenombraYReemplaza: las polÃƒÂ­ticas explÃƒÂ­citas de conflicto.
func TestCommitRenombraYReemplaza(t *testing.T) {
	jar := minimalJar(t)
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jar)
	})

	// rename
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "plugin.jar"), []byte("antiguo"), 0o644); err != nil {
		t.Fatal(err)
	}
	pol := testPolicy()
	pol.Conflict = ConflictRename
	st, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest, Policy: pol})
	if err != nil {
		t.Fatalf("StageURL: %v", err)
	}
	res, err := st.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if res.Name != "plugin-2.jar" || res.RenamedFrom != "plugin.jar" {
		t.Errorf("renombrado inesperado: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dest, "plugin-2.jar")); err != nil {
		t.Errorf("no se creÃƒÂ³ plugin-2.jar: %v", err)
	}
	assertNoTemps(t, dest)

	// replace
	dest2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest2, "plugin.jar"), []byte("antiguo"), 0o644); err != nil {
		t.Fatal(err)
	}
	pol2 := testPolicy()
	pol2.Conflict = ConflictReplace
	st2, err := StageURL(context.Background(), Request{URL: srv.URL + "/plugin.jar", DestDir: dest2, Policy: pol2})
	if err != nil {
		t.Fatalf("StageURL: %v", err)
	}
	res2, err := st2.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !res2.Replaced {
		t.Errorf("se esperaba Replaced=true: %+v", res2)
	}
	raw, err := os.ReadFile(filepath.Join(dest2, "plugin.jar"))
	if err != nil || !bytes.Equal(raw, jar) {
		t.Errorf("el archivo no se reemplazÃƒÂ³ correctamente (%v)", err)
	}
	assertNoTemps(t, dest2)
}

// TestStageFileLocal: la ruta local tambiÃƒÂ©n pasa por temporal, lÃƒÂ­mite y
// validaciÃƒÂ³n de nombre.
func TestStageFileLocal(t *testing.T) {
	dir := t.TempDir()
	jar := minimalJar(t)
	src := filepath.Join(dir, "origen.jar")
	if err := os.WriteFile(src, jar, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "destino")

	st, err := StageFile(src, Request{DestDir: dest, Policy: DefaultPolicy()})
	if err != nil {
		t.Fatalf("StageFile: %v", err)
	}
	res, err := st.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if res.Name != "origen.jar" {
		t.Errorf("nombre = %s", res.Name)
	}
	if raw, err := os.ReadFile(res.Path); err != nil || !bytes.Equal(raw, jar) {
		t.Errorf("contenido copiado incorrecto (%v)", err)
	}
	assertNoTemps(t, dest)

	// Origen inexistente.
	if _, err := StageFile(filepath.Join(dir, "no-existe.jar"), Request{DestDir: dest, Policy: DefaultPolicy()}); err == nil {
		t.Error("se esperaba error por archivo inexistente")
	} else if !errx.Is(err, errx.CodeNotFound) {
		t.Errorf("se esperaba error_not_found, hubo %v", err)
	}
	// Directorio como origen.
	if _, err := StageFile(dir, Request{DestDir: dest, Policy: DefaultPolicy()}); err == nil {
		t.Error("se esperaba error al pasar un directorio")
	}
	// ExtensiÃƒÂ³n no permitida.
	txt := filepath.Join(dir, "notas.txt")
	if err := os.WriteFile(txt, []byte("hola"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := StageFile(txt, Request{DestDir: dest, Policy: DefaultPolicy()}); err == nil {
		t.Error("se esperaba rechazo de la extensiÃƒÂ³n .txt")
	}
	// Por encima del lÃƒÂ­mite.
	pol := DefaultPolicy()
	pol.MaxBytes = 4
	if _, err := StageFile(src, Request{DestDir: dest, Policy: pol}); err == nil {
		t.Error("se esperaba error por tamaÃƒÂ±o")
	} else if !errx.Is(err, errx.CodeCapacity) {
		t.Errorf("se esperaba error_capacity_exceeded, hubo %v", err)
	}
}

// TestPolicyValidate: lÃƒÂ­mites y polÃƒÂ­ticas incoherentes se detectan antes de
// tocar la red.
func TestPolicyValidate(t *testing.T) {
	pol := DefaultPolicy()
	pol.MaxBytes = MaxAllowedBytes + 1
	if err := pol.Validate(); err == nil {
		t.Error("se esperaba rechazo de un lÃƒÂ­mite por encima del mÃƒÂ¡ximo")
	}
	pol = DefaultPolicy()
	pol.Conflict = "lo-que-sea"
	if err := pol.Validate(); err == nil {
		t.Error("se esperaba rechazo de una polÃƒÂ­tica de conflicto desconocida")
	}
	if err := DefaultPolicy().Validate(); err != nil {
		t.Errorf("la polÃƒÂ­tica por defecto debe ser vÃƒÂ¡lida: %v", err)
	}
}

// zipBytes construye un ZIP en memoria con las entradas indicadas (en orden
// estable para que las pruebas sean deterministas).
func zipBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// TestCleanStaleTemps: solo se borran los temporales propios y antiguos.
func TestCleanStaleTemps(t *testing.T) {
	dir := t.TempDir()
	viejo := filepath.Join(dir, ".badomens-download-viejo.tmp")
	nuevo := filepath.Join(dir, ".badomens-download-nuevo.tmp")
	ajeno := filepath.Join(dir, "plugin.jar")
	for _, f := range []string{viejo, nuevo, ajeno} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(viejo, old, old); err != nil {
		t.Fatal(err)
	}
	n, err := CleanStaleTemps(dir, time.Hour)
	if err != nil {
		t.Fatalf("CleanStaleTemps: %v", err)
	}
	if n != 1 {
		t.Errorf("se borraron %d archivos; se esperaba 1", n)
	}
	if _, serr := os.Stat(viejo); !errors.Is(serr, os.ErrNotExist) {
		t.Error("el temporal antiguo debería haberse borrado")
	}
	for _, f := range []string{nuevo, ajeno} {
		if _, serr := os.Stat(f); serr != nil {
			t.Errorf("no debería borrarse %s: %v", f, serr)
		}
	}
	if !IsTempFile(".badomens-download-x.tmp") || IsTempFile("plugin.jar") || IsTempFile(".badomens-download-x.jar") {
		t.Error("IsTempFile no clasifica correctamente")
	}
}
