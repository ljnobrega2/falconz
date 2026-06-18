<?php
/**
 * Senderzz Logistics — Patch Tier 2: V-NEW-01 + V-NEW-02 (v2 fail-closed)
 *
 * Substitui o patch v1 (que chamava endpoint /me/balance/transactions/{id}
 * inexistente). Esta versão é validada contra a doc oficial do ME
 * (https://docs.melhorenvio.com.br/reference + /docs/webhooks).
 *
 * O QUE A DOC PERMITE:
 *  - GET  /api/v2/me/balance       → saldo total do usuário
 *  - POST /api/v2/me/balance       → cria recarga PIX
 *  - Webhooks ME só de etiqueta (order.*) — NÃO existe webhook de PIX
 *
 * O QUE A DOC NÃO DOCUMENTA:
 *  - Endpoint pra consultar status de uma recarga PIX específica por ID
 *  - Listagem de transações da carteira (apesar da permissão transactions-read)
 *
 * ESTRATÉGIA DESTE PATCH (FAIL-CLOSED):
 *  - Cron NUNCA confirma recarga sozinho. Só cancela recargas expiradas.
 *  - Confirmação 100% via webhook PIX (Yapay → /wp-json/tp-carteira/v1/webhook/pix)
 *    que já valida HMAC + status='paid' antes de creditar.
 *  - Endpoint de redirect PIX (/pix/retorno) também NÃO credita —
 *    apenas redireciona o usuário pra carteira pra ele ver o status real.
 *  - Se webhook Yapay nunca chegar, recarga fica pendente até admin
 *    investigar via painel ME. Saldo NUNCA é creditado por suposição.
 *  - Adiciona telemetria: alerta no admin se recargas ficam >30min
 *    pendentes sem webhook (sinal de problema na Yapay ou no endpoint).
 *
 * COMO APLICAR:
 *  1. Mova o arquivo para includes/tpc/pix-confirmation-guard.php
 *  2. Adicione no senderzz-logistics.php DEPOIS do require de pix.php:
 *     require_once 'includes/tpc/pix-confirmation-guard.php';
 *  3. Sem migração de banco. Sem dependência da API ME.
 *
 * REVERSÃO: basta remover o require_once. Nada do código existente é
 * modificado — apenas hooks são adicionados.
 *
 * VALIDAÇÃO ADICIONAL RECOMENDADA: pergunte ao suporte ME se existe
 * endpoint não-documentado correspondente à permissão transactions-read.
 * Se existir e for estável, podemos adicionar uma camada de double-check
 * via API ME no /webhook/pix antes de creditar (defesa em profundidade).
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_PIX_CONFIRMATION_GUARD_LOADED' ) ) return;
define( 'SENDERZZ_PIX_CONFIRMATION_GUARD_LOADED', true );

// ─────────────────────────────────────────────────────────────────────────────
// 1) Substitui o cron handler original. Novo cron NUNCA chama
//    tpc_confirmar_recarga(). Só cancela expiradas e dispara alertas.
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'init', function () {
    // Tira o handler original do hook do cron e registra o novo.
    if ( has_action( 'tpc_cron_verificar_recargas_pix' ) ) {
        remove_all_actions( 'tpc_cron_verificar_recargas_pix' );
    }
    add_action( 'tpc_cron_verificar_recargas_pix', 'senderzz_pix_cron_failclosed_handler' );
}, 25 ); // depois do registro original (priority 20)

function senderzz_pix_cron_failclosed_handler(): void {
    if ( get_transient( 'tpc_pix_cron_lock' ) ) return;
    set_transient( 'tpc_pix_cron_lock', 1, 4 * MINUTE_IN_SECONDS );
    try {
        senderzz_pix_processar_recargas_failclosed( 50 );
    } finally {
        delete_transient( 'tpc_pix_cron_lock' );
    }
}

function senderzz_pix_processar_recargas_failclosed( int $limit = 50 ): array {
    global $wpdb;

    $now = time();
    $stuck_threshold = gmdate( 'Y-m-d H:i:s', $now - 30 * MINUTE_IN_SECONDS );

    $rows = $wpdb->get_results( $wpdb->prepare(
        "SELECT id, status, expires_at, created_at, value
         FROM {$wpdb->prefix}tpc_recargas
         WHERE status IN ('pendente','analise')
         ORDER BY created_at ASC
         LIMIT %d",
        max( 1, min( 100, $limit ) )
    ), ARRAY_A ) ?: [];

    $resultado = [
        'total'       => count( $rows ),
        'canceladas'  => 0, // por expiração
        'stuck'       => 0, // pendentes há mais de 30min sem webhook
        'pendentes'   => 0, // dentro do prazo, aguardando webhook normalmente
    ];

    $stuck_ids = [];

    foreach ( $rows as $r ) {
        $rid = (int) $r['id'];

        // Expirada → cancela direto. Único caminho onde o cron muda estado.
        if ( ! empty( $r['expires_at'] ) && strtotime( $r['expires_at'] ) < $now ) {
            if ( function_exists( 'tpc_cancelar_recarga' ) ) {
                tpc_cancelar_recarga( $rid );
            }
            $resultado['canceladas']++;
            continue;
        }

        // Não expirou, mas tá há mais de 30min sem confirmar via webhook.
        // Sinal de que pode ter problema na Yapay ou no endpoint /webhook/pix.
        // NÃO confirma — apenas marca pra alerta.
        if ( strtotime( $r['created_at'] ) < strtotime( $stuck_threshold ) ) {
            $stuck_ids[] = $rid;
            $resultado['stuck']++;
            continue;
        }

        $resultado['pendentes']++;
    }

    // Dispara alerta se houver recargas presas (com throttle).
    if ( ! empty( $stuck_ids ) ) {
        senderzz_pix_alert_stuck_recargas( $stuck_ids );
    }

    senderzz_pix_guard_log( 'failclosed_cron_result', $resultado );
    return $resultado;
}

// ─────────────────────────────────────────────────────────────────────────────
// 2) Substitui o handler de /pix/retorno (redirect_url da Yapay).
//    O original tentava confirmar baseado em token HMAC determinístico.
//    Novo: APENAS redireciona — não credita.
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'rest_api_init', function () {
    register_rest_route( 'tp-carteira/v1', '/pix/retorno', [
        'methods'             => [ 'GET', 'POST' ],
        'callback'            => 'senderzz_pix_endpoint_retorno_failclosed',
        'permission_callback' => '__return_true',
    ] );
}, 12 ); // depois do registro original (priority padrão 10)

function senderzz_pix_endpoint_retorno_failclosed( WP_REST_Request $request ): void {
    $recarga_id = (int) ( $request->get_param( 'recarga_id' ) ?: 0 );
    $sz_token   = sanitize_text_field( (string) ( $request->get_param( 'sz_token' ) ?: '' ) );

    $dest = function_exists( 'wc_get_account_endpoint_url' )
        ? wc_get_account_endpoint_url( 'carteira' )
        : home_url( '/' );

    // Só anexa o ID na URL pra UI mostrar status. NÃO credita aqui.
    if ( $recarga_id ) {
        // Valida o token só pra log — saber se o redirect veio do fluxo
        // Yapay legítimo, mas não autoriza nenhuma ação.
        if ( $sz_token && function_exists( 'tpc_pix_validar_token' ) ) {
            $valid = tpc_pix_validar_token( $recarga_id, $sz_token );
            senderzz_pix_guard_log( 'redirect_received', [
                'recarga_id'   => $recarga_id,
                'token_valido' => (bool) $valid,
            ] );
        }
        $dest = add_query_arg( 'recarga_id', $recarga_id, $dest );
    }

    wp_safe_redirect( $dest );
    exit;
}

// ─────────────────────────────────────────────────────────────────────────────
// 3) Bloqueia chamadas a tpc_confirmar_recarga vindas de qualquer origem
//    EXCETO 'webhook'. É a defesa em profundidade — mesmo que algum código
//    futuro chame com origem 'cron' ou 'redirect', o filtro recusa.
//
//    O webhook PIX (handler em pix.php) chama com origem 'webhook' depois de
//    validar HMAC + status='paid' do payload da Yapay. Esse caminho continua
//    funcionando.
// ─────────────────────────────────────────────────────────────────────────────

add_filter( 'senderzz_pix_pre_confirmar_recarga', function ( $allow, int $recarga_id, string $origem ) {
    // Aceita apenas webhook (Yapay com HMAC validado) e reconciliação manual de admin.
    if ( in_array( $origem, [ 'webhook', 'admin_reconciliation' ], true ) ) {
        return $allow;
    }
    senderzz_pix_guard_log( 'tpc_confirmar_recarga_blocked', [
        'recarga_id' => $recarga_id,
        'origem'     => $origem,
        'reason'     => 'fail-closed: somente webhook e admin_reconciliation autorizados',
    ] );
    return false;
}, 10, 3 );

// O wrapper público abaixo deve ser usado em qualquer ponto do código que
// queira confirmar uma recarga. Substitui chamadas diretas a
// tpc_confirmar_recarga em pontos não-webhook.
//
// NOTA: o handler original em pix.php chama tpc_confirmar_recarga() direto
// sem passar pelo nosso filtro. Pra ativar a guarda full, é necessário
// modificar essas chamadas no pix.php pra usar:
//
//   senderzz_pix_confirmar_com_guarda($recarga_id, $origem)
//
// OU adicionar este snippet no início de tpc_confirmar_recarga (se pudermos
// modificar a função):
//
//   if (!apply_filters('senderzz_pix_pre_confirmar_recarga', true, $recarga_id, $origem)) {
//       return false;
//   }
//
// Sem essa modificação, o cron e o redirect já não chamam tpc_confirmar_recarga
// (porque substituímos os handlers acima), mas o filtro ainda serve como
// segunda camada de defesa pro futuro.

function senderzz_pix_confirmar_com_guarda( int $recarga_id, string $origem ): bool {
    $allow = apply_filters( 'senderzz_pix_pre_confirmar_recarga', true, $recarga_id, $origem );
    if ( ! $allow ) return false;
    if ( ! function_exists( 'tpc_confirmar_recarga' ) ) return false;
    return tpc_confirmar_recarga( $recarga_id, $origem );
}

// ─────────────────────────────────────────────────────────────────────────────
// 4) Telemetria: alerta admin sobre recargas presas há >30min sem webhook.
//    Throttle: 1 alerta por sessão de stuck a cada 6h.
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_pix_alert_stuck_recargas( array $recarga_ids ): void {
    if ( get_transient( 'sz_pix_stuck_alert_throttle' ) ) return;

    $admin = get_option( 'admin_email' );
    if ( ! $admin ) return;

    $count = count( $recarga_ids );
    $list  = implode( ', ', array_slice( $recarga_ids, 0, 20 ) );
    if ( $count > 20 ) $list .= ' (e mais ' . ( $count - 20 ) . ')';

    $subject = '[Senderzz] Recargas PIX presas sem confirmação';
    $message = sprintf(
        "Detectamos %d recarga(s) PIX há mais de 30 minutos no status pendente sem receber webhook de confirmação da Yapay.\n\n" .
        "IDs: %s\n\n" .
        "Possíveis causas:\n" .
        "  - Endpoint /wp-json/tp-carteira/v1/webhook/pix indisponível ou bloqueado\n" .
        "  - Yapay não enviou ou tentativas falharam\n" .
        "  - Cliente abandonou o pagamento (esperado, será cancelado na expiração)\n\n" .
        "Recomendações:\n" .
        "  1. Verifique o painel Melhor Envio em melhorenvio.com.br/painel/transacoes\n" .
        "     pra ver se as recargas constam como pagas no ME.\n" .
        "  2. Se constam pagas, há problema no webhook Yapay → seu site.\n" .
        "     Use a ferramenta admin pra reconciliar manualmente.\n" .
        "  3. Se não constam pagas, cliente provavelmente abandonou — será cancelada\n" .
        "     automaticamente quando expirar.\n\n" .
        "Esta auditoria não credita saldo automaticamente. A confirmação só ocorre via\n" .
        "webhook autenticado por HMAC, ou pela ação manual do administrador.",
        $count,
        $list
    );

    wp_mail( $admin, $subject, $message );
    set_transient( 'sz_pix_stuck_alert_throttle', 1, 6 * HOUR_IN_SECONDS );

    senderzz_pix_guard_log( 'stuck_alert_sent', [ 'count' => $count ] );
}

// ─────────────────────────────────────────────────────────────────────────────
// 5) Logger isolado.
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_pix_guard_log( string $tipo, $data ): void {
    if ( function_exists( 'tpc_pix_log' ) ) {
        tpc_pix_log( 'guard_' . $tipo, $data );
        return;
    }
    if ( function_exists( 'wc_get_logger' ) ) {
        wc_get_logger()->info(
            wp_json_encode( $data, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ),
            [ 'source' => 'senderzz-pix-guard-' . $tipo ]
        );
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 6) FERRAMENTA ADMIN: reconciliação manual.
//    Endpoint só pra admin: dado um recarga_id, força confirmação MANUAL
//    com motivo registrado. Use APENAS após verificar manualmente no painel
//    do Melhor Envio que a recarga consta como paga.
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'rest_api_init', function () {
    register_rest_route( 'tp-carteira/v1', '/admin/reconciliar-pix', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_pix_admin_reconciliar',
        'permission_callback' => function () {
            return current_user_can( 'manage_woocommerce' );
        },
        'args'                => [
            'recarga_id' => [ 'required' => true, 'sanitize_callback' => 'absint' ],
            'motivo'     => [ 'required' => true, 'sanitize_callback' => 'sanitize_text_field' ],
        ],
    ] );
} );

function senderzz_pix_admin_reconciliar( WP_REST_Request $request ): WP_REST_Response {
    $recarga_id = (int) $request->get_param( 'recarga_id' );
    $motivo     = (string) $request->get_param( 'motivo' );
    $admin_id   = get_current_user_id();

    if ( ! $recarga_id || strlen( $motivo ) < 10 ) {
        return new WP_REST_Response( [
            'success' => false,
            'message' => 'recarga_id e motivo (>=10 caracteres) obrigatórios.',
        ], 400 );
    }

    $recarga = function_exists( 'tpc_get_recarga' ) ? tpc_get_recarga( $recarga_id ) : null;
    if ( ! $recarga ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Recarga não encontrada.' ], 404 );
    }
    if ( ( $recarga['status'] ?? '' ) === 'confirmado' ) {
        return new WP_REST_Response( [ 'success' => true, 'message' => 'Recarga já estava confirmada.' ], 200 );
    }

    senderzz_pix_guard_log( 'admin_reconciliacao_iniciada', [
        'recarga_id' => $recarga_id,
        'admin_id'   => $admin_id,
        'admin_user' => wp_get_current_user()->user_login,
        'motivo'     => $motivo,
    ] );

    // Confirma com origem especial 'admin_reconciliation' (auditável).
    $allow = apply_filters( 'senderzz_pix_pre_confirmar_recarga', true, $recarga_id, 'admin_reconciliation' );
    if ( ! $allow ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Confirmação bloqueada pelo guard.' ], 403 );
    }

    if ( ! function_exists( 'tpc_confirmar_recarga' ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Função de confirmação indisponível.' ], 500 );
    }

    $ok = tpc_confirmar_recarga( $recarga_id, 'admin_reconciliation' );

    senderzz_pix_guard_log( 'admin_reconciliacao_resultado', [
        'recarga_id' => $recarga_id,
        'admin_id'   => $admin_id,
        'sucesso'    => (bool) $ok,
    ] );

    return new WP_REST_Response( [
        'success' => (bool) $ok,
        'message' => $ok
            ? 'Recarga reconciliada manualmente. Ação registrada no log.'
            : 'Falha ao reconciliar — veja os logs.',
    ], $ok ? 200 : 500 );
}

// Permite admin_reconciliation no filtro (única origem não-webhook permitida).
add_filter( 'senderzz_pix_pre_confirmar_recarga', function ( $allow, $rid, $origem ) {
    if ( $origem === 'admin_reconciliation' ) return true;
    return $allow;
}, 5, 3 );
