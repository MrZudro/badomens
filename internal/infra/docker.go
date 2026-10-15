package infra

import (
	"context"
	"strings"

	"github.com/MrZudro/badomens/internal/dockerx"
)

// Docker adapta el diagnóstico y el plan de dockerx al mecanismo común. No
// duplica lógica: reutiliza `dockerx.Diagnose` y `dockerx.Plan`.
type Docker struct {
	// Env es el entorno inyectable de dockerx (vacío = real).
	Env dockerx.Env
}

// NewDocker construye el componente Docker con el entorno dado.
func NewDocker(env dockerx.Env) Docker { return Docker{Env: env} }

// Domain identifica el componente.
func (Docker) Domain() Domain { return DomainDocker }

// Detect traduce el diagnóstico de dockerx a un estado tipado.
func (d Docker) Detect(ctx context.Context) State {
	return dockerState(dockerx.Diagnose(ctx, d.Env))
}

// StartPlan reutiliza el plan de arranque por SO de dockerx.
func (d Docker) StartPlan(st State, platform string) []Step {
	diag, _ := st.raw.(dockerx.Diagnosis)
	if platform == "" {
		platform = diag.Platform
	}
	diag.Platform = platform
	return stepsFromDocker(dockerx.Plan(diag, platform))
}

// InstallHint reutiliza el paso de instalación de dockerx (nunca se instala solo).
func (Docker) InstallHint(platform string) string {
	steps := dockerx.Plan(dockerx.Diagnosis{State: dockerx.StateNotInstalled, Platform: platform}, platform)
	for _, s := range steps {
		if s.Instruction != "" {
			return s.Instruction
		}
	}
	return dockerManualDefault(platform)
}

// ManualGuide devuelve los pasos manuales por SO para el estado dado.
func (d Docker) ManualGuide(st State, platform string) []string {
	diag, _ := st.raw.(dockerx.Diagnosis)
	diag.Platform = platform
	var out []string
	for _, s := range dockerx.Plan(diag, platform) {
		switch {
		case strings.TrimSpace(s.Instruction) != "":
			out = append(out, s.Instruction)
		case len(s.Command) > 0:
			out = append(out, strings.Join(s.Command, " "))
		}
	}
	if len(out) == 0 {
		out = append(out, dockerManualDefault(platform))
	}
	return out
}

// dockerState traduce un dockerx.Diagnosis a un infra.State.
func dockerState(diag dockerx.Diagnosis) State {
	st := State{
		Domain:  DomainDocker,
		Reason:  diag.Reason,
		Message: diag.Message,
		Detail:  diag.Detail,
		Extra: map[string]any{
			"state":           string(diag.State),
			"platform":        diag.Platform,
			"server_version":  diag.ServerVersion,
			"compose_version": diag.ComposeVersion,
			"context":         diag.Context,
		},
		raw: diag,
	}
	switch diag.State {
	case dockerx.StateOK:
		st.Status = StatusReady
	case dockerx.StateNotInstalled:
		st.Status = StatusNotInstalled
		st.Hint = "instala Docker desde https://docs.docker.com/get-docker/"
	case dockerx.StateDaemonDown:
		st.Status = StatusDown
		st.Hint = "intenta arrancarlo con 'badomens docker ensure'"
	case dockerx.StateStarting:
		st.Status = StatusStarting
		st.Hint = "espera unos segundos a que termine de arrancar"
	case dockerx.StatePermissionDenied:
		st.Status = StatusNeedsElevation
		st.Elevation = true
		st.Hint = "añade tu usuario al grupo docker o abre Docker Desktop como administrador"
	case dockerx.StateComposeMissing:
		st.Status = StatusDegraded
		st.Hint = "instala el plugin de Docker Compose v2"
	case dockerx.StateWrongContext:
		st.Status = StatusUnavailable
		st.Hint = "ejecuta 'docker context use default'"
	case dockerx.StateVirtualizationOff:
		st.Status = StatusUnavailable
		st.Hint = "activa WSL2/virtualización en el firmware del equipo"
	default:
		st.Status = StatusUnavailable
		st.Hint = "revisa el diagnóstico con 'badomens docker status'"
	}
	return st
}

// stepsFromDocker convierte pasos de dockerx al tipo común.
func stepsFromDocker(steps []dockerx.Step) []Step {
	out := make([]Step, 0, len(steps))
	for _, s := range steps {
		out = append(out, Step{
			Description:       s.Description,
			Command:           s.Command,
			Instruction:       s.Instruction,
			RequiresElevation: s.Interactive,
		})
	}
	return out
}

// dockerManualDefault es el texto de rescate por SO cuando no hay plan concreto.
func dockerManualDefault(platform string) string {
	switch platform {
	case "windows":
		return "Abre Docker Desktop desde el menú Inicio; si no arranca, ejecútalo como administrador."
	case "darwin":
		return "Abre Docker Desktop (o 'colima start' / 'orb start') desde Aplicaciones."
	}
	return "Ejecuta 'sudo systemctl start docker' y comprueba con 'docker info'."
}
