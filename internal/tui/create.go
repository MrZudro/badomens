package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/provision"
	"github.com/MrZudro/badomens/internal/setup"
)

// createFieldKind clasifica cómo se edita un campo del asistente.
type createFieldKind int

const (
	fieldText createFieldKind = iota
	fieldInt
	fieldSelect
	fieldBool
)

// createField es un campo del asistente de creación.
type createField struct {
	key         string
	label       string
	kind        createFieldKind
	value       string
	options     []string
	index       int
	boolVal     bool
	placeholder string
	help        string
}

// createForm es el formulario de creación de instancias de la TUI. Mantiene un
// único textinput y sincroniza su valor con el campo activo.
type createForm struct {
	fields []createField
	cursor int
	input  textinput.Model
	err    string
}

// newCreateForm construye el formulario con valores por defecto razonables.
func newCreateForm(engineList []engines.Engine, defaultMemory string) *createForm {
	names := make([]string, 0, len(engineList))
	for _, e := range engineList {
		names = append(names, e.Name)
	}
	if len(names) == 0 {
		names = []string{"paper"}
	}
	if defaultMemory == "" {
		defaultMemory = "4G"
	}
	in := textinput.New()
	in.Placeholder = ""
	in.Width = 40
	f := &createForm{
		fields: []createField{
			{key: "name", label: "nombre", kind: fieldText, placeholder: "Survival (obligatorio)"},
			{key: "engine", label: "motor", kind: fieldSelect, options: names, index: indexOf(names, "paper")},
			{key: "version", label: "versión", kind: fieldText, placeholder: "última"},
			{key: "memory", label: "memoria", kind: fieldText, value: defaultMemory},
			{key: "cpu", label: "cpu", kind: fieldInt, value: "0", help: "0 = sin límite"},
			{key: "difficulty", label: "dificultad", kind: fieldSelect, options: []string{"normal", "peaceful", "easy", "hard"}},
			{key: "gamemode", label: "modo", kind: fieldSelect, options: []string{"survival", "creative", "adventure", "spectator"}},
			{key: "max_players", label: "jugadores máx", kind: fieldInt, value: "0", help: "0 = recomendado"},
			{key: "online", label: "premium", kind: fieldBool, help: "cuentas Microsoft/Mojang"},
			{key: "whitelist", label: "whitelist", kind: fieldBool},
			{key: "whitelist_users", label: "  usuarios", kind: fieldText, placeholder: "a, b, c"},
			{key: "ops", label: "ops", kind: fieldText, placeholder: "a, b, c"},
			{key: "seed", label: "semilla", kind: fieldText, placeholder: "aleatoria"},
			{key: "motd", label: "motd", kind: fieldText},
			{key: "plugins", label: "plugins/mods", kind: fieldText, placeholder: "archivos .jar o URLs, comas"},
			{key: "start", label: "arrancar", kind: fieldBool, boolVal: true},
		},
		input: in,
	}
	f.syncInput()
	return f
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return 0
}

// field devuelve el campo por clave (o nil si no existe).
func (f *createForm) field(key string) *createField {
	for i := range f.fields {
		if f.fields[i].key == key {
			return &f.fields[i]
		}
	}
	return nil
}

// active devuelve el campo con el foco.
func (f *createForm) active() *createField { return &f.fields[f.cursor] }

// syncInput carga en el textinput el valor y placeholder del campo activo.
func (f *createForm) syncInput() {
	cur := f.active()
	if cur.kind == fieldText || cur.kind == fieldInt {
		f.input.Placeholder = cur.placeholder
		f.input.SetValue(cur.value)
		f.input.CursorEnd()
		f.input.Focus()
		return
	}
	f.input.Blur()
}

// storeInput guarda el texto tecleado en el campo activo.
func (f *createForm) storeInput() {
	cur := f.active()
	if cur.kind == fieldText || cur.kind == fieldInt {
		cur.value = f.input.Value()
	}
}

// move desplaza el foco y sincroniza el input.
func (f *createForm) move(delta int) {
	f.storeInput()
	f.cursor += delta
	if f.cursor < 0 {
		f.cursor = len(f.fields) - 1
	}
	if f.cursor >= len(f.fields) {
		f.cursor = 0
	}
	f.err = ""
	f.syncInput()
}

// update procesa una tecla. Devuelve el comando generado (por el textinput) y si
// el formulario debe enviarse.
func (f *createForm) update(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		return nil, false
	case "ctrl+s":
		return nil, true
	case "up":
		f.move(-1)
		return nil, false
	case "down", "tab":
		f.move(+1)
		return nil, false
	case "enter":
		if f.cursor == len(f.fields)-1 {
			return nil, true
		}
		f.move(+1)
		return nil, false
	}

	cur := f.active()
	switch cur.kind {
	case fieldSelect:
		switch msg.String() {
		case "left":
			cur.index = (cur.index - 1 + len(cur.options)) % len(cur.options)
		case "right", " ":
			cur.index = (cur.index + 1) % len(cur.options)
		}
		return nil, false
	case fieldBool:
		if msg.String() == " " || msg.String() == "left" || msg.String() == "right" {
			cur.boolVal = !cur.boolVal
		}
		return nil, false
	default:
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		f.storeInput()
		return cmd, false
	}
}

// view renderiza el formulario con el ancho dado.
func (f *createForm) view(width int) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Nueva instancia") + "\n")
	if f.err != "" {
		b.WriteString(styleErr.Render("  "+truncate(f.err, width-2)) + "\n")
	}
	for i := range f.fields {
		cur := &f.fields[i]
		marker := "  "
		label := cur.label
		if i == f.cursor {
			marker = styleKey.Render("▸ ")
			label = styleKey.Render(cur.label)
		}
		var val string
		switch cur.kind {
		case fieldSelect:
			val = "[ " + cur.options[cur.index] + " ]"
		case fieldBool:
			if cur.boolVal {
				val = "[x] sí"
			} else {
				val = "[ ] no"
			}
		default:
			if i == f.cursor {
				val = f.input.View()
			} else if cur.value != "" {
				val = cur.value
			} else {
				val = styleMuted.Render(cur.placeholder)
			}
		}
		line := marker + padLabel(label, 14) + " " + val
		if cur.help != "" {
			line += "  " + styleMuted.Render(cur.help)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// padLabel rellena la etiqueta a un ancho visible fijo.
func padLabel(s string, width int) string {
	pad := width - len([]rune(s))
	if pad < 0 {
		pad = 0
	}
	return s + strings.Repeat(" ", pad)
}

// request valida el formulario y construye la petición de creación.
func (f *createForm) request() (provision.Request, bool) {
	name := strings.TrimSpace(f.field("name").value)
	if name == "" {
		f.err = "el nombre es obligatorio"
		return provision.Request{}, false
	}
	memory := strings.TrimSpace(f.field("memory").value)
	if memory == "" {
		memory = "4G"
	}
	if host.ParseMemory(memory) == 0 {
		f.err = "memoria inválida (ej. 4G)"
		return provision.Request{}, false
	}
	cpu, ok := intValue(f.field("cpu").value)
	if !ok {
		f.err = "cpu inválida (número entero)"
		return provision.Request{}, false
	}
	maxPlayers, ok := intValue(f.field("max_players").value)
	if !ok {
		f.err = "jugadores máx inválido (número entero)"
		return provision.Request{}, false
	}
	engine := f.field("engine")
	req := provision.Request{
		Name:             name,
		Engine:           engine.options[engine.index],
		Version:          strings.TrimSpace(f.field("version").value),
		Memory:           memory,
		CPU:              cpu,
		Difficulty:       f.field("difficulty").options[f.field("difficulty").index],
		Gamemode:         f.field("gamemode").options[f.field("gamemode").index],
		MaxPlayers:       maxPlayers,
		Seed:             strings.TrimSpace(f.field("seed").value),
		Online:           f.field("online").boolVal,
		WhitelistEnabled: f.field("whitelist").boolVal,
		Whitelist:        setup.ParseCommaList(f.field("whitelist_users").value),
		Ops:              setup.ParseCommaList(f.field("ops").value),
		Motd:             strings.TrimSpace(f.field("motd").value),
		Plugins:          setup.ParseCommaList(f.field("plugins").value),
		Start:            f.field("start").boolVal,
	}
	return req, true
}

func intValue(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
