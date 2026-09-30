// FEAT-RBAC-ORDERS-2026-06-24 — teste de INTEGRAÇÃO do escopo por papel nas telas
// de pedidos compartilhadas (DualAuth: admin OU portal token).
//
// Prova, contra um Postgres real (DATABASE_URL; SKIP se ausente), que:
//   - produtor vê SÓ os pedidos com sz_orders.produtor_id = <portalUserID>;
//   - afiliado vê SÓ os pedidos com sz_orders.affiliate_id = <wpUserID>;
//   - admin vê TUDO (superset — comportamento inalterado);
//   - detalhe (GET /orders/{id}) fora do escopo → 404 (cross-user bloqueado);
//   - o token de PORTAL é validado de fato (assinatura JWT_SECRET + sessão viva).
//
// Seed isolado com ids sintéticos altos (≥ 990000000, OVERRIDING SYSTEM VALUE) +
// t.Cleanup — não colide nem deixa resíduo na base de dev.
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
)

// ids sintéticos do cenário (não colidem com dados reais).
const (
	rbacProdutorPortalID int64 = 990001001 // senderzz_portal_users.id do produtor
	rbacProdutorWPID     int64 = 990001101 // wp_user_id do produtor
	rbacAfiliadoPortalID int64 = 990001002 // senderzz_portal_users.id do afiliado
	rbacAfiliadoWPID     int64 = 990001102 // wp_user_id do afiliado (== sz_orders.affiliate_id)

	rbacOrderProd1 int64 = 990002001 // produtor=P, affiliate=A  → ambos veem
	rbacOrderProd2 int64 = 990002002 // produtor=P, affiliate=outro → só produtor
	rbacOrderOther int64 = 990002003 // produtor=outro, affiliate=outro → ninguém (dos dois) vê

	rbacAdminID int64 = 990003001
)

// mintPortal insere um portal_user + sessão e devolve um JWT de portal (iss=senderzz-portal)
// assinado com o JWT_SECRET corrente — espelha o IssuePortalSession/IssuePortalToken.
func mintPortal(t *testing.T, pool *pgxpool.Pool, portalID, wpID int64, role, email string) string {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM senderzz_portal_sessions WHERE user_id=$1`, portalID)
	_, _ = pool.Exec(ctx, `DELETE FROM senderzz_portal_users WHERE id=$1`, portalID)
	// status (active/inactive) e name são colunas GERADAS (de ativo/nome) — não inserir.
	if _, err := pool.Exec(ctx,
		`INSERT INTO senderzz_portal_users (id, wp_user_id, email, nome, role, ativo)
		 OVERRIDING SYSTEM VALUE
		 VALUES ($1,$2,$3,'Teste',$4,TRUE)`,
		portalID, wpID, email, role); err != nil {
		t.Fatalf("mintPortal user: %v", err)
	}
	// token raw + hmac (igual ao go/portal / portal_login.go).
	raw := "rbactok" + hex.EncodeToString([]byte(email))[:24]
	mac := hmac.New(sha256.New, []byte(os.Getenv("WP_SALT_AUTH")))
	mac.Write([]byte(raw))
	tokHMAC := hex.EncodeToString(mac.Sum(nil))
	if _, err := pool.Exec(ctx,
		`INSERT INTO senderzz_portal_sessions (user_id, token, token_hmac, expires_at)
		 VALUES ($1,$2,$3, NOW() + INTERVAL '1 hour')`,
		portalID, raw, tokHMAC); err != nil {
		t.Fatalf("mintPortal session: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM senderzz_portal_sessions WHERE user_id=$1`, portalID)
		_, _ = pool.Exec(c, `DELETE FROM senderzz_portal_users WHERE id=$1`, portalID)
	})
	jwt, err := auth.IssuePortalToken(portalID, email, role, raw)
	if err != nil {
		t.Fatalf("mintPortal IssuePortalToken: %v", err)
	}
	return jwt
}

// seedOrder insere sz_orders + sz_motoboy_pedidos para um cenário de escopo.
func seedOrder(t *testing.T, pool *pgxpool.Pool, orderID, produtorID, affiliateID int64) {
	t.Helper()
	ctx := context.Background()
	wcOrderID := orderID // wp_order_id arbitrário, distinto por pedido.
	_, _ = pool.Exec(ctx, `DELETE FROM sz_motoboy_pedidos WHERE id=$1`, orderID)
	_, _ = pool.Exec(ctx, `DELETE FROM sz_orders WHERE id=$1`, orderID)
	if _, err := pool.Exec(ctx,
		`INSERT INTO sz_orders (id, order_number, user_id, produtor_id, affiliate_id, wp_order_id, status, total)
		 OVERRIDING SYSTEM VALUE
		 VALUES ($1,$2,0,$3,$4,$5,'processing',100.00)`,
		orderID, "RBAC-"+rbacItoa(orderID), produtorID, affiliateID, wcOrderID); err != nil {
		t.Fatalf("seedOrder sz_orders: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO sz_motoboy_pedidos (id, wc_order_id, cd_id, zona_id, status, dest_cep, valor_pedido)
		 OVERRIDING SYSTEM VALUE
		 VALUES ($1,$2,0,0,'agendado','00000000',100.00)`,
		orderID, wcOrderID); err != nil {
		t.Fatalf("seedOrder sz_motoboy_pedidos: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM sz_motoboy_pedidos WHERE id=$1`, orderID)
		_, _ = pool.Exec(c, `DELETE FROM sz_orders WHERE id=$1`, orderID)
	})
}

func rbacItoa(n int64) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestRBACOrdersScope(t *testing.T) {
	// Secrets de teste (assinatura/validação leem o env no momento da chamada).
	t.Setenv("JWT_SECRET", "rbac-test-secret-key-0123456789")
	t.Setenv("ADMIN_JWT_SECRET", "rbac-test-secret-key-0123456789")
	t.Setenv("WP_SALT_AUTH", "rbac-test-salt-auth-0123456789")

	pool := testPoolOrSkip(t)

	// ── Seed identidades ──────────────────────────────────────────────────────
	produtorTok := mintPortal(t, pool, rbacProdutorPortalID, rbacProdutorWPID, "produtor", "rbac-prod@test.local")
	afiliadoTok := mintPortal(t, pool, rbacAfiliadoPortalID, rbacAfiliadoWPID, "afiliado", "rbac-afil@test.local")
	adminTok := mintAdmin(t, pool, rbacAdminID, "rbac-admin@test.local")

	// ── Seed pedidos ──────────────────────────────────────────────────────────
	// Prod1: produtor=P, affiliate=A  → produtor SIM, afiliado SIM.
	seedOrder(t, pool, rbacOrderProd1, rbacProdutorPortalID, rbacAfiliadoWPID)
	// Prod2: produtor=P, affiliate=999 → produtor SIM, afiliado NÃO.
	seedOrder(t, pool, rbacOrderProd2, rbacProdutorPortalID, 990009999)
	// Other: produtor=888, affiliate=999 → produtor NÃO, afiliado NÃO.
	seedOrder(t, pool, rbacOrderOther, 990008888, 990009999)

	ordH := &OrdersHandler{Pool: pool}
	odH := &OrderDetailHandler{Pool: pool}

	// Router idêntico ao de produção (DualAuth + as 4 GETs).
	mountRouter := func() http.Handler {
		r := chi.NewRouter()
		r.Group(func(r chi.Router) {
			r.Use(auth.DualAuth(pool))
			r.Get("/orders/motoboy", ordH.ListMotoboy)
			r.Get("/orders/{id}", odH.Get)
		})
		return r
	}
	router := mountRouter()

	doList := func(bearer string) map[int64]bool {
		req := httptest.NewRequest(http.MethodGet, "/orders/motoboy?limit=200", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		out := map[int64]bool{}
		for _, it := range resp.Items {
			out[it.ID] = true
		}
		return out
	}
	detailStatus := func(bearer string, orderID int64) int {
		req := httptest.NewRequest(http.MethodGet, "/orders/"+rbacItoa(orderID), nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	// ── PRODUTOR: vê Prod1 + Prod2, NÃO vê Other ─────────────────────────────
	pSet := doList(produtorTok)
	if !pSet[rbacOrderProd1] || !pSet[rbacOrderProd2] {
		t.Errorf("produtor deveria ver Prod1+Prod2; viu %v", pSet)
	}
	if pSet[rbacOrderOther] {
		t.Errorf("produtor NÃO deveria ver Other (de outro produtor)")
	}

	// ── AFILIADO: vê Prod1, NÃO vê Prod2 nem Other ───────────────────────────
	aSet := doList(afiliadoTok)
	if !aSet[rbacOrderProd1] {
		t.Errorf("afiliado deveria ver Prod1 (affiliate_id dele); viu %v", aSet)
	}
	if aSet[rbacOrderProd2] || aSet[rbacOrderOther] {
		t.Errorf("afiliado NÃO deveria ver Prod2/Other; viu %v", aSet)
	}

	// ── ADMIN: superset — vê os três ─────────────────────────────────────────
	adSet := doList(adminTok)
	if !adSet[rbacOrderProd1] || !adSet[rbacOrderProd2] || !adSet[rbacOrderOther] {
		t.Errorf("admin deveria ver os três pedidos; viu %v", adSet)
	}

	// ── DETALHE cross-user ───────────────────────────────────────────────────
	// Produtor pega Other (de outro produtor) → 404.
	if st := detailStatus(produtorTok, rbacOrderOther); st != 404 {
		t.Errorf("produtor GET /orders/Other deveria ser 404; foi %d", st)
	}
	// Produtor pega Prod1 (dele) → 200.
	if st := detailStatus(produtorTok, rbacOrderProd1); st != 200 {
		t.Errorf("produtor GET /orders/Prod1 deveria ser 200; foi %d", st)
	}
	// Afiliado pega Prod2 (produtor dono, affiliate de outro) → 404.
	if st := detailStatus(afiliadoTok, rbacOrderProd2); st != 404 {
		t.Errorf("afiliado GET /orders/Prod2 deveria ser 404; foi %d", st)
	}
	// Afiliado pega Prod1 (dele) → 200.
	if st := detailStatus(afiliadoTok, rbacOrderProd1); st != 200 {
		t.Errorf("afiliado GET /orders/Prod1 deveria ser 200; foi %d", st)
	}
	// Admin pega Other → 200 (vê tudo).
	if st := detailStatus(adminTok, rbacOrderOther); st != 200 {
		t.Errorf("admin GET /orders/Other deveria ser 200; foi %d", st)
	}

	// ── Token de portal SEM sessão viva → 401 (prova a validação de sessão) ──
	_, _ = pool.Exec(context.Background(),
		`UPDATE senderzz_portal_sessions SET expires_at = NOW() - INTERVAL '1 hour' WHERE user_id=$1`,
		rbacProdutorPortalID)
	req := httptest.NewRequest(http.MethodGet, "/orders/motoboy", nil)
	req.Header.Set("Authorization", "Bearer "+produtorTok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("token de portal com sessão expirada deveria 401; foi %d", rec.Code)
	}
}

// TestRBACProductsAffiliatesScope prova o escopo dos FILTROS que a tela admin de
// pedidos chama: GET /products e GET /affiliates aceitam token de portal e escopam:
//   - produtor → seus produtos / seus afiliados vinculados;
//   - afiliado → só ele mesmo (em /affiliates); produtos dos produtores vinculados.
//
// Admin continua vendo tudo (superset). Seed isolado com ids sintéticos + cleanup.
func TestRBACProductsAffiliatesScope(t *testing.T) {
	t.Setenv("JWT_SECRET", "rbac-test-secret-key-0123456789")
	t.Setenv("ADMIN_JWT_SECRET", "rbac-test-secret-key-0123456789")
	t.Setenv("WP_SALT_AUTH", "rbac-test-salt-auth-0123456789")

	pool := testPoolOrSkip(t)
	ctx := context.Background()

	const (
		pProdPortal int64 = 990011001
		pProdWP     int64 = 990011101
		pAfilPortal int64 = 990011002
		pAfilWP     int64 = 990011102
		otherProd   int64 = 990018888

		prodA   int64 = 990012001 // produto do produtor P
		prodOth int64 = 990012002 // produto de outro produtor
		affLink int64 = 990013001 // vínculo afiliado A ↔ produtor P
		adminID int64 = 990013999
	)

	produtorTok := mintPortal(t, pool, pProdPortal, pProdWP, "produtor", "rbac-p2@test.local")
	afiliadoTok := mintPortal(t, pool, pAfilPortal, pAfilWP, "afiliado", "rbac-a2@test.local")
	adminTok := mintAdmin(t, pool, adminID, "rbac-admin2@test.local")

	// Produtos.
	for _, p := range []struct {
		id, prod int64
		nome     string
	}{{prodA, pProdPortal, "Produto P"}, {prodOth, otherProd, "Produto Outro"}} {
		_, _ = pool.Exec(ctx, `DELETE FROM sz_products WHERE id=$1`, p.id)
		if _, err := pool.Exec(ctx,
			`INSERT INTO sz_products (id, produtor_id, nome, preco, status, variacao)
			 OVERRIDING SYSTEM VALUE VALUES ($1,$2,$3,0,'active','')`,
			p.id, p.prod, p.nome); err != nil {
			t.Fatalf("seed produto: %v", err)
		}
		pid := p.id
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM sz_products WHERE id=$1`, pid) })
	}

	// Vínculo de afiliado: afiliado A (afiliado_id = wp_user_id) ↔ produtor P (produtor_id = portal id).
	_, _ = pool.Exec(ctx, `DELETE FROM senderzz_affiliates WHERE id=$1`, affLink)
	if _, err := pool.Exec(ctx,
		`INSERT INTO senderzz_affiliates (id, produtor_id, afiliado_id, produto_id, status, comissao_pct)
		 OVERRIDING SYSTEM VALUE VALUES ($1,$2,$3,0,'active',10)`,
		affLink, pProdPortal, pAfilWP); err != nil {
		t.Fatalf("seed vinculo: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM senderzz_affiliates WHERE id=$1`, affLink) })

	prdH := &ProductsHandler{Pool: pool}
	affH := &AffiliatesHandler{Pool: pool}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.DualAuth(pool))
		r.Get("/products", prdH.List)
		r.Get("/affiliates", affH.List)
	})

	productIDs := func(bearer, query string) map[int64]bool {
		req := httptest.NewRequest(http.MethodGet, "/products"+query, nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("/products status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		out := map[int64]bool{}
		for _, it := range resp.Items {
			out[it.ID] = true
		}
		return out
	}
	affiliateUserIDs := func(bearer string) (map[int64]bool, int) {
		req := httptest.NewRequest(http.MethodGet, "/affiliates?limit=200", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("/affiliates status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []struct {
				UserID int64 `json:"user_id"`
			} `json:"items"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		out := map[int64]bool{}
		for _, it := range resp.Items {
			out[it.UserID] = true
		}
		return out, len(resp.Items)
	}

	// PRODUTOR: vê prodA, não prodOth. Mesmo passando ?produtor_id= de OUTRO produtor,
	// o servidor sobrepõe pelo dono autenticado (não vaza catálogo alheio).
	pSet := productIDs(produtorTok, "?produtor_id="+rbacItoa(otherProd))
	if !pSet[prodA] || pSet[prodOth] {
		t.Errorf("produtor /products escopo errado: %v", pSet)
	}

	// AFILIADO: vê prodA (produtor vinculado), não prodOth.
	aSet := productIDs(afiliadoTok, "")
	if !aSet[prodA] || aSet[prodOth] {
		t.Errorf("afiliado /products escopo errado: %v", aSet)
	}

	// ADMIN: vê ambos.
	adSet := productIDs(adminTok, "")
	if !adSet[prodA] || !adSet[prodOth] {
		t.Errorf("admin /products deveria ver ambos: %v", adSet)
	}

	// /affiliates — PRODUTOR vê o afiliado vinculado (A), não vaza a base toda.
	pAff, _ := affiliateUserIDs(produtorTok)
	if !pAff[pAfilPortal] {
		t.Errorf("produtor /affiliates deveria ver o afiliado vinculado (id=%d); viu %v", pAfilPortal, pAff)
	}

	// /affiliates — AFILIADO vê SÓ ele mesmo (1 linha, ele próprio).
	aAff, n := affiliateUserIDs(afiliadoTok)
	if n != 1 || !aAff[pAfilPortal] {
		t.Errorf("afiliado /affiliates deveria ver só ele (1 linha id=%d); viu n=%d %v", pAfilPortal, n, aAff)
	}

	// /affiliates — ADMIN vê o afiliado (e mais — superset).
	adAff, adN := affiliateUserIDs(adminTok)
	if !adAff[pAfilPortal] || adN < 1 {
		t.Errorf("admin /affiliates deveria ver o afiliado (superset); viu n=%d", adN)
	}
}

// setMotoboyStatus força o status de um pedido motoboy (p/ cenário de clone-frustrado).
func setMotoboyStatus(t *testing.T, pool *pgxpool.Pool, mbPedidoID int64, status string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE sz_motoboy_pedidos SET status=$2 WHERE id=$1`, mbPedidoID, status); err != nil {
		t.Fatalf("setMotoboyStatus: %v", err)
	}
}

// TestRBACOrdersMutationsOwnership — SEC-RBAC-MOTOBOY-MUTATIONS-2026-06-24.
// Prova que as 3 MUTAÇÕES de pedido motoboy (reagendar/cancelar/reagendar-clone)
// sob DualAuth aplicam o OWNERSHIP-GATE pela identidade AUTENTICADA:
//   - produtor/afiliado mutando pedido de OUTRO dono → 404, SEM mutar (cross-user);
//   - o DONO chega na mutação (cancelar → 200 + status='cancelado'; clone-frustrado
//     → 201 + nova linha) — prova empírica de "o dono chega na mutação";
//   - admin muta qualquer pedido (comportamento inalterado).
//
// {id} da rota = sz_motoboy_pedidos.id; o dono é resolvido de sz_orders via
// wc_order_id (NUNCA por input do cliente). Seed isolado (ids ≥ 990) + cleanup.
func TestRBACOrdersMutationsOwnership(t *testing.T) {
	t.Setenv("JWT_SECRET", "rbac-test-secret-key-0123456789")
	t.Setenv("ADMIN_JWT_SECRET", "rbac-test-secret-key-0123456789")
	t.Setenv("WP_SALT_AUTH", "rbac-test-salt-auth-0123456789")

	pool := testPoolOrSkip(t)
	ctx := context.Background()

	const (
		mProdPortal int64 = 990021001
		mProdWP     int64 = 990021101
		mAfilPortal int64 = 990021002
		mAfilWP     int64 = 990021102

		// Pedido do produtor P + afiliado A (id == sz_motoboy_pedidos.id no seed).
		mOrderMine  int64 = 990022001
		mOrderOther int64 = 990022003 // de outro produtor/afiliado
		mAdminID    int64 = 990023001
	)

	produtorTok := mintPortal(t, pool, mProdPortal, mProdWP, "produtor", "rbac-mut-p@test.local")
	afiliadoTok := mintPortal(t, pool, mAfilPortal, mAfilWP, "afiliado", "rbac-mut-a@test.local")
	adminTok := mintAdmin(t, pool, mAdminID, "rbac-mut-admin@test.local")

	// Pedido do produtor P + afiliado A; pedido de outro dono.
	seedOrder(t, pool, mOrderMine, mProdPortal, mAfilWP)
	seedOrder(t, pool, mOrderOther, 990028888, 990029999)

	ordH := &OrdersHandler{Pool: pool}
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(auth.DualAuth(pool))
		r.Post("/orders/motoboy/{id}/reagendar", ordH.Reagendar)
		r.Post("/orders/motoboy/{id}/reagendar-clone", ordH.ReagendarClone)
		r.Post("/orders/motoboy/{id}/cancelar", ordH.Cancelar)
	})

	post := func(bearer, path, body string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	statusOf := func(mbPedidoID int64) string {
		var st string
		_ = pool.QueryRow(ctx, `SELECT COALESCE(status,'') FROM sz_motoboy_pedidos WHERE id=$1`, mbPedidoID).Scan(&st)
		return st
	}

	otherPath := "/orders/motoboy/" + rbacItoa(mOrderOther) + "/"
	minePath := "/orders/motoboy/" + rbacItoa(mOrderMine) + "/"
	futureDate := `{"data":"2030-01-15"}`

	// ── CROSS-USER: produtor/afiliado mutando pedido de OUTRO dono → 404, SEM mutar ──
	for _, c := range []struct {
		who, tok string
	}{{"produtor", produtorTok}, {"afiliado", afiliadoTok}} {
		if st := post(c.tok, otherPath+"cancelar", ""); st != 404 {
			t.Errorf("%s cancelar pedido alheio deveria 404; foi %d", c.who, st)
		}
		if st := post(c.tok, otherPath+"reagendar", futureDate); st != 404 {
			t.Errorf("%s reagendar pedido alheio deveria 404; foi %d", c.who, st)
		}
		if st := post(c.tok, otherPath+"reagendar-clone", futureDate); st != 404 {
			t.Errorf("%s clone pedido alheio deveria 404; foi %d", c.who, st)
		}
	}
	// O pedido alheio permaneceu INTACTO (gate retornou antes da escrita).
	if got := statusOf(mOrderOther); got != "agendado" {
		t.Errorf("pedido alheio NÃO deveria ter mudado; status=%q", got)
	}

	// ── DONO chega na mutação: produtor cancela o pedido DELE → 200 + 'cancelado' ──
	if st := post(produtorTok, minePath+"cancelar", ""); st != 200 {
		t.Fatalf("produtor cancelar pedido DELE deveria 200; foi %d", st)
	}
	if got := statusOf(mOrderMine); got != "cancelado" {
		t.Errorf("pedido do produtor deveria estar 'cancelado'; status=%q", got)
	}

	// ── CLONE: afiliado clona pedido FRUSTRADO DELE → 201 + nova linha órfã ──
	setMotoboyStatus(t, pool, mOrderMine, "frustrado")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, minePath+"reagendar-clone", strings.NewReader(futureDate))
	req.Header.Set("Authorization", "Bearer "+afiliadoTok)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("afiliado clone pedido FRUSTRADO DELE deveria 201; foi %d body=%s", rec.Code, rec.Body.String())
	}
	var cloneResp struct {
		ClonePedidoID int64 `json:"clone_pedido_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cloneResp)
	if cloneResp.ClonePedidoID == 0 {
		t.Errorf("clone deveria retornar clone_pedido_id != 0")
	} else {
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM sz_motoboy_pedidos WHERE id=$1`, cloneResp.ClonePedidoID)
		})
	}

	// ── ADMIN muta qualquer pedido (o de outro dono) → cancelar 200 ──
	if st := post(adminTok, otherPath+"cancelar", ""); st != 200 {
		t.Errorf("admin cancelar qualquer pedido deveria 200; foi %d", st)
	}
	if got := statusOf(mOrderOther); got != "cancelado" {
		t.Errorf("admin deveria ter cancelado o pedido alheio; status=%q", got)
	}
}
