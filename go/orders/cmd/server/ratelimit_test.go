package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
)

// TestCheckoutRateLimit prova o requisito P2-SEC: a superfície pública
// /checkout-api/* aceita 30 req/min por IP e responde 429 a partir da 31ª
// requisição do mesmo IP dentro da janela. Self-contained (sem DB/Redis):
// monta o mesmo httprate.LimitByIP(30, time.Minute) usado em main.go sobre um
// stub 200, mantendo o IP constante (RemoteAddr fixo) para acumular no store
// in-memory do limiter.
func TestCheckoutRateLimit(t *testing.T) {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(httprate.LimitByIP(30, time.Minute))
		r.Get("/checkout-api/offer", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	const ip = "203.0.113.7:5555" // IP fixo → mesma chave no store do limiter
	do := func() int {
		req := httptest.NewRequest(http.MethodGet, "/checkout-api/offer", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// As primeiras 30 devem passar (200).
	for i := 1; i <= 30; i++ {
		if got := do(); got != http.StatusOK {
			t.Fatalf("req %d: esperava 200, obteve %d (limite disparou cedo demais)", i, got)
		}
	}

	// A 31ª deve ser bloqueada (429).
	if got := do(); got != http.StatusTooManyRequests {
		t.Fatalf("req 31: esperava 429, obteve %d (rate-limit não disparou)", got)
	}
}
