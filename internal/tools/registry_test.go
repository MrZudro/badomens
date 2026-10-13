package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryRegisterAndInvoke(t *testing.T) {
	reg := New()
	tool := &Tool{
		Name:        "demo",
		Description: "demo",
		ReadOnly:    true,
		Params:      []Param{{Name: "name", Type: TypeString, Description: "n", Required: true}},
		Handler: func(_ context.Context, args Args) (*Result, error) {
			n, err := args.String("name")
			if err != nil {
				return nil, err
			}
			return &Result{Text: "hola " + n, Data: map[string]any{"name": n}}, nil
		},
	}
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Register(tool); err == nil {
		t.Fatal("registrar dos veces debe fallar")
	}

	res, err := reg.Invoke(context.Background(), "demo", json.RawMessage(`{"name":"ana"}`))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if res.Text != "hola ana" {
		t.Fatalf("text = %q", res.Text)
	}
}

func TestInvokeMissingRequired(t *testing.T) {
	reg := New()
	_ = reg.Register(&Tool{
		Name: "x", Description: "x", ReadOnly: true,
		Params:  []Param{{Name: "a", Type: TypeString, Required: true}},
		Handler: func(_ context.Context, _ Args) (*Result, error) { return &Result{Text: ""}, nil },
	})
	if _, err := reg.Invoke(context.Background(), "x", json.RawMessage(`{}`)); err == nil {
		t.Fatal("debe fallar la falta de un argumento requerido")
	}
}

func TestReadOnlyFilter(t *testing.T) {
	reg := New()
	_ = reg.Register(&Tool{Name: "ro", ReadOnly: true, Handler: func(context.Context, Args) (*Result, error) { return &Result{}, nil }})
	_ = reg.Register(&Tool{Name: "rw", ReadOnly: false, Handler: func(context.Context, Args) (*Result, error) { return &Result{}, nil }})
	if got := len(reg.ListForReadOnly(true)); got != 1 {
		t.Fatalf("read-only debe dejar 1 herramienta, got %d", got)
	}
	if got := len(reg.ListForReadOnly(false)); got != 2 {
		t.Fatalf("sin read-only debe haber 2, got %d", got)
	}
}
