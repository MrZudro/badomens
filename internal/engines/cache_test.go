package engines

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// vanillaManifestBody es un manifiesto mínimo y válido para las pruebas de caché.
const vanillaManifestBody = `{"versions":[
	{"id":"1.21.1","type":"release","releaseTime":"2024-10-08T00:00:00Z"},
	{"id":"1.21.2","type":"snapshot","releaseTime":"2024-10-10T00:00:00Z"},
	{"id":"1.21","type":"release","releaseTime":"2024-06-13T00:00:00Z"}
]}`

// stubProvider es un proveedor controlable para las pruebas de caché (permite
// forzar catálogos vacíos, reducciones y errores sin red).
type stubProvider struct {
	engine Engine
	mu     sync.Mutex
	list   []Version
	err    error
	calls  int
}

func (s *stubProvider) Engine() Engine { return s.engine }

func (s *stubProvider) ListVersions(context.Context) ([]Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]Version, len(s.list))
	copy(out, s.list)
	return out, nil
}

func (s *stubProvider) Resolve(context.Context, string) (Resolved, error) {
	return Resolved{Version: "1.0"}, nil
}

func (s *stubProvider) set(list []Version) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = list
}

func (s *stubProvider) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// stubVersions construye N versiones estables distintas.
func stubVersions(n int) []Version {
	out := make([]Version, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Version{Version: "1." + itoaTest(i), Stable: true, Channel: ChannelRelease})
	}
	return out
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

func TestCacheVanillaConFechas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(vanillaManifestBody))
	}))
	defer srv.Close()

	reg := regWith(&vanillaProvider{engine: Vanilla, client: testClient(), base: srv.URL})
	c := NewCache(t.TempDir(), reg)

	versions, stale, err := c.Versions(context.Background(), "vanilla", VersionOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatal("primera carga: no debe ser vencida")
	}
	if len(versions) != 2 {
		t.Fatalf("sin snapshots quiero 2, tengo %d: %+v", len(versions), versions)
	}
	if !versions[len(versions)-1].Recommended {
		t.Fatal("quiero la recomendada marcada en la última")
	}
	if versions[0].ReleasedAt.IsZero() {
		t.Fatal("quiero la fecha de publicación de vanilla")
	}
	if !versions[0].ReleasedAt.Before(versions[1].ReleasedAt) {
		t.Fatal("las versiones deben ir ordenadas por fecha")
	}
	if versions[0].Source != "vanilla" || versions[0].FetchedAt.IsZero() {
		t.Fatalf("quiero procedencia en el modelo normalizado: %+v", versions[0])
	}

	// Con IncludeUnstable sí salen los snapshots.
	all, _, _ := c.Versions(context.Background(), "vanilla", VersionOpts{IncludeUnstable: true})
	if len(all) != 3 {
		t.Fatalf("con snapshots quiero 3, tengo %d", len(all))
	}
	found := false
	for _, v := range all {
		if v.Version == "1.21.2" && v.Channel == ChannelSnapshot {
			found = true
		}
	}
	if !found {
		t.Fatalf("quiero el snapshot con canal snapshot: %+v", all)
	}
}

func TestCacheSirveVencidaSinRed(t *testing.T) {
	var engineDown bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if engineDown {
			http.Error(w, "no disponible", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(vanillaManifestBody))
	}))
	defer srv.Close()

	reg := regWith(&vanillaProvider{engine: Vanilla, client: testClient(), base: srv.URL})
	c := NewCache(t.TempDir(), reg)
	ctx := context.Background()

	now := time.Now()
	c.Now = func() time.Time { return now }
	if _, _, err := c.Versions(ctx, "vanilla", VersionOpts{}); err != nil {
		t.Fatal(err)
	}

	engineDown = true
	c.Now = func() time.Time { return now.Add(7 * time.Hour) }
	versions, stale, err := c.Versions(ctx, "vanilla", VersionOpts{})
	if err != nil {
		t.Fatalf("sin red quiero caché vencida, no error: %v", err)
	}
	if !stale {
		t.Fatal("quiero stale=true al usar datos de hace 7 h")
	}
	if len(versions) != 2 {
		t.Fatalf("caché: %+v", versions)
	}
	if !versions[0].Stale || versions[0].Warning == "" {
		t.Fatalf("una fila de caché vencida debe ir etiquetada: %+v", versions[0])
	}
	if versions[0].Warning == "" || !strings.Contains(versions[0].Warning, "vencida") {
		t.Fatalf("el aviso debe explicar la antigüedad: %q", versions[0].Warning)
	}
}

func TestCacheReutilizaFrescaComoTTL(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = w.Write([]byte(vanillaManifestBody))
	}))
	defer srv.Close()

	reg := regWith(&vanillaProvider{engine: Vanilla, client: testClient(), base: srv.URL})
	c := NewCache(t.TempDir(), reg)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, _, err := c.Versions(ctx, "vanilla", VersionOpts{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("la caché fresca debe evitar la red: %d peticiones", got)
	}
}

func TestCacheCorruptaNoSeSirveYSeRecupera(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(vanillaManifestBody))
	}))
	defer srv.Close()
	reg := regWith(&vanillaProvider{engine: Vanilla, client: testClient(), base: srv.URL})
	dir := t.TempDir()
	c := NewCache(dir, reg)

	// Documento corrupto en disco.
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.path("vanilla"), []byte("{esto no es json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := c.Status("vanilla")
	if st.State != CatalogInvalid {
		t.Fatalf("una caché corrupta debe ser inválida: %+v", st)
	}
	if st.Usable() {
		t.Fatal("una caché inválida no es utilizable")
	}
	// La consulta se recupera del proveedor y vuelve a dejar una caché válida.
	versions, stale, err := c.Versions(context.Background(), "vanilla", VersionOpts{})
	if err != nil {
		t.Fatalf("quiero recuperación del proveedor: %v", err)
	}
	if stale || len(versions) != 2 {
		t.Fatalf("recuperación = %v (stale=%v)", versions, stale)
	}
	if st := c.Status("vanilla"); st.State != CatalogFresh || !st.Validated {
		t.Fatalf("tras recuperarse quiero caché fresca y validada: %+v", st)
	}
}

func TestCacheAusenteSinRespaldoDevuelveError(t *testing.T) {
	p := &stubProvider{engine: Engine{Name: "custom"}}
	p.setErr(ErrUpstreamUnavailable)
	c := NewCache(t.TempDir(), regWith(p))
	versions, _, err := c.Versions(context.Background(), "custom", VersionOpts{})
	if err == nil {
		t.Fatalf("sin caché ni respaldo el error se propaga: %+v", versions)
	}
	if !strings.Contains(err.Error(), CodeUpstreamUnavailable) {
		t.Fatalf("quiero el error tipado del transporte: %v", err)
	}
	if st := c.Status("custom"); st.State != CatalogUnavailable {
		t.Fatalf("estado = %q; quiero unavailable", st.State)
	}
}

func TestCacheEstadoFrescaVencida(t *testing.T) {
	reg := regWith(&vanillaProvider{engine: Vanilla, client: testClient()})
	c := NewCache(t.TempDir(), reg)
	now := time.Now()
	c.Now = func() time.Time { return now }
	doc := cachedDoc{
		Engine:    "vanilla",
		Provider:  "vanilla",
		Fetched:   now,
		UpdatedAt: now,
		Schema:    catalogSchemaVersion,
		Validated: true,
		Versions:  []Version{{Version: "1.21", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible}},
	}
	if err := c.writeCacheDoc(c.path("vanilla"), doc); err != nil {
		t.Fatal(err)
	}
	if st := c.Status("vanilla"); st.State != CatalogFresh || st.Versions != 1 || st.Age == "" {
		t.Fatalf("quiero caché fresca con antigüedad legible: %+v", st)
	}
	now = now.Add(7 * time.Hour)
	st := c.Status("vanilla")
	if st.State != CatalogStale {
		t.Fatalf("quiero caché vencida: %+v", st)
	}
	if !st.Usable() {
		t.Fatal("una caché vencida sigue siendo utilizable")
	}
	if st.AgeSeconds < 3600 || st.Recommended != "1.21" {
		t.Fatalf("estado vencido incompleto: %+v", st)
	}
}

func TestCacheNoSobrescribeConCatalogoVacio(t *testing.T) {
	p := &stubProvider{engine: Engine{Name: "custom"}}
	p.set(stubVersions(30))
	dir := t.TempDir()
	c := NewCache(dir, regWith(p))

	first, stale, err := c.Versions(context.Background(), "custom", VersionOpts{})
	if err != nil || stale || len(first) != 30 {
		t.Fatalf("primera carga = %d versiones (stale=%v, err=%v)", len(first), stale, err)
	}

	// El proveedor devuelve un catálogo VACÍO: no se sobrescribe.
	now := time.Now()
	c.Now = func() time.Time { return now.Add(7 * time.Hour) }
	p.set(nil)
	versions, stale, err := c.Versions(context.Background(), "custom", VersionOpts{})
	if err != nil {
		t.Fatalf("quiero conservar el catálogo previo, no error: %v", err)
	}
	if !stale || len(versions) != 30 {
		t.Fatalf("un catálogo vacío no puede sustituir al válido: %d (stale=%v)", len(versions), stale)
	}
	if !strings.Contains(versions[0].Warning, "vencida") {
		t.Fatalf("quiero etiqueta de conservación: %q", versions[0].Warning)
	}
	// El documento en disco conserva las 30 versiones.
	doc, rerr := c.readCache(c.path("custom"))
	if rerr != nil || doc == nil || len(doc.Versions) != 30 {
		t.Fatalf("documento en disco alterado: %+v (%v)", doc, rerr)
	}
}

func TestCacheDetectaReduccionAnomala(t *testing.T) {
	p := &stubProvider{engine: Engine{Name: "custom"}}
	p.set(stubVersions(40))
	dir := t.TempDir()
	c := NewCache(dir, regWith(p))

	if _, _, err := c.Versions(context.Background(), "custom", VersionOpts{}); err != nil {
		t.Fatal(err)
	}

	// Caso real (AC-07): el proveedor publica solo 3 versiones donde había 40.
	now := time.Now()
	c.Now = func() time.Time { return now.Add(7 * time.Hour) }
	p.set(stubVersions(3))
	versions, stale, err := c.Versions(context.Background(), "custom", VersionOpts{})
	if err != nil {
		t.Fatalf("quiero conservar el catálogo previo: %v", err)
	}
	if !stale || len(versions) != 40 {
		t.Fatalf("reducción anómala no detectada: %d (stale=%v)", len(versions), stale)
	}
	if !strings.Contains(versions[0].Warning, "reducción anómala") {
		t.Fatalf("quiero un aviso de reducción anómala: %q", versions[0].Warning)
	}
	doc, _ := c.readCache(c.path("custom"))
	if doc == nil || len(doc.Versions) != 40 {
		t.Fatalf("no se debe sobrescribir con la reducción: %+v", doc)
	}

	// Recuperación: un catálogo completo se acepta y se persiste.
	c.Now = func() time.Time { return now.Add(14 * time.Hour) }
	p.set(stubVersions(44))
	versions, stale, err = c.Versions(context.Background(), "custom", VersionOpts{})
	if err != nil || stale || len(versions) != 44 {
		t.Fatalf("recuperación = %d versiones (stale=%v, err=%v)", len(versions), stale, err)
	}
	if doc, _ := c.readCache(c.path("custom")); doc == nil || len(doc.Versions) != 44 {
		t.Fatalf("no se persistió la recuperación: %+v", doc)
	}
}

func TestCacheEscrituraAtomica(t *testing.T) {
	p := &stubProvider{engine: Engine{Name: "custom"}}
	p.set(stubVersions(25))
	dir := t.TempDir()
	c := NewCache(dir, regWith(p))
	if _, _, err := c.Versions(context.Background(), "custom", VersionOpts{}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("quedó un archivo temporal de escritura: %s", e.Name())
		}
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, "custom.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc cachedDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("el documento escrito debe ser JSON válido: %v", err)
	}
	if len(doc.Versions) != 25 || !doc.Validated || doc.Schema != catalogSchemaVersion {
		t.Fatalf("documento incompleto: %+v", doc)
	}
}

func TestCacheDeduplicaSolicitudesSimultaneas(t *testing.T) {
	var hits int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-release
		_, _ = w.Write([]byte(vanillaManifestBody))
	}))
	defer srv.Close()

	reg := regWith(&vanillaProvider{engine: Vanilla, client: testClient(), base: srv.URL})
	c := NewCache(t.TempDir(), reg)

	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n)
	results := make([][]Version, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _, errs[i] = c.Versions(context.Background(), "vanilla", VersionOpts{})
		}(i)
	}
	// Se da tiempo a que todas las goroutines entren en la misma consulta.
	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if len(results[i]) != 2 {
			t.Fatalf("goroutine %d: %+v", i, results[i])
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("N solicitudes simultáneas deben producir 1 petición, hubo %d", got)
	}
}

func TestCacheRefreshFuerzaConsulta(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = w.Write([]byte(vanillaManifestBody))
	}))
	defer srv.Close()
	reg := regWith(&vanillaProvider{engine: Vanilla, client: testClient(), base: srv.URL})
	c := NewCache(t.TempDir(), reg)
	ctx := context.Background()

	if _, _, err := c.Versions(ctx, "vanilla", VersionOpts{}); err != nil {
		t.Fatal(err)
	}
	// Aunque la caché esté fresca, refresh vuelve a consultar el proveedor.
	versions, st, err := c.Refresh(ctx, "vanilla")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if st.State != CatalogFresh {
		t.Fatalf("tras refrescar quiero caché fresca: %+v", st)
	}
	// Refresh devuelve el catálogo completo tal cual quedó almacenado (sin el
	// filtro de canales del llamante): releases + snapshot.
	if len(versions) != 3 {
		t.Fatalf("versiones = %+v", versions)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("refresh debe consultar el proveedor: %d peticiones", got)
	}
}

func TestCacheUsaETagYTrata304ComoSinCambios(t *testing.T) {
	const etag = `"v1"`
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write([]byte(vanillaManifestBody))
	}))
	defer srv.Close()

	reg := regWith(&vanillaProvider{engine: Vanilla, client: testClient(), base: srv.URL})
	c := NewCache(t.TempDir(), reg)
	ctx := context.Background()
	now := time.Now()
	c.Now = func() time.Time { return now }

	if _, _, err := c.Versions(ctx, "vanilla", VersionOpts{}); err != nil {
		t.Fatal(err)
	}
	first, rerr := c.readCache(c.path("vanilla"))
	if rerr != nil || first == nil || first.ETag != etag {
		t.Fatalf("quiero el ETag persistido: %+v (%v)", first, rerr)
	}

	// Vence la caché: la siguiente consulta envía If-None-Match y recibe 304.
	now = now.Add(7 * time.Hour)
	versions, stale, err := c.Versions(ctx, "vanilla", VersionOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatal("un 304 refresca la fecha: no debe quedar vencida")
	}
	if len(versions) != 2 {
		t.Fatalf("versiones = %+v", versions)
	}
	second, _ := c.readCache(c.path("vanilla"))
	if second == nil || !second.Fetched.Equal(now) {
		t.Fatalf("quiero la fecha refrescada sin reescribir datos: %+v", second)
	}
	if len(second.Versions) != len(first.Versions) {
		t.Fatalf("los datos no se deben reescribir: %d vs %d", len(second.Versions), len(first.Versions))
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("peticiones = %d; quiero 2 (200 + 304)", got)
	}
	// Y no vuelve a pedir hasta que venza otra vez.
	if _, _, err := c.Versions(ctx, "vanilla", VersionOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("con la fecha refrescada no se debe volver a consultar: %d", got)
	}
}

func TestCacheRefrescaVencidaConBaselineSembrado(t *testing.T) {
	// Con red disponible, un documento vencido se refresca desde el proveedor; el
	// baseline sembrado (NeoForge, SeedCatalog) no debe disparar una falsa caída
	// anómala ni conservar datos vencidos innecesariamente.
	p, _ := neoforgeTestProvider(t, staticBody(readProviderFixture(t, "testdata/neoforge/maven-metadata.xml")))
	c := NewCache(t.TempDir(), regWith(p))
	now := time.Now()
	c.Now = func() time.Time { return now }
	if _, _, err := c.Versions(context.Background(), "neoforge", VersionOpts{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(8 * time.Hour)
	if _, stale, err := c.Versions(context.Background(), "neoforge", VersionOpts{}); err != nil {
		t.Fatal(err)
	} else if stale {
		t.Fatal("con red disponible no debe servirse caché vencida")
	}
}
