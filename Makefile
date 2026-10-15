# Makefile de badomens
# Objetivo: build, test, lint y run para desarrollo local.

BIN      := bin/badomens
PKG      := ./...
VERSION  ?= 0.0.0-dev
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -X github.com/MrZudro/badomens/internal/version.Version=$(VERSION) \
            -X github.com/MrZudro/badomens/internal/version.Commit=$(COMMIT) \
            -X github.com/MrZudro/badomens/internal/version.Date=$(DATE)

# En Windows con Smart App Control activo, algunos binarios de prueba recién
# compilados se bloquean al ser lanzados por `go test`. Quitar el DWARF
# (-ldflags=-w) cambia el hash del binario y evita el bloqueo.
TEST_FLAGS := $(shell if [ "$$(go env GOOS)" = "windows" ]; then echo "-ldflags=-w"; fi)

.PHONY: build test test-race lint lint-fix run doctor check fmt vet golden gen-versions

golden:
	go test $(TEST_FLAGS) ./internal/tui/ -update

gen-versions:
	go run ./internal/engines/gen

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/badomens

test:
	go test $(TEST_FLAGS) ./...

test-race:
	go test $(TEST_FLAGS) -race ./...
# Nota: -race requiere cgo; en Windows sin compilador C usa 'make test'.

lint:
	golangci-lint run ./...

lint-fix:
	golangci-lint run --fix ./...

# OJO: el lint (.golangci.yaml) exige gofmt Y gofumpt. `make fmt` aplica
# exactamente esos formateadores con el mismo motor que `make lint`, para no
# dejar el arbol con algo que el lint rechace despues (gofmt solo no basta).
fmt:
	golangci-lint fmt

vet:
	go vet ./...

run:
	go run -ldflags "$(LDFLAGS)" ./cmd/badomens

doctor:
	go run ./cmd/badomens doctor

check: fmt vet test-race
	echo "todo listo"
