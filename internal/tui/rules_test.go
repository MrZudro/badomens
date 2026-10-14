package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/help"
	"github.com/MrZudro/badomens/internal/tui/i18n"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// TestLintDeBotonesReales (RF-7/§10.3) recorre las acciones de TODAS las
// pantallas reales (no solo acciones sintéticas) y falla si alguna incumple las
// reglas: etiqueta genérica, Hint ausente o Disabled sin motivo.
func TestLintDeBotonesReales(t *testing.T) {
	sv := svc.NewFake([]*instance.Spec{{
		Slug: "survival", Name: "Survival", Engine: "paper", Version: "1.21.4",
		Memory: "4G", Port: 25565, MaxPlayers: 20,
	}})
	pal := theme.For(theme.Dark)

	home := newHomeScreen(sv, pal, false)
	home.Update(pollRowsMsg{rows: []svc.Row{{Slug: "survival", Name: "Survival", State: "running"}}})

	inst := newInstanceScreen(sv, pal, false, "survival")
	wiz := newWizardScreen(sv, pal, false)
	wiz.start(1)
	health := newHealthScreen(sv, pal, false)

	screens := []struct {
		name    string
		actions []components.Action
	}{
		{"home", home.Actions()},
		{"instance", inst.Actions()},
		{"wizard", wiz.Actions()},
		{"health", health.Actions()},
	}
	// La paleta mezcla las acciones de la pantalla con las globales: replicamos
	// las globales aquí para no depender del App.
	screens = append(screens, struct {
		name    string
		actions []components.Action
	}{"globales", []components.Action{
		{ID: "global.health", Verb: "Abrir", Object: "Salud del sistema", Key: "H", Hint: "Muestra Docker, Tailscale, disco y RAM.", Kind: components.Navigation},
		{ID: "global.new", Verb: "Crear", Object: "una instancia nueva", Key: "n", Hint: "Abre el asistente.", Kind: components.Primary},
		{ID: "global.help", Verb: "Ver", Object: "la ayuda", Key: "?", Hint: "Muestra los atajos.", Kind: components.Navigation},
	}})

	for _, sc := range screens {
		for _, a := range sc.actions {
			if problems := a.Validate(); len(problems) > 0 {
				t.Errorf("pantalla %s, acción %q: %v", sc.name, a.ID, problems)
			}
		}
	}
}

// TestClavesI18nUsadasExisten (§10.3, i18n): análisis estático sobre TODAS las
// pantallas (incluidas las subcarpetas). Falla si se usa una clave del catálogo
// que no existe.
func TestClavesI18nUsadasExisten(t *testing.T) {
	cat := i18n.Es()
	re := regexp.MustCompile(`\.Tf?\("([^"]+)"`)
	var files []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	used := 0
	for _, f := range files {
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			used++
			if !cat.Has(m[1]) {
				t.Errorf("%s usa la clave i18n %q que no existe en el catálogo", f, m[1])
			}
		}
	}
	if used == 0 {
		t.Fatal("no se encontró ningún uso de i18n; la carcasa debe usar el catálogo")
	}
}

// TestI18nCarcasaCableada (§10.3, i18n estricto): la carcasa de la app (cabecera,
// banners, toasts, salida) debe tomar sus textos del catálogo, no en línea.
func TestI18nCarcasaCableada(t *testing.T) {
	raw, err := os.ReadFile("app_v2.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	keys := []string{
		"app.too_small", "app.header.docker_ok", "app.header.docker_bad",
		"app.banner.docker_down", "app.banner.docker_starting", "app.footer.hint",
		"app.toast.docker_down", "app.toast.docker_up", "app.toast.repairing",
		"app.toast.created", "app.toast.idle_no_watcher", "app.toast.idle_stopped",
		"app.quit.title", "app.disabled.docker",
		"global.health.obj", "global.new.hint", "global.help.hint",
	}
	for _, k := range keys {
		if !strings.Contains(src, `"`+k+`"`) {
			t.Errorf("app_v2.go no toma %q del catálogo i18n", k)
		}
	}
}

// TestCamposDelAsistenteTienenAyuda (RF-4/§10.3) recorre el registro de campos
// del asistente y falla si algún campo no tiene descripción corta (≤ 90) y larga.
func TestCamposDelAsistenteTienenAyuda(t *testing.T) {
	sv := svc.NewFake(nil)
	wiz := newWizardScreen(sv, theme.For(theme.Dark), false)
	for _, mode := range []int{0, 1} {
		wiz.start(mode)
		for _, st := range wiz.steps {
			for _, f := range st.fields {
				h, ok := help.Get(f.key)
				if !ok {
					t.Errorf("el campo %q del paso %q no tiene entrada en el registro de ayudas", f.key, st.title)
					continue
				}
				if h.Short == "" {
					t.Errorf("el campo %q no tiene descripción corta", f.key)
				}
				if len([]rune(h.Short)) > 90 {
					t.Errorf("la descripción corta de %q supera 90 caracteres (%d)", f.key, len([]rune(h.Short)))
				}
				if h.Long == "" {
					t.Errorf("el campo %q no tiene descripción larga", f.key)
				}
			}
		}
	}
}
