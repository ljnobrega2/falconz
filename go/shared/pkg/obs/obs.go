// Package obs — fundação de observabilidade compartilhada entre os serviços Go.
//
// # CODE-OBS-01
//
// # Problema
//
// Hoje CADA serviço (admin/affiliates/labels/motoboy/orders/portal/wallet)
// reimplementa, com pequenas divergências, os mesmos quatro blocos:
//
//   - propagação de request_id (uns usam chi middleware.RequestID, outros nada);
//   - setup do slog JSON (níveis e flags diferentes por main.go);
//   - probe de readiness (/readyz) que faz pool.Ping (uns com timeout, outros sem);
//   - rede de proteção contra panic (uns com chimw.Recoverer texto, outros com
//     recoverMiddleware próprio).
//
// Esta divergência já causou um bug latente: misturar obs.RequestID com a função
// privada chimw.GetReqID compila limpo e silenciosamente loga request_id VAZIO,
// porque a chave de contexto do chi é privada e inacessível de fora.
//
// # Solução
//
// pkg/obs centraliza os quatro blocos como funções/middlewares PUROS:
//
//	obs.RequestID        middleware net/http (compatível com chi r.Use)
//	obs.GetRequestID     leitor do request_id no context.Context
//	obs.Recover          middleware panic → 500 (envelope httpx) + log com stack
//	obs.Readyz           http.HandlerFunc de readiness sobre um Pinger
//	obs.NewLogger        *slog.Logger JSON estruturado padronizado
//	obs.SetupDefault     atalho que aplica NewLogger como slog.Default
//
// SEM acoplamento a framework: tudo opera sobre net/http stdlib e é plugável no
// chi (cujos middlewares têm a assinatura func(http.Handler) http.Handler).
// SEM novas dependências: request_id usa crypto/rand; Readyz recebe um Pinger
// (interface mínima) para não acoplar pkg/obs ao pgx.
//
// REGRA DE ADOÇÃO: um serviço que troca para obs.RequestID DEVE trocar TODO
// chimw.GetReqID por obs.GetRequestID — meia-migração loga request_id vazio.
package obs
