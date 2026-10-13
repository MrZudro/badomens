// Package dockerx diagnostica y repara el entorno de Docker (RF-6). Es la
// fuente de verdad del estado de Docker; `doctor`, el CLI y la TUI lo reutilizan.
//
// Todo proceso externo se ejecuta con `internal/execx` (sin shell, argumentos
// separados). El diagnóstico y el plan de reparación son funciones puras sobre
// un entorno inyectable, de forma que se pueden probar sin Docker.
package dockerx

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/execx"
)

// State es el estado del entorno Docker.
type State string

// Estados posibles.
const (
	// StateOK: docker info y docker compose responden.
	StateOK State = "OK"
	// StateNotInstalled: no hay `docker` en el PATH.
	StateNotInstalled State = "NotInstalled"
	// StateDaemonDown: el daemon no responde.
	StateDaemonDown State = "DaemonDown"
	// StateStarting: hay un proceso de escritorio vivo pero el daemon aún no responde.
	StateStarting State = "Starting"
	// StatePermissionDenied: permiso denegado sobre el socket.
	StatePermissionDenied State = "PermissionDenied"
	// StateComposeMissing: falta el plugin de Docker Compose.
	StateComposeMissing State = "ComposeMissing"
	// StateWrongContext: el contexto de Docker apunta a un host inalcanzable.
	StateWrongContext State = "WrongContext"
	// StateVirtualizationOff: virtualización/WSL2 no disponibles (Windows/macOS).
	StateVirtualizationOff State = "VirtualizationOff"
	// StateUnknown: fallo no clasificado.
	StateUnknown State = "Unknown"
)

// Reason devuelve el motivo estable asociado al estado (para error.details.reason).
func (s State) Reason() string {
	switch s {
	case StateNotInstalled:
		return "docker_not_installed"
	case StateDaemonDown:
		return "docker_daemon_down"
	case StateStarting:
		return "docker_starting"
	case StatePermissionDenied:
		return "docker_permission_denied"
	case StateComposeMissing:
		return "compose_missing"
	case StateWrongContext:
		return "docker_wrong_context"
	case StateVirtualizationOff:
		return "virtualization_off"
	case StateOK:
		return ""
	}
	return "docker_unknown"
}

// Ready indica si el entorno está listo para operar.
func (s State) Ready() bool { return s == StateOK }

// Diagnosis es el resultado del diagnóstico.
type Diagnosis struct {
	State          State     `json:"state"`
	Reason         string    `json:"reason,omitempty"`
	Message        string    `json:"message"`
	Detail         string    `json:"detail,omitempty"`
	Platform       string    `json:"platform"`
	ServerVersion  string    `json:"server_version,omitempty"`
	ComposeVersion string    `json:"compose_version,omitempty"`
	Context        string    `json:"context,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

// Env reúne las dependencias del entorno para poder inyectarlas en pruebas.
type Env struct {
	// Run ejecuta un proceso externo. Por defecto execx.Run.
	Run func(ctx context.Context, name string, args ...string) (execx.Result, error)
	// LookPath indica si un binario está en el PATH. Por defecto execx.LookPath.
	LookPath func(name string) bool
	// GOOS es el sistema operativo ("windows", "linux", "darwin").
	GOOS string
	// DesktopAlive informa si hay un proceso de Docker Desktop/Colima/OrbStack vivo.
	DesktopAlive func() bool
	// Sleep espera; inyectable en pruebas. Por defecto time.Sleep.
	Sleep func(ctx context.Context, d time.Duration) error
}

// DefaultEnv devuelve el entorno real.
func DefaultEnv() Env {
	return Env{
		Run:          execx.Run,
		LookPath:     execx.LookPath,
		GOOS:         goos(),
		DesktopAlive: func() bool { return false },
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
	}
}

func (e Env) run(ctx context.Context, name string, args ...string) (execx.Result, error) {
	if e.Run == nil {
		return execx.Run(ctx, name, args...)
	}
	return e.Run(ctx, name, args...)
}

func (e Env) lookPath(name string) bool {
	if e.LookPath == nil {
		return execx.LookPath(name)
	}
	return e.LookPath(name)
}

func (e Env) desktopAlive() bool {
	if e.DesktopAlive == nil {
		return false
	}
	return e.DesktopAlive()
}

func (e Env) sleep(ctx context.Context, d time.Duration) error {
	if e.Sleep == nil {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}
	return e.Sleep(ctx, d)
}

const stepTimeout = 5 * time.Second

// Diagnose determina el estado del entorno Docker.
func Diagnose(ctx context.Context, env Env) Diagnosis {
	now := time.Now().UTC()
	platform := env.GOOS
	if platform == "" {
		platform = goos()
	}
	diag := Diagnosis{Platform: platform, CheckedAt: now}

	if !env.lookPath("docker") {
		diag.State = StateNotInstalled
		diag.Message = "Docker no está instalado o no está en el PATH."
		return diag
	}

	infoCtx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	infoRes, infoErr := env.run(infoCtx, "docker", "info", "--format", "{{.ServerVersion}}")
	if infoErr == nil {
		diag.ServerVersion = strings.TrimSpace(infoRes.Stdout)
	}

	// Contexto activo (informativo).
	if ctxRes, ctxErr := env.run(infoCtx, "docker", "context", "show"); ctxErr == nil {
		diag.Context = strings.TrimSpace(ctxRes.Stdout)
	}

	composeRes, composeErr := env.run(infoCtx, "docker", "compose", "version", "--short")
	if composeErr == nil {
		diag.ComposeVersion = strings.TrimSpace(composeRes.Stdout)
	}

	diag.State = classify(classifyInput{
		InfoErr:       infoErr,
		InfoStderr:    infoRes.Stderr,
		InfoStdout:    infoRes.Stdout,
		ComposeErr:    composeErr,
		ComposeStderr: composeRes.Stderr,
		DesktopAlive:  env.desktopAlive(),
	})
	diag.Reason = diag.State.Reason()
	diag.Message = defaultMessage(diag.State)
	diag.Detail = firstNonEmpty(infoRes.Stderr, composeRes.Stderr)
	return diag
}

type classifyInput struct {
	InfoErr       error
	InfoStderr    string
	InfoStdout    string
	ComposeErr    error
	ComposeStderr string
	DesktopAlive  bool
}

// classify traduce la salida de docker a un estado tipado.
func classify(in classifyInput) State {
	if in.InfoErr == nil {
		if in.ComposeErr == nil {
			return StateOK
		}
		return StateComposeMissing
	}
	txt := strings.ToLower(in.InfoStderr + " " + in.InfoStdout)

	switch {
	case containsAny(txt, "permission denied", "access is denied", "acceso denegado"):
		return StatePermissionDenied
	case containsAny(txt, "virtualization", "wsl", "hyper-v", "hcs", "no matching kernel", "virtualisation"):
		return StateVirtualizationOff
	case containsAny(txt, "context", "failed to connect to the docker api") && containsAny(txt, "unreachable", "no such host", "not found"):
		return StateWrongContext
	case containsAny(txt, "cannot connect", "connection refused", "is the docker daemon running",
		"error during connect", "npipe", "pipe/docker_engine", "no such file or directory",
		"the system cannot find the file"):
		if in.DesktopAlive {
			return StateStarting
		}
		return StateDaemonDown
	case in.DesktopAlive:
		return StateStarting
	}
	return StateDaemonDown
}

// defaultMessage devuelve una explicación llana del estado.
func defaultMessage(s State) string {
	switch s {
	case StateOK:
		return "Docker está listo."
	case StateNotInstalled:
		return "Docker no está instalado. Sin Docker no puedo iniciar servidores."
	case StateDaemonDown:
		return "Docker está instalado pero apagado. Sin Docker no puedo iniciar servidores."
	case StateStarting:
		return "Docker está arrancando. Espera unos segundos."
	case StatePermissionDenied:
		return "Tu usuario no tiene permiso para hablar con Docker."
	case StateComposeMissing:
		return "Falta el plugin de Docker Compose."
	case StateWrongContext:
		return "El contexto de Docker apunta a un host que no responde."
	case StateVirtualizationOff:
		return "La virtualización (WSL2/Hyper-V) no está disponible."
	}
	return "No pude determinar el estado de Docker."
}

// Step es un paso del plan de reparación.
type Step struct {
	// Description explica qué se intenta, para mostrar en pantalla.
	Description string `json:"description"`
	// Command es el proceso a ejecutar (nombre + argumentos). Vacío si es una
	// instrucción manual.
	Command []string `json:"command,omitempty"`
	// Instruction es el texto para el usuario (cuando no se automatiza).
	Instruction string `json:"instruction,omitempty"`
	// Interactive indica que requiere una terminal (p. ej. sudo con contraseña).
	Interactive bool `json:"interactive,omitempty"`
}

// Plan devuelve la estrategia de reparación ordenada por plataforma.
func Plan(diag Diagnosis, platform string) []Step {
	if platform == "" {
		platform = diag.Platform
	}
	switch diag.State {
	case StateNotInstalled:
		return installPlan(platform)
	case StatePermissionDenied:
		return permissionPlan(platform)
	case StateVirtualizationOff:
		return virtualizationPlan(platform)
	case StateComposeMissing:
		return composePlan(platform)
	case StateWrongContext:
		return contextPlan(diag)
	}
	// DaemonDown / Starting
	switch platform {
	case "windows":
		return windowsStartPlan()
	case "darwin":
		return macOSStartPlan()
	default:
		return linuxStartPlan()
	}
}

func installPlan(platform string) []Step {
	switch platform {
	case "windows":
		return []Step{{Description: "Abrir la página de Docker Desktop", Instruction: "Descarga Docker Desktop desde https://docs.docker.com/desktop/install/windows-install/ e instálalo."}}
	case "darwin":
		return []Step{{Description: "Abrir la página de Docker Desktop", Instruction: "Descarga Docker Desktop desde https://docs.docker.com/desktop/install/mac-install/ (o instala Colima: 'brew install colima')."}}
	}
	return []Step{{Description: "Instalar Docker", Instruction: "Instala Docker Engine: 'curl -fsSL https://get.docker.com | sh' (revisa antes el script)."}}
}

func permissionPlan(platform string) []Step {
	if platform == "windows" {
		return []Step{{Description: "Abrir Docker Desktop como administrador", Instruction: "Cierra Docker Desktop y vuelve a abrirlo como administrador (clic derecho → Ejecutar como administrador)."}}
	}
	return []Step{
		{Description: "Añadir tu usuario al grupo docker", Command: []string{"sudo", "-n", "usermod", "-aG", "docker", "$USER"}, Interactive: true},
		{Description: "Cerrar sesión y volver a entrar", Instruction: "Cierra sesión y vuelve a entrar, o ejecuta 'newgrp docker' en esta terminal."},
	}
}

func virtualizationPlan(platform string) []Step {
	if platform == "windows" {
		return []Step{
			{Description: "Habilitar WSL2", Command: []string{"wsl", "--install"}},
			{Description: "Habilitar la virtualización en la BIOS/UEFI", Instruction: "Reinicia, entra en la BIOS/UEFI y activa 'Intel VT-x' o 'AMD-V'."},
		}
	}
	return []Step{{Description: "Habilitar la virtualización", Instruction: "Comprueba que la virtualización está activada en el firmware del equipo."}}
}

func composePlan(platform string) []Step {
	switch platform {
	case "windows", "darwin":
		return []Step{{Description: "Reinstalar/actualizar Docker Desktop", Instruction: "Docker Compose v2 viene con Docker Desktop; actualízalo desde la app."}}
	}
	return []Step{{Description: "Instalar el plugin de Compose", Command: []string{"sudo", "-n", "apt-get", "install", "-y", "docker-compose-plugin"}, Interactive: true}}
}

func contextPlan(diag Diagnosis) []Step {
	steps := []Step{{Description: "Volver al contexto por defecto", Command: []string{"docker", "context", "use", "default"}}}
	if diag.Context != "" {
		steps[0].Instruction = "El contexto actual es «" + diag.Context + "»."
	}
	return steps
}

func linuxStartPlan() []Step {
	return []Step{
		{Description: "Intentando: systemctl start docker…", Command: []string{"systemctl", "start", "docker"}},
		{Description: "Arrancar docker.socket", Command: []string{"systemctl", "start", "docker.socket"}},
		{Description: "Intentando con sudo: systemctl start docker…", Command: []string{"sudo", "-n", "systemctl", "start", "docker"}, Interactive: true},
		{Description: "Arrancar Docker en modo rootless", Command: []string{"systemctl", "--user", "start", "docker"}},
		{Description: "Intentando: service docker start…", Command: []string{"service", "docker", "start"}},
	}
}

func macOSStartPlan() []Step {
	return []Step{
		{Description: "Abriendo Docker Desktop…", Command: []string{"open", "-a", "Docker"}},
		{Description: "Iniciando Colima…", Command: []string{"colima", "start"}},
		{Description: "Iniciando OrbStack…", Command: []string{"orb", "start"}},
	}
}

func windowsStartPlan() []Step {
	return []Step{
		{Description: "Abriendo Docker Desktop…", Command: []string{`C:\Program Files\Docker\Docker\Docker Desktop.exe`}},
		{Description: "Iniciando el servicio com.docker.service", Command: []string{"sc", "start", "com.docker.service"}, Interactive: true},
		{Description: "Abrir Docker Desktop manualmente", Instruction: "Si no se abrió, búscalo en el menú Inicio y ábrelo."},
	}
}

// Attempt es el resultado de intentar un paso.
type Attempt struct {
	Step Step   `json:"step"`
	Err  string `json:"error,omitempty"`
	OK   bool   `json:"ok"`
}

// EnsureResult es el resultado de intentar dejar Docker listo.
type EnsureResult struct {
	Diagnosis Diagnosis `json:"diagnosis"`
	Attempts  []Attempt `json:"attempts,omitempty"`
	// Ready indica que docker info y docker compose responden.
	Ready bool `json:"ready"`
	// Pending son los pasos que requieren intervención del usuario.
	Pending []Step `json:"pending,omitempty"`
}

// Ensure diagnostica, intenta los pasos no interactivos y espera a que Docker
// responda (hasta opts.Timeout). Los pasos interactivos se devuelven en Pending.
func Ensure(ctx context.Context, env Env, opts Options) EnsureResult {
	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	res := EnsureResult{Diagnosis: Diagnose(ctx, env)}
	if res.Diagnosis.State.Ready() {
		res.Ready = true
		return res
	}

	for _, step := range Plan(res.Diagnosis, env.GOOS) {
		if step.Interactive || len(step.Command) == 0 {
			res.Pending = append(res.Pending, step)
			continue
		}
		runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err := env.run(runCtx, step.Command[0], step.Command[1:]...)
		cancel()
		attempt := Attempt{Step: step, OK: err == nil}
		if err != nil {
			attempt.Err = err.Error()
		}
		res.Attempts = append(res.Attempts, attempt)
	}

	res.Diagnosis = WaitForReady(ctx, env, opts)
	res.Ready = res.Diagnosis.State.Ready()
	return res
}

// Options controla la espera activa de Ensure.
type Options struct {
	// Timeout es el tiempo máximo de espera (por defecto 120 s).
	Timeout time.Duration
	// PollInterval es el intervalo entre sondeos (por defecto 2 s).
	PollInterval time.Duration
}

// WaitForReady sondea docker info y docker compose hasta que responden o se
// agota el tiempo.
func WaitForReady(ctx context.Context, env Env, opts Options) Diagnosis {
	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	deadline := time.Now().Add(opts.Timeout)
	for {
		diag := Diagnose(ctx, env)
		if diag.State.Ready() {
			return diag
		}
		if time.Now().After(deadline) {
			return diag
		}
		if err := env.sleep(ctx, opts.PollInterval); err != nil {
			return diag
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// firstNonEmpty devuelve el primer valor no vacío.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ErrFor traduce un diagnóstico a un error tipado (para el contrato JSON) con
// el motivo estable en error.details.reason.
func ErrFor(d Diagnosis) error {
	if d.State.Ready() {
		return nil
	}
	code := errx.CodeGeneral
	switch d.State {
	case StateNotInstalled, StateDaemonDown, StateStarting, StatePermissionDenied, StateComposeMissing:
		code = errx.CodeDependency
	}
	return errx.New(code, d.Message, hintFor(d)).WithDetail("reason", d.Reason)
}

func hintFor(d Diagnosis) string {
	switch d.State {
	case StateNotInstalled:
		return "instala Docker (docs.docker.com/get-docker/)"
	case StateDaemonDown:
		return "pulsa 'badomens docker ensure' para intentar arrancarlo"
	case StateStarting:
		return "espera a que termine de arrancar (badomens docker ensure --wait)"
	case StatePermissionDenied:
		return "añade tu usuario al grupo docker o abre Docker Desktop como administrador"
	case StateComposeMissing:
		return "instala el plugin de Docker Compose v2"
	case StateWrongContext:
		return "ejecuta 'docker context use default'"
	case StateVirtualizationOff:
		return "activa WSL2/virtualización en la BIOS (Windows) o el firmware"
	}
	return "revisa el diagnóstico con 'badomens docker status'"
}

func goos() string { return runtime.GOOS }
