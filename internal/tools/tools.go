// Package tools define el registro único de herramientas de badomens.
//
// Cada herramienta (nombre, descripción, esquema de parámetros y función) se
// escribe una sola vez y la consumen los frontends: el servidor MCP y, a través
// de él, el agente de Emiria/Jamcord. Los subcomandos del CLI son la cara humana
// del mismo núcleo.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ParamType es el tipo de un parámetro de herramienta.
type ParamType string

// Tipos de parámetro soportados (subconjunto intencionadamente pequeño).
const (
	TypeString ParamType = "string"
	TypeInt    ParamType = "integer"
	TypeNumber ParamType = "number"
	TypeBool   ParamType = "boolean"
	TypeArray  ParamType = "array"
	TypeObject ParamType = "object"
)

// Param describe un parámetro de una herramienta.
type Param struct {
	// Name es el nombre del parámetro (clave en el objeto JSON).
	Name string `json:"name"`
	// Type es el tipo del parámetro.
	Type ParamType `json:"type"`
	// Description explica el parámetro para el LLM y para los humanos.
	Description string `json:"description"`
	// Required indica si el parámetro es obligatorio.
	Required bool `json:"required"`
	// Enum restringe los valores admitidos (solo para TypeString).
	Enum []string `json:"enum,omitempty"`
	// Default es el valor por defecto (solo para TypeString/TypeBool).
	Default any `json:"default,omitempty"`
	// Items describe el esquema de los elementos de un TypeArray.
	Items *Param `json:"items,omitempty"`
	// Properties describe las propiedades de un TypeObject.
	Properties []Param `json:"properties,omitempty"`
}

func (p *Param) schema() map[string]any {
	s := map[string]any{
		"type":        string(p.Type),
		"description": p.Description,
	}
	if len(p.Enum) > 0 {
		s["enum"] = p.Enum
	}
	if p.Default != nil {
		s["default"] = p.Default
	}
	if p.Type == TypeArray {
		if p.Items != nil {
			s["items"] = p.Items.schema()
		} else {
			s["items"] = map[string]any{"type": "string"}
		}
	}
	if p.Type == TypeObject && len(p.Properties) > 0 {
		props := map[string]any{}
		var req []string
		for _, sub := range p.Properties {
			props[sub.Name] = sub.schema()
			if sub.Required {
				req = append(req, sub.Name)
			}
		}
		s["properties"] = props
		if len(req) > 0 {
			s["required"] = req
		}
	}
	return s
}

// Handler ejecuta una herramienta con los argumentos ya validados.
type Handler func(ctx context.Context, args Args) (*Result, error)

// Tool describe una herramienta.
type Tool struct {
	// Name es el nombre único de la herramienta (snake_case).
	Name string `json:"name"`
	// Description describe la herramienta para el LLM y para los humanos.
	Description string `json:"description"`
	// Params es la lista de parámetros.
	Params []Param `json:"params"`
	// Handler es la función de ejecución.
	Handler Handler `json:"-"`
	// ReadOnly indica que la herramienta no modifica estado externo.
	ReadOnly bool `json:"read_only"`
	// Hidden oculta la herramienta del manifiesto público.
	Hidden bool `json:"-"`
}

// Result es la respuesta de una herramienta: un resumen legible y un resultado
// estructurado.
type Result struct {
	// Text es un resumen para humanos y para el LLM.
	Text string `json:"text"`
	// Data es el resultado estructurado para --json y MCP.
	Data map[string]any `json:"data,omitempty"`
}

// JSONSchema devuelve el JSON Schema (subconjunto, borrador 2020-12) de los
// parámetros de la herramienta, en el formato que esperan MCP, OpenAI y Gemini.
func (t *Tool) JSONSchema() map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, p := range t.Params {
		properties[p.Name] = p.schema()
		if p.Required {
			required = append(required, p.Name)
		}
	}
	out := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// Args son los argumentos ya parseados de una herramienta.
type Args map[string]json.RawMessage

// String devuelve un parámetro de tipo string. El valor vacío se acepta.
func (a Args) String(name string) (string, error) {
	raw, ok := a[name]
	if !ok {
		return "", fmt.Errorf("falta el argumento %q", name)
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("el argumento %q debe ser un string: %w", name, err)
	}
	return v, nil
}

// Int devuelve un parámetro entero.
func (a Args) Int(name string) (int, error) {
	raw, ok := a[name]
	if !ok {
		return 0, fmt.Errorf("falta el argumento %q", name)
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, fmt.Errorf("el argumento %q debe ser un entero: %w", name, err)
	}
	return v, nil
}

// Bool devuelve un parámetro booleano.
func (a Args) Bool(name string) (bool, error) {
	raw, ok := a[name]
	if !ok {
		return false, fmt.Errorf("falta el argumento %q", name)
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, fmt.Errorf("el argumento %q debe ser booleano: %w", name, err)
	}
	return v, nil
}

// Strings devuelve un parámetro de tipo array de strings.
func (a Args) Strings(name string) ([]string, error) {
	raw, ok := a[name]
	if !ok {
		return nil, fmt.Errorf("falta el argumento %q", name)
	}
	var v []string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("el argumento %q debe ser una lista de strings: %w", name, err)
	}
	return v, nil
}

// OptionalString devuelve un string opcional (vacío si falta o es null).
func (a Args) OptionalString(name string) (string, error) {
	if a[name] == nil || string(a[name]) == "null" {
		return "", nil
	}
	return a.String(name)
}

// OptionalInt devuelve un entero opcional con valor por defecto.
func (a Args) OptionalInt(name string, def int) (int, error) {
	if a[name] == nil || string(a[name]) == "null" {
		return def, nil
	}
	return a.Int(name)
}
