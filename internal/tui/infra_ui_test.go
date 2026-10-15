package tui

import (
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/tui/svc"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// UX-06/§8.2: al iniciar, el daemon se comprueba y se fuerza su levantamiento; la
// primera vez sin servicio se OFRECE instalarlo como persistente (con confirmación)
// y, si se confirma, queda instalado.
func TestDaemonArranqueGarantizadoYConfirmacionDeServicio(t *testing.T) {
	s := svc.NewFakeWith(nil, svc.FakeOptions{InfraDaemonDown: true})
	a := NewApp(s, AppOptions{Theme: theme.Dark})

	msg := ejecutaCmd(a.ensureDaemonCmd())
	rm, ok := msg.(infraReadyMsg)
	if !ok || rm.what != "daemon" {
		t.Fatalf("se esperaba el resultado del daemon, tengo %T/%+v", msg, msg)
	}
	if rm.rep.Ready {
		t.Fatal("el fake debe simular el daemon apagado")
	}
	if len(rm.rep.Manual) == 0 || !strings.Contains(rm.rep.Manual[0], "idle daemon") {
		t.Fatalf("el fallo debe traer pasos manuales por SO, tengo %v", rm.rep.Manual)
	}

	a.Update(rm)
	if a.confirm == nil {
		t.Fatal("la primera vez sin servicio debe ofrecer instalarlo (con confirmación)")
	}
	if !a.daemonPrompted {
		t.Fatal("la oferta debe registrarse para no repetirse")
	}

	// Confirmar instala el servicio persistente.
	_, icmd := a.Update(confirmResultMsg{confirmed: true, action: "daemon.install"})
	im := ejecutaCmd(icmd)
	imr, ok := im.(infraReadyMsg)
	if !ok || imr.what != "daemon-install" {
		t.Fatalf("se esperaba el resultado de la instalación, tengo %T/%+v", im, im)
	}
	if !imr.rep.Ready {
		t.Fatalf("la instalación debía quedar lista: %+v", imr.rep)
	}
	a.Update(imr)
	if !s.Infra.DaemonInstalled() {
		t.Fatal("tras confirmar, el daemon debe quedar instalado como servicio")
	}
}

// Si el servicio ya está instalado, no se vuelve a ofrecer: solo se informa.
func TestDaemonInstaladoNoRepiteLaOferta(t *testing.T) {
	s := svc.NewFakeWith(nil, svc.FakeOptions{InfraDaemonDown: true, InfraDaemonInstalled: true})
	a := NewApp(s, AppOptions{Theme: theme.Dark})
	msg := ejecutaCmd(a.ensureDaemonCmd())
	a.Update(msg)
	if a.confirm != nil {
		t.Fatal("con el servicio ya instalado no debe pedirse confirmación")
	}
}

// §8.3: Docker apagado se detecta y se intenta levantar UNA sola vez con el
// mecanismo común, sin duplicar tareas.
func TestDockerSeLevantaUnaSolaVez(t *testing.T) {
	a := testApp()
	a.Update(pollDockerMsg{diag: dockerx.Diagnosis{State: dockerx.StateDaemonDown}})
	if !a.infraTried["docker"] {
		t.Fatal("Docker apagado debe disparar un intento de arranque")
	}
	if got := toastCount(a, "Levantando Docker"); got != 1 {
		t.Fatalf("debe avisarse «Levantando Docker…» una vez, tengo %d", got)
	}
	// Un segundo sondeo con Docker aún apagado no repite el intento.
	a.Update(pollDockerMsg{diag: dockerx.Diagnosis{State: dockerx.StateDaemonDown}})
	if got := toastCount(a, "Levantando Docker"); got != 1 {
		t.Fatalf("no deben duplicarse los intentos de arranque, tengo %d", got)
	}
}

func toastCount(a *App, sub string) int {
	n := 0
	for _, t := range a.toasts {
		if strings.Contains(t.Text, sub) {
			n++
		}
	}
	return n
}
