// Package handlers — endpoint admin para catálogo de APIs REST do Senderzz.
// Espelha tab API em includes/tpc/admin.php:781.
// Sem banco de dados; resposta hardcoded como fonte de verdade.
package handlers

import (
	"net/http"

	"github.com/senderzz/admin-service/internal/httpx"
)

// ApiDocsHandler serve o catálogo estático de endpoints REST do Senderzz.
type ApiDocsHandler struct{}

// apiEndpoint descreve um endpoint individual.
type apiEndpoint struct {
	Method       string `json:"method"`
	Path         string `json:"path"`
	Description  string `json:"description"`
	AuthRequired bool   `json:"auth_required"`
}

// apiNamespace agrupa endpoints por namespace WP REST.
type apiNamespace struct {
	Namespace string        `json:"namespace"`
	Label     string        `json:"label"`
	BaseURL   string        `json:"base_url"`
	Auth      string        `json:"auth"`
	Endpoints []apiEndpoint `json:"endpoints"`
}

// catálogo completo — fonte única de verdade para o frontend.
// Derivado dos register_rest_route em src/Rest/Labels.php, senderzz-rest.php,
// senderzz-async-approval.php, senderzz-integrations.php, motoboy/rest-api.php,
// cod-wallet.php, senderzz-affiliates.php.
var apiCatalog = []apiNamespace{
	{
		Namespace: "wc-melhor-envio/v1",
		Label:     "Labels (Melhor Envio)",
		BaseURL:   "/wp-json/wc-melhor-envio/v1",
		Auth:      "JWT Bearer",
		Endpoints: []apiEndpoint{
			{Method: "POST", Path: "/labels", Description: "Gerar etiqueta", AuthRequired: true},
			{Method: "GET", Path: "/labels/{order_id}", Description: "Buscar etiqueta por pedido", AuthRequired: true},
			{Method: "POST", Path: "/labels/{order_id}/process", Description: "Processar etiqueta (pipeline)", AuthRequired: true},
			{Method: "GET", Path: "/labels/{order_id}/download", Description: "Download PDF da etiqueta", AuthRequired: true},
			{Method: "POST", Path: "/labels/batch-print", Description: "Impressão em lote (PDF único)", AuthRequired: true},
			{Method: "POST", Path: "/tools/migrate-labels", Description: "Migração de etiquetas legadas", AuthRequired: true},
		},
	},
	{
		Namespace: "tp-carteira/v1",
		Label:     "Carteira Frete (TPC)",
		BaseURL:   "/wp-json/tp-carteira/v1",
		Auth:      "JWT Bearer",
		Endpoints: []apiEndpoint{
			{Method: "POST", Path: "/auth/token", Description: "Login → JWT", AuthRequired: false},
			{Method: "GET", Path: "/me", Description: "Dados usuário + saldo", AuthRequired: true},
			{Method: "GET", Path: "/saldo", Description: "Saldo atual", AuthRequired: true},
			{Method: "GET", Path: "/extrato", Description: "Histórico paginado (tipo, data_ini, data_fim, per_page, page)", AuthRequired: true},
			{Method: "POST", Path: "/recarregar", Description: "Criar recarga PIX (body: valor≥10)", AuthRequired: true},
			{Method: "GET", Path: "/recarga/{id}/pix", Description: "Status/QR de uma recarga", AuthRequired: true},
			{Method: "POST", Path: "/webhook/pix", Description: "Webhook PIX Yapay (interno)", AuthRequired: false},
			{Method: "POST", Path: "/webhook/envio", Description: "Webhook Melhor Envio (interno)", AuthRequired: false},
		},
	},
	{
		Namespace: "senderzz/v1",
		Label:     "Pedidos + Webhooks + Operador",
		BaseURL:   "/wp-json/senderzz/v1",
		Auth:      "JWT Bearer",
		Endpoints: []apiEndpoint{
			{Method: "GET", Path: "/dashboard", Description: "Resumo do dashboard (KPIs)", AuthRequired: true},
			{Method: "GET", Path: "/orders", Description: "Lista pedidos (escopo por role)", AuthRequired: true},
			{Method: "GET", Path: "/orders/{id}", Description: "Detalhe do pedido", AuthRequired: true},
			{Method: "POST", Path: "/orders/{id}/status", Description: "Mudar status (whitelist: pending→cancelled apenas)", AuthRequired: true},
			{Method: "GET", Path: "/reports", Description: "Relatórios gerais", AuthRequired: true},
			{Method: "GET", Path: "/wallet", Description: "Saldo/carteira do usuário", AuthRequired: true},
			{Method: "GET", Path: "/transactions", Description: "Transações financeiras", AuthRequired: true},
			{Method: "POST", Path: "/operator/orders/{id}/status", Description: "Operador muda status do pedido", AuthRequired: true},
			{Method: "POST", Path: "/operator/labels/generate", Description: "Operador gera etiqueta", AuthRequired: true},
			{Method: "GET", Path: "/operator/orders", Description: "Lista pedidos (operador)", AuthRequired: true},
			{Method: "POST", Path: "/integrations/{token}", Description: "Webhook integração externa (pedido externo)", AuthRequired: false},
			{Method: "POST", Path: "/webhook/me", Description: "Webhook Melhor Envio eventos", AuthRequired: false},
			{Method: "POST", Path: "/webhook", Description: "Webhook integração externa legada", AuthRequired: false},
			{Method: "GET", Path: "/tickets", Description: "Lista tickets de suporte", AuthRequired: true},
			{Method: "POST", Path: "/tickets", Description: "Criar ticket de suporte", AuthRequired: true},
			{Method: "GET", Path: "/tickets/{id}/msgs", Description: "Mensagens de um ticket", AuthRequired: true},
		},
	},
	{
		Namespace: "sz-aff/v1",
		Label:     "Afiliados",
		BaseURL:   "/wp-json/sz-aff/v1",
		Auth:      "JWT Bearer (Portal)",
		Endpoints: []apiEndpoint{
			{Method: "POST", Path: "/saque", Description: "Afiliado solicita saque", AuthRequired: true},
			{Method: "POST", Path: "/saque/aprovar", Description: "Admin aprova saque de afiliado", AuthRequired: true},
			{Method: "POST", Path: "/pix", Description: "Afiliado vincula chave PIX", AuthRequired: true},
			{Method: "POST", Path: "/pix/aprovar", Description: "Admin aprova chave PIX de afiliado", AuthRequired: true},
		},
	},
	{
		Namespace: "sz-notif/v1",
		Label:     "Notificações Push",
		BaseURL:   "/wp-json/sz-notif/v1",
		Auth:      "JWT Bearer (Portal)",
		Endpoints: []apiEndpoint{
			{Method: "POST", Path: "/subscribe", Description: "Subscrever push notification", AuthRequired: true},
			{Method: "POST", Path: "/unsubscribe", Description: "Cancelar push notification", AuthRequired: true},
			{Method: "POST", Path: "/prefs", Description: "Salvar preferências de notificação", AuthRequired: true},
		},
	},
	{
		Namespace: "sz-portal/v1",
		Label:     "Portal do Usuário (v1)",
		BaseURL:   "/wp-json/sz-portal/v1",
		Auth:      "JWT Bearer (Portal)",
		Endpoints: []apiEndpoint{
			{Method: "POST", Path: "/motoboy/bulk-cancel", Description: "Cancelar pedidos motoboy em lote", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/reschedule", Description: "Reagendar pedido motoboy", AuthRequired: true},
			{Method: "GET", Path: "/wallet/history", Description: "Histórico da carteira", AuthRequired: true},
			{Method: "GET", Path: "/wallet/future", Description: "Saldo futuro (a liberar)", AuthRequired: true},
			{Method: "GET", Path: "/wallet/export-csv", Description: "Exportar histórico da carteira em CSV", AuthRequired: true},
			{Method: "POST", Path: "/pix/confirm", Description: "Confirmar PIX recebido", AuthRequired: true},
			{Method: "POST", Path: "/cod/pix-account", Description: "COD: salvar conta PIX", AuthRequired: true},
			{Method: "GET", Path: "/cod/accounts", Description: "COD: listar contas cadastradas", AuthRequired: true},
			{Method: "POST", Path: "/cod/withdraw", Description: "COD: solicitar saque", AuthRequired: true},
			{Method: "POST", Path: "/cod/account-delete", Description: "COD: excluir conta", AuthRequired: true},
		},
	},
	{
		Namespace: "sz-portal/v2",
		Label:     "Portal do Usuário (v2)",
		BaseURL:   "/wp-json/sz-portal/v2",
		Auth:      "JWT Bearer (Portal)",
		Endpoints: []apiEndpoint{
			{Method: "GET", Path: "/cod/accounts", Description: "COD v2: listar contas", AuthRequired: true},
			{Method: "POST", Path: "/cod/withdraw", Description: "COD v2: solicitar saque", AuthRequired: true},
			{Method: "POST", Path: "/cod/anticipate", Description: "COD v2: antecipar recebimento", AuthRequired: true},
		},
	},
	{
		Namespace: "sz-motoboy/v1",
		Label:     "Motoboy PWA + OL",
		BaseURL:   "/wp-json/sz-motoboy/v1",
		Auth:      "Session token (motoboy) ou Portal JWT (OL)",
		Endpoints: []apiEndpoint{
			// Autenticação motoboy
			{Method: "POST", Path: "/login/verificar", Description: "Verificar token de login motoboy", AuthRequired: false},
			{Method: "POST", Path: "/login/definir-senha", Description: "Definir senha motoboy (primeiro acesso)", AuthRequired: false},
			{Method: "POST", Path: "/login/autenticar", Description: "Autenticar motoboy (login)", AuthRequired: false},
			{Method: "POST", Path: "/motoboy/login", Description: "Login motoboy (telefone + PIN)", AuthRequired: false},
			{Method: "POST", Path: "/motoboy/trocar-senha", Description: "Trocar senha do motoboy", AuthRequired: true},
			// OTP
			{Method: "POST", Path: "/otp/solicitar", Description: "Solicitar código OTP", AuthRequired: false},
			{Method: "POST", Path: "/otp/confirmar", Description: "Confirmar código OTP", AuthRequired: false},
			// Operações motoboy
			{Method: "GET", Path: "/motoboy/lote", Description: "Lote do motoboy (pedidos do dia)", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/iniciar-rota", Description: "Bipar QR → em_rota", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/entregar", Description: "Confirmar entrega (foto + pagamento)", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/frustrar", Description: "Marcar frustrado (motivo)", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/devolver-qr", Description: "Devolução via leitura de QR", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/ping", Description: "Heartbeat de localização do motoboy", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/fechamento", Description: "Confirmar fechamento do dia", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/confirmar-repasse", Description: "Confirmar repasse financeiro", AuthRequired: true},
			{Method: "GET", Path: "/motoboy/pendentes-confirmacao", Description: "Pedidos pendentes de confirmação", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/comprovante", Description: "Upload de comprovante de entrega", AuthRequired: true},
			{Method: "GET", Path: "/motoboy/comprovantes/{order_id}", Description: "Listar comprovantes por pedido", AuthRequired: true},
			{Method: "POST", Path: "/motoboy/push-subscribe", Description: "Subscrever push notifications (motoboy)", AuthRequired: true},
			// OL (Operador Logístico)
			{Method: "POST", Path: "/ol/mudar-status", Description: "OL: mudar status do pedido", AuthRequired: true},
			{Method: "POST", Path: "/ol/trocar-motoboy", Description: "OL: trocar motoboy do pedido", AuthRequired: true},
			{Method: "GET", Path: "/ol/motoboys-do-dia", Description: "OL: motoboys com KPIs do dia", AuthRequired: true},
			{Method: "GET", Path: "/ol/motoboys", Description: "OL: lista motoboys ativos", AuthRequired: true},
			{Method: "GET", Path: "/ol/pedido-historico", Description: "OL: histórico de um pedido", AuthRequired: true},
			// Alan (expedição/almoxarife)
			{Method: "GET", Path: "/alan/localizacao", Description: "Localização dos motoboys em tempo real (mapa)", AuthRequired: true},
			{Method: "GET", Path: "/alan/historico/{motoboy_id}", Description: "Histórico de rota do motoboy (Alan)", AuthRequired: true},
			{Method: "POST", Path: "/alan/etiquetas", Description: "Gerar/listar etiquetas (Alan)", AuthRequired: true},
			{Method: "POST", Path: "/alan/push-subscribe", Description: "Subscrever push notifications (Alan)", AuthRequired: true},
			{Method: "GET", Path: "/alan/pedidos", Description: "Lista pedidos para expedição (Alan)", AuthRequired: true},
			{Method: "POST", Path: "/alan/embalar", Description: "Embalar pedido (Alan)", AuthRequired: true},
			{Method: "POST", Path: "/alan/confirmar-fechamento", Description: "Confirmar fechamento do dia (Alan)", AuthRequired: true},
			{Method: "GET", Path: "/alan/dashboard", Description: "Dashboard de expedição (Alan)", AuthRequired: true},
			// Carteira motoboy
			{Method: "GET", Path: "/wallet/saldo", Description: "Saldo da carteira do motoboy", AuthRequired: true},
			{Method: "GET", Path: "/wallet/historico", Description: "Histórico da carteira do motoboy", AuthRequired: true},
			{Method: "GET", Path: "/wallet/bancario", Description: "Dados bancários do motoboy", AuthRequired: true},
			// Público/tracking
			{Method: "GET", Path: "/zona-cep", Description: "Zona/CD para CEP (?cep=)", AuthRequired: false},
			{Method: "GET", Path: "/tracking/{order_id}", Description: "Rastreio público por wc_order_id", AuthRequired: false},
			{Method: "GET", Path: "/tracking/{order_id}/reagendar", Description: "Reagendar via link de rastreio", AuthRequired: false},
			// Outros
			{Method: "POST", Path: "/link-expedicao", Description: "Gerar link de expedição", AuthRequired: true},
			{Method: "POST", Path: "/dispensar-cpf", Description: "Dispensar CPF do recebedor", AuthRequired: true},
		},
	},
}

// GetDocs retorna o catálogo completo de APIs REST do Senderzz.
func (h *ApiDocsHandler) GetDocs(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, 200, map[string]any{"namespaces": apiCatalog})
}
