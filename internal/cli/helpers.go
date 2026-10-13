package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/ports"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/MrZudro/badomens/internal/runtime"
	"github.com/spf13/cobra"
)

// engineNames devuelve los nombres de motores en orden estable.
func engineNames() []string {
	names := make([]string, 0, len(engines.Order))
	for _, e := range engines.Order {
		names = append(names, e.Name)
	}
	return names
}

// manager construye el gestor de instancias sobre el data dir.
func (e *Env) manager() (*instance.Manager, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	if _, err := cfg.EnsureDataDir(); err != nil {
		return nil, err
	}
	return instance.New(cfg.DataDir, engineNames()), nil
}

// runtime construye el ejecutor de Compose.
func (e *Env) runtime() (*runtime.Runtime, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	return runtime.New(cfg), nil
}

// specFor carga una instancia por slug.
func (e *Env) specFor(slug string) (*instance.Spec, error) {
	m, err := e.manager()
	if err != nil {
		return nil, err
	}
	return m.Load(slug)
}

// isTTY indica si el reader es una terminal real.
func isTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// isTTYWriter indica si el writer es una terminal real (para stdout).
func isTTYWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// prompt pregunta interactivamente y devuelve el valor tecleado o el default.
func (e *Env) prompt(label, def string) string {
	if !isTTY(e.stdin) {
		return def
	}
	reader := bufio.NewReader(e.stdin)
	suffix := ""
	if def != "" {
		suffix = " [" + def + "]"
	}
	_, _ = fmt.Fprintf(e.stderr, "%s%s: ", label, suffix)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// confirm pide confirmación para operaciones destructivas. Con yes=true (flag
// --yes) se acepta sin preguntar.
func (e *Env) confirm(action, target string, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	msg := fmt.Sprintf("¿Seguro que quieres %s %q? (s/N)", action, target)
	if !isTTY(e.stdin) {
		return false, errx.Usage("operación destructiva sin confirmar", "añade --yes si quieres proceder")
	}
	ans := strings.ToLower(e.prompt(msg, "N"))
	return ans == "s" || ans == "si" || ans == "sí" || ans == "y" || ans == "yes", nil
}

// reg construye el registro de motores con su cliente por defecto.
func reg() *engines.Registry {
	return engines.NewRegistry(engines.DefaultClient())
}

// portsAllocator construye el asignador de puertos (RF-3) con el prober real:
// registro, docker, bind y peers según la configuración.
func (e *Env) portsAllocator(ctx context.Context) (*ports.Allocator, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	rng, err := ports.ParseRange(cfg.Ports.Range)
	if err != nil {
		return nil, err
	}
	mode, err := ports.ParseScanMode(cfg.Ports.TailnetScan)
	if err != nil {
		return nil, err
	}
	checker := &ports.DefaultChecker{
		Prober: ports.NewRealProber(func(ctx context.Context) ([]ports.RegistryPort, error) {
			m, merr := e.manager()
			if merr != nil {
				return nil, merr
			}
			specs, lerr := m.List()
			if lerr != nil {
				return nil, lerr
			}
			out := make([]ports.RegistryPort, 0, len(specs))
			for _, s := range specs {
				out = append(out, ports.RegistryPort{Instance: s.Slug, Port: s.Port})
			}
			return out, nil
		}),
		TailnetScan: mode,
	}
	return ports.NewAllocator(cfg.DataDir, checker, rng), nil
}

// activeSpec devuelve la instancia indicada con --instance.
func (e *Env) activeSpec() (*instance.Spec, error) {
	if e.instanceFlag == "" {
		return nil, errx.Usage("falta --instance", "indica la instancia con --instance <slug> (o -i)")
	}
	return e.specFor(e.instanceFlag)
}

// withStopped ejecuta mutate con la instancia detenida y la relanza si estaba
// corriendo. Devuelve un texto de fases para el resultado.
func (e *Env) withStopped(cmd *cobra.Command, slug string, mutate func() error) (phases []result.Phase, err error) {
	rt, err := e.runtime()
	if err != nil {
		return nil, err
	}
	state, err := rt.State(cmd.Context(), slug)
	if err != nil {
		return nil, err
	}
	wasRunning := state == "running"
	if wasRunning {
		if err := rt.Stop(cmd.Context(), slug); err != nil {
			return nil, err
		}
		phases = append(phases, result.Phase{Name: "stop", Status: "ok"})
	}
	if err := mutate(); err != nil {
		return phases, err
	}
	phases = append(phases, result.Phase{Name: "op", Status: "ok"})
	if wasRunning {
		spec, lerr := e.specFor(slug)
		if lerr != nil {
			return nil, lerr
		}
		if err := rt.Start(cmd.Context(), spec); err != nil {
			return nil, err
		}
		phases = append(phases, result.Phase{Name: "start", Status: "ok"})
	}
	return phases, nil
}
