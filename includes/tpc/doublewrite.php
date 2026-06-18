<?php
/**
 * Double-write adapter — Carteira + PIX (Fase 2 migração strangler fig).
 *
 * Durante a janela de migração, escritas críticas da carteira são replicadas
 * de forma assíncrona para o serviço Go via HTTP. O MySQL/WordPress continua
 * sendo o sistema de registro principal (source of truth). Falhas na
 * replicação Go são registradas em log mas NÃO bloqueiam a operação —
 * fail-open por design nessa fase.
 *
 * Após cutover confirmado via infra/scripts/verify-migration.sh, remover
 * este arquivo e os hooks abaixo.
 *
 * Variável de ambiente: WALLET_GO_URL=http://go-wallet:8081
 * Se ausente: double-write desativado silenciosamente.
 *
 * Segurança: autenticação via HMAC-SHA256 no header X-Internal-Sig,
 * usando a variável de ambiente WALLET_INTERNAL_SECRET. Se o secret
 * não estiver configurado, o double-write é desativado com aviso em log.
 */

if ( ! function_exists( 'tpc_dw_enabled' ) ) :

/**
 * Verifica se o double-write está ativado (WALLET_GO_URL configurado).
 *
 * @return bool true se WALLET_GO_URL estiver definida como URL válida.
 */
function tpc_dw_enabled(): bool {
    $url = getenv( 'WALLET_GO_URL' );
    return ! empty( $url ) && filter_var( $url, FILTER_VALIDATE_URL ) !== false;
}

endif;

if ( ! function_exists( 'tpc_dw_base_url' ) ) :

/**
 * Retorna a URL base do serviço Go de carteira (sem barra final).
 *
 * @return string URL base ex: "http://go-wallet:8081"
 */
function tpc_dw_base_url(): string {
    return rtrim( (string) getenv( 'WALLET_GO_URL' ), '/' );
}

endif;

/**
 * Envia payload para o serviço Go via wp_remote_post não-bloqueante (timeout 3s).
 * Assina o payload com HMAC-SHA256 usando WALLET_INTERNAL_SECRET.
 *
 * @param string $path   Caminho relativo, ex: /internal/transacoes
 * @param array  $body   Dados a enviar como JSON
 * @return bool          true se 2xx, false em caso de erro/timeout/secret ausente
 */
if ( ! function_exists( 'tpc_dw_post' ) ) :

function tpc_dw_post( string $path, array $body ): bool {
    if ( ! tpc_dw_enabled() ) return false;

    $secret = (string) getenv( 'WALLET_INTERNAL_SECRET' );
    if ( $secret === '' ) {
        error_log( '[tpc_dw] WALLET_INTERNAL_SECRET não configurado — double-write ignorado.' );
        return false;
    }

    $url     = tpc_dw_base_url() . $path;
    $payload = wp_json_encode( $body );
    $hmac    = hash_hmac( 'sha256', $payload, $secret );

    $response = wp_remote_post( $url, [
        'timeout'     => 3,
        'blocking'    => false,
        'headers'     => [
            'Content-Type'   => 'application/json',
            'X-Internal-Sig' => $hmac,
            'X-Source'       => 'wp-doublewrite-wallet',
        ],
        'body'        => $payload,
        'data_format' => 'body',
    ] );

    if ( is_wp_error( $response ) ) {
        error_log( '[tpc_dw] Erro replicando ' . $path . ': ' . $response->get_error_message() );
        return false;
    }

    $code = (int) wp_remote_retrieve_response_code( $response );
    if ( $code < 200 || $code >= 300 ) {
        error_log( '[tpc_dw] HTTP ' . $code . ' ao replicar ' . $path );
        return false;
    }

    return true;
}

endif;

// ── Hooks de double-write ────────────────────────────────────────────────────

/**
 * Replica inserção de transação na carteira para o serviço Go.
 *
 * Chamado após INSERT bem-sucedido em wp_tpc_transacoes.
 * O Go usa ON CONFLICT (user_id, referencia, tipo) DO NOTHING para idempotência.
 *
 * Hook: do_action( 'tpc_transacao_inserida', $transacao_id, $user_id, $tipo, $dados );
 *
 * @param int    $transacao_id ID gerado pelo MySQL
 * @param int    $user_id      WP user_id do dono da carteira
 * @param string $tipo         'credito', 'debito' ou 'reserva'
 * @param array  $dados        Demais campos da transação (referencia, valor, descricao, etc.)
 */
if ( ! function_exists( 'tpc_dw_on_transacao_inserida' ) ) :

function tpc_dw_on_transacao_inserida( int $transacao_id, int $user_id, string $tipo, array $dados ): void {
    if ( ! tpc_dw_enabled() ) return;

    tpc_dw_post( '/internal/transacoes', array_merge( $dados, [
        '_action'      => 'insert',
        'id'           => $transacao_id,
        'user_id'      => $user_id,
        'tipo'         => $tipo,
        'ts'           => gmdate( 'Y-m-d H:i:s' ),
    ] ) );
}

endif;

add_action( 'tpc_transacao_inserida', 'tpc_dw_on_transacao_inserida', 10, 4 );

/**
 * Replica criação de recarga PIX para o serviço Go.
 *
 * Chamado após INSERT bem-sucedido em wp_tpc_recargas.
 * O Go usa ON CONFLICT (me_pix_id) DO NOTHING para idempotência.
 *
 * Hook: do_action( 'tpc_recarga_criada', $recarga_id, $user_id, $dados );
 *
 * @param int   $recarga_id ID gerado pelo MySQL
 * @param int   $user_id    WP user_id do solicitante
 * @param array $dados      Demais campos da recarga (valor, me_pix_id, pix_qr, etc.)
 */
if ( ! function_exists( 'tpc_dw_on_recarga_criada' ) ) :

function tpc_dw_on_recarga_criada( int $recarga_id, int $user_id, array $dados ): void {
    if ( ! tpc_dw_enabled() ) return;

    tpc_dw_post( '/internal/recargas', array_merge( $dados, [
        '_action'   => 'create',
        'id'        => $recarga_id,
        'user_id'   => $user_id,
        'status'    => $dados['status'] ?? 'pendente',
        'ts'        => gmdate( 'Y-m-d H:i:s' ),
    ] ) );
}

endif;

add_action( 'tpc_recarga_criada', 'tpc_dw_on_recarga_criada', 10, 3 );

/**
 * Replica confirmação de recarga PIX para o serviço Go.
 *
 * Chamado após UPDATE status='confirmado' em wp_tpc_recargas.
 * O endpoint Go atualiza a linha e dispara o crédito na carteira Postgres.
 *
 * Hook: do_action( 'tpc_recarga_confirmada', $recarga_id, $user_id, $valor );
 *
 * @param int   $recarga_id ID da recarga confirmada
 * @param int   $user_id    WP user_id do dono
 * @param float $valor      Valor confirmado (BRL)
 */
if ( ! function_exists( 'tpc_dw_on_recarga_confirmada' ) ) :

function tpc_dw_on_recarga_confirmada( int $recarga_id, int $user_id, float $valor ): void {
    if ( ! tpc_dw_enabled() ) return;

    tpc_dw_post( '/internal/recargas/' . $recarga_id . '/confirmar', [
        '_action'   => 'confirmar',
        'recarga_id' => $recarga_id,
        'user_id'   => $user_id,
        'valor'     => number_format( $valor, 2, '.', '' ),
        'ts'        => gmdate( 'Y-m-d H:i:s' ),
    ] );
}

endif;

add_action( 'tpc_recarga_confirmada', 'tpc_dw_on_recarga_confirmada', 10, 3 );

/**
 * Replica atualização de saldo da carteira para o serviço Go.
 *
 * Chamado após qualquer UPDATE em wp_tpc_carteira que altere saldo e/ou
 * saldo_reservado. O Go faz UPSERT com os valores recebidos (estado atual
 * do MySQL — eventual consistency durante a janela de migração).
 *
 * Hook: do_action( 'tpc_saldo_atualizado', $user_id, $saldo, $saldo_reservado );
 *
 * @param int   $user_id          WP user_id
 * @param float $saldo            Saldo total atualizado
 * @param float $saldo_reservado  Saldo reservado atualizado
 */
if ( ! function_exists( 'tpc_dw_on_saldo_atualizado' ) ) :

function tpc_dw_on_saldo_atualizado( int $user_id, float $saldo, float $saldo_reservado ): void {
    if ( ! tpc_dw_enabled() ) return;

    tpc_dw_post( '/internal/carteira/' . $user_id, [
        '_action'         => 'upsert',
        'user_id'         => $user_id,
        'saldo'           => number_format( $saldo, 2, '.', '' ),
        'saldo_reservado' => number_format( $saldo_reservado, 2, '.', '' ),
        'ts'              => gmdate( 'Y-m-d H:i:s' ),
    ] );
}

endif;

add_action( 'tpc_saldo_atualizado', 'tpc_dw_on_saldo_atualizado', 10, 3 );
