package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Registry es el registro único de herramientas. Las herramientas se registran
// una sola vez, en orden, y se exponen a todos los frontends.
type Registry struct {
	tools map[string]*Tool
	order []string
}

// New crea un registro vacío.
func New() *Registry {
	return &Registry{tools: map[string]*Tool{}}
}

// Register añade una herramienta. Devuelve error si el nombre ya existe.
func (r *Registry) Register(t *Tool) error {
	if t == nil {
		return fmt.Errorf("no se puede registrar una herramienta nula")
	}
	if t.Name == "" {
		return fmt.Errorf("la herramienta debe tener nombre")
	}
	if t.Handler == nil {
		return fmt.Errorf("la herramienta %q no tiene handler", t.Name)
	}
	if _, ok := r.tools[t.Name]; ok {
		return fmt.Errorf("la herramienta %q ya está registrada", t.Name)
	}
	seen := map[string]bool{}
	for _, p := range t.Params {
		if seen[p.Name] {
			return fmt.Errorf("parámetro %q duplicado en la herramienta %q", p.Name, t.Name)
		}
		seen[p.Name] = true
	}
	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
	return nil
}

// Get devuelve la herramienta por nombre.
func (r *Registry) Get(name string) (*Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List devuelve las herramientas visibles en orden de registro.
func (r *Registry) List() []*Tool {
	out := make([]*Tool, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		if !t.Hidden {
			out = append(out, t)
		}
	}
	return out
}

// Names devuelve los nombres visibles en orden.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.order))
	for _, t := range r.List() {
		names = append(names, t.Name)
	}
	return names
}

// ListForReadOnly devuelve las herramientas visibles, excluyendo (según el
// modo) las que modifican estado.
func (r *Registry) ListForReadOnly(readOnly bool) []*Tool {
	out := r.List()
	if !readOnly {
		return out
	}
	filtered := make([]*Tool, 0, len(out))
	for _, t := range out {
		if t.ReadOnly {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// SortedNames devuelve los nombres ordenados alfabéticamente (para manifiestos
// estables en --json).
func (r *Registry) SortedNames() []string {
	names := r.Names()
	sort.Strings(names)
	return names
}

// Invoke ejecuta la herramienta por nombre con los argumentos en crudo,
// validando los parámetros requeridos.
func (r *Registry) Invoke(ctx context.Context, name string, raw json.RawMessage) (*Result, error) {
	t, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("herramienta desconocida %q", name)
	}
	return InvokeTool(ctx, t, raw)
}

// InvokeTool ejecuta una herramienta con validación de argumentos.
func InvokeTool(ctx context.Context, t *Tool, raw json.RawMessage) (*Result, error) {
	args := Args{}
	if len(raw) > 0 && string(raw) != "null" {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("argumentos de %q inválidos: %w", t.Name, err)
		}
		args = Args(m)
	}
	for _, p := range t.Params {
		if p.Required {
			if _, ok := args[p.Name]; !ok {
				return nil, fmt.Errorf("falta el argumento requerido %q en %q", p.Name, t.Name)
			}
		}
	}
	return t.Handler(ctx, args)
}
