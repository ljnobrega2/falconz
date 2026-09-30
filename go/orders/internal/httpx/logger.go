// logger.go — logger estruturado com request_id propagado via context.
//
// OBSERVABILIDADE: o slogMiddleware (cmd/server/main.go) já loga a linha de acesso
// `[http]` com request_id. Mas as linhas de NEGÓCIO dos handlers (`[checkout] …`,
// `[payments] …`) usavam slog.Default() direto — sem request_id —, então não dava
// para correlacionar um log de pagamento/checkout com a requisição que o originou.
//
// Solução (mínima e sem mudar negócio): o middleware injeta no context um
// *slog.Logger já decorado com "request_id" via WithLogger(); os handlers obtêm esse
// logger com LoggerFrom(ctx) e logam normalmente. Quando NÃO há logger no context
// (ex.: testes httptest sem o middleware), LoggerFrom devolve slog.Default() — os
// handlers continuam funcionando, apenas sem o campo request_id. Fail-soft.
package httpx

import (
	"context"
	"log/slog"
)

// loggerCtxKey é a chave (não exportada, tipo privado) sob a qual o *slog.Logger
// decorado é guardado no context — evita colisão com outras chaves de context.
type loggerCtxKey struct{}

// WithLogger devolve um novo context carregando o logger fornecido.
// Chamado uma vez por requisição pelo slogMiddleware, com o logger já decorado
// com request_id (e quaisquer outros campos comuns).
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerCtxKey{}, l)
}

// LoggerFrom recupera o *slog.Logger do context (decorado com request_id pelo
// middleware). Se não houver — caso de testes sem o middleware HTTP — devolve
// slog.Default(), de modo que os handlers NUNCA quebram por ausência de logger.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerCtxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
