package cli

import (
	"errors"
	"os"

	"github.com/manuelserver/badomens/internal/errx"
	"github.com/manuelserver/badomens/internal/output"
	"github.com/manuelserver/badomens/internal/version"
	"github.com/spf13/cobra"
)

// exitError es un error centinela cuyo único propósito es transportar el código
// de salida ya calculado. El resultado ya se imprimió.
type exitError struct{ code int }

func (e *exitError) Error() string { return "" }

// Execute es la entrada del binario. Devuelve el código de salida del proceso.
func Execute() int {
	env := &Env{stdout: os.Stdout, stderr: os.Stderr, stdin: os.Stdin}
	root := newRootCmd(env)
	err := root.Execute()
	if err == nil {
		return 0
	}

	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}

	// Error previo al despacho (flags, parseo o configuración).
	if env.printer == nil {
		format := output.Human
		if hasFlag(os.Args, "--json") {
			format = output.JSON
		}
		env.printer = output.New(env.stdout, env.stderr, format)
	}
	env.printer.Error("badomens", err)
	return errx.ExitCode(err)
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func newRootCmd(e *Env) *cobra.Command {
	root := &cobra.Command{
		Use:     "badomens",
		Short:   "Gestiona varios servidores de Minecraft (Paper/Fabric/Forge/NeoForge/Vanilla)",
		Version: version.Version,
		Long: `badomens es un binario único que gestiona hasta varias instancias de
servidor Minecraft en Docker Compose (imagen itzg/minecraft-server), cada una
con su motor, versión, mundo y recursos, publicadas solo en tu red privada.

Cada comando imprime un documento JSON estable. Detalle: 'badomens <comando> --help'.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return e.setup()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("badomens version {{.Version}}\n")

	pf := root.PersistentFlags()
	pf.BoolVar(&e.jsonFlag, "json", false, "imprime el documento JSON estable")
	pf.BoolVarP(&e.verbose, "verbose", "v", false, "salida más detallada")
	pf.BoolVar(&e.debug, "debug", false, "modo depuración")
	pf.StringVar(&e.configFlag, "config", "", "ruta al archivo de configuración")
	pf.StringVarP(&e.instanceFlag, "instance", "i", "", "instancia activa (slug) para comandos que la requieren")

	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return errx.Usage(err.Error(), "ejecuta 'badomens help' para ver las opciones")
	})

	root.AddCommand(
		newVersionCmd(e),
		newDoctorCmd(e),
		newUpdateCmd(e),
		newConfigCmd(e),
		newCapacityCmd(e),
		newCommandsCmd(e),
		newMCPCmd(e),
		newEngineCmd(e),
		newInstanceCmd(e),
		newSetupCmd(e),
		newWorldCmd(e),
		newBackupCmd(e),
		newPluginCmd(e),
		newLogsCmd(e),
		newConsoleCmd(e),
		newResolveIPCmd(e),
		newAccessCmd(e),
	)
	return root
}
