package idle

import (
	"context"
	"regexp"
	"strconv"
	"time"

	"github.com/MrZudro/badomens/internal/execx"
)

// Counter detecta cuántos jugadores hay conectados en una instancia. Devuelve
// (online, known, err): known=false significa que no se pudo medir.
type Counter interface {
	Online(ctx context.Context, instance string) (int, bool, error)
}

// CounterFunc adapta una función a Counter.
type CounterFunc func(ctx context.Context, instance string) (int, bool, error)

// Online implementa Counter.
func (f CounterFunc) Online(ctx context.Context, instance string) (int, bool, error) {
	return f(ctx, instance)
}

// SLPCounter detecta jugadores por Server List Ping (método principal).
type SLPCounter struct {
	// Endpoint devuelve host y puerto de una instancia (ok=false si no se sabe).
	Endpoint func(instance string) (host string, port int, ok bool)
	// Timeout es el tiempo máximo del sondeo.
	Timeout time.Duration
}

// Online implementa Counter con Server List Ping.
func (c *SLPCounter) Online(ctx context.Context, instance string) (int, bool, error) {
	if c.Endpoint == nil {
		return 0, false, nil
	}
	host, port, ok := c.Endpoint(instance)
	if !ok || host == "" || port == 0 {
		return 0, false, nil
	}
	st, err := Ping(ctx, host, port, c.Timeout)
	if err != nil {
		return 0, false, err
	}
	return st.Players.Online, true, nil
}

var rconListRe = regexp.MustCompile(`There are (\d+) of a max`)

// DockerCounter es el respaldo: consulta `rcon-cli list` dentro del contenedor.
// Requiere que RCON esté habilitado; si no, devuelve known=false.
type DockerCounter struct {
	// Run ejecuta el proceso (por defecto execx.Run).
	Run func(ctx context.Context, name string, args ...string) (execx.Result, error)
	// Container devuelve el nombre del contenedor de una instancia.
	Container func(instance string) string
}

// Online implementa Counter con `docker exec … rcon-cli list`.
func (c *DockerCounter) Online(ctx context.Context, instance string) (int, bool, error) {
	run := c.Run
	if run == nil {
		run = execx.Run
	}
	if c.Container == nil {
		return 0, false, nil
	}
	res, err := run(ctx, "docker", "exec", c.Container(instance), "rcon-cli", "list")
	if err != nil {
		return 0, false, err
	}
	m := rconListRe.FindStringSubmatch(res.Stdout)
	if m == nil {
		return 0, false, nil
	}
	n, convErr := strconv.Atoi(m[1])
	if convErr != nil {
		return 0, false, nil
	}
	return n, true, nil
}

// ChainCounter prueba los contadores en orden y usa el primero que conozca la
// respuesta.
type ChainCounter []Counter

// Online implementa Counter probando cada contador en orden.
func (c ChainCounter) Online(ctx context.Context, instance string) (int, bool, error) {
	var lastErr error
	for _, counter := range c {
		n, known, err := counter.Online(ctx, instance)
		if known {
			return n, true, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return 0, false, lastErr
}
