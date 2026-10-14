// Package runtime genera el compose.yml de cada instancia y ejecuta su ciclo de
// vida con Docker Compose (proyecto badomens-<slug>). Nunca edita compose.yml a
// mano: se regenera en cada start con la IP actual del tailnet.
package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/execx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/mcuuid"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/tailscale"
)

// ComposeTemplate es la plantilla del compose de una instancia. Mapea los
// campos del Spec a las variables de la imagen itzg/minecraft-server.
const ComposeTemplate = `services:
  minecraft:
    image: {{.Image}}
    container_name: badomens-{{.Slug}}
    restart: unless-stopped
    stdin_open: true
    tty: true

    environment:
      EULA: "TRUE"
      TYPE: "{{.TypeValue}}"
      VERSION: "{{.Version}}"
      PAPER_BUILD: "{{.PinPAPER}}"
      FABRIC_LOADER_VERSION: "{{.PinFABRIC}}"
      FORGE_VERSION: "{{.PinFORGE}}"
      NEOFORGE_VERSION: "{{.PinNEOFORGE}}"
      MEMORY: "{{.Memory}}"
      SEED: {{.Seed}}
      DIFFICULTY: "{{.Difficulty}}"
      MODE: "{{.Gamemode}}"
      MAX_PLAYERS: "{{.MaxPlayers}}"
      MOTD: {{.Motd}}
      ICON: "{{.IconContainer}}"
      ONLINE_MODE: "{{.OnlineMode}}"
      ENFORCE_SECURE_PROFILE: "{{.SecureProfile}}"
      ENABLE_WHITELIST: "{{.WLEnabled}}"
      WHITELIST: "{{.Whitelist}}"
      OPS: "{{.Ops}}"
      CREATE_CONSOLE_IN_PIPE: "TRUE"
      ENABLE_RCON: "FALSE"
      JVM_OPTS: >-
        -XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200
        -XX:+UnlockExperimentalVMOptions -XX:+DisableExplicitGC -XX:+AlwaysPreTouch
        -XX:G1NewSizePercent=30 -XX:G1MaxNewSizePercent=40 -XX:G1HeapRegionSize=8M
        -XX:G1ReservePercent=20 -XX:G1HeapWastePercent=5 -XX:G1MixedGCCountTarget=4
        -XX:InitiatingHeapOccupancyPercent=15 -XX:G1MixedGCLiveThresholdPercent=90
        -XX:G1RSetUpdatingPauseTimePercent=5 -XX:SurvivorRatio=32
        -XX:+PerfDisableSharedMem -XX:MaxTenuringThreshold=1
        -Dusing.aikars.flags=https://mcflags.emc.gs -Daikars.new.flags=true

    ports:
      - "{{.HostIP}}:{{.Port}}:25565"

    volumes:
      - ./server:/data
`

type composeData struct {
	Image         string
	Slug          string
	TypeValue     string
	Version       string
	PinPAPER      string
	PinFABRIC     string
	PinFORGE      string
	PinNEOFORGE   string
	Memory        string
	Seed          string
	Difficulty    string
	Gamemode      string
	MaxPlayers    int
	Motd          string
	IconContainer string
	OnlineMode    string
	SecureProfile string
	WLEnabled     string
	Whitelist     string
	Ops           string
	HostIP        string
	Port          int
}

// Runtime ejecuta las operaciones de Docker Compose.
type Runtime struct {
	Cfg   *config.Config
	types map[string]string // engine -> TYPE value
	dirs  map[string]string // engine -> carpeta de plugins/mods
}

// New crea un Runtime.
func New(cfg *config.Config) *Runtime {
	return &Runtime{
		Cfg:   cfg,
		types: map[string]string{"paper": "PAPER", "fabric": "FABRIC", "forge": "FORGE", "neoforge": "NEOFORGE", "vanilla": "VANILLA"},
		dirs:  map[string]string{"paper": "plugins", "fabric": "mods", "forge": "mods", "neoforge": "mods", "vanilla": ""},
	}
}

// Project devuelve el nombre del proyecto Compose de una instancia.
func (r *Runtime) Project(slug string) string { return "badomens-" + slug }

// ContainerName devuelve el nombre del contenedor.
func (r *Runtime) ContainerName(slug string) string { return "badomens-" + slug }

// ComposePath devuelve la ruta del compose de una instancia.
func (r *Runtime) ComposePath(base, slug string) string {
	return filepath.Join(base, "instances", slug, "compose.yml")
}

// Render construye el YAML del compose para un spec y una IP de host.
func (r *Runtime) Render(spec *instance.Spec, cfg *config.Config, hostIP string) ([]byte, error) {
	data := composeData{
		Image:         r.imageFor(spec),
		Slug:          spec.Slug,
		TypeValue:     r.types[spec.Engine],
		Version:       spec.Version,
		PinPAPER:      spec.Pins["PAPER_BUILD"],
		PinFABRIC:     spec.Pins["FABRIC_LOADER_VERSION"],
		PinFORGE:      spec.Pins["FORGE_VERSION"],
		PinNEOFORGE:   spec.Pins["NEOFORGE_VERSION"],
		Memory:        spec.Memory,
		Seed:          yamlValue(spec.Seed),
		Difficulty:    spec.Difficulty,
		Gamemode:      spec.Gamemode,
		MaxPlayers:    spec.MaxPlayers,
		Motd:          yamlValue(spec.Motd),
		IconContainer: iconContainer(spec),
		OnlineMode:    boolVal(spec.OnlineMode),
		SecureProfile: "FALSE",
		WLEnabled:     boolVal(spec.WhitelistEnabled),
		Whitelist:     entriesValue(spec.Whitelist, spec.OnlineMode),
		Ops:           entriesValue(spec.Ops, spec.OnlineMode),
		HostIP:        hostIP,
		Port:          spec.Port,
	}
	var buf bytes.Buffer
	tmpl, err := template.New("compose").Parse(ComposeTemplate)
	if err != nil {
		return nil, err
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// entriesValue convierte una lista de jugadores en el valor de una variable de
// entorno de la imagen (WHITELIST/OPS).
//
// En modo online se envían los nombres tal cual (la imagen los resuelve contra
// PlayerDB). En modo offline hay que enviar el UUID offline (versión 3) de cada
// nombre: con el nombre, la imagen resolvería un UUID premium contra PlayerDB
// que no coincide con el del servidor sin autenticación y el jugador sería
// expulsado al entrar.
//
// Se ignoran las entradas vacías y se deduplica de forma exacta conservando el
// orden; el resultado se une con comas.
func entriesValue(names []string, online bool) string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		value := name
		if !online {
			value = mcuuid.Offline(name)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return strings.Join(out, ",")
}

// imageFor elige la imagen del contenedor según el Java que requiere la versión
// de Minecraft (RF-5). Usa cfg.Image como respaldo si la versión no se reconoce.
func (r *Runtime) imageFor(spec *instance.Spec) string {
	fallback := "itzg/minecraft-server:java25"
	if r.Cfg != nil && r.Cfg.Image != "" {
		fallback = r.Cfg.Image
	}
	if spec == nil {
		return fallback
	}
	return engines.JavaImageFor(spec.Version, fallback)
}

// HostIP devuelve la IP de publicación del puerto según la configuración.
func (r *Runtime) HostIP(ctx context.Context) (string, error) {
	if !r.Cfg.Tailscale.Enabled {
		return "127.0.0.1", nil
	}
	return tailscale.ResolveIPv4(ctx)
}

// Prepare crea las carpetas de la instancia y copia el icono al volumen.
func (r *Runtime) Prepare(ctx context.Context, spec *instance.Spec) error {
	dir := r.InstanceDir(spec)
	for _, d := range []string{"server", r.dirs[spec.Engine], "backups"} {
		if d == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return err
		}
	}
	return r.copyIcon(ctx, spec, dir)
}

// InstanceDir devuelve la carpeta de la instancia.
func (r *Runtime) InstanceDir(spec *instance.Spec) string {
	return filepath.Join(r.Cfg.DataDir, "instances", spec.Slug)
}

// Start genera el compose (con la IP actual) y arranca la instancia.
func (r *Runtime) Start(ctx context.Context, spec *instance.Spec) error {
	if err := r.Prepare(ctx, spec); err != nil {
		return err
	}
	hostIP, err := r.HostIP(ctx)
	if err != nil {
		return err
	}
	raw, err := r.Render(spec, r.Cfg, hostIP)
	if err != nil {
		return err
	}
	if err := os.WriteFile(r.ComposePath(r.Cfg.DataDir, spec.Slug), raw, 0o644); err != nil {
		return err
	}
	if res, uerr := r.compose(ctx, spec.Slug, "up", "-d"); uerr != nil {
		msg := uerr.Error()
		if res.Stderr != "" {
			msg += "\n" + res.Stderr
		}
		if _, ok := ports.TranslateUpError(msg); ok {
			return errx.Capacity(
				fmt.Sprintf("el puerto %d está ocupado y Docker no pudo publicar la instancia", spec.Port),
				"usa otro puerto libre o libera el que está ocupado",
			).WithDetail("reason", "port_in_use").WithDetail("port", spec.Port)
		}
		return uerr
	}
	return nil
}

// Stop detiene el contenedor sin quitarlo.
func (r *Runtime) Stop(ctx context.Context, slug string) error {
	_, err := r.compose(ctx, slug, "stop")
	return err
}

// Close apaga con cuidado: envía "stop" a la consola (guarda el mundo) y a
// continuación detiene el contenedor con compose para que `restart:
// unless-stopped` no lo vuelva a lanzar.
func (r *Runtime) Close(ctx context.Context, slug string) error {
	res, err := execx.Run(ctx, "docker", "exec", "--user", "1000", r.ContainerName(slug), "mc-send-to-console", "stop")
	if err != nil && !alreadyStopped(res) {
		return err
	}
	_, err = r.compose(ctx, slug, "stop")
	return err
}

func alreadyStopped(res execx.Result) bool {
	if res.ExitCode == 0 {
		return false
	}
	return strings.Contains(res.Stderr, "is not running") ||
		strings.Contains(res.Stderr, "no such")
}

// Restart fuerza la recreación del contenedor (aplica cambios de compose).
func (r *Runtime) Restart(ctx context.Context, slug string) error {
	_, err := r.compose(ctx, slug, "up", "-d", "--force-recreate")
	return err
}

// Down elimina el contenedor y la red de la instancia.
func (r *Runtime) Down(ctx context.Context, slug string) error {
	_, err := r.compose(ctx, slug, "down")
	return err
}

// State devuelve el estado del contenedor: "running", "exited" o "" (no existe).
func (r *Runtime) State(ctx context.Context, slug string) (string, error) {
	label := "com.docker.compose.project=" + r.Project(slug)
	res, err := execx.Run(ctx, "docker", "ps", "-a", "--filter", "label="+label, "--format", "{{.State}}")
	if err != nil {
		if errx.Is(err, errx.CodeDependency) {
			return "", err
		}
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// Logs devuelve las últimas N líneas de los logs de la instancia.
func (r *Runtime) Logs(ctx context.Context, slug string, lines int) (string, error) {
	res, err := r.compose(ctx, slug, "logs", "--tail", fmt.Sprintf("%d", lines))
	if err != nil {
		return "", err
	}
	return res.Stdout + "\n" + res.Stderr, nil
}

// Console envía un comando a la consola del servidor (sin barra).
func (r *Runtime) Console(ctx context.Context, slug, command string) error {
	_, err := execx.Run(ctx, "docker", "exec", "--user", "1000", r.ContainerName(slug), "mc-send-to-console", command)
	return err
}

// RunningCount cuenta las instancias de la lista que están corriendo.
func (r *Runtime) RunningCount(ctx context.Context, specs []*instance.Spec) (int, error) {
	count := 0
	for _, s := range specs {
		st, err := r.State(ctx, s.Slug)
		if err != nil {
			return 0, err
		}
		if st == "running" {
			count++
		}
	}
	return count, nil
}

func (r *Runtime) compose(ctx context.Context, slug string, args ...string) (execx.Result, error) {
	cmd := []string{"compose", "-p", r.Project(slug), "-f", r.ComposePath(r.Cfg.DataDir, slug)}
	cmd = append(cmd, args...)
	return execx.Run(ctx, "docker", cmd...)
}

func (r *Runtime) copyIcon(ctx context.Context, spec *instance.Spec, dir string) error {
	if spec.Icon == "" {
		return nil
	}
	raw, err := os.ReadFile(spec.Icon)
	if err != nil {
		return nil // icono no disponible: se omite con aviso del llamador
	}
	return os.WriteFile(filepath.Join(dir, "server", "icon.png"), raw, 0o644) // #nosec G703 -- ruta fija (icon.png) dentro de la instancia (slug validado)
}

func iconContainer(spec *instance.Spec) string {
	if spec.Icon == "" {
		return ""
	}
	return "/data/icon.png"
}

// yamlValue devuelve un valor YAML seguro entre comillas (para MOTD, seed…).
func yamlValue(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func boolVal(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}
