package events

import (
	"testing"
	"time"
)

func TestPubSubEntrega(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(4)
	defer cancel()

	b.Publish(Event{Kind: PlayerJoined, Instance: "alpha", Data: map[string]any{"player": "ana"}})

	select {
	case e := <-ch:
		if e.Kind != PlayerJoined || e.Instance != "alpha" {
			t.Fatalf("evento inesperado: %+v", e)
		}
		if e.At.IsZero() {
			t.Fatal("Publish debe sellar la hora")
		}
	case <-time.After(time.Second):
		t.Fatal("no llegó el evento")
	}
}

func TestBajaCierraCanal(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(1)
	cancel()
	cancel() // idempotente
	if _, ok := <-ch; ok {
		t.Fatal("el canal debe quedar cerrado")
	}
	if b.Subscribers() != 0 {
		t.Fatalf("suscriptores: quiero 0, tengo %d", b.Subscribers())
	}
}

func TestSuscriptorLlenoNoBloquea(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(1)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(Event{Kind: AvailableKind})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish se bloqueó con un suscriptor lleno")
	}
	// Solo cabe (y se conserva) el primer evento.
	if got := <-ch; got.Kind != AvailableKind {
		t.Fatalf("primer evento inesperado: %+v", got)
	}
	select {
	case e := <-ch:
		t.Fatalf("no debe haber más eventos en cola: %+v", e)
	default:
	}
}

func TestCloseCierraTodos(t *testing.T) {
	b := New()
	ch1, _ := b.Subscribe(1)
	ch2, _ := b.Subscribe(1)
	b.Close()
	b.Close()
	for i, ch := range []<-chan Event{ch1, ch2} {
		if _, ok := <-ch; ok {
			t.Fatalf("canal %d debe estar cerrado", i)
		}
	}
}

// AvailableKind es un tipo de evento cualquiera para las pruebas.
const AvailableKind Kind = "test.event"
