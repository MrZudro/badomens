package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client es un cliente HTTP con reintento mínimo para las APIs de catálogos.
type Client struct {
	HTTP    *http.Client
	Timeout time.Duration
	// UserAgent identifica las peticiones.
	UserAgent string
}

// DefaultClient devuelve un cliente con valores razonables.
func DefaultClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}, UserAgent: "badomens/0"}
}

// Get lee el cuerpo de una URL. Errores 4xx/5xx se reportan como
// ErrUnavailable con el status.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no se pudo consultar %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s respondió %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
}

// GetJSON lee y decodifica JSON.
func (c *Client) GetJSON(ctx context.Context, url string, out any) error {
	raw, err := c.Get(ctx, url)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("respuesta ilegible de %s: %w", url, err)
	}
	return nil
}
