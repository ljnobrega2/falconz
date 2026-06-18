<?php
/**
 * pix-auto-reconcile.php — Reconciliação automática via API /me/transactions
 * Versão 2 — cruzamento direto por me_pix_id, individual por usuário.
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_PIX_AUTO_RECONCILE_LOADED' ) ) return;
define( 'SENDERZZ_PIX_AUTO_RECONCILE_LOADED', true );

add_filter( 'cron_schedules', function ( array $s ): array {
    if ( ! isset( $s['sz_every_5min'] ) ) $s['sz_every_5min'] = [ 'interval' => 300, 'display' => 'A cada 5 min (Senderzz PIX)' ];
    return $s;
} );

add_action( 'init', function (): void {
    if ( ! wp_next_scheduled( 'sz_pix_auto_reconcile_cron' ) ) wp_schedule_event( time(), 'sz_every_5min', 'sz_pix_auto_reconcile_cron' );
} );

add_action( 'sz_pix_auto_reconcile_cron', 'sz_pix_auto_reconcile_run' );

function sz_pix_auto_reconcile_run(): void {
    if ( get_transient( 'sz_pix_reconcile_lock' ) ) return;
    set_transient( 'sz_pix_reconcile_lock', 1, 4 * MINUTE_IN_SECONDS );
    try {
        $r = sz_pix_reconcile_por_transactions();
        update_option( 'sz_pix_reconcile_last_run', [ 'at' => current_time( 'mysql' ), 'result' => $r ], false );
    } catch ( \Throwable $e ) {
        sz_reconcile_log( 'cron_exception', [ 'error' => $e->getMessage() ] );
    } finally {
        delete_transient( 'sz_pix_reconcile_lock' );
    }
}

function sz_pix_reconcile_por_transactions(): array {
    global $wpdb;
    $res = [ 'verificadas' => 0, 'confirmadas' => 0, 'sem_match' => 0, 'erros' => 0 ];

    if ( ! function_exists( 'tpc_me_token' ) || ! function_exists( 'tpc_confirmar_recarga' ) ) { sz_reconcile_log( 'skip_no_functions', [] ); return $res; }

    $recargas = $wpdb->get_results(
        "SELECT id, user_id, valor, me_pix_id, created_at FROM {$wpdb->prefix}tpc_recargas
         WHERE status IN ('pendente','analise') AND me_pix_id IS NOT NULL AND me_pix_id != ''
           AND created_at <= DATE_SUB(NOW(), INTERVAL 3 MINUTE)
           AND created_at >= DATE_SUB(NOW(), INTERVAL 29 MINUTE)
         ORDER BY created_at ASC LIMIT 20", ARRAY_A ) ?: [];

    if ( empty( $recargas ) ) { sz_reconcile_log( 'no_pending', [] ); return $res; }
    $res['verificadas'] = count( $recargas );

    $transactions = sz_pix_get_me_transactions();
    if ( $transactions === null ) { sz_reconcile_log( 'transactions_api_error', [] ); $res['erros']++; return $res; }

    // Flatten double-nested e indexa por transaction.id e payment.id
    $idx = [];
    foreach ( $transactions as $item ) {
        $items = ( is_array( $item ) && isset( $item[0] ) ) ? $item : [ $item ];
        foreach ( $items as $t ) {
            if ( ! is_array( $t ) ) continue;
            if ( ! empty( $t['id'] ) )             $idx[ (string) $t['id'] ]             = $t;
            if ( ! empty( $t['payment']['id'] ) )  $idx[ (string) $t['payment']['id'] ]  = $t;
        }
    }

    sz_reconcile_log( 'transactions_fetched', [ 'indexed' => count( $idx ) ] );

    foreach ( $recargas as $recarga ) {
        $rid = (int) $recarga['id'];
        $mpid = (string) $recarga['me_pix_id'];

        $cur = $wpdb->get_var( $wpdb->prepare( "SELECT status FROM {$wpdb->prefix}tpc_recargas WHERE id = %d", $rid ) );
        if ( $cur === 'confirmado' ) { sz_reconcile_log( 'already_confirmed', [ 'recarga_id' => $rid ] ); continue; }

        $t = $idx[ $mpid ] ?? null;
        if ( ! $t ) { foreach ( $idx as $k => $v ) { if ( strcasecmp( $k, $mpid ) === 0 ) { $t = $v; break; } } }

        if ( ! $t ) { sz_reconcile_log( 'no_match', [ 'recarga_id' => $rid, 'me_pix_id' => $mpid ] ); $res['sem_match']++; continue; }

        if ( ( $t['type'] ?? '' ) !== 'credit' || ( $t['status'] ?? '' ) !== 'authorized' ) {
            sz_reconcile_log( 'not_ready', [ 'recarga_id' => $rid, 'type' => $t['type'] ?? '', 'status' => $t['status'] ?? '' ] );
            $res['sem_match']++; continue;
        }

        if ( abs( round( (float) $recarga['valor'], 2 ) - round( (float) ( $t['value'] ?? 0 ), 2 ) ) > 0.02 ) {
            sz_reconcile_log( 'valor_mismatch', [ 'recarga_id' => $rid, 'recarga' => $recarga['valor'], 'transacao' => $t['value'] ?? 0 ] );
            $res['erros']++; continue;
        }

        $ok = tpc_confirmar_recarga( $rid, 'admin_reconciliation' );
        if ( $ok ) {
            $res['confirmadas']++;
            sz_reconcile_log( 'confirmed', [ 'recarga_id' => $rid, 'me_pix_id' => $mpid, 'valor' => $recarga['valor'], 'user_id' => $recarga['user_id'] ] );
            sz_pix_notificar_usuario( (int) $recarga['user_id'], (float) $recarga['valor'] );
        } else {
            $res['erros']++; sz_reconcile_log( 'confirm_failed', [ 'recarga_id' => $rid ] );
        }
    }
    return $res;
}

function sz_pix_get_me_transactions(): ?array {
    $cached = get_transient( 'sz_pix_me_transactions_cache' );
    if ( $cached !== false ) return $cached;

    $token = function_exists( 'tpc_me_token' ) ? tpc_me_token() : '';
    if ( ! $token ) return null;
    $base = function_exists( 'tpc_me_api_base' ) ? tpc_me_api_base() : 'https://melhorenvio.com.br/api/v2';

    $resp = wp_remote_get( $base . '/me/transactions?per_page=50', [
        'timeout' => 20,
        'headers' => [ 'Authorization' => 'Bearer ' . $token, 'Accept' => 'application/json', 'User-Agent' => 'Senderzz Logistics (suporte@app.senderzz.com.br)' ],
    ] );

    if ( is_wp_error( $resp ) ) { sz_reconcile_log( 'transactions_wp_error', [ 'error' => $resp->get_error_message() ] ); return null; }
    $code = (int) wp_remote_retrieve_response_code( $resp );
    $data = json_decode( wp_remote_retrieve_body( $resp ), true );
    if ( $code !== 200 || ! is_array( $data ) ) { sz_reconcile_log( 'transactions_http_error', [ 'code' => $code ] ); return null; }

    $list = $data['data'] ?? [];
    set_transient( 'sz_pix_me_transactions_cache', $list, 2 * MINUTE_IN_SECONDS );
    return $list;
}

function sz_pix_notificar_usuario( int $uid, float $valor ): void {
    $u = get_userdata( $uid );
    if ( ! $u || ! $u->user_email ) return;
    $vf = 'R$ ' . number_format( $valor, 2, ',', '.' );
    wp_mail( $u->user_email, "Senderzz: Recarga de {$vf} confirmada!",
        "Olá {$u->display_name},\n\nSua recarga de {$vf} foi confirmada e já está na sua carteira Senderzz.\n\nEquipe Senderzz" );
}

function sz_reconcile_log( string $tipo, array $data ): void {
    if ( function_exists( 'wc_get_logger' ) ) wc_get_logger()->info( wp_json_encode( array_merge( [ 'tipo' => $tipo ], $data ), JSON_UNESCAPED_UNICODE ), [ 'source' => 'sz-pix-reconcile' ] );
    $h = (array) get_option( 'sz_pix_reconcile_history', [] );
    $h[] = array_merge( [ 'tipo' => $tipo, 'at' => current_time( 'mysql' ) ], $data );
    if ( count( $h ) > 100 ) array_splice( $h, 0, count( $h ) - 100 );
    update_option( 'sz_pix_reconcile_history', $h, false );
}

// Painel integrado na aba PIX / Reconciliação da Carteira de Frete (tpc/admin.php).


register_deactivation_hook( defined('TPC_FILE') ? TPC_FILE : __FILE__, function(): void { wp_clear_scheduled_hook('sz_pix_auto_reconcile_cron'); } );

// ── Cron diário: checar divergência saldo carteira vs soma de transações ───────
add_action( 'init', function (): void {
    if ( ! wp_next_scheduled( 'sz_wallet_divergence_check' ) ) {
        wp_schedule_event( time(), 'daily', 'sz_wallet_divergence_check' );
    }
} );

add_action( 'sz_wallet_divergence_check', function (): void {
    global $wpdb;

    // Para cada usuário: saldo_carteira deve ser igual a SUM(credito) - SUM(debito) confirmados
    $divergentes = $wpdb->get_results(
        "SELECT c.user_id,
                c.saldo                                           AS saldo_carteira,
                COALESCE(SUM(CASE WHEN t.tipo='credito' THEN t.valor ELSE -t.valor END), 0) AS saldo_calculado,
                ABS(c.saldo - COALESCE(SUM(CASE WHEN t.tipo='credito' THEN t.valor ELSE -t.valor END), 0)) AS divergencia
           FROM {$wpdb->prefix}tpc_carteira c
           LEFT JOIN {$wpdb->prefix}tpc_transacoes t ON t.user_id = c.user_id AND t.status = 'confirmado'
          GROUP BY c.user_id, c.saldo
         HAVING divergencia > 0.01
          LIMIT 10",
        ARRAY_A
    );

    if ( ! empty( $divergentes ) ) {
        $msgs = [];
        foreach ( $divergentes as $d ) {
            $msgs[] = "user_id={$d['user_id']} carteira={$d['saldo_carteira']} calculado={$d['saldo_calculado']} diff={$d['divergencia']}";
        }
        $log_msg = '[tpc_wallet_divergence] Divergência detectada: ' . implode( ' | ', $msgs );
        error_log( $log_msg );
        if ( function_exists( 'wc_get_logger' ) ) {
            wc_get_logger()->error( $log_msg, [ 'source' => 'tp-carteira-audit' ] );
        }
        update_option( 'sz_wallet_divergence_last', [ 'at' => current_time( 'mysql' ), 'items' => $divergentes ], false );
    } else {
        delete_option( 'sz_wallet_divergence_last' );
    }
} );

// ── Observabilidade: admin notice se cron PIX parou há mais de 15 min ─────────
add_action( 'admin_notices', function (): void {
    if ( ! current_user_can( 'manage_options' ) ) return;

    $last = get_option( 'sz_pix_reconcile_last_run' );
    if ( ! $last || empty( $last['at'] ) ) return; // nunca rodou = instalação nova, silencioso

    $last_ts = strtotime( (string) $last['at'] );
    $age_min  = ( time() - $last_ts ) / 60;

    if ( $age_min >= 15 ) {
        $age_fmt = $age_min >= 60
            ? number_format( $age_min / 60, 1 ) . 'h'
            : (int) $age_min . ' min';
        echo '<div class="notice notice-error"><p>'
            . '<strong>⚠️ Senderzz:</strong> Cron de reconciliação PIX não roda há <strong>' . esc_html( $age_fmt ) . '</strong>. '
            . 'Verifique se o WP-Cron está ativo ou configure um cron real no servidor. '
            . 'Último run: ' . esc_html( wp_date( 'd/m/Y H:i', $last_ts ) ) . '.'
            . '</p></div>';
    }

    // Divergência saldo: alerta se última reconciliação registrou erros críticos
    if ( ! empty( $last['result']['errors'] ) && is_array( $last['result']['errors'] ) ) {
        $n = count( $last['result']['errors'] );
        if ( $n > 0 ) {
            echo '<div class="notice notice-warning"><p>'
                . '<strong>Senderzz PIX:</strong> Última reconciliação registrou ' . (int) $n . ' erro(s). '
                . 'Verifique os logs em <code>wp-content/uploads/wc-logs/sz-pix-reconcile-*.log</code>.'
                . '</p></div>';
        }
    }

    // Divergência carteira vs ledger
    $div = get_option( 'sz_wallet_divergence_last' );
    if ( ! empty( $div['items'] ) ) {
        $n = count( $div['items'] );
        echo '<div class="notice notice-error"><p>'
            . '<strong>⚠️ Senderzz Carteira:</strong> ' . (int) $n . ' usuário(s) com divergência entre saldo em tpc_carteira e soma de tpc_transacoes. '
            . 'Detectado em: ' . esc_html( $div['at'] ?? '' ) . '. '
            . 'Verifique <code>wp-content/uploads/wc-logs/tp-carteira-audit-*.log</code> e o RUNBOOK.md.'
            . '</p></div>';
    }
} );
