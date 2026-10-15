package engines

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func testClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 2 * time.Second}, UserAgent: "badomens-test"}
}

func regWith(p Provider) *Registry {
	return &Registry{providers: map[string]Provider{p.Engine().Name: p}}
}

// TestEnrichNormalizaOrdenaYRecomiendaAlFinal (API-05, QC-05): la recomendada se
// calcula DESPUÉS de normalizar y ordenar, el orden de entrada no importa y la
// recomendación cae en la última estable correcta.
func TestEnrichNormalizaOrdenaYRecomiendaAlFinal(t *testing.T) {
	// Entrada DESORDENADA (como la devolvería un proveedor newest-first).
	list := []Version{
		{Version: "1.21", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
		{Version: "1.20.1", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
		{Version: "1.20.5", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
	}
	out := Enrich("paper", list)
	if len(out) != 3 {
		t.Fatalf("quiero 3 versiones, tengo %d", len(out))
	}
	want := []string{"1.20.1", "1.20.5", "1.21"}
	for i, id := range want {
		if out[i].Version != id {
			t.Fatalf("orden = %v; quiero %v", versionsOf(out), want)
		}
	}
	if !out[len(out)-1].Recommended || out[len(out)-1].Version != "1.21" {
		t.Fatalf("la recomendada debe ser la última estable (1.21): %+v", out)
	}
	for i, v := range out {
		if v.Recommended != (i == len(out)-1) {
			t.Fatalf("marca recommended mal colocada en %d: %+v", i, out)
		}
	}
	// Java inferido por regla (nunca verificado sin dato del proveedor).
	if out[0].JavaMajor != 17 || out[0].JavaStatus != JavaStatusInferred {
		t.Fatalf("1.20.1 → Java 17 inferido, tengo %+v", out[0])
	}
	if out[2].JavaMajor != 21 || out[2].JavaStatus != JavaStatusInferred {
		t.Fatalf("1.21 → Java 21 inferido, tengo %+v", out[2])
	}
	// La compatibilidad se conserva (la fijó el proveedor).
	for _, v := range out {
		if v.Compatibility != CompatibilityCompatible || !v.Compatible {
			t.Fatalf("compatibilidad perdida: %+v", v)
		}
	}
}

// TestEnrichNoConvierteDesconocidoEnCompatible (T-03, API-08): sin un dato que
// verifique la compatibilidad, el estado es `unknown` y `compatible` es false.
func TestEnrichNoConvierteDesconocidoEnCompatible(t *testing.T) {
	out := Enrich("paper", []Version{{Version: "1.21", Stable: true}})
	if out[0].Compatibility != CompatibilityUnknown {
		t.Fatalf("compatibilidad = %q; quiero %q", out[0].Compatibility, CompatibilityUnknown)
	}
	if out[0].Compatible {
		t.Fatal("un dato desconocido NUNCA puede quedar como compatible")
	}
	// Migración de un documento anterior a la Fase 3: `compatible: true` sin el
	// campo trivalente se conserva como compatible verificado.
	legacy := Enrich("paper", []Version{{Version: "1.21", Stable: true, Compatible: true}})
	if legacy[0].Compatibility != CompatibilityCompatible || !legacy[0].Compatible {
		t.Fatalf("migración heredada incorrecta: %+v", legacy[0])
	}
	// Un estado explícito de incompatibilidad no se pierde.
	inc := Enrich("paper", []Version{{Version: "1.21", Stable: true, Compatibility: CompatibilityIncompatible, IncompatibleWhy: "sin artefacto"}})
	if inc[0].Compatibility != CompatibilityIncompatible || inc[0].Compatible {
		t.Fatalf("incompatibilidad explícita perdida: %+v", inc[0])
	}
}

// TestEnrichEsIdempotente: aplicar Enrich dos veces no cambia canal, Java,
// compatibilidad, orden ni recomendada.
func TestEnrichEsIdempotente(t *testing.T) {
	list := []Version{
		{Version: "26.3", Stable: true, Compatibility: CompatibilityCompatible},
		{Version: "1.21.1", Stable: false, Compatibility: CompatibilityCompatible},
		{Version: "26.1", Stable: true, Compatibility: CompatibilityCompatible},
	}
	once := Enrich("neoforge", list)
	twice := Enrich("neoforge", once)
	if len(once) != len(twice) {
		t.Fatalf("longitudes distintas: %d y %d", len(once), len(twice))
	}
	for i := range once {
		if once[i] != twice[i] {
			t.Fatalf("Enrich no es idempotente en %d:\n%+v\n%+v", i, once[i], twice[i])
		}
	}
}

// TestEnrichNormalizaMavenDeForge comprueba la agrupación Minecraft↔build por
// esquema de Forge y la elección del build por comparación ESTRUCTURADA.
func TestEnrichNormalizaMavenDeForge(t *testing.T) {
	list := []Version{
		{Version: "1.21.1-52.0.1", Stable: true},
		{Version: "1.21.1-52.0.9", Stable: true},
		{Version: "1.21.1-52.0.10", Stable: true}, // alfabéticamente GANA 52.0.9
		{Version: "1.20.1-47.3.0", Stable: true},
	}
	out := normalizeMaven("forge", list)
	if len(out) != 2 {
		t.Fatalf("quiero 2 versiones de Minecraft, tengo %d: %+v", len(out), out)
	}
	byID := map[string]Version{}
	for _, v := range out {
		byID[v.Version] = v
	}
	if got := byID["1.21.1"].Build; got != "52.0.10" {
		t.Fatalf("build de 1.21.1 = %q; quiero 52.0.10 (el máximo numérico, no el alfabético)", got)
	}
	if got := byID["1.20.1"].Build; got != "47.3.0" {
		t.Fatalf("build de 1.20.1 = %q", got)
	}
}

// TestEnrichMavenEsUnSoloMinecraftPorVersion: un catálogo crudo con varios
// builds de la MISMA versión de Minecraft (incluidos sufijos beta/rc) deja una
// sola fila, prefiriendo el estable.
func TestEnrichMavenEsUnSoloMinecraftPorVersion(t *testing.T) {
	list := []Version{
		{Version: "20.2.9-beta", Stable: false, Channel: ChannelBeta},
		{Version: "20.2.29-beta", Stable: false, Channel: ChannelBeta},
		{Version: "20.2.30", Stable: true, Channel: ChannelRelease},
	}
	out := normalizeMaven("neoforge", list)
	if len(out) != 1 {
		t.Fatalf("quiero 1 versión de Minecraft, tengo %d: %+v", len(out), out)
	}
	if out[0].Version != "1.20.2" {
		t.Fatalf("versión de Minecraft = %q; quiero 1.20.2", out[0].Version)
	}
	if out[0].Build != "20.2.30" || !out[0].Stable {
		t.Fatalf("quiero el build estable más reciente: %+v", out[0])
	}
}

// TestEnrichNoOrdenaAlfabeticamenteEntreEsquemas (NeoForge): "26.10" es MÁS
// nuevo que "26.2" y "26.1" es más nuevo que "1.21.1", aunque alfabéticamente
// digan lo contrario.
func TestEnrichNoOrdenaAlfabeticamenteEntreEsquemas(t *testing.T) {
	list := []Version{
		{Version: "1.21.1", Stable: true, Compatibility: CompatibilityCompatible},
		{Version: "26.1", Stable: true, Compatibility: CompatibilityCompatible},
		{Version: "26.10", Stable: true, Compatibility: CompatibilityCompatible},
		{Version: "26.2", Stable: true, Compatibility: CompatibilityCompatible},
	}
	out := Enrich("neoforge", list)
	want := []string{"1.21.1", "26.1", "26.2", "26.10"}
	if got := versionsOf(out); !equalStrings(got, want) {
		t.Fatalf("orden = %v; quiero %v", got, want)
	}
	if !out[len(out)-1].Recommended || out[len(out)-1].Version != "26.10" {
		t.Fatalf("la recomendada debe ser 26.10: %+v", out)
	}
}

// versionsOf devuelve los identificadores en orden.
func versionsOf(list []Version) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.Version)
	}
	return out
}

// equalStrings compara dos listas de cadenas.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCatalogoEmbebidoEsCompatibleYUtilizable: el respaldo sin red conserva el
// modelo normalizado (compatibilidad verificada, canal, Java y recomendada).
func TestCatalogoEmbebidoEsCompatibleYUtilizable(t *testing.T) {
	for _, name := range []string{"paper", "fabric", "forge", "neoforge", "vanilla"} {
		list, ok := EmbeddedVersions(name)
		if !ok || len(list) == 0 {
			t.Fatalf("falta el catálogo embebido de %s", name)
		}
		for _, v := range list {
			if v.Compatibility != CompatibilityCompatible || !v.Compatible {
				t.Fatalf("%s: %s sin compatibilidad verificada: %+v", name, v.Version, v)
			}
			if v.Channel == "" {
				t.Fatalf("%s: %s sin canal normalizado", name, v.Version)
			}
			if v.JavaStatus == "" {
				t.Fatalf("%s: %s sin origen de Java", name, v.Version)
			}
		}
		if !list[len(list)-1].Recommended {
			t.Fatalf("%s: el catálogo embebido no marca la recomendada: %+v", name, list)
		}
	}
}

// TestCacheCaeAlCatalogoEmbebidoYLoEtiqueta: si el proveedor falla y no hay
// caché, se sirve el catálogo embebido marcado como vencido y con aviso.
func TestCacheCaeAlCatalogoEmbebidoYLoEtiqueta(t *testing.T) {
	p := &stubProvider{engine: Engine{Name: "vanilla"}}
	p.setErr(ErrUpstreamUnavailable)
	c := NewCache(t.TempDir(), regWith(p))
	versions, stale, err := c.Versions(context.Background(), "vanilla", VersionOpts{})
	if err != nil {
		t.Fatalf("quiero el respaldo embebido, no error: %v", err)
	}
	if !stale || len(versions) == 0 {
		t.Fatalf("respaldo embebido = %d versiones (stale=%v)", len(versions), stale)
	}
	if !versions[0].Stale || versions[0].Warning == "" {
		t.Fatalf("el respaldo debe ir etiquetado como no confirmado: %+v", versions[0])
	}
	st := c.Status("vanilla")
	if st.Source != "embedded" || st.State != CatalogStale {
		t.Fatalf("estado del respaldo = %+v", st)
	}
}
