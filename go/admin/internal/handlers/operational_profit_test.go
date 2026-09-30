package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestOperationalProfitDates(t *testing.T) {
	t.Run("aceita intervalo valido", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/?date_from=2026-08-01&date_to=2026-08-16", nil)
		from, to, ok := operationalProfitDates(r)
		if !ok || from != "2026-08-01" || to != "2026-08-16" {
			t.Fatalf("intervalo = %q..%q ok=%v", from, to, ok)
		}
	})

	t.Run("rejeita data invalida", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/?date_from=2026-02-30&date_to=2026-08-16", nil)
		_, _, ok := operationalProfitDates(r)
		if ok {
			t.Fatal("data inexistente foi aceita")
		}
	})

	t.Run("rejeita periodo invertido", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/?date_from=2026-08-17&date_to=2026-08-16", nil)
		_, _, ok := operationalProfitDates(r)
		if ok {
			t.Fatal("periodo invertido foi aceito")
		}
	})
}
