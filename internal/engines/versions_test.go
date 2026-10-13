package engines

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 2 * time.Second}, UserAgent: "badomens-test"}
}

func regWith(p Provider) *Registry {
	return &Registry{providers: map[string]Provider{p.Engine().Name: p}}
}

func TestEnrichMarcaRecomendadaYJava(t *testing.T) {
	list := []Version{
		{Version: "1.20.1", Stable: true, Channel: ChannelRelease},
		{Version: "1.21", Stable: true, Channel: ChannelRelease},
	}
	out := Enrich("paper", list)
	if !out[1].Recommended {
		t.Fatal("la última estable debe ser la recomendada")
	}
	if out[1].JavaMajor != 21 {
		t.Fatalf("1.21 → Java 21, tengo %d", out[1].JavaMajor)
	}
	if out[0].JavaMajor != 17 {
		t.Fatalf("1.20.1 → Java 17, tengo %d", out[0].JavaMajor)
	}
	if !out[0].Compatible {
		t.Fatal("las versiones del catálogo deben ser compatibles")
	}
}

func TestEnrichNormalizaMavenDeForge(t *testing.T) {
	list := []Version{
		{Version: "1.21.1-52.0.1", Stable: true},
		{Version: "1.21.1-52.0.5", Stable: true},
		{Version: "1.20.1-47.3.0", Stable: true},
	}
	out := normalizeMaven(list)
	if len(out) != 2 {
		t.Fatalf("quiero 2 versiones de Minecraft, tengo %d: %+v", len(out), out)
	}
	for _, v := range out {
		if v.Version != "1.21.1" && v.Version != "1.20.1" {
			t.Fatalf("ID inesperada: %q", v.Version)
		}
		if v.Version == "1.21.1" && v.Build != "52.0.5" {
			t.Fatalf("quiero el build más reciente, tengo %+v", v)
		}
	}
}

func TestCacheVanillaConFechas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"versions":[
			{"id":"1.21.1","type":"release","releaseTime":"2024-10-08T00:00:00Z"},
			{"id":"1.21.2","type":"snapshot","releaseTime":"2024-10-10T00:00:00Z"},
			{"id":"1.21","type":"release","releaseTime":"2024-06-13T00:00:00Z"}
		]}`))
	}))
	defer srv.Close()

	cli := testClient()
	reg := regWith(&vanillaProvider{engine: Vanilla, client: cli, base: srv.URL})
	c := NewCache(t.TempDir(), reg)

	versions, stale, err := c.Versions(context.Background(), "vanilla", VersionOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatal("primera carga: no debe ser vencida")
	}
	if len(versions) != 2 {
		t.Fatalf("sin incluir snapshots quiero 2, tengo %d: %+v", len(versions), versions)
	}
	if !versions[len(versions)-1].Recommended {
		t.Fatal("quiero la recomendada marcada")
	}
	if versions[0].ReleasedAt.IsZero() {
		t.Fatal("quiero la fecha de publicación de vanilla")
	}
	if !versions[0].ReleasedAt.Before(versions[1].ReleasedAt) {
		t.Fatal("las versiones deben ir ordenadas por fecha")
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
		_, _ = w.Write([]byte(`{"versions":[{"id":"1.21","type":"release","releaseTime":"2024-06-13T00:00:00Z"}]}`))
	}))
	defer srv.Close()

	cli := testClient()
	reg := regWith(&vanillaProvider{engine: Vanilla, client: cli, base: srv.URL})
	dir := t.TempDir()
	c := NewCache(dir, reg)
	ctx := context.Background()

	// Envejecemos la caché: primera carga ahora, luego forzamos que pasen 7 h.
	now := time.Now()
	c.Now = func() time.Time { return now }
	if _, _, err := c.Versions(ctx, "vanilla", VersionOpts{}); err != nil {
		t.Fatal(err)
	}

	// Falla la red y la caché está vencida: se sirve igualmente, marcado stale.
	engineDown = true
	c.Now = func() time.Time { return now.Add(7 * time.Hour) }
	versions, stale, err := c.Versions(ctx, "vanilla", VersionOpts{})
	if err != nil {
		t.Fatalf("sin red quiero caché vencida, no error: %v", err)
	}
	if !stale {
		t.Fatal("quiero stale=true al usar datos de hace 7 h")
	}
	if len(versions) != 1 || versions[0].Version != "1.21" {
		t.Fatalf("caché: %+v", versions)
	}
}

func TestCacheReutilizaFrescaComoTTL(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"versions":[{"id":"1.21","type":"release","releaseTime":"2024-06-13T00:00:00Z"}]}`))
	}))
	defer srv.Close()

	cli := testClient()
	reg := regWith(&vanillaProvider{engine: Vanilla, client: cli, base: srv.URL})
	c := NewCache(t.TempDir(), reg)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, _, err := c.Versions(ctx, "vanilla", VersionOpts{}); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 1 {
		t.Fatalf("la caché fresca debe evitar la red: %d peticiones", requests)
	}
}
