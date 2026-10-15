package engines

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Nota para la Fase 3 (mapeo de Java): Minecraft 26.x exige Java 25.
//   - El manifiesto de Mojang no declara Java, pero Fill v3 sí: el fixture real
//     `paper_fill_v3/versions.json` da `java.version.minimum = 25` para 26.3,
//     26.2, 26.1.2 y 26.1.1 (y 21 para 1.21.x, 17 para 1.19-1.20.4, 8 para
//     1.12-1.13 y 6 para 1.7.10).
//   - `JavaMajorFor("26.3")` devuelve hoy 8 (valor por defecto) y `JavaTag` no
//     conoce la etiqueta `java25`: ambas cosas son de engines.go (Fase 3).

// TestVanillaBasePorDefecto fija el manifiesto oficial de Mojang.
func TestVanillaBasePorDefecto(t *testing.T) {
	p := &vanillaProvider{engine: Vanilla}
	if got := p.Base(); got != "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json" {
		t.Fatalf("base de Vanilla = %q", got)
	}
	p.base = "http://127.0.0.1:1234/manifest.json"
	if p.Base() != "http://127.0.0.1:1234/manifest.json" {
		t.Fatalf("base inyectada ignorada: %q", p.Base())
	}
}

// TestVanillaListVersionsSoloReleases: ListVersions mantiene el contrato (solo
// releases, ascendente) y rellena canal y fecha.
func TestVanillaListVersionsSoloReleases(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/manifest.json", "mojang", "version_manifest_v2.json")

	client := staticTestClient()
	var records []LogRecord
	client.Log = func(rec LogRecord) { records = append(records, rec) }
	p := &vanillaProvider{engine: Vanilla, client: client, base: f.url() + "/manifest.json"}

	list, err := p.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("releases = %d; el fixture tiene 5", len(list))
	}
	for _, v := range list {
		if !v.Stable || v.Channel != ChannelRelease {
			t.Fatalf("%s = %+v; ListVersions solo devuelve releases estables", v.Version, v)
		}
		if v.ReleasedAt.IsZero() {
			t.Fatalf("%s sin fecha de publicación", v.Version)
		}
		if strings.Contains(v.Version, "snapshot") || strings.HasPrefix(v.Version, "a1.") {
			t.Fatalf("se coló una versión no publicada: %+v", v)
		}
	}
	// Ascendente (antiguo → nuevo): la última es la release más reciente del
	// manifiesto (`latest.release` = 26.3).
	if first, last := list[0].Version, list[len(list)-1].Version; first != "26.1" || last != "26.3" {
		t.Fatalf("orden = %s … %s; quiero 26.1 … 26.3", first, last)
	}
	for i := 1; i < len(list); i++ {
		if list[i].ReleasedAt.Before(list[i-1].ReleasedAt) {
			t.Fatalf("las fechas no van ascendentes en %d: %+v", i, list)
		}
	}
	if len(records) != 1 || records[0].Provider != "vanilla" || records[0].Operation != "list_releases" {
		t.Fatalf("diagnóstico = %+v; quiero vanilla/list_releases", records)
	}
	f.requireHits(t, "/manifest.json")
}

// TestVanillaListAllVersionsDiferenciaCanales: releases, snapshots y versiones
// antiguas se distinguen por canal, con fecha real y orden por fecha.
func TestVanillaListAllVersionsDiferenciaCanales(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/manifest.json", "mojang", "version_manifest_v2.json")

	client := staticTestClient()
	var records []LogRecord
	client.Log = func(rec LogRecord) { records = append(records, rec) }
	p := &vanillaProvider{engine: Vanilla, client: client, base: f.url() + "/manifest.json"}

	all, err := p.ListAllVersions(context.Background())
	if err != nil {
		t.Fatalf("ListAllVersions: %v", err)
	}
	if len(all) != 80 {
		t.Fatalf("versiones = %d; el fixture tiene 80 entradas", len(all))
	}
	byID := map[string]Version{}
	for _, v := range all {
		byID[v.Version] = v
		if v.ReleasedAt.IsZero() {
			t.Fatalf("%s sin fecha de publicación", v.Version)
		}
	}
	// release (26.3) → canal release y estable.
	if v := byID["26.3"]; v.Channel != ChannelRelease || !v.Stable {
		t.Fatalf("26.3 = %+v; quiero release estable", v)
	}
	// snapshot (26.4-snapshot-4, y 26.3-rc-3 que Mojang también publica como
	// snapshot) → canal snapshot y no estable.
	for _, id := range []string{"26.4-snapshot-4", "26.3-rc-3"} {
		if v, ok := byID[id]; !ok || v.Channel != ChannelSnapshot || v.Stable {
			t.Fatalf("%s = %+v; quiero snapshot no estable", id, v)
		}
	}
	// old_alpha → canal alpha y no estable.
	if v := byID["a1.0.5_01"]; v.Channel != ChannelAlpha || v.Stable {
		t.Fatalf("a1.0.5_01 = %+v; quiero canal alpha", v)
	}
	// Orden ascendente por fecha: la más antigua es de 2009 y la más nueva, el
	// snapshot de hoy.
	if all[0].Version != "rd-132211" || !all[0].ReleasedAt.Equal(time.Date(2009, 5, 13, 20, 11, 0, 0, time.UTC)) {
		t.Fatalf("primera versión = %+v; quiero rd-132211 de 2009-05-13", all[0])
	}
	if last := all[len(all)-1].Version; last != "26.4-snapshot-4" {
		t.Fatalf("última versión = %q; quiero 26.4-snapshot-4", last)
	}
	if len(records) != 1 || records[0].Operation != "list_all_versions" {
		t.Fatalf("diagnóstico = %+v; quiero vanilla/list_all_versions", records)
	}
}

// TestVanillaResolveVersionExplicitaYUltima resuelve una release concreta y la
// última del canal (26.3, el `latest.release` del manifiesto real).
func TestVanillaResolveVersionExplicitaYUltima(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/manifest.json", "mojang", "version_manifest_v2.json")
	p := &vanillaProvider{engine: Vanilla, client: staticTestClient(), base: f.url() + "/manifest.json"}

	res, err := p.Resolve(context.Background(), "26.1.1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Version != "26.1.1" || len(res.Pins) != 0 {
		t.Fatalf("resolución = %+v; vanilla no necesita pines", res)
	}
	latest, err := p.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve (última): %v", err)
	}
	if latest.Version != "26.3" {
		t.Fatalf("última release = %q; quiero 26.3", latest.Version)
	}
}

// TestVanillaResolveVersionInexistenteFalla: una versión que no existe da error
// tipado, sin resolución parcial.
func TestVanillaResolveVersionInexistenteFalla(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/manifest.json", "mojang", "version_manifest_v2.json")
	p := &vanillaProvider{engine: Vanilla, client: staticTestClient(), base: f.url() + "/manifest.json"}

	res, err := p.Resolve(context.Background(), "0.0.0-nope")
	if err == nil {
		t.Fatalf("resolver una versión inexistente debía fallar: %+v", res)
	}
	if code := CodeOf(err); code != CodeNotFound {
		t.Fatalf("código = %q; quiero %q (%v)", code, CodeNotFound, err)
	}
	if !strings.Contains(err.Error(), "0.0.0-nope") {
		t.Fatalf("el error debe nombrar la versión pedida: %v", err)
	}
	if res.Version != "" {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
}

// TestVanillaSinRedOFuenteRotaFalla: ni un 5xx ni un documento con otra forma
// pueden acabar en un catálogo vacío silencioso.
func TestVanillaSinRedOFuenteRotaFalla(t *testing.T) {
	t.Run("servidor 500", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.status("/manifest.json", http.StatusInternalServerError)
		p := &vanillaProvider{engine: Vanilla, client: staticTestClient(), base: f.url() + "/manifest.json"}
		_, err := p.ListVersions(context.Background())
		if code := CodeOf(err); code != CodeUpstreamUnavailable {
			t.Fatalf("código = %q; quiero %q (%v)", code, CodeUpstreamUnavailable, err)
		}
	})
	t.Run("servidor apagado", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close()
		p := &vanillaProvider{engine: Vanilla, client: staticTestClient(), base: srv.URL}
		if _, err := p.ListVersions(context.Background()); err == nil {
			t.Fatal("sin red debe fallar, no devolver un catálogo vacío")
		}
	})
	t.Run("esquema distinto", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.json("/manifest.json", `{"latest":{"release":"26.3"},"items":[]}`)
		p := &vanillaProvider{engine: Vanilla, client: staticTestClient(), base: f.url() + "/manifest.json"}
		err := mustFail(t, func() error { _, err := p.ListAllVersions(context.Background()); return err })
		if code := CodeOf(err); code != CodeSchemaMismatch {
			t.Fatalf("código = %q; quiero %q", code, CodeSchemaMismatch)
		}
	})
	t.Run("manifiesto vacio", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.json("/manifest.json", `{"versions":[]}`)
		p := &vanillaProvider{engine: Vanilla, client: staticTestClient(), base: f.url() + "/manifest.json"}
		err := mustFail(t, func() error { _, err := p.ListVersions(context.Background()); return err })
		if code := CodeOf(err); code != CodeSchemaMismatch {
			t.Fatalf("código = %q; quiero %q", code, CodeSchemaMismatch)
		}
	})
	t.Run("sin releases", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.json("/manifest.json", `{"versions":[{"id":"26.4-snapshot-1","type":"snapshot","releaseTime":"2026-10-01T00:00:00+00:00"}]}`)
		p := &vanillaProvider{engine: Vanilla, client: staticTestClient(), base: f.url() + "/manifest.json"}
		err := mustFail(t, func() error { _, err := p.ListVersions(context.Background()); return err })
		if code := CodeOf(err); code != CodeNotFound {
			t.Fatalf("código = %q; quiero %q", code, CodeNotFound)
		}
	})
	t.Run("fecha no RFC3339", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.json("/manifest.json", `{"versions":[{"id":"26.3","type":"release","releaseTime":"13/09/2026"}]}`)
		p := &vanillaProvider{engine: Vanilla, client: staticTestClient(), base: f.url() + "/manifest.json"}
		err := mustFail(t, func() error { _, err := p.ListAllVersions(context.Background()); return err })
		if code := CodeOf(err); code != CodeSchemaMismatch {
			t.Fatalf("código = %q; quiero %q", code, CodeSchemaMismatch)
		}
	})
}

// mustFail exige que la operación falle y devuelve su error.
func mustFail(t *testing.T, op func() error) error {
	t.Helper()
	err := op()
	if err == nil {
		t.Fatal("se esperaba un error")
	}
	return err
}

// TestVanillaCanalesYFechasUnitarios cubre las funciones puras del proveedor.
func TestVanillaCanalesYFechasUnitarios(t *testing.T) {
	cases := []struct {
		kind    string
		channel Channel
		stable  bool
	}{
		{"release", ChannelRelease, true},
		{"Release", ChannelRelease, true},
		{"snapshot", ChannelSnapshot, false},
		{"old_beta", ChannelBeta, false},
		{"old_alpha", ChannelAlpha, false},
		{"nuevo-tipo-desconocido", ChannelSnapshot, false},
		{"", ChannelSnapshot, false},
	}
	for _, c := range cases {
		channel, stable := vanillaChannel(c.kind)
		if channel != c.channel || stable != c.stable {
			t.Fatalf("vanillaChannel(%q) = %q/%v; quiero %q/%v", c.kind, channel, stable, c.channel, c.stable)
		}
	}

	at, err := parseReleaseTime("2026-09-13T12:05:46+00:00")
	if err != nil {
		t.Fatalf("parseReleaseTime: %v", err)
	}
	if !at.Equal(time.Date(2026, 9, 13, 12, 5, 46, 0, time.UTC)) {
		t.Fatalf("fecha = %v", at)
	}
	if zero, err := parseReleaseTime("  "); err != nil || !zero.IsZero() {
		t.Fatalf("una fecha vacía se acepta como desconocida: %v / %v", zero, err)
	}
	if _, err := parseReleaseTime("13/09/2026"); err == nil {
		t.Fatal("una fecha malformada debe fallar")
	}
}
