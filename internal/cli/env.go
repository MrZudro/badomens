// Package cli define los comandos del binario badomens (cobra). Contrato
// general: cada comando imprime un único documento JSON en stdout (con -–json)
// y el código de salida es 0 éxito, 1 aviso, 2 error. Los errores van en
// `error.code`/`error.message` dentro del documento.
package cli

import (
	"io"
	"log/slog"
	"os"

	"github.com/manuelserver/badomens/internal/config"
	"github.com/manuelserver/badomens/internal/output"
	"github.com/manuelserver/badomens/internal/result"
	"github.com/manuelserver/badomens/internal/tools"
)

// Env concentra el estado compartido por todos los comandos.
type Env struct {
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader

	jsonFlag     bool
	verbose      bool
	debug        bool
	configFlag   string
	instanceFlag string

	printer  *output.Printer
	logger   *slog.Logger
	cfg      *config.Config
	cfgErr   error
	registry *tools.Registry
}

func (e *Env) setup() error {
	level := slog.LevelInfo
	if e.verbose {
		level = slog.LevelDebug
	}
	if e.debug {
		level = slog.LevelDebug
	}
	if v := os.Getenv("BADOMENS_LOG_LEVEL"); v != "" {
		_ = level.UnmarshalText([]byte(v))
	}
	e.logger = slog.New(slog.NewTextHandler(e.stderr, &slog.HandlerOptions{Level: level}))

	format := output.Human
	if e.jsonFlag {
		format = output.JSON
	}
	e.printer = output.New(e.stdout, e.stderr, format)

	cfg, err := config.Load()
	e.cfg, e.cfgErr = cfg, err
	return nil
}

// requireConfig devuelve la configuración o el error de carga.
func (e *Env) requireConfig() (*config.Config, error) {
	if e.cfgErr != nil {
		return nil, e.cfgErr
	}
	return e.cfg, nil
}

// Registry devuelve el registro único de herramientas, construyéndolo la
// primera vez.
func (e *Env) Registry() (*tools.Registry, error) {
	if e.registry != nil {
		return e.registry, nil
	}
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	reg, err := buildRegistry(cfg)
	if err != nil {
		return nil, err
	}
	e.registry = reg
	return reg, nil
}

// finish imprime el resultado y devuelve un error centinela con el código de
// salida si es distinto de cero.
func (e *Env) finish(res *result.Result, human string) error {
	e.printer.Emit(res, human)
	if res.ExitCode != 0 {
		return &exitError{code: res.ExitCode}
	}
	return nil
}
