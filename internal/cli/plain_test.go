package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/output"
	"github.com/spf13/cobra"
)

// A11Y-06 (§6.1): «badomens tui --plain» preserva una salida LINEAL (sin cajas ni
// animación) para lectores de pantalla y entornos no interactivos.
func TestTUIPlainResumenLineal(t *testing.T) {
	dataDir := t.TempDir()
	var out, errBuf bytes.Buffer
	env := &Env{
		stdout: &out, stderr: &errBuf, stdin: strings.NewReader(""),
		cfg:     &config.Config{DataDir: dataDir},
		printer: output.New(&out, &errBuf, output.Human),
	}
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newTUICmd(env))
	root.SetArgs([]string{"tui", "--plain"})
	if err := root.Execute(); err != nil {
		t.Fatalf("tui --plain: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "No hay instancias") {
		t.Fatalf("--plain debe imprimir un resumen lineal, tengo %q", got)
	}
	if strings.ContainsAny(got, "│┌┐└┘─") {
		t.Fatalf("--plain no debe dibujar cajas: %q", got)
	}
}
