// Package infra implementa el mecanismo común de los servicios externos de
// badomens —Docker, Tailscale y el propio daemon—: detectar → levantar →
// elevar → informar. Todos los procesos se lanzan con argumentos separados
// (internal/execx); nunca se concatena una cadena de shell.
//
//  1. Detectar instalación; si falta, devolver la instrucción (no instalar solo).
//  2. Detectar si el servicio está activo.
//  3. Si está apagado, intentar levantarlo por SO. La elevación (sudo/UAC) solo
//     se pide con el motivo explicado; si el usuario la rechaza, se degrada a
//     modo sesión con un aviso, sin bloquear el resto de la app.
//  4. Reintentar la verificación con timeout y estado «Cargando» mediante una API
//     asíncrona y cancelable (context.Context).
//  5. Si falla, devolver causa y pasos manuales por SO.
//
// El motor (Ensure) es una máquina de estados pura sobre un entorno inyectable,
// así que se prueba sin Docker, sin Tailscale y sin privilegios.
package infra

import (
	"context"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/execx"
)

// Domain identifica el servicio gestionado.
type Domain string

// Dominios soportados.
const (
	DomainDocker    Domain = "docker"
	DomainTailscale Domain = "tailscale"
	DomainDaemon    Domain = "daemon"
)

// Status es el estado tipado y estable de un servicio.
type Status string

// Estados posibles.
const (
	// StatusReady: el servicio está operativo.
	StatusReady Status = "ready"
	// StatusStarting: está arrancando.
	StatusStarting Status = "starting"
	// StatusDown: instalado pero apagado.
	StatusDown Status = "down"
	// StatusNotInstalled: falta el binario o el paquete.
	StatusNotInstalled Status = "not_installed"
	// StatusNeedsElevation: levantarlo requiere privilegios que no se concedieron.
	StatusNeedsElevation Status = "needs_elevation"
	// StatusNeedsLogin: pide autenticación del usuario (Tailscale).
	StatusNeedsLogin Status = "needs_login"
	// StatusDegraded: funciona con limitaciones (p. ej. falta Compose).
	StatusDegraded Status = "degraded"
	// StatusUnavailable: no responde o no se pudo determinar.
	StatusUnavailable Status = "unavailable"
)

// Ready indica que el servicio está plenamente operativo.
func (s Status) Ready() bool { return s == StatusReady }

// Operating indica que se puede operar, aunque sea degradado.
func (s Status) Operating() bool { return s == StatusReady || s == StatusDegraded }

// State es el resultado de detectar un componente.
type State struct {
	Domain  Domain `json:"domain"`
	Status  Status `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Detail  string `json:"detail,omitempty"`
	// Elevation indica que levantarlo requiere privilegios.
	Elevation bool `json:"elevation,omitempty"`
	// Extra lleva datos específicos del dominio (IP, MagicDNS, versión…).
	Extra map[string]any `json:"extra,omitempty"`

	// raw conserva el diagnóstico nativo (p. ej. dockerx.Diagnosis) para que el
	// componente construya el plan de arranque sin volver a diagnosticar.
	raw any
}

// Ready indica si el componente está operativo.
func (s State) Ready() bool { return s.Status.Ready() }

// Component describe un servicio externo concreto.
type Component interface {
	// Domain identifica el servicio.
	Domain() Domain
	// Detect consulta el estado actual. No bloquea: el llamante controla el timeout.
	Detect(ctx context.Context) State
	// StartPlan devuelve los pasos de arranque ordenados para la plataforma.
	StartPlan(st State, platform string) []Step
	// InstallHint devuelve la instrucción de instalación (nunca se instala solo).
	InstallHint(platform string) string
	// ManualGuide devuelve los pasos manuales por SO cuando no se pudo levantar.
	ManualGuide(st State, platform string) []string
}

// Step es un paso del plan de arranque.
type Step struct {
	// Description explica qué se intenta, para mostrar en pantalla.
	Description string `json:"description"`
	// Command es el proceso a ejecutar: argv con argumentos separados, nunca una
	// cadena de shell. Vacío si es una instrucción manual.
	Command []string `json:"command,omitempty"`
	// Instruction es el texto para el usuario cuando no se automatiza.
	Instruction string `json:"instruction,omitempty"`
	// RequiresElevation indica que el paso necesita privilegios (sudo/UAC).
	RequiresElevation bool `json:"requires_elevation,omitempty"`
	// Detached indica que el paso arranca un proceso desacoplado (daemon de
	// sesión) en vez de esperar a que termine.
	Detached bool `json:"detached,omitempty"`
}

// Attempt es el resultado de intentar un paso.
type Attempt struct {
	Step  Step   `json:"step"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Phase describe una etapa del mecanismo (contrato JSON: phases).
type Phase struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Progress es una notificación de avance para la UI (estado «Cargando»).
type Progress struct {
	Domain  Domain `json:"domain"`
	Phase   string `json:"phase"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Options controla la ejecución del mecanismo.
type Options struct {
	// Timeout es el tiempo máximo de espera tras intentar levantar (120 s).
	Timeout time.Duration
	// PollInterval es el intervalo entre sondeos (2 s).
	PollInterval time.Duration
	// AllowElevation indica que el usuario autorizó pedir privilegios. Sin esto,
	// los pasos que requieren elevación quedan pendientes y se explica el motivo.
	AllowElevation bool
	// Platform fuerza el SO ("linux", "darwin", "windows"); vacío usa el real.
	Platform string
	// Progress recibe avisos de avance sin bloquear. Puede ser nil.
	Progress func(Progress)
}

// Report es el resultado completo del mecanismo.
type Report struct {
	Domain         Domain         `json:"domain"`
	Status         Status         `json:"status"`
	Reason         string         `json:"reason,omitempty"`
	Message        string         `json:"message"`
	Hint           string         `json:"hint,omitempty"`
	Ready          bool           `json:"ready"`
	NeedsElevation bool           `json:"needs_elevation,omitempty"`
	Degraded       bool           `json:"degraded,omitempty"`
	Canceled       bool           `json:"canceled,omitempty"`
	Attempts       []Attempt      `json:"attempts,omitempty"`
	Pending        []Step         `json:"pending,omitempty"`
	Manual         []string       `json:"manual,omitempty"`
	Phases         []Phase        `json:"phases,omitempty"`
	Elapsed        string         `json:"elapsed,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
}

// Env reúne las dependencias del motor para poder inyectarlas en pruebas.
type Env struct {
	// GOOS es el sistema operativo ("windows", "linux", "darwin").
	GOOS string
	// Run ejecuta un proceso externo. Por defecto execx.Run.
	Run func(ctx context.Context, name string, args ...string) (execx.Result, error)
	// LookPath indica si un binario está en el PATH. Por defecto execx.LookPath.
	LookPath func(name string) bool
	// Sleep espera; inyectable en pruebas. Por defecto time.Sleep con contexto.
	Sleep func(ctx context.Context, d time.Duration) error
	// Elevated informa si el proceso actual ya corre con privilegios.
	Elevated func() bool
	// Spawn arranca un proceso desacoplado (daemon de sesión). Por defecto
	// spawnDetached. Inyectable en pruebas.
	Spawn func(ctx context.Context, exe string, args ...string) error
}

// DefaultEnv devuelve el entorno real.
func DefaultEnv() Env {
	return Env{
		GOOS:     goos(),
		Run:      execx.Run,
		LookPath: execx.LookPath,
		Sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
		Elevated: isElevated,
		Spawn:    spawnDetached,
	}
}

func (e Env) withDefaults() Env {
	if e.GOOS == "" {
		e.GOOS = goos()
	}
	if e.Run == nil {
		e.Run = execx.Run
	}
	if e.LookPath == nil {
		e.LookPath = execx.LookPath
	}
	if e.Sleep == nil {
		e.Sleep = DefaultEnv().Sleep
	}
	if e.Elevated == nil {
		e.Elevated = isElevated
	}
	if e.Spawn == nil {
		e.Spawn = spawnDetached
	}
	return e
}

func (e Env) run(ctx context.Context, name string, args ...string) (execx.Result, error) {
	return e.Run(ctx, name, args...)
}

func (e Env) elevated() bool {
	if e.Elevated == nil {
		return false
	}
	return e.Elevated()
}

// stepTimeout es el límite por paso de arranque.
const stepTimeout = 20 * time.Second

// Ensure ejecuta el mecanismo completo sobre un componente.
func Ensure(ctx context.Context, env Env, c Component, opts Options) Report {
	env = env.withDefaults()
	platform := opts.Platform
	if platform == "" {
		platform = env.GOOS
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}

	start := time.Now()
	rep := Report{Domain: c.Domain()}
	progress := func(phase string, st State, message string) {
		rep.Phases = append(rep.Phases, Phase{Name: phase, Status: string(st.Status)})
		if opts.Progress != nil {
			opts.Progress(Progress{Domain: c.Domain(), Phase: phase, Status: st.Status, Message: message})
		}
	}

	// 1-2. Detectar.
	st := c.Detect(ctx)
	applyState(&rep, st)
	progress("detect", st, st.Message)

	if st.Ready() {
		rep.Elapsed = time.Since(start).String()
		return rep
	}

	// Instalación ausente: nunca se instala en silencio.
	if st.Status == StatusNotInstalled {
		hint := c.InstallHint(platform)
		rep.Pending = append(rep.Pending, Step{Description: "instalar el servicio", Instruction: hint})
		rep.Manual = append(rep.Manual, c.ManualGuide(st, platform)...)
		rep.Elapsed = time.Since(start).String()
		return rep
	}

	// 3. Levantar.
	attempted := false
	elevationPending := false
	for _, step := range c.StartPlan(st, platform) {
		switch {
		case len(step.Command) == 0:
			rep.Pending = append(rep.Pending, step)
		case step.RequiresElevation && !(opts.AllowElevation || env.elevated()):
			// Elevación no autorizada: se explica el motivo y se degrada sin bloquear.
			step.Instruction = firstNonEmpty(step.Instruction, step.Description+": requiere permisos de administrador")
			rep.Pending = append(rep.Pending, step)
			elevationPending = true
		case step.Detached:
			// Daemon de sesión: se arranca desacoplado y no se espera.
			attempted = true
			a := Attempt{Step: step, OK: true}
			if err := env.Spawn(ctx, step.Command[0], step.Command[1:]...); err != nil {
				a.OK = false
				a.Error = err.Error()
			}
			rep.Attempts = append(rep.Attempts, a)
		default:
			runCtx, cancel := context.WithTimeout(ctx, stepTimeout)
			_, err := env.run(runCtx, step.Command[0], step.Command[1:]...)
			cancel()
			attempted = true
			a := Attempt{Step: step, OK: err == nil}
			if err != nil {
				a.Error = err.Error()
			}
			rep.Attempts = append(rep.Attempts, a)
		}
		if ctx.Err() != nil {
			break
		}
	}

	// 4. Reintentar la verificación con timeout (estado «Cargando»).
	if attempted || st.Status == StatusStarting {
		progress("wait", State{Domain: c.Domain(), Status: StatusStarting}, "Cargando…")
		st = waitFor(ctx, env, c, opts, &rep)
	} else {
		st = c.Detect(ctx)
	}
	applyState(&rep, st)

	if st.Ready() {
		progress("done", st, st.Message)
		rep.Elapsed = time.Since(start).String()
		return rep
	}

	// 5. Causa + pasos manuales por SO, sin bloquear.
	if elevationPending {
		rep.NeedsElevation = true
		rep.Degraded = true
		if rep.Status == StatusDown || rep.Status == StatusUnavailable {
			rep.Status = StatusNeedsElevation
		}
		rep.Hint = firstNonEmpty(rep.Hint, "concede permisos de administrador para levantarlo, o continúa en modo sesión")
	}
	rep.Manual = append(rep.Manual, c.ManualGuide(st, platform)...)
	rep.Canceled = ctx.Err() != nil
	progress("done", st, st.Message)
	rep.Elapsed = time.Since(start).String()
	return rep
}

// EnsureAsync ejecuta el mecanismo en segundo plano y devuelve un canal con el
// resultado final. El consumidor puede cancelar con el context.Context.
func EnsureAsync(ctx context.Context, env Env, c Component, opts Options) <-chan Report {
	out := make(chan Report, 1)
	go func() {
		defer close(out)
		out <- Ensure(ctx, env, c, opts)
	}()
	return out
}

// waitFor sondea Detect hasta Ready, agotar el timeout o cancelarse.
func waitFor(ctx context.Context, env Env, c Component, opts Options, _ *Report) State {
	deadline := time.Now().Add(opts.Timeout)
	st := c.Detect(ctx)
	for {
		if st.Ready() {
			return st
		}
		if ctx.Err() != nil {
			return st
		}
		if time.Now().After(deadline) {
			return st
		}
		if opts.Progress != nil {
			opts.Progress(Progress{Domain: c.Domain(), Phase: "wait", Status: StatusStarting, Message: "Cargando…"})
		}
		if err := env.sleep(ctx, opts.PollInterval); err != nil {
			return st
		}
		st = c.Detect(ctx)
	}
}

// applyState copia el estado detectado al reporte (sin tocar los pasos).
func applyState(rep *Report, st State) {
	rep.Status = st.Status
	rep.Reason = st.Reason
	rep.Message = st.Message
	rep.Hint = firstNonEmpty(st.Hint, rep.Hint)
	rep.Ready = st.Ready()
	if st.Extra != nil {
		rep.Extra = st.Extra
	}
}

func (e Env) sleep(ctx context.Context, d time.Duration) error {
	if e.Sleep == nil {
		return nil
	}
	return e.Sleep(ctx, d)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
