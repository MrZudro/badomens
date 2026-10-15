package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// ===========================================================================
// La TUI consume el servicio de catálogo (T-01) y descarta respuestas tardías (§7)
// ===========================================================================

// TestAsistenteDescartaRespuestaTardia: la respuesta del catálogo de un motor
// pedido ANTES de cambiar de motor no puede sustituir la selección vigente.
func TestAsistenteDescartaRespuestaTardia(t *testing.T) {
	w := newWizardScreen(svc.NewFake(nil), theme.For(theme.Dark), false)
	if cmd := w.start(0); cmd == nil {
		t.Fatal("el arranque debe pedir las versiones")
	}
	staleSeq := w.verSeq
	if staleSeq == 0 {
		t.Fatal("cada petición de catálogo debe llevar su secuencia")
	}
	stale := wizardVersionsMsg{
		engine: "paper", snaps: false, seq: staleSeq,
		versions: []engines.Version{{Version: "1.20.1", Channel: engines.ChannelRelease, Compatible: true}},
	}

	// El usuario cambia de motor: la petición anterior queda obsoleta.
	w.cur = 1 // campo Motor
	if cmd := w.cycle(1); cmd == nil {
		t.Fatal("cambiar de motor debe pedir las versiones del nuevo motor")
	}
	if w.engine.Name != "fabric" {
		t.Fatalf("motor = %q; quiero fabric", w.engine.Name)
	}

	// Llega la respuesta TARDÍA del motor anterior: se descarta.
	w.Update(stale)
	for _, v := range w.versions {
		if v.Version == "1.20.1" {
			t.Fatalf("una respuesta tardía no debe aplicarse: %v", w.versions)
		}
	}
	if !w.verLoading {
		t.Fatal("la pantalla debe seguir esperando el catálogo del motor vigente")
	}
	// Y sí queda en la caché por motor: es válida para la clave que la pidió.
	if got := w.verCache[verKeyOf("paper", false)]; len(got) != 1 || got[0].Version != "1.20.1" {
		t.Fatalf("la lista debe cachearse por motor: %v", w.verCache)
	}

	// La respuesta VIGENTE sí se aplica y elige la recomendada.
	w.Update(wizardVersionsMsg{
		engine: "fabric", snaps: false, seq: w.verSeq,
		versions: []engines.Version{{Version: "1.21.1", Channel: engines.ChannelRelease, Recommended: true, Compatible: true}},
	})
	if len(w.versions) != 1 || w.versions[0].Version != "1.21.1" || w.verLoading {
		t.Fatalf("la respuesta vigente debe aplicarse: %+v", w.versions)
	}
	if got := w.versionValue(); got != "1.21.1" {
		t.Fatalf("debe quedar elegida la recomendada, tengo %q", got)
	}
}

// TestInstanciaCambiarVersionGuardaPins: el cambio de versión pide la resolución
// al servicio y guarda versión Y pins juntos (AC-06: no hay media resolución).
func TestInstanciaCambiarVersionGuardaPins(t *testing.T) {
	s, spec := testInstance()
	spec.Engine = "paper"
	msg := ejecutaCmd(s.changeVersionCmd("1.20.1"))
	if toast, ok := msg.(toastMsg); !ok || strings.Contains(toast.Text, "✖") {
		t.Fatalf("el cambio debía aplicarse, tengo %#v", msg)
	}
	if spec.Version != "1.20.1" {
		t.Fatalf("versión = %q", spec.Version)
	}
	if spec.Pins[engines.Paper.DepVar] == "" {
		t.Fatalf("los pins deben guardarse CON la versión: %v", spec.Pins)
	}
}

// TestInstanciaCambiarVersionNoAplicaSiFallaResolucion: si el servicio no puede
// resolver, no se hace backup ni se toca la versión ni los pins.
func TestInstanciaCambiarVersionNoAplicaSiFallaResolucion(t *testing.T) {
	s, spec := testInstance()
	before := spec.Pins
	s.svc = svc.NewFakeWith([]*instance.Spec{spec}, svc.FakeOptions{
		CatalogResolveErr: errors.New("catálogo caído"),
	})
	msg := ejecutaCmd(s.changeVersionCmd("9.9.9"))
	if spec.Version != "1.21.4" {
		t.Fatalf("la versión no debía cambiar: %q", spec.Version)
	}
	if spec.Pins != nil || before != nil {
		t.Fatalf("los pins no debían cambiar: %v", spec.Pins)
	}
	toast, ok := msg.(toastMsg)
	if !ok || !strings.Contains(toast.Text, "resolver") {
		t.Fatalf("quiero un aviso explícito del fallo, tengo %#v", msg)
	}
}

// TestFachadaCatalogoDeLaTUIEsElServicio: la fachada svc expone lista, resolución
// y estado, y las tres cosas vienen del mismo servicio de catálogo.
func TestFachadaCatalogoDeLaTUIEsElServicio(t *testing.T) {
	catalog := svc.NewFake(nil).Catalog
	if _, err := catalog.Resolve(context.Background(), "paper", "1.20.1"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	st, err := catalog.Status(context.Background(), "paper")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Usable() {
		t.Fatalf("estado del catálogo = %+v", st)
	}
	list, err := catalog.Versions(context.Background(), "paper", false)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(list.Versions) == 0 || list.Status.State == "" {
		t.Fatalf("la lista debe traer versiones y su estado: %+v", list)
	}
	if _, err := catalog.Refresh(context.Background(), "paper"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
}
