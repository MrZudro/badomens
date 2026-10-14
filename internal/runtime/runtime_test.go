package runtime

import (
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/mcuuid"
)

// TestRenderEligeJavaPorVersion verifica que el compose usa la imagen de itzg
// con el Java que requiere la versión de Minecraft (RF-5).
func TestRenderEligeJavaPorVersion(t *testing.T) {
	cfg := config.Default()
	rt := New(cfg)
	casos := []struct {
		version string
		want    string
	}{
		{"1.21.4", "image: itzg/minecraft-server:java21"},
		{"1.20.1", "image: itzg/minecraft-server:java17"},
		{"1.17.1", "image: itzg/minecraft-server:java16"},
		{"desconocida", "image: itzg/minecraft-server:java25"},
	}
	for _, c := range casos {
		spec := &instance.Spec{Slug: "demo", Name: "Demo", Engine: "paper", Version: c.version, Memory: "4G", Port: 25565}
		raw, err := rt.Render(spec, cfg, "100.64.0.1")
		if err != nil {
			t.Fatalf("Render(%s): %v", c.version, err)
		}
		if !strings.Contains(string(raw), c.want) {
			t.Errorf("Render(%s) no contiene %q:\n%s", c.version, c.want, raw)
		}
	}
}

// TestImageForRespetaOverride comprueba que una imagen personalizada conserva su
// repositorio al adaptar la etiqueta de Java.
func TestImageForRespetaOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Image = "mi/registro/itzg-mc:java25"
	rt := New(cfg)
	spec := &instance.Spec{Slug: "demo", Engine: "paper", Version: "1.21.4"}
	if got := rt.imageFor(spec); got != "mi/registro/itzg-mc:java21" {
		t.Errorf("imageFor = %q, quiero mi/registro/itzg-mc:java21", got)
	}
}

// TestRenderOfflineUsaUUIDEnWhitelistYOps comprueba que, con ONLINE_MODE=FALSE,
// el compose envía el UUID offline (versión 3) en WHITELIST/OPS en lugar del
// nombre. Si se enviara el nombre, la imagen lo resolvería contra PlayerDB y
// escribiría un UUID premium que no coincide con el del servidor sin
// autenticación: el jugador sería expulsado al entrar.
func TestRenderOfflineUsaUUIDEnWhitelistYOps(t *testing.T) {
	cfg := config.Default()
	rt := New(cfg)
	spec := &instance.Spec{
		Slug: "demo", Name: "Demo", Engine: "paper", Version: "1.21.4", Memory: "4G", Port: 25565,
		OnlineMode:       false,
		WhitelistEnabled: true,
		Whitelist:        []string{"Notch", "jeb_", ""}, // la entrada vacía se ignora
		Ops:              []string{"Notch"},
	}
	raw, err := rt.Render(spec, cfg, "100.64.0.1")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(raw)
	uuidNotch := mcuuid.Offline("Notch")
	uuidJeb := mcuuid.Offline("jeb_")
	if want := "WHITELIST: \"" + uuidNotch + "," + uuidJeb + "\""; !strings.Contains(got, want) {
		t.Errorf("WHITELIST no contiene los UUID offline (%s):\n%s", want, got)
	}
	if want := "OPS: \"" + uuidNotch + "\""; !strings.Contains(got, want) {
		t.Errorf("OPS no contiene el UUID offline (%s):\n%s", want, got)
	}
	if strings.Contains(got, "Notch") || strings.Contains(got, "jeb_") {
		t.Errorf("en modo offline el compose no debe llevar nombres de jugador:\n%s", got)
	}
	if !strings.Contains(got, "ONLINE_MODE: \"FALSE\"") {
		t.Errorf("ONLINE_MODE debería seguir en FALSE:\n%s", got)
	}
	if !strings.Contains(got, "ENABLE_WHITELIST: \"TRUE\"") {
		t.Errorf("ENABLE_WHITELIST debe reflejar WhitelistEnabled:\n%s", got)
	}
}

// TestRenderOnlineUsaNombres comprueba que en modo premium el compose envía los
// nombres tal cual (la imagen los resuelve contra PlayerDB) y que no aparece
// ningún UUID offline.
func TestRenderOnlineUsaNombres(t *testing.T) {
	cfg := config.Default()
	rt := New(cfg)
	spec := &instance.Spec{
		Slug: "demo", Name: "Demo", Engine: "paper", Version: "1.21.4", Memory: "4G", Port: 25565,
		OnlineMode:       true,
		WhitelistEnabled: false,
		Whitelist:        []string{"Notch", "ana"},
		Ops:              []string{"ana"},
	}
	raw, err := rt.Render(spec, cfg, "100.64.0.1")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "WHITELIST: \"Notch,ana\"") {
		t.Errorf("WHITELIST debería llevar los nombres tal cual:\n%s", got)
	}
	if !strings.Contains(got, "OPS: \"ana\"") {
		t.Errorf("OPS debería llevar los nombres tal cual:\n%s", got)
	}
	if !strings.Contains(got, "ONLINE_MODE: \"TRUE\"") {
		t.Errorf("ONLINE_MODE debería ser TRUE:\n%s", got)
	}
	if !strings.Contains(got, "ENABLE_WHITELIST: \"FALSE\"") {
		t.Errorf("ENABLE_WHITELIST debe reflejar WhitelistEnabled:\n%s", got)
	}
	if uuid := mcuuid.Offline("Notch"); strings.Contains(got, uuid) {
		t.Errorf("en modo online no debe aparecer el UUID offline %s:\n%s", uuid, got)
	}
}

// TestEntriesValueDedupYVacios comprueba el helper de forma directa: ignora
// vacíos, deduplica exactamente y conserva el orden de entrada.
func TestEntriesValueDedupYVacios(t *testing.T) {
	if got := entriesValue(nil, false); got != "" {
		t.Errorf("entriesValue(nil, false) = %q, quiero cadena vacía", got)
	}
	if got := entriesValue([]string{"", "  "}, false); got != "" {
		t.Errorf("entriesValue(solo vacíos, false) = %q, quiero cadena vacía", got)
	}
	got := entriesValue([]string{"Notch", "", "jeb_", "Notch"}, false)
	want := mcuuid.Offline("Notch") + "," + mcuuid.Offline("jeb_")
	if got != want {
		t.Errorf("entriesValue offline = %q, quiero %q", got, want)
	}
	if got := entriesValue([]string{"ana", "luis", "ana"}, true); got != "ana,luis" {
		t.Errorf("entriesValue online = %q, quiero \"ana,luis\"", got)
	}
	if got := entriesValue([]string{"ana"}, true); got != "ana" {
		t.Errorf("entriesValue online simple = %q, quiero \"ana\"", got)
	}
}
