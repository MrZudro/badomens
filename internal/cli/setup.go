package cli

import (
	"fmt"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/setup"
	"github.com/spf13/cobra"
)

// createOpts agrupa todos los campos del asistente.
type createOpts struct {
	name             string
	engine           string
	version          string
	memory           string
	cpu              int
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
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Asistente de configuración de una instancia (pregunta lo que falte)",
		Long: `Crea una instancia de servidor preguntando todo lo que no venga por
flags: nombre, motor, versión, recursos, dificultad, modo, máximo de jugadores
(recomendado según capacidad), seed, crackeado/premium, whitelist, ops, MOTD,
icono y plugins. Con flags en su sitio funciona desatendido (CI/agentes).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runSetup(cmd, e, opts, true)
			if err != nil {
				return e.finish(res, "")
			}
			return e.finish(res, setupHuman(res))
		},
	}
	addCreateFlags(cmd, opts)
	return cmd
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
	m, err := e.manager()
	if err != nil {
		return result.FromError("setup", err), err
	}
	cfg, _ := e.requireConfig()

	// 1) Nombre → slug
	name := o.name
	if name == "" && interactive {
		name = e.prompt("Nombre de la instancia", "")
	}
	if name == "" {
		return result.FromError("setup", errx.Usage("falta el nombre de la instancia", "usa --name")), err
	}
	slug, err := instance.NormalizeSlug(name)
	if err != nil {
		return result.FromError("setup", err), err
	}
	if _, err := m.Load(slug); err == nil {
		return result.FromError("setup", errx.Usage("ya existe una instancia "+slug, "usa otro nombre")), err
	}

	// 2) Motor
	engineName := o.engine
	if engineName == "" && interactive {
		engineName = e.prompt(fmt.Sprintf("Motor (%s)", strings.Join(engineNames(), "/")), "paper")
	}
	if engineName == "" {
		engineName = "paper"
	}
	provider, err := reg().Provider(engineName)
	if err != nil {
		return result.FromError("setup", err), err
	}

	// 3) Versión (resolución en línea)
	versionWanted := o.version
	if versionWanted == "" && interactive {
		versionWanted = e.prompt("Versión de Minecraft (Enter = última)", "")
	}
	resolved, err := provider.Resolve(cmd.Context(), versionWanted)
	if err != nil {
		return result.FromError("setup", err), err
	}

	// 4) Recursos
	memory := o.memory
	if memory == "" {
		memory = cfg.DefaultMemory
	}
	if memory == "" && interactive {
		memory = e.prompt("Memoria JVM (ej. 4G)", memory)
	}
	if host.ParseMemory(memory) == 0 {
		return result.FromError("setup", errx.Usage(fmt.Sprintf("memoria inválida %q", memory), "ej. 4G")), err
	}

	// 5) Máximo de jugadores (recomendado)
	modded := engineName != "paper" && engineName != "vanilla"
	recommended := setup.RecommendMaxPlayers(float64(host.ParseMemory(memory))/float64(1024*1024*1024), modded)
	maxPlayers := o.maxPlayers
	if maxPlayers <= 0 && interactive {
		maxPlayers = atoiPrompt(e.prompt(fmt.Sprintf("Máximo de jugadores (recomendado %d)", recommended), "0"))
	}
	if maxPlayers <= 0 {
		maxPlayers = recommended
	}

	// 6) Dificultad y modo
	difficulty := o.difficulty
	if difficulty == "" && interactive {
		difficulty = e.prompt("Dificultad (peaceful/easy/normal/hard)", "normal")
	}
	if difficulty == "" {
		difficulty = "normal"
	}
	gamemode := o.gamemode
	if gamemode == "" && interactive {
		gamemode = e.prompt("Modo de juego (survival/creative/adventure/spectator)", "survival")
	}
	if gamemode == "" {
		gamemode = "survival"
	}

	// 7) Crackeado o premium
	online := o.online
	if interactive {
		ans := e.prompt("Crackeado (entran sin cuenta premium)? (S/n)", "S")
		online = !yesBool(ans, true)
	}

	// 8) Seed, whitelist, ops, motd, icono
	seed := o.seed
	if seed == "" && interactive {
		seed = e.prompt("Semilla del mundo (Enter = aleatoria)", "")
	}
	whitelistEnabled := o.whitelistEnabled
	whitelistUsers := o.whitelist
	if interactive && !o.whitelistEnabled {
		ans := e.prompt("¿Activar whitelist? (s/N)", "N")
		if yesBool(ans, false) {
			whitelistEnabled = true
		}
	}
	if whitelistEnabled && whitelistUsers == "" && interactive {
		whitelistUsers = e.prompt("Nombres para la whitelist (separados por coma)", "")
	}
	opsList := o.ops
	if opsList == "" && interactive {
		opsList = e.prompt("Operadores (separados por coma)", "")
	}
	motd := o.motd
	if motd == "" && interactive {
		motd = e.prompt("Descripción visible en Minecraft (MOTD)", "")
	}
	icon := o.icon
	if icon == "" && interactive {
		icon = e.prompt("Ruta al icono del servidor (PNG) (Enter = ninguno)", "")
	}

	// 9) Puerto libre
	port, err := m.NextPort(25565)
	if err != nil {
		return result.FromError("setup", err), err
	}

	spec := &instance.Spec{
		Slug:             slug,
		Name:             name,
		Engine:           provider.Engine().Name,
		Version:          resolved.Version,
		Pins:             resolved.Pins,
		Memory:           memory,
		CPU:              o.cpu,
		Port:             port,
		Difficulty:       difficulty,
		Gamemode:         gamemode,
		MaxPlayers:       maxPlayers,
		OnlineMode:       online,
		Motd:             motd,
		Icon:             icon,
		WhitelistEnabled: whitelistEnabled,
		Whitelist:        setup.ParseCommaList(whitelistUsers),
		Ops:              setup.ParseCommaList(opsList),
		Seed:             seed,
		CreatedAt:        instance.Stamp(),
	}
	if err := spec.Validate(m.EngineNames()); err != nil {
		return result.FromError("setup", errx.Wrap(errx.CodeGeneral, err, "configuración inválida", "")), err
	}
	if err := m.Save(spec); err != nil {
		return result.FromError("setup", err), err
	}

	// 10) Plugins/mods
	warnings := []string{}
	if srcs := setup.ParseCommaList(o.plugins); len(srcs) > 0 {
		inst := &plugins.Installer{DataDir: cfg.DataDir}
		names := make([]string, 0, len(srcs))
		for _, src := range srcs {
			added, aerr := inst.Add(cmd.Context(), spec, src)
			if aerr != nil {
				return result.FromError("setup", aerr), aerr
			}
			names = append(names, added)
		}
		spec.Plugins = append(spec.Plugins, names...)
		_ = m.Save(spec)
		if w := plugins.WarnFor(names); w != "" {
			warnings = append(warnings, w)
		}
	}

	data := specSummary(spec)
	if len(warnings) > 0 {
		data["warnings"] = warnings
	}

	// 11) Arranque opcional
	if o.noStart {
		res := result.OK("setup", "instancia "+slug+" configurada (sin arrancar)", data)
		res.WithPhases(resultPhase("created"))
		return res, nil
	}
	rt, err := e.runtime()
	if err != nil {
		return result.FromError("setup", err), err
	}
	if err := rt.Start(cmd.Context(), spec); err != nil {
		res := result.FromError("setup", err)
		res.Data = data
		return res, err
	}
	return result.OK("setup", "instancia "+slug+" configurada y arrancando", data), nil
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
