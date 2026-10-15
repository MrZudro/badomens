package engines

import (
	"context"
	"testing"
	"time"
)

// TestPickVersionIndependienteDelOrden (API-05): el resultado es el mismo con la
// lista ascendente, descendente, desordenada o con duplicados estables.
func TestPickVersionIndependienteDelOrden(t *testing.T) {
	asc := []Version{
		{Version: "1.20.1", Stable: true},
		{Version: "1.21", Stable: true},
		{Version: "1.21.4", Stable: true},
	}
	desc := []Version{asc[2], asc[1], asc[0]}
	shuffled := []Version{asc[1], asc[0], asc[2]}
	for name, list := range map[string][]Version{"ascendente": asc, "descendente": desc, "desordenada": shuffled} {
		got, err := pickVersion("paper", list, "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Version != "1.21.4" {
			t.Fatalf("%s: latest = %q; quiero 1.21.4", name, got.Version)
		}
	}
	// Lista VACÍA: error tipado, nunca un resultado inventado.
	if _, err := pickVersion("paper", nil, ""); err == nil {
		t.Fatal("una lista vacía debe fallar")
	}
	// Pre-releases por debajo de su release base, sin importar el orden.
	withPre := []Version{
		{Version: "1.21.4-pre1", Stable: false},
		{Version: "1.21.4", Stable: true},
		{Version: "1.21.3", Stable: true},
	}
	got, err := pickVersion("paper", withPre, "")
	if err != nil || got.Version != "1.21.4" {
		t.Fatalf("latest = %+v (%v); quiero la release 1.21.4", got, err)
	}
}

// TestPickVersionRechazaInexistenteOIncompatible (T-02): una versión explícita
// que no existe o que está marcada como incompatible NUNCA resuelve.
func TestPickVersionRechazaInexistenteOIncompatible(t *testing.T) {
	list := []Version{
		{Version: "1.21.4", Stable: true, Compatibility: CompatibilityCompatible},
		{Version: "1.21.5", Stable: false, Compatibility: CompatibilityIncompatible, IncompatibleWhy: "sin artefacto descargable"},
	}
	if _, err := pickVersion("paper", list, "9.9.9"); err == nil {
		t.Fatal("una versión inexistente debe fallar")
	}
	if _, err := pickVersion("paper", list, "1.21.5"); err == nil {
		t.Fatal("una versión incompatible debe fallar")
	}
	v, err := pickVersion("paper", list, "1.21.4")
	if err != nil || v.Version != "1.21.4" {
		t.Fatalf("la versión válida debe resolverse: %+v (%v)", v, err)
	}
}

// TestMavenOrdenLexicograficoEngañoso: "26.10" es más nuevo que "26.2" y
// "1.21.10" que "1.21.9"; el orden alfabético diría lo contrario.
func TestMavenOrdenLexicograficoEngañoso(t *testing.T) {
	if compareProviderVersions("26.10", "26.2") <= 0 {
		t.Fatal("26.10 debe ser mayor que 26.2")
	}
	if compareProviderVersions("26.3", "26.2") <= 0 {
		t.Fatal("26.3 debe ser mayor que 26.2")
	}
	list := []Version{
		{Version: "26.2", Stable: true, Compatibility: CompatibilityCompatible},
		{Version: "26.10", Stable: true, Compatibility: CompatibilityCompatible},
		{Version: "26.1", Stable: true, Compatibility: CompatibilityCompatible},
	}
	out := Enrich("forge", list)
	if got := versionsOf(out); !equalStrings(got, []string{"26.1", "26.2", "26.10"}) {
		t.Fatalf("orden = %v; quiero [26.1 26.2 26.10]", got)
	}
	got, err := pickVersion("forge", out, "")
	if err != nil || got.Version != "26.10" {
		t.Fatalf("latest = %+v (%v); quiero 26.10", got, err)
	}
}

// TestMavenSufijosBetaYRcNoGanan: un build con sufijo de pre-release no gana a
// uno estable de la misma versión de Minecraft, y el orden de los sufijos es
// numérico (beta.10 > beta.9).
func TestMavenSufijosBetaYRcNoGanan(t *testing.T) {
	if compareProviderVersions("26.1.0.10-beta", "26.1.0.9-beta") <= 0 {
		t.Fatal("beta.10 debe ser mayor que beta.9")
	}
	list := []Version{
		{Version: "26.3.0.9-beta", Stable: false, Channel: ChannelBeta},
		{Version: "26.3.0.10-beta", Stable: false, Channel: ChannelBeta},
	}
	out := normalizeMaven("neoforge", list)
	if len(out) != 1 {
		t.Fatalf("quiero 1 versión de Minecraft: %+v", out)
	}
	if out[0].Build != "26.3.0.10-beta" {
		t.Fatalf("build = %q; quiero 26.3.0.10-beta", out[0].Build)
	}
	if out[0].Stable {
		t.Fatalf("un build beta no es estable: %+v", out[0])
	}
}

// TestNeoForgeAntiguoYModernoNormalizados: la asociación Minecraft↔NeoForge es
// explícita y convive con los dos esquemas.
func TestNeoForgeAntiguoYModernoNormalizados(t *testing.T) {
	list := []Version{
		{Version: "21.1.9-beta", Stable: false, Channel: ChannelBeta},
		{Version: "21.1.72-beta", Stable: false, Channel: ChannelBeta},
		{Version: "26.1.0.3", Stable: true, Channel: ChannelRelease},
		{Version: "26.1.0.10", Stable: true, Channel: ChannelRelease},
	}
	out := normalizeMaven("neoforge", list)
	if len(out) != 2 {
		t.Fatalf("quiero 2 versiones de Minecraft, tengo %d: %+v", len(out), out)
	}
	byMC := map[string]Version{}
	for _, v := range out {
		byMC[v.Version] = v
	}
	if byMC["1.21.1"].Build != "21.1.72-beta" {
		t.Fatalf("1.21.1 = %+v; el máximo es NUMÉRICO (21.1.72)", byMC["1.21.1"])
	}
	if byMC["26.1"].Build != "26.1.0.10" {
		t.Fatalf("26.1 = %+v; el máximo es 26.1.0.10", byMC["26.1"])
	}
}

// TestComparacionPorMotor: la estrategia de orden depende del esquema y, en
// vanilla, de la fecha de publicación autoritativa.
func TestComparacionPorMotor(t *testing.T) {
	// Vanilla: la fecha manda (el manifiesto la publica).
	a := Version{Version: "26.3", ReleasedAt: mustTime(t, "2026-09-13T00:00:00Z")}
	b := Version{Version: "1.21.4", ReleasedAt: mustTime(t, "2024-12-03T00:00:00Z")}
	if compareForEngine("vanilla", b, a) >= 0 {
		t.Fatal("vanilla debe ordenar por fecha de publicación")
	}
	if compareForEngine("paper", b, a) >= 0 {
		t.Fatal("paper debe ordenar por versión estructurada")
	}
}

// TestCompatibilidadTrivalente (T-03): el estado es compatible, incompatible o
// desconocido, y NUNCA se presume compatible sin verificación.
func TestCompatibilidadTrivalente(t *testing.T) {
	base := CompatibilityInput{
		Engine:           Paper,
		MinecraftVersion: "26.3",
		Pins:             map[string]string{Paper.DepVar: "190"},
		JavaMajor:        25,
		JavaStatus:       JavaStatusInferred,
	}
	if got, why := CheckCompatibility(base); got != CompatibilityCompatible || why != "" {
		t.Fatalf("conjunto completo = (%q, %q); quiero compatible sin motivo", got, why)
	}
	// Sin el pin obligatorio del motor: desconocido, no compatible.
	noPin := base
	noPin.Pins = map[string]string{}
	if got, _ := CheckCompatibility(noPin); got != CompatibilityUnknown {
		t.Fatalf("sin pin obligatorio = %q; quiero unknown", got)
	}
	// Pin "latest" no es un pin válido.
	latestPin := base
	latestPin.Pins = map[string]string{Paper.DepVar: "latest"}
	if got, _ := CheckCompatibility(latestPin); got != CompatibilityUnknown {
		t.Fatalf("pin latest = %q; quiero unknown", got)
	}
	// Java desconocido: desconocido (nunca compatible por conveniencia).
	noJava := base
	noJava.JavaMajor, noJava.JavaStatus = 0, JavaStatusUnknown
	if got, _ := CheckCompatibility(noJava); got != CompatibilityUnknown {
		t.Fatalf("Java desconocido = %q; quiero unknown", got)
	}
	// Sin versión de Minecraft: desconocido.
	noMC := base
	noMC.MinecraftVersion = ""
	if got, _ := CheckCompatibility(noMC); got != CompatibilityUnknown {
		t.Fatalf("sin Minecraft = %q; quiero unknown", got)
	}
	// Motor desconocido: incompatible.
	badEngine := base
	badEngine.Engine = Engine{Name: "nope"}
	if got, _ := CheckCompatibility(badEngine); got != CompatibilityIncompatible {
		t.Fatalf("motor desconocido = %q; quiero incompatible", got)
	}
	// Imagen Docker incoherente con el Java requerido: incompatible.
	badImage := base
	badImage.DockerImage = "itzg/minecraft-server:java17"
	if got, _ := CheckCompatibility(badImage); got != CompatibilityIncompatible {
		t.Fatalf("imagen incoherente = %q; quiero incompatible", got)
	}
	// Una imagen con Java suficiente se acepta (el requisito es un mínimo).
	okImage := base
	okImage.DockerImage = "itzg/minecraft-server:java25"
	if got, why := CheckCompatibility(okImage); got != CompatibilityCompatible {
		t.Fatalf("imagen java25 con requisito 25 = (%q, %q); quiero compatible", got, why)
	}
	// Java declarado por el proveedor MÁS BAJO que la regla (Paper declara mínimo
	// 6 para 1.7.10) con una imagen que lo satisface: compatible.
	legacy := CompatibilityInput{
		Engine:           Paper,
		MinecraftVersion: "1.7.10",
		Pins:             map[string]string{Paper.DepVar: "1"},
		JavaMajor:        6,
		JavaStatus:       JavaStatusVerified,
		DockerImage:      "itzg/minecraft-server:java8",
	}
	if got, why := CheckCompatibility(legacy); got != CompatibilityCompatible {
		t.Fatalf("Java 6 con imagen java8 = (%q, %q); quiero compatible", got, why)
	}
}

// TestResolvedValidateExigeTodosLosPines (AC-06): no existe un objeto de
// resolución parcialmente válido.
func TestResolvedValidateExigeTodosLosPines(t *testing.T) {
	casos := []struct {
		name   string
		engine Engine
		res    Resolved
		ok     bool
	}{
		{"paper completo", Paper, Resolved{Version: "26.3", Pins: map[string]string{Paper.DepVar: "190"}}, true},
		{"paper sin build", Paper, Resolved{Version: "26.3", Pins: map[string]string{}}, false},
		{"paper build latest", Paper, Resolved{Version: "26.3", Pins: map[string]string{Paper.DepVar: "latest"}}, false},
		{"fabric sin loader", Fabric, Resolved{Version: "26.3", Pins: map[string]string{}}, false},
		{"fabric completo", Fabric, Resolved{Version: "26.3", Pins: map[string]string{Fabric.DepVar: "0.19.5"}}, true},
		{"forge sin versión del juego", Forge, Resolved{Pins: map[string]string{Forge.DepVar: "26.3-66.0.11"}}, false},
		{"vanilla sin pines", Vanilla, Resolved{Version: "26.3"}, true},
	}
	for _, c := range casos {
		err := c.res.Validate(c.engine)
		if c.ok && err != nil {
			t.Fatalf("%s: no debía fallar: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("%s: debía fallar (resolución parcial)", c.name)
		}
	}
}

// TestRegistryResolveNormalizaYValida: la ruta única de resolución normaliza el
// modelo (build, Java, imagen, compatibilidad) y valida los pins.
func TestRegistryResolveNormalizaYValida(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
	f.fixture(t, "/versions/26.3/builds", "paper_fill_v3", "builds_26.3.json")
	reg := &Registry{providers: map[string]Provider{
		Paper.Name: &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()},
	}}
	res, err := reg.Resolve(context.Background(), "paper", "26.3")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Version != "26.3" || res.Build != "190" {
		t.Fatalf("resolución = %+v", res)
	}
	if res.JavaMajor != 25 || res.JavaStatus != JavaStatusInferred {
		t.Fatalf("Java = %d (%s); 26.3 exige Java 25", res.JavaMajor, res.JavaStatus)
	}
	if res.Image != "itzg/minecraft-server:java25" {
		t.Fatalf("imagen = %q; quiero java25", res.Image)
	}
	if res.Compatibility != CompatibilityCompatible {
		t.Fatalf("compatibilidad = %q (%s)", res.Compatibility, res.Warning)
	}
	if res.Source != "paper" {
		t.Fatalf("procedencia = %q", res.Source)
	}
}

// mustTime parsea una fecha RFC3339 o falla la prueba.
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := parseReleaseTime(s)
	if err != nil {
		t.Fatalf("fecha %q: %v", s, err)
	}
	return parsed
}

// TestEnrichClasificaEstabilidadPorCanal cubre release, snapshot, beta, alpha y
// el caso de canal ausente (se infiere de la estabilidad de la fuente); la
// recomendada es la única release estable, no la más nueva a secas.
func TestEnrichClasificaEstabilidadPorCanal(t *testing.T) {
	list := []Version{
		{Version: "1.21.4", Stable: false, Channel: ChannelSnapshot, Compatibility: CompatibilityCompatible},
		{Version: "1.21.3", Stable: false, Channel: ChannelBeta, Compatibility: CompatibilityCompatible},
		{Version: "1.21.2", Stable: false, Channel: ChannelAlpha, Compatibility: CompatibilityCompatible},
		{Version: "1.21.1", Stable: false, Compatibility: CompatibilityCompatible},
		{Version: "1.21", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
	}
	out := Enrich("paper", list)
	byID := map[string]Version{}
	for _, v := range out {
		byID[v.Version] = v
	}
	for id, want := range map[string]Channel{
		"1.21.4": ChannelSnapshot,
		"1.21.3": ChannelBeta,
		"1.21.2": ChannelAlpha,
		"1.21.1": ChannelSnapshot, // canal ausente y no estable: snapshot
		"1.21":   ChannelRelease,
	} {
		if got := byID[id].Channel; got != want {
			t.Fatalf("canal de %s = %q; quiero %q", id, got, want)
		}
	}
	if got := versionsOf(out); !equalStrings(got, []string{"1.21", "1.21.1", "1.21.2", "1.21.3", "1.21.4"}) {
		t.Fatalf("orden = %v", got)
	}
	if !byID["1.21"].Recommended {
		t.Fatalf("la recomendada debe ser la única release (1.21): %+v", out)
	}
	if byID["1.21.4"].Recommended {
		t.Fatal("la más nueva (un snapshot) no puede ser la recomendada")
	}
}

// TestRecomendadaEsLaUltimaEstableEnLosCincoMotores: el algoritmo produce la
// recomendación correcta para cada ESQUEMA de motor.
func TestRecomendadaEsLaUltimaEstableEnLosCincoMotores(t *testing.T) {
	casos := map[string]struct {
		list []Version
		want string
	}{
		"paper": {
			list: []Version{
				{Version: "1.21.4", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
				{Version: "26.3-rc-3", Stable: false, Channel: ChannelBeta, Compatibility: CompatibilityCompatible},
				{Version: "26.3", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
			},
			want: "26.3",
		},
		"fabric": {
			// Newest-first, con un snapshot más nuevo que la última estable.
			list: []Version{
				{Version: "26.4-snapshot-4", Stable: false, Channel: ChannelSnapshot, Compatibility: CompatibilityCompatible},
				{Version: "26.3", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
				{Version: "26.1.2", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
			},
			want: "26.3",
		},
		"forge": {
			list: []Version{
				{Version: "26.2", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
				{Version: "26.10", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
			},
			want: "26.10",
		},
		"neoforge": {
			list: []Version{
				{Version: "1.21.1", Stable: false, Channel: ChannelBeta, Compatibility: CompatibilityCompatible},
				{Version: "26.1", Stable: true, Channel: ChannelRelease, Compatibility: CompatibilityCompatible},
			},
			want: "26.1",
		},
		"vanilla": {
			list: []Version{
				{Version: "26.3", Stable: true, Channel: ChannelRelease, ReleasedAt: mustTime(t, "2026-09-13T00:00:00Z"), Compatibility: CompatibilityCompatible},
				{Version: "1.21.4", Stable: true, Channel: ChannelRelease, ReleasedAt: mustTime(t, "2024-12-03T00:00:00Z"), Compatibility: CompatibilityCompatible},
				{Version: "26.4-snapshot-4", Stable: false, Channel: ChannelSnapshot, ReleasedAt: mustTime(t, "2026-10-10T00:00:00Z"), Compatibility: CompatibilityCompatible},
			},
			want: "26.3",
		},
	}
	for engine, c := range casos {
		out := Enrich(engine, c.list)
		got := ""
		for _, v := range out {
			if v.Recommended {
				got = v.Version
			}
		}
		if got != c.want {
			t.Fatalf("%s: recomendada = %q; quiero %q (%v)", engine, got, c.want, versionsOf(out))
		}
		// Ninguna release estable puede quedar DESPUÉS de la recomendada, y una
		// versión inestable más nueva (snapshot) no puede ser la recomendada.
		seen := false
		for _, v := range out {
			if v.Version == c.want {
				if !v.Stable || v.Channel != ChannelRelease {
					t.Fatalf("%s: %s debería ser release estable: %+v", engine, c.want, v)
				}
				seen = true
				continue
			}
			if seen && v.Stable && v.Channel == ChannelRelease {
				t.Fatalf("%s: hay una release estable posterior a la recomendada: %v", engine, versionsOf(out))
			}
		}
		if !seen {
			t.Fatalf("%s: la recomendada %q no está en el catálogo: %v", engine, c.want, versionsOf(out))
		}
	}
}
