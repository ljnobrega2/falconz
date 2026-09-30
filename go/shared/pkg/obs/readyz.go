package obs

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/senderzz/shared/pkg/httpx"
)

// Pinger é a interface mínima de readiness: "a dependência crítica responde?".
// *pgxpool.Pool a satisfaz estruturalmente (Ping(context.Context) error), então
// pkg/obs NÃO precisa importar o pgx (e foge do skew de versão v5.5.5 × v5.7.1).
//
// Atenção: *sql.DB NÃO satisfaz esta interface — seu Ping() não recebe ctx; mas
// todos os call sites de readyz dos serviços usam pgxpool, então é seguro.
type Pinger interface {
	Ping(ctx context.Context) error
}

// readyzTimeout limita o ping de readiness para que um banco travado não pendure
// o probe (e, por tabela, o readinessProbe do orquestrador).
const readyzTimeout = 2 * time.Second

// Readyz devolve o http.HandlerFunc de readiness padronizado:
//
//   - pool.Ping OK   → 200 {"ok":true,"status":"ready","service":<service>}
//   - pool.Ping erro → 503 {"ok":false,"erro":"banco inacessível"} + log warn
//
// O ping roda sob um contexto com timeout curto (readyzTimeout). service rotula
// a resposta e o log. Liveness (/health) é responsabilidade do serviço — este
// helper é SÓ readiness ("estou apto a receber tráfego?").
func Readyz(pool Pinger, service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyzTimeout)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			slog.WarnContext(ctx, "[readyz] banco inacessível",
				"service", service,
				"err", err,
				"request_id", GetRequestID(r.Context()),
			)
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ready", "service": service})
	}
}
