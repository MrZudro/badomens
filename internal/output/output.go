// Package output imprime el documento de resultado en modo JSON (stdout) o
// texto legible en modo humano. En modo humano, los errores también se anotan
// en stderr para que sean visibles en una terminal.
package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/MrZudro/badomens/internal/result"
)

// Format es el modo de salida.
type Format int

// Modos de salida.
const (
	// Human imprime texto legible.
	Human Format = iota
	// JSON imprime el documento de resultado estable.
	JSON
)

// Printer emite la salida de un comando.
type Printer struct {
	stdout io.Writer
	stderr io.Writer
	format Format
}

// New crea un Printer para stdout (w) y stderr (errW).
func New(w, errW io.Writer, format Format) *Printer {
	return &Printer{stdout: w, stderr: errW, format: format}
}

// IsJSON indica si el modo es JSON.
func (p *Printer) IsJSON() bool { return p.format == JSON }

// Emit imprime el resultado: en JSON, el documento a stdout; en humano, el
// texto indicado y, si hay error, una línea en stderr.
func (p *Printer) Emit(res *result.Result, human string) {
	if p.format == JSON {
		p.emitJSON(res)
		return
	}
	if human == "" {
		human = res.Message
	}
	if human != "" {
		_, _ = fmt.Fprintln(p.stdout, human)
	}
	if res.Error != nil {
		line := res.Error.Message
		if res.Error.Hint != "" {
			line += " (" + res.Error.Hint + ")"
		}
		_, _ = fmt.Fprintf(p.stderr, "error: %s\n", line)
	}
}

// Error imprime un error previo al despacho (fallo de parseo o de configuración).
func (p *Printer) Error(action string, err error) {
	p.Emit(result.FromError(action, err), "")
}

func (p *Printer) emitJSON(res *result.Result) {
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(p.stdout, string(raw))
}
