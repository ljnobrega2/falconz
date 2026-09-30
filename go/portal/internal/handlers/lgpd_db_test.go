// Testes de INTEGRAÇÃO (Postgres real) que TRAVAM os caminhos 200 dos fixes LGPD
// que os testes herméticos (lgpd_rbac_test.go, Pool=nil) deliberadamente NÃO cobrem
// — os que SÓ existem contra o banco:
//
//	P1 LGPD — Consent/Revoke (AccountConsentRevoke): o UPDATE revoked_at=NOW()
//	          na linha ATIVA + a idempotência (WHERE revoked_at IS NULL) + o 404
//	          quando não há consentimento. Provas que exigem ler/escrever
//	          senderzz_consents — um teste puro não exercita nada disso.
//	P0 LGPD — DataRequest (canal público do titular): o INSERT em
//	          senderzz_data_subject_requests (1 linha, status default 'recebido').
//
// Gate: pulam (t.Skip) quando DATABASE_URL não está setada — mesma convenção do
// go/wallet (integration_db_test.go). Com DATABASE_URL exportada, RODAM de verdade.
//
// Isolamento (sem tocar dados REAIS): user_id/email sintéticos de namespace ALTO e
// único; cada teste limpa o que criou em t.Cleanup; SEM t.Parallel. As tabelas
// senderzz_consents / senderzz_data_subject_requests NÃO têm FK p/ portal_users no
// schema, então um user_id sintético sem cadastro insere normalmente (e o e-mail
// sintético não casa nenhum usuário → user_id resolvido como NULL, como o schema
// prevê para o titular não-cadastrado).
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
)

// lgpdTestUserBase — namespace de user_id sintético, alto e improvável em dados
// reais (não colide com titulares de produção/dev).
const lgpdTestUserBase int64 = 990000000

// requireLGPDDB abre um pool contra DATABASE_URL ou pula o teste se ausente.
// Falha (não pula) se a URL existe mas a conexão não sobe — ambiente quebrado
// deve ser visível, não silenciosamente verde.
func requireLGPDDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL não definida — pulando teste de integração LGPD DB")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("falha ao criar pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("DATABASE_URL definida mas banco inacessível: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// authReqCtx monta um POST /x com corpo JSON e PortalUser (role + ID) no contexto,
// simulando o pós-AuthPortalJWT. ID load-bearing: AccountConsentRevoke escopa o
// UPDATE por u.ID. (authReq de lgpd_rbac_test.go fixa ID=10; aqui precisamos do ID
// sintético, por isso a variante.)
func authReqCtx(id int64, role, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	u := &auth.PortalUser{ID: id, WPUserID: id, Email: "syn@lgpd.test", Role: role}
	return req.WithContext(auth.ContextWithUser(context.Background(), u))
}

// ── P1 LGPD — Consent/Revoke contra o banco ───────────────────────────────────

// TestConsentRevokeDBActiveAndIdempotent: titular com consentimento ATIVO →
// 200 e revoked_at PREENCHIDO na linha (revogar = setar revoked_at, nunca deletar).
// Revogar de novo a MESMA versão → 200 idempotente com o MESMO revoked_at (não
// re-escreve a data — prova do WHERE revoked_at IS NULL). // SEC-RBAC / Art. 8º §5º
func TestConsentRevokeDBActiveAndIdempotent(t *testing.T) {
	pool := requireLGPDDB(t)
	ctx := context.Background()
	uid := lgpdTestUserBase + 1
	docType, docVersion := "privacy", "lgpd-test-v1"

	// fixture: consentimento ATIVO (revoked_at NULL) do titular sintético.
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM senderzz_consents WHERE user_id = $1`, uid)
	})
	_, _ = pool.Exec(ctx, `DELETE FROM senderzz_consents WHERE user_id = $1`, uid)
	if _, err := pool.Exec(ctx,
		`INSERT INTO senderzz_consents (user_id, doc_type, doc_version) VALUES ($1, $2, $3)`,
		uid, docType, docVersion,
	); err != nil {
		t.Fatalf("falha ao inserir consentimento de fixture: %v", err)
	}

	h := &SettingsHandler{Pool: pool}
	body := `{"doc_type":"` + docType + `","doc_version":"` + docVersion + `"}`

	// 1ª revogação → 200 e revoked_at preenchido no banco.
	rec := httptest.NewRecorder()
	h.AccountConsentRevoke(rec, authReqCtx(uid, "produtor", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("AccountConsentRevoke(ativo) = %d, esperado 200; body=%s", rec.Code, rec.Body.String())
	}
	var revokedAt *string
	if err := pool.QueryRow(ctx,
		`SELECT revoked_at::text FROM senderzz_consents WHERE user_id = $1 AND doc_type = $2 AND doc_version = $3`,
		uid, docType, docVersion,
	).Scan(&revokedAt); err != nil {
		t.Fatalf("falha ao reler consentimento: %v", err)
	}
	if revokedAt == nil || *revokedAt == "" {
		t.Fatalf("revoked_at NÃO foi preenchido após revogação — o fix não escreveu a revogação")
	}
	first := *revokedAt

	// 2ª revogação (mesma versão) → 200 idempotente, MESMO revoked_at no banco.
	rec2 := httptest.NewRecorder()
	h.AccountConsentRevoke(rec2, authReqCtx(uid, "produtor", body))
	if rec2.Code != http.StatusOK {
		t.Fatalf("AccountConsentRevoke(já revogado) = %d, esperado 200 idempotente; body=%s", rec2.Code, rec2.Body.String())
	}
	var revokedAt2 *string
	if err := pool.QueryRow(ctx,
		`SELECT revoked_at::text FROM senderzz_consents WHERE user_id = $1 AND doc_type = $2 AND doc_version = $3`,
		uid, docType, docVersion,
	).Scan(&revokedAt2); err != nil {
		t.Fatalf("falha ao reler consentimento (2ª): %v", err)
	}
	if revokedAt2 == nil || *revokedAt2 != first {
		t.Fatalf("revoked_at mudou na 2ª revogação (%v != %q) — idempotência quebrada (WHERE revoked_at IS NULL)", revokedAt2, first)
	}
}

// TestConsentRevokeDBNotFound: titular SEM consentimento daquele (doc_type,
// doc_version) → 404 (nada a revogar). Confirma o comportamento REAL do handler
// (404, não "200 nada a revogar"). // Art. 8º §5º
func TestConsentRevokeDBNotFound(t *testing.T) {
	pool := requireLGPDDB(t)
	ctx := context.Background()
	uid := lgpdTestUserBase + 2

	// Garante ausência total de consentimento p/ o titular sintético.
	_, _ = pool.Exec(ctx, `DELETE FROM senderzz_consents WHERE user_id = $1`, uid)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM senderzz_consents WHERE user_id = $1`, uid)
	})

	h := &SettingsHandler{Pool: pool}
	rec := httptest.NewRecorder()
	h.AccountConsentRevoke(rec, authReqCtx(uid, "produtor", `{"doc_type":"privacy","doc_version":"inexistente-v9"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("AccountConsentRevoke(sem consentimento) = %d, esperado 404; body=%s", rec.Code, rec.Body.String())
	}
}

// ── P0 LGPD — DataRequest (canal público) contra o banco ──────────────────────

// TestDataRequestDBInsertsRow: payload VÁLIDO (sem auth) → 200 e EXATAMENTE 1 linha
// em senderzz_data_subject_requests, com status default 'recebido'. Trava a regressão
// do canal do titular (Art. 18): se o INSERT sumisse/virasse no-op, o titular
// não-cadastrado ficaria sem via de exercício. // P0 LGPD-no-data-subject-channel
func TestDataRequestDBInsertsRow(t *testing.T) {
	pool := requireLGPDDB(t)
	ctx := context.Background()
	// E-mail sintético que NÃO casa nenhum portal_user → user_id resolvido NULL
	// (como o schema prevê para titular não-cadastrado).
	synEmail := "titular-lgpd-test-990000003@lgpd.test"

	_, _ = pool.Exec(ctx, `DELETE FROM senderzz_data_subject_requests WHERE email = $1`, synEmail)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM senderzz_data_subject_requests WHERE email = $1`, synEmail)
	})

	h := &DataRequestHandler{Pool: pool}
	body := `{"email":"` + synEmail + `","request_type":"acesso","reason":"teste de regressão LGPD"}`
	req := httptest.NewRequest(http.MethodPost, "/portal/data-request", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.DataRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DataRequest(válido, público) = %d, esperado 200; body=%s", rec.Code, rec.Body.String())
	}

	var count int
	var status, reqType string
	if err := pool.QueryRow(ctx,
		`SELECT count(*), coalesce(max(status),''), coalesce(max(request_type),'')
		   FROM senderzz_data_subject_requests WHERE email = $1`,
		synEmail,
	).Scan(&count, &status, &reqType); err != nil {
		t.Fatalf("falha ao contar pedidos do titular: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperado EXATAMENTE 1 linha em senderzz_data_subject_requests p/ %s, encontrado %d", synEmail, count)
	}
	if status != "recebido" {
		t.Errorf("status da linha = %q, esperado 'recebido' (default do schema)", status)
	}
	if reqType != "acesso" {
		t.Errorf("request_type da linha = %q, esperado 'acesso'", reqType)
	}
}
