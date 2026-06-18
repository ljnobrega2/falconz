<?php
/**
 * Webhook.php
 *
 * Módulo de rastreio por webhook.
 *
 * Fluxo:
 *  1. ME dispara evento de status → POST /wp-json/senderzz/v1/tracking/inbound
 *  2. Senderzz atualiza status do pedido WooCommerce
 *  3. Dispara webhooks de saída para cada OL vinculado ao pedido
 *
 * Campos enviados ao OL:
 *  order_id, order_number, tracking_code, carrier, status, status_label,
 *  status_date, city, state — SEM: custo, margem, saldo.
 */

namespace WC_MelhorEnvio\Webhook;

defined( 'ABSPATH' ) || exit;

class Tracking_Webhook {

    public function __construct() {
        add_action( 'rest_api_init', [ $this, 'register_routes' ] );
        // DISABLED: substituído pelo senderzz-producer-webhooks.php que usa payload completo
        // add_action( 'woocommerce_order_status_changed', [ $this, 'on_order_status_changed' ], 20, 4 );
    }

    /* ── Rotas REST ──────────────────────────────────────────────────── */

    public function register_routes(): void {
        $ns = 'senderzz/v1';

        // Entrada: recebe evento do Melhor Envio
        register_rest_route( $ns, '/tracking/inbound', [
            'methods'             => 'POST',
            'callback'            => [ $this, 'handle_inbound' ],
            'permission_callback' => '__return_true', // validado via HMAC internamente
        ] );

        // Configuração de webhook do OL (portal auth)
        register_rest_route( $ns, '/webhooks', [
            'methods'             => 'GET',
            'callback'            => [ $this, 'list_webhooks' ],
            'permission_callback' => [ $this, 'portal_auth' ],
        ] );
        register_rest_route( $ns, '/webhooks', [
            'methods'             => 'POST',
            'callback'            => [ $this, 'save_webhook' ],
            'permission_callback' => [ $this, 'portal_auth' ],
        ] );
        register_rest_route( $ns, '/webhooks/(?P<id>\d+)', [
            'methods'             => 'DELETE',
            'callback'            => [ $this, 'delete_webhook' ],
            'permission_callback' => [ $this, 'portal_auth' ],
        ] );
        register_rest_route( $ns, '/webhooks/(?P<id>\d+)/test', [
            'methods'             => 'POST',
            'callback'            => [ $this, 'test_webhook' ],
            'permission_callback' => [ $this, 'portal_auth' ],
        ] );
        register_rest_route( $ns, '/webhooks/(?P<id>\d+)/logs', [
            'methods'             => 'GET',
            'callback'            => [ $this, 'webhook_logs' ],
            'permission_callback' => [ $this, 'portal_auth' ],
        ] );
    }

    /* ── Autenticação portal ─────────────────────────────────────────── */

    public function portal_auth(): bool|int {
        global $wpdb;
        $token = sanitize_text_field(
            $_COOKIE['senderzz_portal_session'] ?? ( $_SERVER['HTTP_X_SENDERZZ_TOKEN'] ?? '' )
        );
        if ( ! $token ) return false;

        // V-SEC-01: sessões novas armazenam HMAC do token — comparar raw+hash (suporte a sessões legacy)
        $token_hash = \WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token( $token );
        $sess = $wpdb->get_row( $wpdb->prepare(
            "SELECT s.user_id FROM {$wpdb->prefix}senderzz_portal_sessions s
             WHERE s.token IN (%s,%s) AND s.expires_at > NOW() LIMIT 1",
            $token, $token_hash
        ) );
        if ( ! $sess ) return false;

        $user = $wpdb->get_row( $wpdb->prepare(
            "SELECT id, status FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d AND status = 'active'",
            $sess->user_id
        ) );
        if ( ! $user ) return false;

        return (int) $user->id;
    }

    private function portal_user_id(): int {
        return (int) $this->portal_auth();
    }

    /* ── Webhook de entrada (Melhor Envio → Senderzz) ────────────────── */

    public function handle_inbound( \WP_REST_Request $request ): \WP_REST_Response {
        $secret = get_option( 'tpc_webhook_secret', '' );

        // CRIT-03: fail-closed. Tracking inbound altera status de pedido —
        // não pode aceitar request sem assinatura.
        if ( ! $secret ) {
            return new \WP_REST_Response( [ 'error' => 'Webhook secret não configurado.' ], 503 );
        }

        $sig  = $request->get_header( 'x-melhorenvio-signature' ) ?: $request->get_header( 'x-signature' );
        $body = $request->get_body();
        $expected = 'sha256=' . hash_hmac( 'sha256', $body, $secret );
        if ( ! hash_equals( $expected, (string) $sig ) ) {
            return new \WP_REST_Response( [ 'error' => 'Assinatura inválida.' ], 401 );
        }

        $payload = $request->get_json_params();
        if ( empty( $payload ) ) {
            return new \WP_REST_Response( [ 'error' => 'Payload vazio.' ], 400 );
        }

        // Localiza pedido pelo me_order_id ou tracking
        $order = $this->find_order_by_payload( $payload );
        if ( ! $order ) {
            return new \WP_REST_Response( [ 'ok' => true, 'note' => 'Pedido não encontrado — ignorado.' ], 200 );
        }

        // Atualiza status/rastreio
        $new_status  = $this->map_me_status( $payload['status'] ?? $payload['situacao'] ?? '' );
        $status_date = $payload['updated_at'] ?? $payload['data'] ?? current_time( 'mysql', true );
        $tracking    = $payload['tracking'] ?? $payload['codigo'] ?? '';

        if ( $tracking ) {
            $this->update_tracking_code( $order, $tracking );
        }
        if ( $new_status ) {
            $order->update_status( $new_status, 'Rastreio ME: ' . ( $payload['status'] ?? '' ) );
        }
        $order->update_meta_data( '_senderzz_last_tracking_event', wp_json_encode( $payload ) );
        $order->save();

        // Dispara saída (via hook já que status mudou — mas também dispara aqui para garantir)
        $this->fire_outbound_webhooks( $order, $payload['status'] ?? 'status_changed', $payload );

        return new \WP_REST_Response( [ 'ok' => true ], 200 );
    }

    private function find_order_by_payload( array $payload ): ?\WC_Order {
        global $wpdb;

        // S12: usar helpers HPOS-safe — $wpdb->postmeta direto quebra sob WC HPOS
        $me_id = $payload['order_id'] ?? $payload['protocol'] ?? '';
        if ( $me_id && function_exists( 'senderzz_hpos_find_order_by_meta' ) ) {
            $order = senderzz_hpos_find_order_by_meta( '_melhor_envio_order_id', (string) $me_id );
            if ( $order ) return $order;
        } elseif ( $me_id ) {
            $order_id = $wpdb->get_var( $wpdb->prepare(
                "SELECT post_id FROM {$wpdb->postmeta} WHERE meta_key = '_melhor_envio_order_id' AND meta_value = %s LIMIT 1",
                $me_id
            ) );
            if ( $order_id ) return wc_get_order( $order_id ) ?: null;
        }

        $tracking = $payload['tracking'] ?? $payload['codigo'] ?? '';
        if ( $tracking && function_exists( 'senderzz_hpos_find_order_by_meta_like' ) ) {
            $order = senderzz_hpos_find_order_by_meta_like( [ '_melhor_envio_tracking_codes', '_melhor_envio_tracking' ], $tracking );
            if ( $order ) return $order;
        } elseif ( $tracking ) {
            $order_id = $wpdb->get_var( $wpdb->prepare(
                "SELECT post_id FROM {$wpdb->postmeta}
                 WHERE meta_key IN ('_melhor_envio_tracking_codes','_melhor_envio_tracking')
                 AND meta_value LIKE %s LIMIT 1",
                '%' . $wpdb->esc_like( $tracking ) . '%'
            ) );
            if ( $order_id ) return wc_get_order( $order_id ) ?: null;
        }

        return null;
    }

    private function map_me_status( string $me_status ): string {
        $map = [
            'posted'           => 'wc-enviado',
            'in_transit'       => 'wc-acaminho',
            'delivered'        => 'wc-entregue',
            'collecting'       => 'wc-emretirada',
            'collected'        => 'wc-coletado',
            'lost'             => 'wc-extravio',
            'canceled'         => 'wc-cancelled',
            'cancelled'        => 'wc-cancelled',
            // PT
            'postado'          => 'wc-enviado',
            'em_transito'      => 'wc-acaminho',
            'entregue'         => 'wc-entregue',
        ];
        return $map[ strtolower( $me_status ) ] ?? '';
    }

    private function update_tracking_code( \WC_Order $order, string $code ): void {
        $codes = $order->get_meta( '_melhor_envio_tracking_codes' );
        if ( ! is_array( $codes ) ) $codes = [];
        $exists = false;
        foreach ( $codes as $c ) {
            if ( ( is_array( $c ) ? ( $c['tracking'] ?? '' ) : $c ) === $code ) { $exists = true; break; }
        }
        if ( ! $exists ) {
            $codes[] = [ 'tracking' => $code, 'added_at' => current_time( 'mysql', true ) ];
            $order->update_meta_data( '_melhor_envio_tracking_codes', $codes );
        }
    }

    /* ── Webhook de saída (Senderzz → OL) ───────────────────────────── */

    public function on_order_status_changed( int $order_id, string $old, string $new, \WC_Order $order ): void {
        $last_event = $order->get_meta( '_senderzz_last_tracking_event' );
        $payload    = $last_event ? json_decode( $last_event, true ) : [];
        $this->fire_outbound_webhooks( $order, 'order_status_' . $new, $payload );
    }

    public function fire_outbound_webhooks( \WC_Order $order, string $event, array $me_payload = [] ): void {
        global $wpdb;

        $order_id = $order->get_id();

        // Resolve o portal_user_id a partir da classe do pedido
        $class_id = (int) $order->get_meta( '_senderzz_product_shipping_class_id', true );
        if ( ! $class_id ) return;

        $portal_user_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}senderzz_portal_users WHERE shipping_class_id = %d AND status = 'active' LIMIT 1",
            $class_id
        ) );
        if ( ! $portal_user_id ) return;

        $hooks = $wpdb->get_results( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}senderzz_webhooks WHERE portal_user_id = %d AND active = 1 AND url != ''",
            $portal_user_id
        ) );
        if ( empty( $hooks ) ) return;

        // DT-CODE-02: filtrar webhooks que aceitam o evento atual.
        // Vazio = aceita tudo (comportamento anterior, preserva compatibilidade).
        $hooks = array_filter( $hooks, static function( $wh ) use ( $event ): bool {
            $types = isset( $wh->event_types ) ? trim( (string) $wh->event_types ) : '';
            if ( $types === '' ) return true;
            $list = json_decode( $types, true );
            return is_array( $list ) && in_array( $event, $list, true );
        } );
        if ( empty( $hooks ) ) return;

        // Monta payload uma vez para todos os hooks
        $safe_payload = $this->build_payload( $order, $event, $me_payload );

        foreach ( $hooks as $wh ) {
            // v-scale: dispatch assíncrono via Action Scheduler.
            // Evita bloquear o PHP por N×8s quando endpoints dos produtores estão lentos.
            if ( function_exists( 'as_enqueue_async_action' ) ) {
                as_enqueue_async_action(
                    'senderzz_webhook_dispatch_async',
                    [
                        'webhook_id' => (int) $wh->id,
                        'order_id'   => $order_id,
                        'event'      => $event,
                        'payload'    => $safe_payload,
                    ],
                    'senderzz-webhooks'
                );
            } else {
                // Fallback síncrono se Action Scheduler não estiver disponível
                $this->dispatch( $wh, $safe_payload, $order_id, $event );
            }
        }
    }

    /**
     * Monta o payload do webhook (extraído de fire_outbound_webhooks para reuso).
     */
    private function build_payload( \WC_Order $order, string $event, array $me_payload = [] ): array {
        $tracking_codes = function_exists( 'senderzz_portal_tracking_codes' ) ? senderzz_portal_tracking_codes( $order ) : [];
        $items = [];
        foreach ( $order->get_items() as $item ) {
            $items[] = [
                'name'     => $item->get_name(),
                'quantity' => $item->get_quantity(),
                'total'    => (float) $item->get_total(),
            ];
        }
        return [
            'event'          => $event,
            'order_id'       => $order->get_id(),
            'order_number'   => $order->get_order_number(),
            'status'         => $order->get_status(),
            'total'          => (float) $order->get_total(),
            'shipping_total' => (float) $order->get_shipping_total(),
            'customer'       => [
                'name'  => $order->get_billing_first_name() . ' ' . $order->get_billing_last_name(),
                'email' => $order->get_billing_email(),
                'phone' => $order->get_billing_phone(),
                'cpf'   => $order->get_meta( '_billing_cpf' ) ?: $order->get_meta( 'billing_cpf' ) ?: '',
            ],
            'shipping' => [
                'address'  => $order->get_shipping_address_1(),
                'city'     => $order->get_shipping_city(),
                'state'    => $order->get_shipping_state(),
                'postcode' => $order->get_shipping_postcode(),
            ],
            'tracking_codes' => $tracking_codes,
            'items'          => $items,
            'me_payload'     => $me_payload,
            'timestamp'      => current_time( 'c' ),
        ];
    }


    private function dispatch( object $wh, array $payload, int $order_id, string $event ): void {
        global $wpdb;
        $body = wp_json_encode( $payload );
        $headers = [
            'Content-Type'         => 'application/json',
            'X-Senderzz-Event'     => $event,
            'X-Senderzz-Delivery'  => wp_generate_uuid4(),
        ];
        if ( $wh->secret ) {
            $headers['X-Senderzz-Signature'] = 'sha256=' . hash_hmac( 'sha256', $body, $wh->secret );
        }

        // SEC-02: usar wrapper SSRF-safe que re-valida DNS no momento da chamada.
        // Fallback para wp_safe_remote_post (sem validação DNS adicional) mas NUNCA para wp_remote_post puro.
        $response = function_exists( 'senderzz_safe_outbound_post' )
            ? senderzz_safe_outbound_post( $wh->url, [
                'body'    => $body,
                'headers' => $headers,
                'timeout' => 8,
            ] )
            : wp_safe_remote_post( $wh->url, [
                'body'    => $body,
                'headers' => $headers,
                'timeout' => 8,
            ] );

        $code         = is_wp_error( $response ) ? 0 : wp_remote_retrieve_response_code( $response );
        $response_body = is_wp_error( $response ) ? $response->get_error_message() : substr( wp_remote_retrieve_body( $response ), 0, 500 );

        $wpdb->insert( $wpdb->prefix . 'senderzz_webhook_log', [
            'webhook_id'    => (int) $wh->id,
            'order_id'      => $order_id,
            'event'         => $event,
            'payload_json'  => $body,
            'response_code' => $code,
            'response_body' => $response_body,
            'fired_at'      => current_time( 'mysql', true ),
        ] );
    }

    /* ── CRUD webhooks (portal) ──────────────────────────────────────── */

    public function list_webhooks( \WP_REST_Request $request ): \WP_REST_Response {
        global $wpdb;
        $uid = $this->portal_user_id();
        $rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT id, url, active, created_at FROM {$wpdb->prefix}senderzz_webhooks WHERE portal_user_id = %d ORDER BY id DESC",
            $uid
        ), ARRAY_A );
        return new \WP_REST_Response( [ 'success' => true, 'webhooks' => $rows ?: [] ], 200 );
    }

    public function save_webhook( \WP_REST_Request $request ): \WP_REST_Response {
        global $wpdb;
        $uid    = $this->portal_user_id();
        $url    = esc_url_raw( $request->get_param( 'url' ) ?: '' );
        $secret = sanitize_text_field( $request->get_param( 'secret' ) ?: '' );

        if ( ! $url || ! filter_var( $url, FILTER_VALIDATE_URL ) ) {
            return new \WP_REST_Response( [ 'success' => false, 'message' => 'URL inválida.' ], 400 );
        }

        // Máximo 3 webhooks por usuário
        $count = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COUNT(*) FROM {$wpdb->prefix}senderzz_webhooks WHERE portal_user_id = %d",
            $uid
        ) );
        if ( $count >= 3 ) {
            return new \WP_REST_Response( [ 'success' => false, 'message' => 'Limite de 3 webhooks atingido.' ], 400 );
        }

        $wpdb->insert( $wpdb->prefix . 'senderzz_webhooks', [
            'portal_user_id' => $uid,
            'url'            => $url,
            'secret'         => $secret,
            'active'         => 1,
        ] );

        return new \WP_REST_Response( [ 'success' => true, 'id' => (int) $wpdb->insert_id ], 201 );
    }

    public function delete_webhook( \WP_REST_Request $request ): \WP_REST_Response {
        global $wpdb;
        $uid = $this->portal_user_id();
        $id  = absint( $request->get_param( 'id' ) );
        $ok  = $wpdb->delete( $wpdb->prefix . 'senderzz_webhooks', [ 'id' => $id, 'portal_user_id' => $uid ] );
        return new \WP_REST_Response( [ 'success' => (bool) $ok ], $ok ? 200 : 404 );
    }

    public function test_webhook( \WP_REST_Request $request ): \WP_REST_Response {
        global $wpdb;
        $uid = $this->portal_user_id();
        $id  = absint( $request->get_param( 'id' ) );
        $wh  = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}senderzz_webhooks WHERE id = %d AND portal_user_id = %d",
            $id, $uid
        ) );
        if ( ! $wh ) return new \WP_REST_Response( [ 'success' => false, 'message' => 'Webhook não encontrado.' ], 404 );

        $test_payload = [
            'event'          => 'test',
            'fired_at'       => current_time( 'mysql', true ),
            'order_id'       => 0,
            'order_number'   => 'TESTE',
            'status'         => 'wc-enviado',
            'tracking_codes' => [ 'AA123456789BR' ],
            'carrier'        => 'Jadlog',
            'destination'    => [ 'city' => 'São Paulo', 'state' => 'SP' ],
            'me_event'       => [ 'status' => 'posted', 'description' => 'Objeto postado', 'date' => current_time( 'mysql', true ) ],
        ];

        $body    = wp_json_encode( $test_payload );
        $headers = [ 'Content-Type' => 'application/json', 'X-Senderzz-Event' => 'test' ];
        if ( $wh->secret ) {
            $headers['X-Senderzz-Signature'] = 'sha256=' . hash_hmac( 'sha256', $body, $wh->secret );
        }

        // SEC-02: usar wrapper SSRF-safe que re-valida DNS no momento da chamada
        $response = function_exists( 'senderzz_safe_outbound_post' )
            ? senderzz_safe_outbound_post( $wh->url, [ 'body' => $body, 'headers' => $headers, 'timeout' => 8 ] )
            : wp_safe_remote_post( $wh->url, [ 'body' => $body, 'headers' => $headers, 'timeout' => 8 ] );
        $code     = is_wp_error( $response ) ? 0 : wp_remote_retrieve_response_code( $response );

        return new \WP_REST_Response( [
            'success'       => $code >= 200 && $code < 300,
            'response_code' => $code,
            'payload_sent'  => $test_payload,
        ], 200 );
    }

    public function webhook_logs( \WP_REST_Request $request ): \WP_REST_Response {
        global $wpdb;
        $uid = $this->portal_user_id();
        $id  = absint( $request->get_param( 'id' ) );

        // Verifica ownership
        $owns = $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}senderzz_webhooks WHERE id = %d AND portal_user_id = %d",
            $id, $uid
        ) );
        if ( ! $owns ) return new \WP_REST_Response( [ 'success' => false ], 404 );

        $logs = $wpdb->get_results( $wpdb->prepare(
            "SELECT id, order_id, event, response_code, response_body, fired_at
             FROM {$wpdb->prefix}senderzz_webhook_log
             WHERE webhook_id = %d ORDER BY fired_at DESC LIMIT 50",
            $id
        ), ARRAY_A );

        return new \WP_REST_Response( [ 'success' => true, 'logs' => $logs ?: [] ], 200 );
    }
}


// ── Handler assíncrono para dispatch de webhooks ──────────────────────────────
add_action( 'senderzz_webhook_dispatch_async', function( int $webhook_id, int $order_id, string $event, array $payload ): void {
    global $wpdb;
    $wh = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}senderzz_webhooks WHERE id = %d AND active = 1",
        $webhook_id
    ) );
    if ( ! $wh ) return;

    $body    = wp_json_encode( $payload );
    $headers = [
        'Content-Type'         => 'application/json',
        'X-Senderzz-Event'     => $event,
        'X-Senderzz-Delivery'  => wp_generate_uuid4(),
    ];
    if ( ! empty( $wh->secret ) ) {
        $headers['X-Senderzz-Signature'] = 'sha256=' . hash_hmac( 'sha256', $body, $wh->secret );
    }

    $response = function_exists( 'senderzz_safe_outbound_post' )
        ? senderzz_safe_outbound_post( $wh->url, [ 'body' => $body, 'headers' => $headers, 'timeout' => 15 ] )
        : wp_safe_remote_post( $wh->url, [ 'body' => $body, 'headers' => $headers, 'timeout' => 15 ] );

    $code          = is_wp_error( $response ) ? 0 : wp_remote_retrieve_response_code( $response );
    $response_body = is_wp_error( $response ) ? $response->get_error_message() : substr( wp_remote_retrieve_body( $response ), 0, 500 );
    $success       = $code >= 200 && $code < 300;

    $wpdb->insert( $wpdb->prefix . 'senderzz_webhook_log', [
        'webhook_id'    => $webhook_id,
        'order_id'      => $order_id,
        'event'         => $event,
        'payload_json'  => $body,
        'response_code' => $code,
        'response_body' => $response_body,
        'fired_at'      => current_time( 'mysql', true ),
    ] );

    // Retry automático: reagenda se falhou (máximo 3 tentativas, backoff exponencial)
    if ( ! $success ) {
        $attempts_key = 'sz_wh_attempts_' . $webhook_id . '_' . $order_id . '_' . md5( $event );
        $attempts     = (int) get_transient( $attempts_key );
        if ( $attempts < 3 ) {
            set_transient( $attempts_key, $attempts + 1, HOUR_IN_SECONDS * 6 );
            $delay = pow( 2, $attempts ) * 300; // 5min, 10min, 20min
            as_schedule_single_action(
                time() + $delay,
                'senderzz_webhook_dispatch_async',
                [ $webhook_id, $order_id, $event, $payload ],
                'senderzz-webhooks'
            );
        }
    }
}, 10, 4 );
