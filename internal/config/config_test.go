package config

import "testing"

func TestNormalize(t *testing.T) {
	c := &Config{MaxParallel: 10, MaxInstances: 5}
	c.normalize()
	if c.MaxParallel != 5 {
		t.Fatalf("max_parallel debe recortarse a max_instances, got %d", c.MaxParallel)
	}
	if c.Image == "" || c.DefaultMemory == "" || c.Repo == "" {
		t.Fatal("normalize debe rellenar los valores por defecto")
	}
}

func TestParsePositiveInt(t *testing.T) {
	if _, err := parsePositiveInt("0"); err == nil {
		t.Fatal("0 no es un entero positivo válido")
	}
	if n, err := parsePositiveInt("3"); err != nil || n != 3 {
		t.Fatalf("parsePositiveInt(3) = %d, %v", n, err)
	}
}
