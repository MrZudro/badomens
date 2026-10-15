package svc

import (
	"context"
	"sync"
	"time"

	"github.com/MrZudro/badomens/internal/idle"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/runtime"
	"github.com/MrZudro/badomens/internal/tailscale"
)

// PlayerReason explica POR QUÉ no se pudo medir la lista de jugadores: la UI da
// un mensaje distinto por causa (SPEC 2 §7), en vez de culpar siempre al
// servidor apagado.
type PlayerReason string

// Causas posibles de una consulta de jugadores.
const (
	// PlayerOK: la consulta se pudo completar (Known = true).
	PlayerOK PlayerReason = ""
	// PlayerStopped: el servidor está apagado; no hay a quién preguntar.
	PlayerStopped PlayerReason = "stopped"
	// PlayerStarting: el servidor está arrancando; la consulta se reintenta.
	PlayerStarting PlayerReason = "starting"
	// PlayerNoAddress: no hay dirección (ni IP del tailnet ni host persistido).
	PlayerNoAddress PlayerReason = "no_address"
	// PlayerQueryDisabled: el Server List Ping no responde y RCON está
	// deshabilitado en la imagen (no hay método de consulta disponible).
	PlayerQueryDisabled PlayerReason = "query_disabled"
	// PlayerUnreachable: error de conexión al consultar (timeout, red…).
	PlayerUnreachable PlayerReason = "unreachable"
)

// PlayerReport es el resultado de consultar los jugadores de una instancia.
type PlayerReport struct {
	// Online y Max son los jugadores conectados y el cupo (si Known).
	Online int
	Max    int
	// Known indica que la medición se pudo completar.
	Known bool
	// State es el estado del contenedor en el momento de la consulta.
	State string
	// Method es el método que respondió: "slp" (Server List Ping, principal) o
	// "rcon" (respaldo por RCON).
	Method string
	// Reason y Detail explican por qué no se pudo medir (Known = false).
	Reason PlayerReason
	Detail string
}

// Players consulta los jugadores conectados de una instancia. El método real es
// Server List Ping (protocolo estándar de Minecraft) con RCON como respaldo; la
// TUI no sabe de protocolos: pide el informe y lo pinta.
type Players interface {
	Report(ctx context.Context, slug string) (PlayerReport, error)
}

// realPlayers mide los jugadores por Server List Ping y, si no responde, por
// RCON dentro del contenedor (`docker exec … rcon-cli list`).
type realPlayers struct {
	m  *instance.Manager
	rt *runtime.Runtime
	// attempts es el número de intentos del Server List Ping (por defecto 2):
	// justo después de pasar a «running» el puerto puede tardar en aceptar
	// conexiones, así que se reintenta con un timeout corto y razonable.
	attempts int
}

var _ Players = (*realPlayers)(nil)

// Report mide los jugadores de una instancia sin bloquear el bucle de la TUI
// (se llama desde un tea.Cmd). No consulta si el servidor no está corriendo.
func (p *realPlayers) Report(ctx context.Context, slug string) (PlayerReport, error) {
	spec, err := p.m.Load(slug)
	if err != nil {
		return PlayerReport{}, err
	}
	rep := PlayerReport{Max: spec.MaxPlayers}
	if p.rt != nil {
		state, serr := p.rt.State(ctx, slug)
		if serr != nil {
			return rep, serr
		}
		rep.State = state
		if state == "restarting" || state == "starting" {
			rep.Reason = PlayerStarting
			return rep, nil
		}
		if state != "running" {
			rep.Reason = PlayerStopped
			return rep, nil
		}
	}
	host := ""
	if spec.Connection != nil {
		host = spec.Connection.Host
	}
	if host == "" {
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		host, _ = tailscale.ResolveIPv4(rctx)
		cancel()
	}
	if host == "" || spec.Port <= 0 {
		rep.Reason = PlayerNoAddress
		return rep, nil
	}

	attempts := p.attempts
	if attempts < 1 {
		attempts = 2
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		pctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		st, perr := idle.Ping(pctx, host, spec.Port, 3*time.Second)
		cancel()
		if perr == nil {
			rep.Online, rep.Known, rep.Method = st.Players.Online, true, "slp"
			if st.Players.Max > 0 {
				rep.Max = st.Players.Max
			}
			return rep, nil
		}
		lastErr = perr
		if i+1 < attempts {
			time.Sleep(300 * time.Millisecond)
		}
	}

	// Respaldo: RCON dentro del contenedor (requiere ENABLE_RCON).
	if p.rt != nil {
		rcon := &idle.DockerCounter{Container: func(string) string { return p.rt.ContainerName(slug) }}
		n, known, rerr := rcon.Online(ctx, slug)
		if known {
			rep.Online, rep.Known, rep.Method = n, true, "rcon"
			return rep, nil
		}
		if rerr != nil {
			rep.Reason, rep.Detail = PlayerUnreachable, rerr.Error()
		} else {
			rep.Reason = PlayerQueryDisabled
		}
	} else {
		rep.Reason = PlayerQueryDisabled
	}
	if rep.Detail == "" && lastErr != nil {
		rep.Detail = lastErr.Error()
	}
	return rep, nil
}

// FakePlayers es el adaptador determinista de Players para los tests (sin red ni
// Docker): devuelve el informe guionizado y registra las consultas.
type FakePlayers struct {
	mu     sync.Mutex
	report PlayerReport
	err    error
	calls  []string
}

var _ Players = (*FakePlayers)(nil)

// Report devuelve el informe guionizado.
func (f *FakePlayers) Report(_ context.Context, slug string) (PlayerReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, slug)
	if f.err != nil {
		return PlayerReport{}, f.err
	}
	rep := f.report
	if rep.State == "" {
		rep.State = "running"
	}
	if rep.Online > 0 && rep.Method == "" {
		rep.Method = "slp"
	}
	return rep, nil
}

// Calls devuelve los slugs consultados, en orden.
func (f *FakePlayers) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}

// Reset limpia las consultas registradas.
func (f *FakePlayers) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// SetReport cambia el informe que devuelve el fake.
func (f *FakePlayers) SetReport(rep PlayerReport) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.report = rep
}

// SetErr fuerza el error del fake ("" para dejarlo sin error).
func (f *FakePlayers) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}
