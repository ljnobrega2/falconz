<?php
/**
 * Double-write adapter — Motoboy (Fase 1 migração strangler fig).
 *
 * Durante a janela de migração, escritas críticas do módulo motoboy são
 * replicadas de forma assíncrona (wp_schedule_single_event) para o serviço
 * Go via HTTP. O WP continua sendo o sistema de registro principal (source of
 * truth). Falhas na replicação Go são registradas em log mas NÃO bloqueiam a
 * operação — fail-open por design nessa fase.
 *
 * Após cutover confirmado via go/infra/scripts/verify-migration.sh, remover
 * este arquivo e os hooks abaixo. A coluna `synced_go` pode ser dropada.
 *
 * Variável de ambiente: MOTOBOY_GO_URL=http://go-motoboy:8082
 * Se ausente: double-write desativado silenciosamente.
 */

if ( ! function_exists( 'sz_mb_dw_enabled' ) ) :

function sz_mb_dw_enabled(): bool {
    $url = getenv( 'MOTOBOY_GO_URL' );
    return ! empty( $url ) && filter_var( $url, FILTER_VALIDATE_URL ) !== false;
}

endif;

if ( ! function_exists( 'sz_mb_dw_base_url' ) ) :

function sz_mb_dw_base_url(): string {
    return rtrim( (string) getenv( 'MOTOBOY_GO_URL' ), '/' );
}

endif;

/**
 * Envia payload para o serviço Go via wp_remote_post não-bloqueante (timeout 3s).
 *
 * @param string $path   Caminho relativo, ex: /internal/pedidos
 * @param array  $body   Dados a enviar como JSON
 * @return bool          true se 2xx, false em caso de erro/timeout
 */
if ( ! function_exists( 'sz_mb_dw_post' ) ) :

function sz_mb_dw_post( string $path, array $body ): bool {
    if ( ! sz_mb_dw_enabled() ) return false;

    $secret = (string) getenv( 'MOTOBOY_INTERNAL_SECRET' );
    if ( $secret === '' ) {
        error_log( '[senderzz_motoboy_dw] MOTOBOY_INTERNAL_SECRET não configurado — double-write ignorado.' );
        return false;
    }

    $url      = sz_mb_dw_base_url() . $path;
    $payload  = wp_json_encode( $body );
    $hmac     = hash_hmac( 'sha256', $payload, $secret );

    $response = wp_remote_post( $url, [
        'timeout'     => 3,
        'blocking'    => false,
        'headers'     => [
            'Content-Type'        => 'application/json',
            'X-Internal-Sig'      => $hmac,
            'X-Source'            => 'wp-doublewrite',
        ],
        'body'        => $payload,
        'data_format' => 'body',
    ] );

    if ( is_wp_error( $response ) ) {
        error_log( '[senderzz_motoboy_dw] Erro replicando ' . $path . ': ' . $response->get_error_message() );
        return false;
    }

    $code = (int) wp_remote_retrieve_response_code( $response );
    if ( $code < 200 || $code >= 300 ) {
        error_log( '[senderzz_motoboy_dw] HTTP ' . $code . ' em ' . $path );
        return false;
    }

    return true;
}

endif;

// ── Hooks de double-write ────────────────────────────────────────────────────

/**
 * Replica criação de pedido motoboy para o serviço Go.
 * Chamado por sz_motoboy_criar_pedido() após insert bem-sucedido no MySQL.
 *
 * @param int   $pedido_id   ID no MySQL
 * @param int   $wc_order_id ID do pedido WooCommerce
 * @param array $row         Dados inseridos (mesma estrutura do insert MySQL)
 */
if ( ! function_exists( 'sz_mb_dw_on_pedido_criado' ) ) :

function sz_mb_dw_on_pedido_criado( int $pedido_id, int $wc_order_id, array $row ): void {
    if ( ! sz_mb_dw_enabled() ) return;

    sz_mb_dw_post( '/internal/pedidos', array_merge( $row, [
        'id'          => $pedido_id,
        'wc_order_id' => $wc_order_id,
        '_action'     => 'create',
    ] ) );
}

endif;

add_action( 'sz_motoboy_pedido_criado', 'sz_mb_dw_on_pedido_criado', 10, 3 );

/**
 * Replica mudança de status para o serviço Go.
 * Assinatura espelha o do_action em router.php:697:
 *   do_action( 'sz_motoboy_status_changed', $pedido_id, $de_status, $novo_status, $pedido )
 *
 * @param int       $pedido_id  ID do pedido
 * @param string    $status_old Status anterior
 * @param string    $status_new Novo status
 * @param object    $pedido     Row do pedido (já recarregado após update)
 */
if ( ! function_exists( 'sz_mb_dw_on_status_changed' ) ) :

function sz_mb_dw_on_status_changed( int $pedido_id, string $status_old, string $status_new, object $pedido ): void {
    if ( ! sz_mb_dw_enabled() ) return;

    sz_mb_dw_post( '/internal/pedidos/' . $pedido_id . '/status', [
        '_action'    => 'status_change',
        'pedido_id'  => $pedido_id,
        'status_old' => $status_old,
        'status_new' => $status_new,
        'motoboy_id' => isset( $pedido->motoboy_id ) ? (int) $pedido->motoboy_id : null,
        'ts'         => gmdate( 'Y-m-d H:i:s' ),
    ] );
}

endif;

add_action( 'sz_motoboy_status_changed', 'sz_mb_dw_on_status_changed', 10, 4 );

/**
 * Replica troca de motoboy para o serviço Go.
 *
 * @param int      $pedido_id  ID do pedido
 * @param int|null $motoboy_id Novo motoboy_id (null = sem motoboy)
 * @param int      $actor_id   user_id WP responsável
 */
if ( ! function_exists( 'sz_mb_dw_on_motoboy_trocado' ) ) :

function sz_mb_dw_on_motoboy_trocado( int $pedido_id, ?int $motoboy_id, int $actor_id = 0 ): void {
    if ( ! sz_mb_dw_enabled() ) return;

    sz_mb_dw_post( '/internal/pedidos/' . $pedido_id . '/motoboy', [
        '_action'    => 'motoboy_change',
        'pedido_id'  => $pedido_id,
        'motoboy_id' => $motoboy_id,
        'actor_id'   => $actor_id,
        'ts'         => gmdate( 'Y-m-d H:i:s' ),
    ] );
}

endif;

add_action( 'sz_motoboy_motoboy_trocado', 'sz_mb_dw_on_motoboy_trocado', 10, 3 );

/**
 * Replica comprovante de entrega (photo/assinatura) para o serviço Go.
 * Envia apenas o ID do comprovante, não o binário (Go busca via internal API se precisar).
 *
 * @param int $comprovante_id ID na tabela sz_motoboy_comprovantes
 * @param int $pedido_id      ID do pedido
 */
if ( ! function_exists( 'sz_mb_dw_on_comprovante' ) ) :

function sz_mb_dw_on_comprovante( int $comprovante_id, int $pedido_id ): void {
    if ( ! sz_mb_dw_enabled() ) return;

    sz_mb_dw_post( '/internal/comprovantes', [
        '_action'        => 'comprovante_create',
        'comprovante_id' => $comprovante_id,
        'pedido_id'      => $pedido_id,
        'ts'             => gmdate( 'Y-m-d H:i:s' ),
    ] );
}

endif;

add_action( 'sz_motoboy_comprovante_salvo', 'sz_mb_dw_on_comprovante', 10, 2 );
