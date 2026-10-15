package engines

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
)

// ===========================================================================
// Servicio de catálogo unificado (Fase 5, T-01)
// ===========================================================================

// fakeServiceProvider es un proveedor de catálogo controlado, sin red.
type fakeServiceProvider struct {
	engine Engine
	list   []Version
	// pin es el valor que se devuelve para cada pin obligatorio del motor.
	pin string
	// fail hace fallar la consulta del catálogo (respaldo/caché vencida).
	fail bool
	// listCalls cuenta las consultas al catálogo que han llegado al proveedor.
	listCalls int
	// resolveCalls cuenta las resoluciones que han llegado al proveedor.
	resolveCalls int
}

func (p *fakeServiceProvider) Engine() Engine { return p.engine }

func (p *fakeServiceProvider) ListVersions(context.Context) ([]Version, error) {
	if p.fail {
		return nil, catalogContentError(p.engine.Name, CodeUpstreamUnavailable, "list_versions", "", Meta{},
			errors.New("proveedor caído"))
	}
	return p.list, nil
}

// ListAllWithMeta implementa la vía condicional: permite contar consultas y fija
// la fecha del catálogo.
func (p *fakeServiceProvider) ListAllWithMeta(_ context.Context, _ CacheValidators) ([]Version, Meta, error) {
	p.listCalls++
	if p.fail {
		return nil, Meta{}, catalogContentError(p.engine.Name, CodeUpstreamUnavailable, "list_versions", "", Meta{},
			errors.New("proveedor caído"))
	}
	return p.list, Meta{Status: 200, FetchedAt: time.Now()}, nil
}

func (p *fakeServiceProvider) Resolve(_ context.Context, version string) (Resolved, error) {
	p.resolveCalls++
	v, err := pickVersion(p.engine.Name, p.list, version)
	if err != nil {
		return Resolved{}, catalogContentError(p.engine.Name, CodeNotFound, "resolve", "", Meta{}, err)
	}
	pins := map[string]string{}
	for _, dep := range RequiredPins(p.engine) {
		pins[dep] = p.pin
	}
	return Resolved{Version: v.Version, Pins: pins, Source: p.engine.Name}, nil
}

// servicePaperVersions es un catálogo de Paper con releases y una beta.
func servicePaperVersions() []Version {
	return []Version{
		{Version: "1.20.1", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
		{Version: "1.21.1", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
		{Version: "26.2", Stable: false, Channel: ChannelBeta, Compatibility: CompatibilityCompatible},
		{Version: "26.3", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
	}
}

// newPaperService construye el servicio sobre un proveedor de Paper controlado.
func newPaperService(t *testing.T) (*CatalogService, *fakeServiceProvider, *Cache) {
	t.Helper()
	provider := &fakeServiceProvider{engine: Paper, list: servicePaperVersions(), pin: "99"}
	cache := NewCache(t.TempDir(), NewRegistryFrom(provider))
	return NewCatalogService(cache), provider, cache
}

func TestCatalogServiceListVersionsConFiltros(t *testing.T) {
	service, _, _ := newPaperService(t)
	ctx := context.Background()

	res, err := service.ListVersions(ctx, "Paper", Filters{})
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if res.Engine != "paper" {
		t.Fatalf("motor normalizado = %q", res.Engine)
	}
	if got := versionsOf(res.Versions); !equalStrings(got, []string{"1.20.1", "1.21.1", "26.3"}) {
		t.Fatalf("releases = %v", got)
	}
	if res.Stale || !res.Status.Usable() {
		t.Fatalf("estado inesperado: stale=%v status=%+v", res.Stale, res.Status)
	}

	all, err := service.ListVersions(ctx, "paper", Filters{IncludeUnstable: true})
	if err != nil {
		t.Fatalf("ListVersions --all: %v", err)
	}
	if got := versionsOf(all.Versions); !equalStrings(got, []string{"1.20.1", "1.21.1", "26.2", "26.3"}) {
		t.Fatalf("con inestables = %v", got)
	}

	beta, err := service.ListVersions(ctx, "paper", Filters{IncludeUnstable: true, Channel: ChannelBeta})
	if err != nil {
		t.Fatalf("ListVersions canal beta: %v", err)
	}
	if got := versionsOf(beta.Versions); !equalStrings(got, []string{"26.2"}) {
		t.Fatalf("solo beta = %v", got)
	}

	last, err := service.ListVersions(ctx, "paper", Filters{Limit: 1})
	if err != nil {
		t.Fatalf("ListVersions limit: %v", err)
	}
	if got := versionsOf(last.Versions); !equalStrings(got, []string{"26.3"}) {
		t.Fatalf("limit 1 = %v", got)
	}

	if _, err := service.ListVersions(ctx, "noexiste", Filters{}); !errx.Is(err, errx.CodeUsage) {
		t.Fatalf("un motor desconocido debe ser un error de USO, tengo %v", err)
	}
}

func TestCatalogServiceResolveVersionExplicitaYUltima(t *testing.T) {
	service, _, _ := newPaperService(t)
	ctx := context.Background()

	explicit, err := service.ResolveVersion(ctx, "paper", "1.21.1")
	if err != nil {
		t.Fatalf("ResolveVersion explícita: %v", err)
	}
	if explicit.Version != "1.21.1" || explicit.Pins[Paper.DepVar] != "99" {
		t.Fatalf("resolución explícita = %+v", explicit)
	}
	if explicit.JavaMajor != 21 || explicit.JavaStatus != JavaStatusInferred {
		t.Fatalf("Java = %d/%s", explicit.JavaMajor, explicit.JavaStatus)
	}
	if explicit.Compatibility != CompatibilityCompatible {
		t.Fatalf("compatibilidad = %q", explicit.Compatibility)
	}

	latest, err := service.ResolveVersion(ctx, "paper", "")
	if err != nil {
		t.Fatalf("ResolveVersion última: %v", err)
	}
	if latest.Version != "26.3" {
		t.Fatalf("la última estable debe ser 26.3, tengo %q", latest.Version)
	}
	if latest.Pins[Paper.DepVar] != "99" || latest.Pins[Paper.DepVar] == "latest" {
		t.Fatalf("pins = %v", latest.Pins)
	}
}

// TestCatalogServiceResolveVersionHeredaJavaVerificado: el requisito de Java que
// el proveedor VERIFICA (Fill v3) gana a la regla versionada y se propaga a la
// resolución (y a la imagen coherente).
func TestCatalogServiceResolveVersionHeredaJavaVerificado(t *testing.T) {
	list := servicePaperVersions()
	for i := range list {
		if list[i].Version == "26.3" {
			list[i].JavaMajor, list[i].JavaStatus = 25, JavaStatusVerified
		}
	}
	provider := &fakeServiceProvider{engine: Paper, list: list, pin: "99"}
	service := NewCatalogService(NewCache(t.TempDir(), NewRegistryFrom(provider)))
	if _, err := service.ListVersions(context.Background(), "paper", Filters{}); err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	res, err := service.ResolveVersion(context.Background(), "paper", "26.3")
	if err != nil {
		t.Fatalf("ResolveVersion: %v", err)
	}
	if res.JavaMajor != 25 || res.JavaStatus != JavaStatusVerified {
		t.Fatalf("quiero el Java verificado del catálogo, tengo %d/%s", res.JavaMajor, res.JavaStatus)
	}
	if res.Image != "itzg/minecraft-server:java25" {
		t.Fatalf("imagen = %q", res.Image)
	}
}

func TestCatalogServiceResolveVersionInexistente(t *testing.T) {
	service, _, _ := newPaperService(t)
	_, err := service.ResolveVersion(context.Background(), "paper", "9.9.9")
	if err == nil {
		t.Fatal("una versión inexistente debe fallar")
	}
	if !errx.Is(err, errx.CodeNotFound) {
		t.Fatalf("quiero error_not_found, tengo %v", err)
	}
	if got := errx.Details(err)["catalog_code"]; got != CodeNotFound {
		t.Fatalf("details.catalog_code = %v", got)
	}
}

// sinEmbebido quita temporalmente el catálogo embebido de un motor: es la única
// forma de llegar al estado "no disponible" (todos los motores reales tienen
// respaldo embebido).
func sinEmbebido(t *testing.T, engine string) {
	t.Helper()
	saved, ok := embeddedCatalog[engine]
	delete(embeddedCatalog, engine)
	t.Cleanup(func() {
		if ok {
			embeddedCatalog[engine] = saved
		}
	})
}

func TestCatalogServiceGetCatalogStatusCuatroEstados(t *testing.T) {
	service, _, cache := newPaperService(t)
	ctx := context.Background()

	// Fresca: el catálogo se acaba de consultar.
	if _, err := service.ListVersions(ctx, "paper", Filters{}); err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	st, err := service.GetCatalogStatus(ctx, "paper")
	if err != nil {
		t.Fatalf("GetCatalogStatus: %v", err)
	}
	if st.State != CatalogFresh || !st.Usable() || st.Versions != 4 {
		t.Fatalf("quiero fresca con 4 versiones, tengo %+v", st)
	}

	// Vencida: se viaja en el tiempo más allá del TTL.
	base := time.Now()
	cache.Now = func() time.Time { return base.Add(7 * time.Hour) }
	st, err = service.GetCatalogStatus(ctx, "paper")
	if err != nil {
		t.Fatalf("GetCatalogStatus vencida: %v", err)
	}
	if st.State != CatalogStale || !st.Usable() || st.Age == "" {
		t.Fatalf("quiero vencida y utilizable, tengo %+v", st)
	}
	cache.Now = nil

	// Inválida: el documento de caché no es legible.
	if err := os.WriteFile(cache.path("paper"), []byte("{no es json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ = service.GetCatalogStatus(ctx, "paper")
	if st.State != CatalogInvalid || st.Usable() {
		t.Fatalf("quiero inválida e inutilizable, tengo %+v", st)
	}

	// No disponible: sin caché y sin catálogo embebido.
	if err := os.Remove(cache.path("paper")); err != nil {
		t.Fatal(err)
	}
	sinEmbebido(t, "paper")
	st, _ = service.GetCatalogStatus(ctx, "paper")
	if st.State != CatalogUnavailable || st.Usable() {
		t.Fatalf("quiero no disponible, tengo %+v", st)
	}

	if _, err := service.GetCatalogStatus(ctx, "noexiste"); !errx.Is(err, errx.CodeUsage) {
		t.Fatalf("motor desconocido: quiero error de uso, tengo %v", err)
	}
}

func TestCatalogServiceRefreshFuerzaConsulta(t *testing.T) {
	service, provider, _ := newPaperService(t)
	ctx := context.Background()

	if _, err := service.ListVersions(ctx, "paper", Filters{}); err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if provider.listCalls != 1 {
		t.Fatalf("consultas = %d; quiero 1", provider.listCalls)
	}
	// El TTL está fresco: otra lista NO vuelve a consultar.
	if _, err := service.ListVersions(ctx, "paper", Filters{}); err != nil {
		t.Fatalf("ListVersions 2: %v", err)
	}
	if provider.listCalls != 1 {
		t.Fatalf("una lista en caché no debe consultar: %d", provider.listCalls)
	}
	// Refresh SÍ fuerza, aunque el TTL esté fresco (y dos veces, dos consultas).
	for i := 0; i < 2; i++ {
		res, err := service.RefreshCatalog(ctx, "paper")
		if err != nil {
			t.Fatalf("RefreshCatalog: %v", err)
		}
		if len(res.Versions) != 4 || !res.Status.Usable() {
			t.Fatalf("refresco %d = %d versiones, estado %+v", i, len(res.Versions), res.Status)
		}
	}
	if provider.listCalls != 3 {
		t.Fatalf("consultas = %d; quiero 3", provider.listCalls)
	}

	// Si el proveedor falla, el catálogo previo sigue disponible y el error se
	// normaliza (estado PARCIAL: no se sobrescribe).
	provider.fail = true
	res, err := service.RefreshCatalog(ctx, "paper")
	if err == nil {
		t.Fatal("un proveedor caído debe devolver error")
	}
	if !errx.Is(err, errx.CodeUnavailable) {
		t.Fatalf("código = %v; quiero error_unavailable", err)
	}
	if len(res.Versions) == 0 || !res.Status.Usable() {
		t.Fatalf("el catálogo previo debe seguir disponible: %+v", res.Status)
	}
}

func TestCatalogServiceCheckCompatibilityTrivalente(t *testing.T) {
	service, _, _ := newPaperService(t)
	ctx := context.Background()
	base := CompatibilityRequest{
		Engine:           "paper",
		MinecraftVersion: "1.21.1",
		Pins:             map[string]string{Paper.DepVar: "99"},
	}

	// Java verificado que satisface el requisito: compatible.
	verified := base
	verified.JavaMajor, verified.JavaStatus = 21, JavaStatusVerified
	verified.DockerImage = "itzg/minecraft-server:java21"
	res, err := service.CheckCompatibility(ctx, verified)
	if err != nil {
		t.Fatalf("CheckCompatibility: %v", err)
	}
	if res.Compatibility != CompatibilityCompatible || res.Reason != "" {
		t.Fatalf("quiero compatible, tengo %+v", res)
	}

	// Java INFERIDO: también compatible (el origen no cambia el veredicto).
	inferred := verified
	inferred.JavaStatus = JavaStatusInferred
	if res, err = service.CheckCompatibility(ctx, inferred); err != nil || res.Compatibility != CompatibilityCompatible {
		t.Fatalf("java inferido: %+v (%v)", res, err)
	}

	// Un `java_major` declarado sin origen se publica como VERIFICADO (lo ha
	// declarado el llamante).
	soloMayor := base
	soloMayor.JavaMajor = 21
	res, err = service.CheckCompatibility(ctx, soloMayor)
	if err != nil {
		t.Fatalf("java declarado: %v", err)
	}
	if res.JavaStatus != JavaStatusVerified || res.Compatibility != CompatibilityCompatible {
		t.Fatalf("quiero java verificado y compatible, tengo %+v", res)
	}

	// Java DESCONOCIDO: nunca compatible.
	unknown := base
	if res, err = service.CheckCompatibility(ctx, unknown); err != nil || res.Compatibility != CompatibilityUnknown {
		t.Fatalf("java desconocido: %+v (%v)", res, err)
	}

	// Imagen Docker con un Java INFERIOR al requerido: incompatible.
	badImage := verified
	badImage.DockerImage = "itzg/minecraft-server:java17"
	if res, err = service.CheckCompatibility(ctx, badImage); err != nil || res.Compatibility != CompatibilityIncompatible {
		t.Fatalf("imagen incompatible: %+v (%v)", res, err)
	}

	// Sin el pin obligatorio del motor: desconocido (nunca compatible).
	noPin := verified
	noPin.Pins = nil
	if res, err = service.CheckCompatibility(ctx, noPin); err != nil || res.Compatibility != CompatibilityUnknown {
		t.Fatalf("sin pins: %+v (%v)", res, err)
	}
}

// TestCatalogServiceDerivaPinesDelBuild: el servicio completa los pines
// obligatorios con el build/loader declarados, sin inventar valores.
func TestCatalogServiceDerivaPinesDelBuild(t *testing.T) {
	service, _, _ := newPaperService(t)
	res, err := service.CheckCompatibility(context.Background(), CompatibilityRequest{
		Engine: "paper", MinecraftVersion: "1.21.1",
		Build: "110", JavaMajor: 21, JavaStatus: JavaStatusVerified,
	})
	if err != nil {
		t.Fatalf("CheckCompatibility: %v", err)
	}
	if res.Pins[Paper.DepVar] != "110" {
		t.Fatalf("el pin debe derivarse del build: %+v", res.Pins)
	}
	if res.Compatibility != CompatibilityCompatible {
		t.Fatalf("compatibilidad = %q (%s)", res.Compatibility, res.Reason)
	}
	// Sin build ni loader no hay pin: nunca se inventa.
	sinDatos, err := service.CheckCompatibility(context.Background(), CompatibilityRequest{
		Engine: "paper", MinecraftVersion: "1.21.1", JavaMajor: 21, JavaStatus: JavaStatusVerified,
	})
	if err != nil || sinDatos.Compatibility != CompatibilityUnknown {
		t.Fatalf("sin build: %+v (%v)", sinDatos, err)
	}
}

// TestCatalogServiceVersionInexistenteSinRed: con el catálogo ya en caché, una
// versión inexistente falla SIN volver a consultar al proveedor.
func TestCatalogServiceVersionInexistenteSinRed(t *testing.T) {
	service, provider, _ := newPaperService(t)
	ctx := context.Background()
	if _, err := service.ListVersions(ctx, "paper", Filters{}); err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	provider.fail = true
	provider.resolveCalls = 0

	_, err := service.ResolveVersion(ctx, "paper", "9.9.9")
	if !errx.Is(err, errx.CodeNotFound) {
		t.Fatalf("quiero error_not_found, tengo %v", err)
	}
	if provider.resolveCalls != 0 {
		t.Fatalf("no debe resolverse en el proveedor: %d", provider.resolveCalls)
	}
}

// TestCatalogServiceCheckCompatibilityVersionNoPublicada: la compatibilidad se
// cruza con el catálogo del motor; una versión que el motor no publica es
// INCOMPATIBLE, y si el catálogo no se puede consultar queda DESCONOCIDA.
func TestCatalogServiceCheckCompatibilityVersionNoPublicada(t *testing.T) {
	service, _, _ := newPaperService(t)
	req := CompatibilityRequest{
		Engine: "paper", MinecraftVersion: "9.9.9",
		Build: "110", JavaMajor: 21, JavaStatus: JavaStatusVerified,
	}
	res, err := service.CheckCompatibility(context.Background(), req)
	if err != nil {
		t.Fatalf("CheckCompatibility: %v", err)
	}
	if res.Compatibility != CompatibilityIncompatible || !strings.Contains(res.Reason, "9.9.9") {
		t.Fatalf("quiero incompatible con motivo, tengo %+v", res)
	}

	// Sin catálogo consultable (proveedor caído y sin respaldo embebido) y sin
	// caché: no se puede confirmar → DESCONOCIDO, nunca compatible.
	sinEmbebido(t, "paper")
	caido := &fakeServiceProvider{engine: Paper, list: servicePaperVersions(), pin: "99", fail: true}
	servicioSinCatalogo := NewCatalogService(NewCache(t.TempDir(), NewRegistryFrom(caido)))
	res, err = servicioSinCatalogo.CheckCompatibility(context.Background(), req)
	if err != nil {
		t.Fatalf("CheckCompatibility sin catálogo: %v", err)
	}
	if res.Compatibility != CompatibilityUnknown {
		t.Fatalf("sin catálogo no se puede afirmar compatibilidad: %+v", res)
	}
	if !strings.Contains(res.Reason, "confirmar") {
		t.Fatalf("quiero un motivo explícito, tengo %q", res.Reason)
	}
}

func TestCatalogServiceCheckCompatibilityConPlugins(t *testing.T) {
	service, _, _ := newPaperService(t)
	ctx := context.Background()
	req := CompatibilityRequest{
		Engine:           "paper",
		MinecraftVersion: "1.21.1",
		Pins:             map[string]string{Paper.DepVar: "99"},
		JavaMajor:        21,
		JavaStatus:       JavaStatusVerified,
		DockerImage:      "itzg/minecraft-server:java21",
	}

	// Plugin que declara el ecosistema y la versión: compatible.
	ok := req
	ok.Plugins = []PluginRequirement{{Name: "ViaVersion", Loaders: []string{"paper", "bukkit"}, MinecraftVersions: []string{"1.20", "1.21.1"}}}
	res, err := service.CheckCompatibility(ctx, ok)
	if err != nil || res.Compatibility != CompatibilityCompatible {
		t.Fatalf("plugin compatible: %+v (%v)", res, err)
	}

	// Motor distinto: INCOMPATIBLE (conflicto comprobado).
	bad := req
	bad.Plugins = []PluginRequirement{{Name: "FabricAPI", Loaders: []string{"fabric"}, MinecraftVersions: []string{"1.21.1"}}}
	if res, err = service.CheckCompatibility(ctx, bad); err != nil || res.Compatibility != CompatibilityIncompatible {
		t.Fatalf("plugin de otro motor: %+v (%v)", res, err)
	}

	// Versión de Minecraft no declarada: INCOMPATIBLE.
	oldMC := req
	oldMC.Plugins = []PluginRequirement{{Name: "Viejo", Loaders: []string{"paper"}, MinecraftVersions: []string{"1.16.5"}}}
	if res, err = service.CheckCompatibility(ctx, oldMC); err != nil || res.Compatibility != CompatibilityIncompatible {
		t.Fatalf("plugin sin la versión: %+v (%v)", res, err)
	}

	// Plugin sin metadatos: DESCONOCIDO, nunca compatible.
	mute := req
	mute.Plugins = []PluginRequirement{{Name: "Misterioso"}}
	res, err = service.CheckCompatibility(ctx, mute)
	if err != nil || res.Compatibility != CompatibilityUnknown {
		t.Fatalf("plugin sin metadatos: %+v (%v)", res, err)
	}
	if res.Warning == "" || !strings.Contains(res.Warning, "Misterioso") {
		t.Fatalf("quiero el aviso estructurado del plugin, tengo %+v", res)
	}
}

func TestCatalogServiceCheckCompatibilityFill(t *testing.T) {
	service, _, _ := newPaperService(t)
	// Sin datos: con Fill, el servicio resuelve la versión y hereda pines y Java.
	res, err := service.CheckCompatibility(context.Background(), CompatibilityRequest{
		Engine: "paper", MinecraftVersion: "1.21.1", Fill: true,
	})
	if err != nil {
		t.Fatalf("CheckCompatibility --fill: %v", err)
	}
	if res.Pins[Paper.DepVar] != "99" || res.JavaMajor != 21 {
		t.Fatalf("no se completaron los datos: %+v", res)
	}
	if res.Compatibility != CompatibilityCompatible {
		t.Fatalf("compatibilidad = %q (%s)", res.Compatibility, res.Reason)
	}
}

// blockingProvider se queda bloqueado en la consulta hasta que la prueba lo
// libera: sirve para demostrar que una respuesta TARDÍA no produce resultado si
// el contexto ya se canceló.
type blockingProvider struct {
	engine  Engine
	list    []Version
	started chan struct{}
	release chan struct{}
}

func (p *blockingProvider) Engine() Engine { return p.engine }

func (p *blockingProvider) ListVersions(ctx context.Context) ([]Version, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	<-p.release
	return p.list, nil
}

func (p *blockingProvider) Resolve(context.Context, string) (Resolved, error) {
	return Resolved{}, errors.New("no usado")
}

func TestCatalogServiceConsultaCanceladaNoProduceResultado(t *testing.T) {
	provider := &blockingProvider{
		engine:  Paper,
		list:    servicePaperVersions(),
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	service := NewCatalogService(NewCache(t.TempDir(), NewRegistryFrom(provider)))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var got ListResult
	go func() {
		res, err := service.ListVersions(ctx, "paper", Filters{})
		got = res
		done <- err
	}()
	<-provider.started
	cancel()
	close(provider.release)

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("una consulta cancelada debe devolver context.Canceled, tengo %v", err)
	}
	if len(got.Versions) != 0 {
		t.Fatalf("una consulta cancelada no debe entregar versiones: %v", got.Versions)
	}
}

func TestCatalogServiceContextoCanceladoAntesDeConsultar(t *testing.T) {
	service, provider, _ := newPaperService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.ListVersions(ctx, "paper", Filters{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListVersions = %v; quiero cancelación", err)
	}
	if _, err := service.ResolveVersion(ctx, "paper", "1.21.1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveVersion = %v; quiero cancelación", err)
	}
	if _, err := service.RefreshCatalog(ctx, "paper"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RefreshCatalog = %v; quiero cancelación", err)
	}
	if provider.listCalls != 0 {
		t.Fatalf("no debe consultarse al proveedor con el contexto cancelado: %d", provider.listCalls)
	}
}

func TestNormalizeErrorConservaElCodigoDelTransporte(t *testing.T) {
	err := NormalizeError("paper", "resolve", catalogContentError("paper", CodeTimeout, "resolve", "", Meta{}, errors.New("tardó")))
	if !errx.Is(err, errx.CodeUnavailable) {
		t.Fatalf("quiero error_unavailable, tengo %v", err)
	}
	if got := errx.Details(err)["catalog_code"]; got != CodeTimeout {
		t.Fatalf("details.catalog_code = %v", got)
	}
	// La cancelación NO se envuelve: el consumidor la detecta tal cual.
	if err := NormalizeError("paper", "list", context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("la cancelación debe conservarse: %v", err)
	}
}

func TestRegistryFromSoloConProveedoresDados(t *testing.T) {
	reg := NewRegistryFrom(&fakeServiceProvider{engine: Fabric, list: servicePaperVersions(), pin: "0.16"})
	if _, err := reg.Provider("fabric"); err != nil {
		t.Fatalf("fabric debía estar: %v", err)
	}
	if _, err := reg.Provider("paper"); err == nil {
		t.Fatal("paper no debía estar en el registro")
	}
	if err := reg.providers["fabric"].(*fakeServiceProvider).Engine(); err != Fabric {
		t.Fatalf("motor mal registrado: %v", err)
	}
}
