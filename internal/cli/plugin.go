package cli

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/plugins/archive"
	"github.com/MrZudro/badomens/internal/plugins/download"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newPluginCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Plugins/mods por instancia (según motor)",
	}
	cmd.AddCommand(newPluginAddCmd(e), newPluginListCmd(e), newPluginRemoveCmd(e))
	return cmd
}

func (e *Env) pluginInstaller() (*plugins.Installer, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	return &plugins.Installer{DataDir: cfg.DataDir}, nil
}

func newPluginAddCmd(e *Env) *cobra.Command {
	var (
		sha256Flag    string
		maxSizeFlag   string
		nameFlag      string
		conflictFlag  string
		allowHTTP     bool
		allowPrivate  bool
		allowLoopback bool
	)
	cmd := &cobra.Command{
		Use:   "add <archivo-o-url>",
		Short: "Instala un plugin/mod en la instancia (carpeta destino según motor)",
		Long: "Instala un plugin/mod desde una URL https:// o desde un archivo local.\n\n" +
			"El artefacto se descarga/copia a un archivo temporal dentro de la carpeta destino,\n" +
			"se verifica (límite de tamaño durante la transferencia, checksum SHA-256 opcional y\n" +
			"estructura JAR/ZIP) y solo entonces se mueve ATÓMICAMENTE a su sitio. El registro de\n" +
			"la instancia se actualiza únicamente cuando la instalación ha terminado bien.\n\n" +
			"Por defecto: solo https://, se rechazan destinos loopback/privados/link-local, nunca\n" +
			"se sobrescribe un archivo existente y el límite de descarga es de 64 MiB.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("plugin add", err), "")
			}
			inst, err := e.pluginInstaller()
			if err != nil {
				return e.finish(result.FromError("plugin add", err), "")
			}
			opts, err := pluginInstallOptions(sha256Flag, maxSizeFlag, nameFlag, conflictFlag, allowHTTP, allowPrivate, allowLoopback)
			if err != nil {
				return e.finish(result.FromError("plugin add", err), "")
			}

			res, err := inst.Install(cmd.Context(), spec, args[0], opts)
			if err != nil {
				return e.finish(result.FromError("plugin add", err), "")
			}

			// El registro persistente se actualiza SOLO tras la instalación
			// completa. Si el guardado falla, se retira el archivo recién
			// instalado para no dejar registro y disco desincronizados.
			spec.Plugins = appendUnique(spec.Plugins, res.Name)
			if err := specSaved(e, spec); err != nil {
				if rmErr := inst.Remove(spec, res.Name); rmErr != nil {
					err = errx.Wrap(errx.CodeGeneral, err,
						"no se pudo guardar el registro de la instancia",
						"el archivo "+res.Name+" quedó instalado; bórralo con 'plugin remove' si no lo quieres")
				}
				return e.finish(result.FromError("plugin add", err), "")
			}

			names := []string{res.Name}
			data := map[string]any{
				"instance":          spec.Slug,
				"added":             res.Name,
				"source":            res.Source,
				"size_bytes":        res.Bytes,
				"sha256":            res.SHA256,
				"checksum_verified": res.ChecksumVerified,
				"archive":           res.Archive,
				"security_warning":  plugins.ThirdPartyWarning(),
			}
			if res.RenamedFrom != "" {
				data["renamed_from"] = res.RenamedFrom
			}
			if res.Replaced {
				data["replaced"] = true
			}

			human := fmt.Sprintf("%s instalado en %s/%s", res.Name, spec.Slug, inst.Dir(spec))
			human += fmt.Sprintf("\n%s · sha256 %s · %d entradas en el JAR/ZIP", download.HumanBytes(res.Bytes), res.SHA256, res.Archive.Entries)
			if res.RenamedFrom != "" {
				human += fmt.Sprintf("\nya existía %s: se instaló como %s", res.RenamedFrom, res.Name)
			}
			human += "\n⚠️ " + plugins.ThirdPartyWarning()
			if w := plugins.WarnFor(names); w != "" {
				human += "\n⚠️ " + w
				data["warning"] = w
				return e.finish(result.Warn("plugin add", human, data), human)
			}
			return e.finish(result.OK("plugin add", human, data), human)
		},
	}
	cmd.Flags().StringVar(&sha256Flag, "sha256", "", "SHA-256 esperado del artefacto (hex; admite el prefijo sha256:)")
	cmd.Flags().StringVar(&maxSizeFlag, "max-size", "", "límite de la descarga en bytes o con sufijo K/M/G (por defecto 64M)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "nombre final del archivo (para URLs que no terminan en .jar/.zip)")
	cmd.Flags().StringVar(&conflictFlag, "on-conflict", string(download.ConflictFail), "qué hacer si el archivo ya existe: fail|rename|replace")
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "permitir descarga por http:// sin cifrado (riesgo asumido)")
	cmd.Flags().BoolVar(&allowPrivate, "allow-private", false, "permitir destinos en redes privadas/link-local/metadatos (riesgo de SSRF)")
	cmd.Flags().BoolVar(&allowLoopback, "allow-loopback", false, "permitir destinos loopback (127.0.0.1, ::1)")
	return cmd
}

// pluginInstallOptions traduce los flags a la política de instalación.
func pluginInstallOptions(sha256Flag, maxSizeFlag, nameFlag, conflictFlag string, allowHTTP, allowPrivate, allowLoopback bool) (plugins.InstallOptions, error) {
	sum, err := parseSHA256(sha256Flag)
	if err != nil {
		return plugins.InstallOptions{}, err
	}
	maxBytes, err := parseSize(maxSizeFlag)
	if err != nil {
		return plugins.InstallOptions{}, err
	}
	pol := download.DefaultPolicy()
	pol.MaxBytes = maxBytes
	pol.AllowHTTP = allowHTTP
	pol.AllowPrivate = allowPrivate
	pol.AllowLoopback = allowLoopback
	pol.Conflict = download.Conflict(strings.ToLower(strings.TrimSpace(conflictFlag)))
	if err := pol.Validate(); err != nil {
		return plugins.InstallOptions{}, err
	}
	return plugins.InstallOptions{
		Download: pol,
		Archive:  archive.DefaultLimits(),
		SHA256:   sum,
		Name:     strings.TrimSpace(nameFlag),
	}, nil
}

// parseSHA256 valida el checksum indicado por el usuario. Cadena vacía ⇒ sin
// verificación por checksum (se avisa en el resultado con checksum_verified).
func parseSHA256(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "sha256:")
	if s == "" {
		return "", nil
	}
	if len(s) != 64 {
		return "", errx.New(errx.CodeUsage,
			"checksum SHA-256 inválido: se esperaban 64 caracteres hexadecimales",
			"copia el checksum publicado por el origen del artefacto").
			WithDetail("reason", "checksum_invalido")
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", errx.New(errx.CodeUsage,
			"checksum SHA-256 inválido: contiene caracteres no hexadecimales",
			"copia el checksum publicado por el origen del artefacto").
			WithDetail("reason", "checksum_invalido")
	}
	return s, nil
}

// parseSize acepta bytes ("4096") o un sufijo K/M/G en base 1024 (32K, 32KB,
// 32KiB, 64M…). Cadena vacía ⇒ 0 (se usa el límite por defecto).
func parseSize(raw string) (int64, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return 0, nil
	}
	mult := int64(1)
	for _, suf := range []string{"KIB", "MIB", "GIB", "KB", "MB", "GB", "K", "M", "G"} {
		if !strings.HasSuffix(s, suf) {
			continue
		}
		switch suf[0] {
		case 'K':
			mult = 1 << 10
		case 'M':
			mult = 1 << 20
		case 'G':
			mult = 1 << 30
		}
		s = strings.TrimSpace(strings.TrimSuffix(s, suf))
		break
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, errx.New(errx.CodeUsage,
			"tamaño inválido: "+raw,
			"usa bytes (4096) o un sufijo K/M/G (32M, 64MiB)").
			WithDetail("reason", "limite_invalido")
	}
	return n * mult, nil
}

func newPluginListCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista los plugins/mods instalados",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("plugin list", err), "")
			}
			inst, err := e.pluginInstaller()
			if err != nil {
				return e.finish(result.FromError("plugin list", err), "")
			}
			list, err := inst.List(spec)
			if err != nil {
				return e.finish(result.FromError("plugin list", err), "")
			}
			human := fmt.Sprintf("Plugins/mods de %s (%d):", spec.Slug, len(list))
			for _, p := range list {
				human += "\n  " + p
			}
			return e.finish(result.OK("plugin list", "plugins", map[string]any{"instance": spec.Slug, "plugins": list}), human)
		},
	}
}

func newPluginRemoveCmd(e *Env) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <nombre>",
		Short: "Elimina un plugin/mod instalado",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("plugin remove", err), "")
			}
			ok, err := e.confirm("eliminar el plugin", args[0], yes)
			if err != nil || !ok {
				return e.finish(result.FromError("plugin remove", errx.Usage("operación cancelada", "")), "operación cancelada")
			}
			inst, err := e.pluginInstaller()
			if err != nil {
				return e.finish(result.FromError("plugin remove", err), "")
			}
			if err := inst.Remove(spec, args[0]); err != nil {
				return e.finish(result.FromError("plugin remove", err), "")
			}
			spec.Plugins = removeString(spec.Plugins, args[0])
			_ = specSaved(e, spec)
			human := "plugin " + args[0] + " eliminado de " + spec.Slug
			return e.finish(result.OK("plugin remove", human, map[string]any{"instance": spec.Slug, "removed": args[0]}), human)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirmar sin preguntar")
	return cmd
}

func specSaved(e *Env, spec *instance.Spec) error {
	m, err := e.manager()
	if err != nil {
		return err
	}
	return m.Save(spec)
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func removeString(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
