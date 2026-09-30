// Testes unitários do pacote auth — emissão/validação de JWT admin + helpers PUROS.
//
// Estilo seguindo go/affiliates/internal/handlers/affiliates_test.go: tabela de
// casos, vetores claros, comentários em PT-BR. NÃO toca Postgres: o Middleware só
// chega ao pool.QueryRow num token TOTALMENTE válido — todas as rejeições (token
// ausente, assinatura errada, issuer errado, sem exp, alg != HS256) retornam 401
// ANTES da consulta ao DB. Por isso os caminhos negativos usam Middleware(nil) com
// segurança (o nil nunca é desreferenciado nesses fluxos), exercitando exatamente
// as guardas SEC-GO-04 (iss=senderzz-admin) e SEC-GO-09 (exp obrigatório) que SÃO
// código deste pacote — não da biblioteca jwt.
//
// O caminho POSITIVO não passa pelo Middleware (que faria nil-panic no QueryRow):
// usa round-trip IssueToken → ParseWithClaims com as MESMAS opções de validação.
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ─── secret() — precedência ADMIN_JWT_SECRET > JWT_SECRET ────────────────────

// TestSecretPrecedence trava a regra: ADMIN_JWT_SECRET vence; só cai pro
// JWT_SECRET compartilhado quando o específico está vazio.
func TestSecretPrecedence(t *testing.T) {
	t.Run("ADMIN_JWT_SECRET vence quando setado", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "compartilhado")
		t.Setenv("ADMIN_JWT_SECRET", "especifico-admin")
		if got := string(secret()); got != "especifico-admin" {
			t.Errorf("secret() = %q, esperado %q (ADMIN_JWT_SECRET tem precedência)", got, "especifico-admin")
		}
	})

	t.Run("fallback para JWT_SECRET quando ADMIN_JWT_SECRET vazio", func(t *testing.T) {
		t.Setenv("ADMIN_JWT_SECRET", "")
		t.Setenv("JWT_SECRET", "compartilhado")
		if got := string(secret()); got != "compartilhado" {
			t.Errorf("secret() = %q, esperado %q (fallback)", got, "compartilhado")
		}
	})
}

// ─── IssueToken — round-trip + claims canônicos ──────────────────────────────

// parseAdminToken reproduz a validação EXATA do Middleware (HS256-only, iss
// obrigatório, exp obrigatório) sem tocar no Postgres. É o que testa as guardas
// SEC-GO-04 / SEC-GO-09 no caminho positivo.
func parseAdminToken(tokStr string, sec []byte) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return sec, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer("senderzz-admin"),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	return tok.Claims.(*Claims), nil
}

// TestIssueTokenRoundTrip emite um token e valida que ele sobrevive ao parse com
// as mesmas opções do Middleware, com claims corretos (sub/email/iss/exp ~12h).
func TestIssueTokenRoundTrip(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "segredo-de-teste-32-bytes-aaaaaa")
	a := Admin{ID: 42, Email: "admin@senderzz.local", Nome: "Admin Teste"}

	tokStr, err := IssueToken(a)
	if err != nil {
		t.Fatalf("IssueToken: erro inesperado: %v", err)
	}
	if tokStr == "" {
		t.Fatal("IssueToken devolveu string vazia")
	}

	cl, err := parseAdminToken(tokStr, secret())
	if err != nil {
		t.Fatalf("token emitido NÃO passou na validação canônica: %v", err)
	}
	if cl.AdminID != 42 {
		t.Errorf("claim sub = %d, esperado 42", cl.AdminID)
	}
	if cl.Email != "admin@senderzz.local" {
		t.Errorf("claim email = %q, esperado admin@senderzz.local", cl.Email)
	}
	if cl.Issuer != "senderzz-admin" {
		t.Errorf("claim iss = %q, esperado senderzz-admin (SEC-GO-04)", cl.Issuer)
	}
	if cl.ExpiresAt == nil {
		t.Fatal("claim exp ausente — token sem expiração violaria SEC-GO-09")
	}
	// Validade ~12h (tolerância de 1min pro tempo decorrido entre issue e assert).
	dur := time.Until(cl.ExpiresAt.Time)
	if dur < 12*time.Hour-time.Minute || dur > 12*time.Hour {
		t.Errorf("validade do token = %v, esperado ~12h", dur)
	}
}

// TestIssueTokenWrongSecretRejected garante que um token emitido com um segredo é
// rejeitado ao validar com outro (fecha cross-secret/spoof).
func TestIssueTokenWrongSecretRejected(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "segredo-emissor")
	tokStr, err := IssueToken(Admin{ID: 1, Email: "x@y.z"})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if _, err := parseAdminToken(tokStr, []byte("OUTRO-segredo")); err == nil {
		t.Error("token validou com segredo ERRADO — assinatura não conferida")
	}
}

// ─── Middleware — caminhos negativos (401 sem tocar DB) ──────────────────────

// flagHandler é o "next" do middleware: marca que foi chamado. Se o middleware
// barrar corretamente, called fica false.
type flagHandler struct{ called bool }

func (f *flagHandler) ServeHTTP(http.ResponseWriter, *http.Request) { f.called = true }

// runMiddleware aplica o Middleware (com pool nil — seguro nos caminhos de
// rejeição, que retornam antes do QueryRow) sobre uma request com o header dado.
func runMiddleware(t *testing.T, authHeader string) (int, bool) {
	t.Helper()
	next := &flagHandler{}
	h := Middleware(nil)(next)
	r := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code, next.called
}

// signWith emite um JWT arbitrário (alg + claims controlados) p/ os casos negativos.
func signWith(t *testing.T, method jwt.SigningMethod, claims jwt.Claims, key any) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("assinatura de fixture falhou: %v", err)
	}
	return s
}

// TestMiddlewareRejectsMissingBearer — sem header ou sem prefixo "Bearer " → 401.
func TestMiddlewareRejectsMissingBearer(t *testing.T) {
	cases := map[string]string{
		"header ausente":         "",
		"sem prefixo Bearer":     "abc.def.ghi",
		"Basic em vez de Bearer": "Basic dXNlcjpwYXNz",
		"só a palavra Bearer":    "Bearer ",
	}
	for name, hdr := range cases {
		t.Run(name, func(t *testing.T) {
			code, called := runMiddleware(t, hdr)
			if code != http.StatusUnauthorized {
				t.Errorf("status = %d, esperado 401", code)
			}
			if called {
				t.Error("next foi chamado — request deveria ter sido barrada")
			}
		})
	}
}

// TestMiddlewareRejectsBadSignature — token bem-formado mas assinado com outro
// segredo é rejeitado (401) sem chegar ao DB.
func TestMiddlewareRejectsBadSignature(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "segredo-do-servidor")
	cl := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "senderzz-admin",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	bad := signWith(t, jwt.SigningMethodHS256, cl, []byte("segredo-do-atacante"))

	code, called := runMiddleware(t, "Bearer "+bad)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, esperado 401 (assinatura inválida)", code)
	}
	if called {
		t.Error("next foi chamado com assinatura inválida")
	}
}

// TestMiddlewareRejectsWrongIssuer — SEC-GO-04: token de outro serviço
// (iss=senderzz-portal / vazio) é rejeitado mesmo com assinatura válida.
func TestMiddlewareRejectsWrongIssuer(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "segredo-compartilhado")
	for _, iss := range []string{"senderzz-portal", "senderzz-wallet", ""} {
		t.Run("iss="+iss, func(t *testing.T) {
			cl := Claims{
				AdminID: 1,
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    iss,
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				},
			}
			tokStr := signWith(t, jwt.SigningMethodHS256, cl, secret())
			code, called := runMiddleware(t, "Bearer "+tokStr)
			if code != http.StatusUnauthorized {
				t.Errorf("status = %d, esperado 401 (iss=%q rejeitado por SEC-GO-04)", code, iss)
			}
			if called {
				t.Errorf("next foi chamado com iss=%q — privesc entre serviços", iss)
			}
		})
	}
}

// TestMiddlewareRejectsNoExpiration — SEC-GO-09: token sem exp não é eterno → 401.
func TestMiddlewareRejectsNoExpiration(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "segredo-compartilhado")
	cl := Claims{
		AdminID: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "senderzz-admin",
			// SEM ExpiresAt — deve ser barrado por WithExpirationRequired.
		},
	}
	tokStr := signWith(t, jwt.SigningMethodHS256, cl, secret())
	code, called := runMiddleware(t, "Bearer "+tokStr)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, esperado 401 (sem exp viola SEC-GO-09)", code)
	}
	if called {
		t.Error("next foi chamado com token sem expiração")
	}
}

// TestMiddlewareRejectsExpiredToken — token já expirado → 401.
func TestMiddlewareRejectsExpiredToken(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "segredo-compartilhado")
	cl := Claims{
		AdminID: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "senderzz-admin",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // já venceu
		},
	}
	tokStr := signWith(t, jwt.SigningMethodHS256, cl, secret())
	code, called := runMiddleware(t, "Bearer "+tokStr)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, esperado 401 (token expirado)", code)
	}
	if called {
		t.Error("next foi chamado com token expirado")
	}
}

// TestMiddlewareRejectsNoneAlg — alg=none (token sem assinatura) → 401.
// Guarda clássica contra o bypass "alg:none".
func TestMiddlewareRejectsNoneAlg(t *testing.T) {
	t.Setenv("ADMIN_JWT_SECRET", "segredo-compartilhado")
	cl := Claims{
		AdminID: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "senderzz-admin",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	// jwt exige UnsafeAllowNoneSignatureType como key para emitir alg=none.
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, cl)
	tokStr, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("não foi possível emitir fixture alg=none: %v", err)
	}
	code, called := runMiddleware(t, "Bearer "+tokStr)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, esperado 401 (alg=none deve ser rejeitado)", code)
	}
	if called {
		t.Error("next foi chamado com alg=none — bypass de assinatura")
	}
}

// ─── loadLoginRateMax — env parse + default seguro ───────────────────────────

// TestLoadLoginRateMax cobre a leitura de ADMIN_LOGIN_RATE_MAX.
// Regra atual: ausência/valor inválido ⇒ limite efetivamente desativado;
// configuração explícita positiva continua sendo respeitada.
func TestLoadLoginRateMax(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want int
	}{
		{name: "não setado → limite desativado", set: false, want: 1 << 30},
		{name: "vazio → limite desativado", set: true, val: "", want: 1 << 30},
		{name: "valor de DEV 2000", set: true, val: "2000", want: 2000},
		{name: "1 é positivo e aceito", set: true, val: "1", want: 1},
		{name: "zero rejeitado → limite desativado", set: true, val: "0", want: 1 << 30},
		{name: "negativo rejeitado → limite desativado", set: true, val: "-3", want: 1 << 30},
		{name: "não numérico → limite desativado", set: true, val: "abc", want: 1 << 30},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.set {
				t.Setenv("ADMIN_LOGIN_RATE_MAX", c.val)
			} else {
				// Garante ambiente limpo dentro deste subteste.
				t.Setenv("ADMIN_LOGIN_RATE_MAX", "")
			}
			if got := loadLoginRateMax(); got != c.want {
				t.Errorf("loadLoginRateMax() = %d, esperado %d", got, c.want)
			}
		})
	}
}

// ─── clientIP — extração defensiva de host ───────────────────────────────────

// TestClientIP cobre o stripper de porta de r.RemoteAddr.
func TestClientIP(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7:54321": "203.0.113.7", // IPv4 host:port
		"203.0.113.7":       "203.0.113.7", // sem porta (RealIP normalizado)
		"[2001:db8::1]:443": "2001:db8::1", // IPv6 com porta
		"":                  "",            // vazio → vazio
	}
	for remote, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		if got := clientIP(r); got != want {
			t.Errorf("clientIP(%q) = %q, esperado %q", remote, got, want)
		}
	}
}

// ─── FromCtx — recupera Admin do contexto ────────────────────────────────────

// TestFromCtx cobre o getter de Admin do ctx (nil quando ausente).
func TestFromCtx(t *testing.T) {
	// Sem Admin no contexto → nil.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := FromCtx(r.Context()); got != nil {
		t.Errorf("FromCtx(ctx sem admin) = %v, esperado nil", got)
	}
}
