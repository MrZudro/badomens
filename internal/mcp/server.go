// Package mcp implementa el servidor MCP de badomens sobre el transporte stdio.
// Expone el mismo registro único de herramientas que el núcleo, con un modo
// --read-only que oculta las que modifican estado.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/tools"
	"github.com/MrZudro/badomens/internal/version"
)

// ProtocolVersion es la versión de MCP anunciada.
const ProtocolVersion = "2024-11-05"

// Server es el servidor MCP.
type Server struct {
	// Registry es el registro único de herramientas.
	Registry *tools.Registry
	// ReadOnly oculta las herramientas que modifican estado.
	ReadOnly bool
	// Name y Version identifican el servidor.
	Name    string
	Version string
}

// New crea el servidor MCP.
func New(registry *tools.Registry, readOnly bool) *Server {
	return &Server{Registry: registry, ReadOnly: readOnly, Name: "badomens", Version: version.Version}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve procesa mensajes JSON-RPC delimitados por líneas desde in y escribe las
// respuestas en out hasta que el contexto se cancele o se cierre la entrada.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var mu sync.Mutex
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			s.write(&mu, out, response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		resp := s.dispatch(ctx, &req)
		s.write(&mu, out, resp)
	}
	return scanner.Err()
}

func (s *Server) write(mu *sync.Mutex, out io.Writer, resp response) {
	resp.JSONRPC = "2.0"
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	_, _ = out.Write(append(raw, '\n'))
}

func (s *Server) dispatch(ctx context.Context, req *request) response {
	switch req.Method {
	case "initialize":
		return response{ID: req.ID, Result: map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}}
	case "ping":
		return response{ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return response{ID: req.ID, Result: map[string]any{"tools": s.toolList()}}
	case "tools/call":
		return s.toolCall(ctx, req)
	default:
		return response{ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}}
	}
}

func (s *Server) toolList() []map[string]any {
	list := s.Registry.ListForReadOnly(s.ReadOnly)
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.JSONSchema(),
		})
	}
	return out
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) toolCall(ctx context.Context, req *request) response {
	var p callParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return response{ID: req.ID, Error: &rpcError{Code: -32602, Message: "params inválidos"}}
	}
	tool, ok := s.Registry.Get(p.Name)
	if !ok || (s.ReadOnly && !tool.ReadOnly) {
		return response{ID: req.ID, Error: &rpcError{Code: -32602, Message: "herramienta no disponible: " + p.Name}}
	}
	result, err := s.Registry.Invoke(ctx, p.Name, p.Arguments)
	if err != nil {
		code, message, hint := errx.From(err)
		text := message
		if hint != "" {
			text += " (" + hint + ")"
		}
		return response{ID: req.ID, Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": true,
			"_meta":   map[string]any{"code": code},
		}}
	}
	content := []map[string]any{{"type": "text", "text": result.Text}}
	return response{ID: req.ID, Result: map[string]any{
		"content":           content,
		"structuredContent": result.Data,
		"isError":           false,
	}}
}

// DescribeTool es un auxiliar para documentación/pruebas.
func (s *Server) DescribeTool(name string) (string, error) {
	t, ok := s.Registry.Get(name)
	if !ok {
		return "", fmt.Errorf("herramienta %q no encontrada", name)
	}
	raw, _ := json.Marshal(t.JSONSchema())
	return string(raw), nil
}
