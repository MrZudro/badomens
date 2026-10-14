package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/atotto/clipboard"

	"github.com/MrZudro/badomens/internal/connection"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/tailscale"
	"github.com/spf13/cobra"
)

func newConnectCmd(e *Env) *cobra.Command {
	var copyFlag bool
	cmd := &cobra.Command{
		Use:   "connect [instancia]",
		Short: "Información de conexión de una instancia (RF-2)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, aerr := e.instanceArg(args)
			if aerr != nil {
				return e.finish(result.FromError("connect", aerr), "")
			}
			spec, err := e.specFor(slug)
			if err != nil {
				return e.finish(result.FromError("connect", err), "")
			}
			info := buildConnection(cmd.Context(), e, spec)
			data := map[string]any{"connection": connectionData(info)}
			human := connectionHuman(info)
			if copyFlag {
				if cerr := copiarTexto(info.Preferred()); cerr != nil {
					human += "\n(no se pudo copiar al portapapeles: " + cerr.Error() + ")"
				} else {
					human += "\nDirección copiada: " + info.Preferred()
				}
			}
			return e.finish(result.OK("connect", "información de conexión de "+spec.Slug, data), human)
		},
	}
	cmd.Flags().BoolVar(&copyFlag, "copy", false, "copiar la dirección preferida al portapapeles")
	return cmd
}

// buildConnection calcula la información de conexión de una instancia con los
// datos actuales de la tailnet (la IP puede cambiar entre arranques).
func buildConnection(ctx context.Context, e *Env, spec *instance.Spec) connection.Info {
	ip, _ := tailscale.ResolveIPv4(ctx)
	if ip == "" && spec.Connection != nil {
		ip = spec.Connection.Host // respaldo: la IP guardada del último arranque
	}
	dns, _ := tailscale.MagicDNS(ctx)
	if dns == "" && spec.Connection != nil {
		dns = spec.Connection.MagicDNS
	}
	return connection.FromData(
		spec.Slug, spec.Name, spec.Engine, spec.Version, spec.Port, spec.Pins,
		spec.OnlineMode, spec.WhitelistEnabled, ip, dns, time.Now(),
	)
}

// rememberConnection calcula, guarda y devuelve la conexión (RF-2). Se llama al
// crear y en cada arranque.
func rememberConnection(ctx context.Context, e *Env, spec *instance.Spec) (connection.Info, error) {
	info := buildConnection(ctx, e, spec)
	spec.Connection = &info
	m, err := e.manager()
	if err != nil {
		return info, nil // sin guardar, no es crítico
	}
	_ = m.Save(spec)
	return info, nil
}

// connectionData serializa una Info a un mapa JSON.
func connectionData(info connection.Info) map[string]any {
	raw, _ := json.Marshal(info)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

func connectionHuman(info connection.Info) string {
	out := fmt.Sprintf("Servidor «%s» — Minecraft %s %s", nameOf(info), info.Edition, info.McVersion)
	if info.Address != "" {
		out += "\nDirección:  " + info.Address
	}
	if info.AddressDNS != "" {
		out += "\nDirección con nombre: " + info.AddressDNS
	}
	if info.Loader != "" {
		out += "\nLoader:     " + info.Loader
	}
	if info.OnlineMode {
		out += "\nAcceso:     cuentas oficiales (modo online)"
	} else {
		out += "\nAcceso:     cualquier nombre (modo offline)"
	}
	if info.Whitelisted {
		out += "\nAcceso:     lista blanca activa"
	}
	if len(info.Warnings) > 0 {
		for _, w := range info.Warnings {
			out += "\n⚠ " + w
		}
	}
	return out
}

func nameOf(info connection.Info) string {
	if info.Name != "" {
		return info.Name
	}
	if info.Instance != "" {
		return info.Instance
	}
	return "?"
}

// copiarTexto copia al portapapeles del sistema.
func copiarTexto(s string) error {
	if s == "" {
		return nil
	}
	return clipboard.WriteAll(s)
}
