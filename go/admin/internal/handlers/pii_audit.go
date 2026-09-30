// Package handlers — accountability LGPD (Art. 37 / Art. 46).
//
// LGPD-PII-AUDIT: registra cada acesso a dado pessoal de titular (cliente,
// produtor, afiliado) numa trilha de auditoria. A tabela senderzz_pii_access_log
// já existe (infra/postgres/240-fixes-v472-lgpd-sku.sql). Aqui só plugamos a
// gravação nos pontos reais de leitura de PII.
//
// Princípios:
//   - best-effort: a auditoria NUNCA derruba a request do operador. Se a tabela
//     não existir ou o INSERT falhar, apenas logamos um warning e seguimos.
//   - barato: uma única checagem de existência (cacheada) + um INSERT.
package handlers

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// logPIIAccess grava uma linha de auditoria de acesso a PII.
//
// Parâmetros:
//
//	pool        — pool de conexão (helper é stateless, não vive num handler).
//	actorID     — id do admin que acessou (0 = desconhecido → NULL).
//	actorEmail  — email do admin (vazio → NULL).
//	subjectType — "customer" | "producer" | "affiliate" | "order".
//	subjectID   — id do titular/recurso acessado (0 → NULL).
//	fields      — campos PII tocados, ex.: ["nome","email","cpf"].
//	action      — "view" | "export" | "edit".
//	ip          — IP de origem (r.RemoteAddr, já normalizado pelo RealIP).
//
// Best-effort: erros são apenas logados; jamais propagados ao chamador.
func logPIIAccess(ctx context.Context, pool *pgxpool.Pool, actorID int64, actorEmail, subjectType string, subjectID int64, fields []string, action, ip string) {
	if pool == nil {
		return
	}
	if !tableExistsCached(ctx, pool, "senderzz_pii_access_log") {
		return // tabela não migrada — degradação graciosa, sem ruído.
	}

	// Colunas NULL-friendly: usamos ponteiros para gravar NULL quando o valor
	// não é conhecido (mantém a semântica do schema: actor_*/subject_id NULLáveis).
	var actorIDp *int64
	if actorID > 0 {
		actorIDp = &actorID
	}
	var actorEmailp *string
	if actorEmail != "" {
		actorEmailp = &actorEmail
	}
	var subjectIDp *int64
	if subjectID > 0 {
		subjectIDp = &subjectID
	}
	var ipp *string
	if ip != "" {
		ipp = &ip
	}
	if action == "" {
		action = "view"
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO senderzz_pii_access_log
		   (actor_user_id, actor_email, subject_type, subject_id, fields, action, ip)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		actorIDp, actorEmailp, subjectType, subjectIDp,
		strings.Join(fields, ","), action, ipp,
	)
	if err != nil {
		slog.Warn("[senderzz_lgpd] falha ao registrar acesso a PII (ignorado)",
			"err", err, "subject_type", subjectType, "subject_id", subjectID, "action", action)
	}
}
