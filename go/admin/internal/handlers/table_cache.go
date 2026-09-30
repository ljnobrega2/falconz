package handlers

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
//
// Cache em nível de processo para o resultado de tableExists. O schema do banco
// NÃO muda em runtime, então o resultado de cada lookup no information_schema é
// estável durante toda a vida do processo. Antes desta mudança, cada handler
// definia seu próprio tableExists que executava uma query no
// information_schema.tables a CADA chamada (handlers como TpcClientes chamam
// ~16 vezes por request) — puro desperdício de round-trips ao banco.
//
// tableExistsCache é compartilhado por TODOS os handlers (sync.Map é
// concorrente-seguro). Chave = nome da tabela (string), valor = bool.
var tableExistsCache sync.Map // map[string]bool

// tableExistsCached consulta o cache; em hit retorna direto, em miss executa a
// MESMA query do information_schema que os métodos tableExists originais usavam,
// grava no cache e retorna.
//
// Semântica preservada: o cache só é gravado quando o Scan tem sucesso (err==nil).
// Em erro transitório de banco a função retorna false SEM cachear — exatamente
// como o corpo antigo (`_ = ...Scan(&ok)`), que descartava o erro e retornava o
// zero-value. Assim, um blip de conexão não marca permanentemente uma tabela
// existente como inexistente para o resto da vida do processo.
func tableExistsCached(ctx context.Context, pool *pgxpool.Pool, name string) bool {
	if v, ok := tableExistsCache.Load(name); ok {
		return v.(bool)
	}
	var ok bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok); err == nil {
		tableExistsCache.Store(name, ok)
	}
	return ok
}
