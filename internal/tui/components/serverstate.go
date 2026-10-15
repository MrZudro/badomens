package components

import "github.com/MrZudro/badomens/internal/tui/theme"

// ServerState es el estado único y tipado de un servidor (§8 de la SPEC UX): una
// SOLA fuente de verdad que alimenta el semáforo, la pestaña Jugadores, la
// regeneración, el borrado y el backup.
type ServerState string

// Estados del servidor.
const (
	// ServerOnline: el servidor acepta conexiones.
	ServerOnline ServerState = "online"
	// ServerStarting: arrancando, deteniéndose o reiniciando.
	ServerStarting ServerState = "starting"
	// ServerStopped: apagado o sin crear.
	ServerStopped ServerState = "stopped"
	// ServerUnknown: no se pudo determinar (contenedor pausado, error…).
	ServerUnknown ServerState = "unknown"
)

// ServerStateOf traduce el estado crudo de Docker al estado único del servidor.
// «Cargando» cubre arranque, parada en curso y reinicio (SPEC §8.2).
func ServerStateOf(raw string) ServerState {
	switch raw {
	case "running":
		return ServerOnline
	case "restarting", "starting", "removing":
		return ServerStarting
	case "exited", "created", "dead", "":
		return ServerStopped
	}
	return ServerUnknown
}

// Ready indica que el servidor está en línea.
func (s ServerState) Ready() bool { return s == ServerOnline }

// Loading indica que hay una transición en curso (arrancando/deteniéndose/reiniciando).
func (s ServerState) Loading() bool { return s == ServerStarting }

// LED devuelve la luz del semáforo: 🟢/🟡/🔴, o su equivalente plano en ASCII.
func (s ServerState) LED(ascii bool) string {
	switch s {
	case ServerOnline:
		if ascii {
			return "[OK]"
		}
		return "\U0001F7E2"
	case ServerStarting:
		if ascii {
			return "[..]"
		}
		return "\U0001F7E1"
	case ServerStopped:
		if ascii {
			return "[XX]"
		}
		return "\U0001F534"
	}
	if ascii {
		return "[??]"
	}
	return "\u26AA"
}

// Label es el texto que SIEMPRE acompaña a la luz (nunca se comunica solo con color).
func (s ServerState) Label(ascii bool) string {
	switch s {
	case ServerOnline:
		return "En línea"
	case ServerStarting:
		return "Cargando…"
	case ServerStopped:
		return "Apagado"
	}
	return "Desconocido"
}

// Pill devuelve luz + texto, la forma canónica del semáforo.
func (s ServerState) Pill(ascii bool) string { return s.LED(ascii) + " " + s.Label(ascii) }

// Color devuelve el color del tema asociado al estado.
func (s ServerState) Color(pal theme.Palette) string {
	switch s {
	case ServerOnline:
		return pal.Good
	case ServerStarting:
		return pal.Warn
	case ServerStopped:
		return pal.Bad
	}
	return pal.Muted
}
