package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// MintJWT deve produzir um token byte-a-byte idêntico ao tpc_jwt_encode() do PHP.
// Vetor PHP (secret/sub/iat/exp fixos) gerado por:
//
//	b64u(json{"alg":"HS256","typ":"JWT"}) . b64u(json{"sub","iat","exp"}) . b64u(hmac_sha256)
//
// Como MintJWT usa time.Now() para iat/exp, validamos aqui (a) o header exato e
// (b) o round-trip com AuthJWT. A igualdade byte-a-byte com o PHP foi confirmada
// em verificação offline com iat/exp fixos (mesmo header/payload/assinatura).
func TestMintJWT_HeaderMatchesPHP(t *testing.T) {
	os.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	tok, ttl, err := MintJWT(42)
	if err != nil {
		t.Fatal(err)
	}
	if ttl != 3600 {
		t.Fatalf("ttl = %d, want 3600 (HOUR_IN_SECONDS)", ttl)
	}
	// base64url de {"alg":"HS256","typ":"JWT"} — idêntico ao tpc_base64url do PHP.
	const wantHeader = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
	if len(tok) < len(wantHeader) || tok[:len(wantHeader)] != wantHeader {
		t.Fatalf("header inesperado: %s", tok)
	}
	dots := 0
	for _, c := range tok {
		if c == '.' {
			dots++
		}
	}
	if dots != 2 {
		t.Fatalf("token deve ter 3 partes (2 pontos), got %d: %s", dots, tok)
	}
}

// Fail-closed (CRIT-05): secret curto demais → erro, nunca assina com chave fraca.
func TestMintJWT_FailsClosedOnShortSecret(t *testing.T) {
	os.Setenv("JWT_SECRET", "curto")
	if _, _, err := MintJWT(1); err == nil {
		t.Fatal("MintJWT deveria falhar com secret < 32 chars")
	}
	os.Setenv("JWT_SECRET", "")
	if _, _, err := MintJWT(1); err == nil {
		t.Fatal("MintJWT deveria falhar com secret vazio")
	}
}

// Round-trip: um token emitido por MintJWT é aceito por AuthJWT e expõe o user_id.
func TestMintJWT_RoundTripWithAuthJWT(t *testing.T) {
	os.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	tok, _, err := MintJWT(777)
	if err != nil {
		t.Fatal(err)
	}
	var gotUID int64
	h := AuthJWT(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUID = GetUserID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if gotUID != 777 {
		t.Fatalf("user_id = %d, want 777", gotUID)
	}
}
