package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/setup"
	"github.com/MrZudro/badomens/internal/tui/help"
	"github.com/spf13/cobra"
)

// createOpts agrupa todos los campos del asistente.
type createOpts struct {
	name             string
	engine           string
	version          string
	memory           string
	cpu              int
	port             int
	difficulty       string
	gamemode         string
	maxPlayers       int
	seed             string
	online           bool
	whitelistEnabled bool
	whitelist        string
	ops              string
	motd             string
	icon             string
	plugins          string
	noStart          bool
}

func newSetupCmd(e *Env) *cobra.Command {
	opts := &createOpts{}
	var explain string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Asistente de configuración de una instancia (pregunta lo que falte)",
		Long: `Crea una instancia de servidor preguntando todo lo que no venga por
flags: nombre, motor, versión, recursos, dificultad, modo, máximo de jugadores
(recomendado según capacidad), seed, crackeado/premium, whitelist, ops, MOTD,
icono y plugins. Con flags en su sitio funciona desatendido (CI/agentes).
Usa --explain <campo> para ver la ayuda larga de una opción.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if explain != "" {
				return e.explain(cmd.Context(), explain)
			}
			res, err := runSetup(cmd, e, opts, true)
			if err != nil {
				return e.finish(res, "")
			}
			return e.finish(res, setupHuman(res))
		},
	}
	addCreateFlags(cmd, opts)
	cmd.Flags().StringVar(&explain, "explain", "", "imprime la ayuda larga de un campo (RF-4)")
	return cmd
}

// explain imprime la ayuda larga de un campo del asistente.
func (e *Env) explain(_ context.Context, field string) error {
	h, ok := help.Get(field)
	if !ok {
		return e.finish(result.FromError("setup explain", errx.NotFound(
			"campo desconocido "+field,
			"campos: "+help.FieldsList(),
		)), "")
	}
	data := map[string]any{"field": map[string]any{
		"title":   h.Title,
		"short":   h.Short,
		"long":    h.Long,
		"example": h.Example,
		"impact":  h.Impact,
		"default": h.Default,
	}}
	human := h.Title + "\n" + h.Short
	if h.Example != "" {
		human += "\n\nEjemplo: " + h.Example
	}
	human += "\n\n" + h.Long
	if h.Default != "" {
		human += "\n\nPor defecto: " + h.Default
	}
	if len(h.Impact) > 0 {
		human += "\n\nImpacto: " + strings.Join(tagsToStrings(h.Impact), ", ")
	}
	return e.finish(result.OK("setup explain", h.Title, data), human)
}

func tagsToStrings(tags []help.Tag) []string {
	out := make([]string, 0, len(tags))
	for _, tg := range tags {
		out = append(out, string(tg))
	}
	return out
}

func newInstanceCreateCmd(e *Env) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Crea una instancia de forma desatendida (flags)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runSetup(cmd, e, opts, false)
			if err != nil {
				return e.finish(res, "")
			}
			return e.finish(res, setupHuman(res))
		},
	}
	addCreateFlags(cmd, opts)
	return cmd
}

func addCreateFlags(cmd *cobra.Command, o *createOpts) {
	f := cmd.Flags()
	f.StringVar(&o.name, "name", "", "nombre de la instancia (obligatorio desatendido)")
	f.StringVar(&o.engine, "engine", "", "motor: paper|fabric|forge|neoforge|vanilla")
	f.StringVar(&o.version, "version", "", "versión de Minecraft (vacía = última)")
	f.StringVar(&o.memory, "memory", "", "RAM JVM (ej. 4G)")
	f.IntVar(&o.cpu, "cpu", 0, "límite de CPU (0 = sin límite)")
	f.IntVar(&o.port, "port", 0, "puerto preferido (0 = el primero libre)")
	f.StringVar(&o.difficulty, "difficulty", "", "peaceful|easy|normal|hard")
	f.StringVar(&o.gamemode, "gamemode", "", "survival|creative|adventure|spectator")
	f.IntVar(&o.maxPlayers, "max-players", 0, "máximo de jugadores (0 = recomendado)")
	f.StringVar(&o.seed, "seed", "", "semilla del mundo")
	f.BoolVar(&o.online, "online", false, "modo premium (cuentas Microsoft/Mojang); por defecto crackeado")
	f.BoolVar(&o.whitelistEnabled, "whitelist", false, "activar whitelist")
	f.StringVar(&o.whitelist, "whitelist-users", "", "lista separada por comas")
	f.StringVar(&o.ops, "ops", "", "lista de operadores separada por comas")
	f.StringVar(&o.motd, "motd", "", "descripción visible en Minecraft")
	f.StringVar(&o.icon, "icon", "", "ruta al PNG del icono del servidor")
	f.StringVar(&o.plugins, "plugins", "", "archivos .jar o URLs separados por comas")
	f.BoolVar(&o.noStart, "no-start", false, "configurar sin arrancar")
}

func runSetup(cmd *cobra.Command, e *Env, o *createOpts, interactive bool) (*result.Result, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return result.FromError("setup", err), err
	}
	m, err := e.manager()
	if err != nil {
		return result.FromError("setup", err), err
	}
	if err := fillCreateOpts(e, o, interactive, cfg); err != nil {
		return result.FromError("setup", err), err
	}

	rt, err := e.runtime()
	if err != nil {
		return result.FromError("setup", err), err
	}

	alloc, err := e.portsAllocator(cmd.Context())
	if err != nil {
		return result.FromError("setup", err), err
	}

	req := provision.Request{
		Name:             o.name,
		Engine:           o.engine,
		Version:          o.version,
		Memory:           o.memory,
		CPU:              o.cpu,
		Port:             o.port,
		Difficulty:       o.difficulty,
		Gamemode:         o.gamemode,
		MaxPlayers:       o.maxPlayers,
		Seed:             o.seed,
		Online:           o.online,
		WhitelistEnabled: o.whitelistEnabled,
		Whitelist:        setup.ParseCommaList(o.whitelist),
		Ops:              setup.ParseCommaList(o.ops),
		Motd:             o.motd,
		Icon:             o.icon,
		Plugins:          setup.ParseCommaList(o.plugins),
		Start:            !o.noStart,
	}

	pr, err := provision.Create(cmd.Context(), provision.Deps{
		Cfg:      cfg,
		Manager:  m,
		Resolver: reg(),
		Runtime:  rt,
		Ports:    alloc,
	}, req)
	if err != nil {
		res := result.FromError("setup", err)
		if pr != nil && pr.Spec != nil {
			res.Data = specSummary(pr.Spec)
		}
		return res, err
	}

	data := specSummary(pr.Spec)
	if len(pr.Warnings) > 0 {
		data["warnings"] = pr.Warnings
	}
	// RF-2: la conexión se calcula y guarda al crear, para mostrarla de inmediato.
	if conn, cerr := rememberConnection(cmd.Context(), e, pr.Spec); cerr == nil {
		data["connection"] = connectionData(conn)
	}
	// RF-8: estado de auto-apagado por instancia.
	data["idle"] = idleData(pr.Spec.Idle)
	if o.noStart {
		res := result.OK("setup", "instancia "+pr.Spec.Slug+" configurada (sin arrancar)", data)
		res.WithPhases(resultPhase("created"))
		return res, nil
	}
	return result.OK("setup", "instancia "+pr.Spec.Slug+" configurada y arrancando", data), nil
}

// fillCreateOpts completa con prompts (si interactive) los campos que no vengan
// por flags. No resuelve el motor ni la versión: eso lo hace provision.Create.
func fillCreateOpts(e *Env, o *createOpts, interactive bool, cfg *config.Config) error {
	if o.name == "" && interactive {
		o.name = e.prompt("Nombre de la instancia", "")
	}
	if o.name == "" {
		return errx.Usage("falta el nombre de la instancia", "usa --name")
	}
	if o.engine == "" && interactive {
		o.engine = e.prompt(fmt.Sprintf("Motor (%s)", strings.Join(engineNames(), "/")), "paper")
	}
	if o.engine == "" {
		o.engine = "paper"
	}
	if o.version == "" && interactive {
		o.version = e.prompt("Versión de Minecraft (Enter = última)", "")
	}
	if o.memory == "" {
		o.memory = cfg.DefaultMemory
	}
	if o.memory == "" && interactive {
		o.memory = e.prompt("Memoria JVM (ej. 4G)", o.memory)
	}
	modded := o.engine != "paper" && o.engine != "vanilla"
	recommended := setup.RecommendMaxPlayers(float64(host.ParseMemory(o.memory))/float64(1024*1024*1024), modded)
	if o.maxPlayers <= 0 && interactive {
		o.maxPlayers = atoiPrompt(e.prompt(fmt.Sprintf("Máximo de jugadores (recomendado %d)", recommended), "0"))
	}
	if o.difficulty == "" && interactive {
		o.difficulty = e.prompt("Dificultad (peaceful/easy/normal/hard)", "normal")
	}
	if o.gamemode == "" && interactive {
		o.gamemode = e.prompt("Modo de juego (survival/creative/adventure/spectator)", "survival")
	}
	if interactive {
		ans := e.prompt("Crackeado (entran sin cuenta premium)? (S/n)", "S")
		o.online = !yesBool(ans, true)
	}
	if o.seed == "" && interactive {
		o.seed = e.prompt("Semilla del mundo (Enter = aleatoria)", "")
	}
	if interactive && !o.whitelistEnabled {
		if yesBool(e.prompt("¿Activar whitelist? (s/N)", "N"), false) {
			o.whitelistEnabled = true
		}
	}
	if o.whitelistEnabled && o.whitelist == "" && interactive {
		o.whitelist = e.prompt("Nombres para la whitelist (separados por coma)", "")
	}
	if o.ops == "" && interactive {
		o.ops = e.prompt("Operadores (separados por coma)", "")
	}
	if o.motd == "" && interactive {
		o.motd = e.prompt("Descripción visible en Minecraft (MOTD)", "")
	}
	if o.icon == "" && interactive {
		o.icon = e.prompt("Ruta al icono del servidor (PNG) (Enter = ninguno)", "")
	}
	return nil
}

func specSummary(s *instance.Spec) map[string]any {
	return map[string]any{
		"slug": s.Slug, "name": s.Name, "engine": s.Engine, "version": s.Version,
		"memory": s.Memory, "port": s.Port, "difficulty": s.Difficulty, "gamemode": s.Gamemode,
		"max_players": s.MaxPlayers, "online_mode": s.OnlineMode,
		"whitelist_enabled": s.WhitelistEnabled, "whitelist": s.Whitelist, "ops": s.Ops,
		"seed": s.Seed, "motd": s.Motd, "plugins": s.Plugins,
	}
}

func setupHuman(res *result.Result) string {
	if res == nil || res.Error != nil {
		return ""
	}
	if d, ok := res.Data.(map[string]any); ok {
		if slug, _ := d["slug"].(string); slug != "" {
			return fmt.Sprintf("Instancia %s (%s %s) · RAM %s · puerto %d · %d jugadores",
				slug, d["engine"], d["version"], d["memory"], d["port"], d["max_players"])
		}
	}
	return res.Message
}

func resultPhase(name string) []result.Phase {
	return []result.Phase{{Name: name, Status: "ok"}}
}

func atoiPrompt(s string) int {
	n := 0
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

func yesBool(ans string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(ans)) {
	case "s", "si", "sí", "y", "yes", "true", "1":
		return true
	case "n", "no", "false", "0":
		return false
	}
	return def
}
