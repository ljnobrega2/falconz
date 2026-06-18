<?php
/**
 * senderzz-async-approval.php
 *
 * Patch: Aprovação assíncrona de etiquetas no portal do operador.
 *
 * PROBLEMA RESOLVIDO:
 *   A função senderzz_rest_operator_generate_labels() e senderzz_operator_update_status()
 *   executavam todo o pipeline de etiqueta (consulta ME, compra, download de PDF,
 *   salvamento) de forma síncrona durante o clique do operador, causando 30s+ por pedido.
 *
 * SOLUÇÃO:
 *   1. Aprovação responde em <1s (só valida, debita saldo, muda status, enfileira job).
 *   2. Action Scheduler processa a etiqueta em background.
 *   3. Tela do operador mostra "Gerando…" / "Baixar" / "Tentar novamente" conforme meta.
 *   4. Transição aprovado → embalado liberada.
 *   5. Lock de duplicidade por transient.
 *
 * DEPENDÊNCIA: Action Scheduler (incluído no WooCommerce ≥ 3.5).
 *
 * INSTALAÇÃO:
 *   Incluir este arquivo no senderzz-logistics.php:
 *     require_once plugin_dir_path( __FILE__ ) . 'includes/senderzz-async-approval.php';
 *   Remover / comentar o hook antigo em operator.php:
 *     - register_rest_route( $ns, '/operator/labels/generate', [...] )
 *   (ou deixar: este arquivo sobrescreve o callback via priority mais alta.)
 *
 * @version 1.0.0
 */

if ( ! defined( 'ABSPATH' ) ) exit;

/* ═══════════════════════════════════════════════════════════════════════
 * 1. TRANSIÇÃO DE STATUS: aprovado → embalado (CORREÇÃO DO BUG)
 * ═══════════════════════════════════════════════════════════════════════ */

/**
 * Remove o hook original de senderzz_operator_update_status e substitui
 * com versão que permite aprovado → embalado além de separado → embalado.
 *
 * Estratégia: sobrescrever o endpoint REST /operator/orders/{id}/status
 * com novo callback (priority 20 > default 10).
 */
add_action( 'rest_api_init', function () {
    register_rest_route( 'senderzz/v1', '/operator/orders/(?P<id>\d+)/status', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_async_operator_status',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ], /* override */ true );
}, 20 );

/**
 * Callback do endpoint de status — versão corrigida.
 * Permite: aprovado → embalado  e  separado → embalado.
 */
function senderzz_async_operator_status( WP_REST_Request $request ): WP_REST_Response {
    $order_id  = absint( $request->get_param( 'id' ) );
    $ol_status = sanitize_key( $request->get_param( 'ol_status' ) ?: '' );

    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Pedido não encontrado.' ], 404 );
    }

    // IDOR: verificar classe logística do operador apenas para Expedição.
    // Motoboy COD é isolado e usa a própria tabela operacional/faixa CEP.
    $token = sanitize_text_field(
        $_COOKIE['senderzz_portal_session'] ?? ( $_SERVER['HTTP_X_SENDERZZ_TOKEN'] ?? '' )
    );

    // Cash on Delivery Motoboy: OL só pode mover agendado/aprovado -> embalado.
    // Este branch é necessário porque este arquivo sobrescreve o endpoint de status
    // do operador e, sem isso, o pedido caía na regra antiga de Expedição.
    $motoboy_row = function_exists( 'senderzz_operator_motoboy_row' ) ? senderzz_operator_motoboy_row( $order_id ) : null;
    $is_motoboy  = $motoboy_row || ( function_exists( 'senderzz_operator_order_is_motoboy_cod' ) && senderzz_operator_order_is_motoboy_cod( $order ) );

    if ( $is_motoboy ) {
        $current_mb_status = function_exists( 'senderzz_operator_motoboy_status_for_order' )
            ? senderzz_operator_motoboy_status_for_order( $order, $motoboy_row )
            : sanitize_key( (string) ( $motoboy_row->status ?? $order->get_meta( '_senderzz_motoboy_flow_status', true ) ?: $order->get_status() ) );

        if ( $ol_status !== 'embalado' || ! in_array( $current_mb_status, [ 'agendado', 'aprovado' ], true ) ) {
            return new WP_REST_Response(
                [ 'success' => false, 'message' => 'Motoboy COD permite ao OL apenas agendado → embalado.' ],
                400
            );
        }

        if ( ! $motoboy_row && function_exists( 'sz_motoboy_criar_pedido' ) ) {
            sz_motoboy_criar_pedido( $order_id );
            $motoboy_row = function_exists( 'senderzz_operator_motoboy_row' ) ? senderzz_operator_motoboy_row( $order_id ) : null;
        }

        if ( $motoboy_row ) {
            global $wpdb;
            $table = $wpdb->prefix . 'sz_motoboy_pedidos';
            if ( function_exists( 'sz_motoboy_mudar_status' ) ) {
                sz_motoboy_mudar_status( (int) $motoboy_row->id, 'embalado', [], 'ol', get_current_user_id() );
            } else {
                $wpdb->update( $table, [ 'status' => 'embalado', 'updated_at' => current_time( 'mysql' ) ], [ 'id' => (int) $motoboy_row->id ], [ '%s', '%s' ], [ '%d' ] );
            }
        }

        if ( ! $order->has_status( 'embalado' ) ) {
            $order->update_status( 'embalado', 'Senderzz COD Motoboy: pedido embalado pelo operador logístico.' );
        }
        $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
        $order->update_meta_data( '_senderzz_motoboy_flow_status', 'embalado' );
        $order->save();

        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'operator.motoboy_status_updated', [
                'order_id' => $order_id,
                'from'     => $current_mb_status,
                'to'       => 'embalado',
            ] );
        }

        return new WP_REST_Response(
            [ 'success' => true, 'message' => "Pedido Motoboy #{$order_id} embalado." ],
            200
        );
    }

    if ( $token ) {
        global $wpdb;
        $operator_user = $wpdb->get_row( $wpdb->prepare(
            "SELECT u.shipping_class_id
             FROM {$wpdb->prefix}senderzz_portal_sessions s
             JOIN {$wpdb->prefix}senderzz_portal_users u ON u.id = s.user_id
             WHERE s.token = %s AND s.expires_at > NOW()
               AND u.role = 'operator' AND u.status = 'active'
             LIMIT 1",
            $token
        ) );
        if ( $operator_user && (int) $operator_user->shipping_class_id > 0 ) {
            if ( function_exists( 'senderzz_operator_order_class_info' ) ) {
                $order_class = senderzz_operator_order_class_info( $order );
                if ( (int) ( $order_class['id'] ?? 0 ) !== (int) $operator_user->shipping_class_id ) {
                    return new WP_REST_Response(
                        [ 'success' => false, 'message' => 'Pedido não pertence à sua classe logística.' ],
                        403
                    );
                }
            }
        }
    }

    // Expedição/OL: separado/aprovado -> embalado. Motoboy nunca passa aqui.
    $allowed = [
        'separado' => 'embalado',
        'aprovado' => 'embalado',
    ];

    $current = $order->get_status();
    $target  = $allowed[ $current ] ?? null;

    if ( ! $target || $ol_status !== $target ) {
        return new WP_REST_Response(
            [ 'success' => false, 'message' => "Transição de '{$current}' para '{$ol_status}' não permitida." ],
            400
        );
    }

    $labels = [
        'embalado' => 'Senderzz: pedido embalado pelo operador logístico.',
        'enviado'  => 'Senderzz: pedido marcado como enviado pelo operador logístico.',
    ];

    $order->update_status( $target, $labels[ $target ] ?? 'Status atualizado pelo operador.' );

    if ( function_exists( 'senderzz_me_log' ) ) {
        senderzz_me_log( 'operator.status_updated', [
            'order_id' => $order_id,
            'from'     => $current,
            'to'       => $target,
        ] );
    }

    return new WP_REST_Response(
        [ 'success' => true, 'message' => "Pedido #{$order_id} movido para '{$target}'." ],
        200
    );
}

/* ═══════════════════════════════════════════════════════════════════════
 * 2. ENDPOINT /operator/labels/generate — VERSÃO ASSÍNCRONA
 *    Sobrescreve o endpoint síncrono original com override=true.
 * ═══════════════════════════════════════════════════════════════════════ */

add_action( 'rest_api_init', function () {
    register_rest_route( 'senderzz/v1', '/operator/labels/generate', [
        'methods'             => 'POST',
        'callback'            => 'senderzz_async_operator_generate_labels',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ], /* override */ true );
}, 20 );

/**
 * Enfileira geração de etiqueta em background para cada pedido.
 * Responde em <1s independentemente da quantidade de pedidos.
 */
function senderzz_async_operator_generate_labels( WP_REST_Request $request ): WP_REST_Response {
    $order_ids = array_values( array_filter( array_map( 'absint', (array) $request->get_param( 'order_ids' ) ) ) );

    if ( empty( $order_ids ) ) {
        return new WP_REST_Response( [ 'success' => false, 'message' => 'Selecione pelo menos um pedido.' ], 400 );
    }

    $queued  = [];
    $skipped = [];
    $errors  = [];

    foreach ( $order_ids as $order_id ) {
        $order = wc_get_order( $order_id );
        if ( ! $order ) {
            $errors[] = "#$order_id: pedido não encontrado";
            continue;
        }

        // Já tem etiqueta válida? Não precisa regerar.
        $existing_path = (string) $order->get_meta( '_melhor_envio_pdf_local_path' );
        $existing_url  = (string) $order->get_meta( '_melhor_envio_pdf_local_url' );
        if (
            $existing_url &&
            function_exists( 'senderzz_operator_is_valid_pdf_file' ) &&
            senderzz_operator_is_valid_pdf_file( $existing_path )
        ) {
            $skipped[] = $order_id;
            continue;
        }

        // Lock: impede múltiplos jobs para o mesmo pedido.
        $lock_key = 'senderzz_label_lock_' . $order_id;
        if ( get_transient( $lock_key ) ) {
            $skipped[] = $order_id; // já está em fila
            continue;
        }
        set_transient( $lock_key, 1, 300 ); // 5 min de proteção

        // Marca como "pendente" para a UI mostrar "Gerando…"
        $order->update_meta_data( '_senderzz_label_pending', 'yes' );
        $order->update_meta_data( '_senderzz_label_ready',   'no' );
        $order->update_meta_data( '_senderzz_label_error',   '' );
        $order->save();

        // Enfileira via Action Scheduler (nativo no WooCommerce).
        if ( function_exists( 'as_schedule_single_action' ) ) {
            as_schedule_single_action(
                time(),
                'senderzz_generate_label_async',
                [ 'order_id' => $order_id ],
                'senderzz-labels'
            );
            $queued[] = $order_id;
        } else {
            // Fallback: wp_schedule_single_event se Action Scheduler não disponível.
            wp_schedule_single_event( time() + 1, 'senderzz_generate_label_cron', [ $order_id ] );
            $queued[] = $order_id;
        }
    }

    return new WP_REST_Response( [
        'success'  => true,
        'queued'   => $queued,
        'skipped'  => $skipped,
        'errors'   => $errors,
        'message'  => sprintf(
            '%d pedido(s) enfileirado(s) para geração de etiqueta em background.',
            count( $queued )
        ),
        'async'    => true,
    ], 200 );
}

/* ═══════════════════════════════════════════════════════════════════════
 * 3. ACTION SCHEDULER: handler background de geração de etiqueta
 * ═══════════════════════════════════════════════════════════════════════ */

add_action( 'senderzz_generate_label_async', 'senderzz_bg_generate_label', 10, 1 );
add_action( 'senderzz_generate_label_cron',  'senderzz_bg_generate_label', 10, 1 ); // fallback

/**
 * Processa a geração de etiqueta em background.
 * Chamado pelo Action Scheduler ou wp-cron.
 *
 * @param int|array $order_id_or_args  Action Scheduler passa array ['order_id' => N]
 *                                     wp-cron passa inteiro direto.
 */
function senderzz_bg_generate_label( $order_id_or_args ): void {
    // Normaliza argumento (AS passa array, wp-cron passa scalar)
    $order_id = is_array( $order_id_or_args )
        ? absint( $order_id_or_args['order_id'] ?? 0 )
        : absint( $order_id_or_args );

    if ( ! $order_id ) return;

    $lock_key = 'senderzz_label_lock_' . $order_id;

    $order = wc_get_order( $order_id );
    if ( ! $order ) {
        delete_transient( $lock_key );
        return;
    }

    // Idempotência: se já existe etiqueta válida, não reprocessa.
    $item_id  = (string) $order->get_meta( '_melhor_envio_item_id' );
    $order_me = (string) $order->get_meta( '_melhor_envio_order_id' );
    $pdf_path = (string) $order->get_meta( '_melhor_envio_pdf_local_path' );
    $pdf_url  = (string) $order->get_meta( '_melhor_envio_pdf_local_url' );

    $already_done = (
        $pdf_url &&
        function_exists( 'senderzz_operator_is_valid_pdf_file' ) &&
        senderzz_operator_is_valid_pdf_file( $pdf_path )
    );

    if ( $already_done ) {
        // Já estava pronta — apenas atualiza metas de estado.
        $order->update_meta_data( '_senderzz_label_pending', 'no' );
        $order->update_meta_data( '_senderzz_label_ready',   'yes' );
        $order->save();
        delete_transient( $lock_key );
        return;
    }

    // Tenta gerar a etiqueta.
    try {
        if ( ! function_exists( 'senderzz_operator_ensure_label_pdf' ) ) {
            throw new \Exception( 'Função senderzz_operator_ensure_label_pdf indisponível.' );
        }

        $result = senderzz_operator_ensure_label_pdf( $order );

        // Sucesso
        $order = wc_get_order( $order_id ); // recarrega após pipeline
        if ( $order ) {
            $order->update_meta_data( '_senderzz_label_pending', 'no' );
            $order->update_meta_data( '_senderzz_label_ready',   'yes' );
            $order->update_meta_data( '_senderzz_label_error',   '' );
            $order->save();

            $order->add_order_note(
                'Senderzz: etiqueta gerada em background com sucesso. PDF disponível para o operador.'
            );
        }

        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'operator.label_generated_async', [
                'order_id' => $order_id,
                'url'      => $result['url'] ?? '',
            ] );
        }

    } catch ( \Throwable $e ) {

        $error_msg = $e->getMessage();

        $order = wc_get_order( $order_id );
        if ( $order ) {
            $order->update_meta_data( '_senderzz_label_pending', 'no' );
            $order->update_meta_data( '_senderzz_label_ready',   'no' );
            $order->update_meta_data( '_senderzz_label_error',   $error_msg );
            $order->save();

            $order->add_order_note(
                'Senderzz: falha ao gerar etiqueta em background. Erro: ' . $error_msg
            );

            // IMPORTANTE: NÃO muda o status do pedido para "erro".
            // O pedido foi aprovado com sucesso; apenas a etiqueta falhou.
            // O operador verá o botão "Tentar novamente" na tela.
        }

        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'operator.label_async_error', [
                'order_id' => $order_id,
                'error'    => $error_msg,
            ] );
        }

    } finally {
        delete_transient( $lock_key );
    }
}

/* ═══════════════════════════════════════════════════════════════════════
 * 4. ENDPOINT: status da etiqueta (polling da UI)
 *    GET /senderzz/v1/operator/orders/{id}/label-status
 * ═══════════════════════════════════════════════════════════════════════ */

add_action( 'rest_api_init', function () {
    register_rest_route( 'senderzz/v1', '/operator/orders/(?P<id>\d+)/label-status', [
        'methods'             => 'GET',
        'callback'            => 'senderzz_async_label_status',
        'permission_callback' => 'senderzz_rest_operator_permission',
    ] );
} );

/**
 * Retorna o estado atual da etiqueta de um pedido para a UI fazer polling.
 *
 * Resposta:
 *   { state: "pending"|"ready"|"error"|"none", url: "...", error: "..." }
 */
function senderzz_async_label_status( WP_REST_Request $request ): WP_REST_Response {
    $order_id = absint( $request->get_param( 'id' ) );
    $order    = wc_get_order( $order_id );

    if ( ! $order ) {
        return new WP_REST_Response( [ 'success' => false, 'state' => 'none' ], 404 );
    }

    $pending = (string) $order->get_meta( '_senderzz_label_pending' );
    $ready   = (string) $order->get_meta( '_senderzz_label_ready' );
    $error   = (string) $order->get_meta( '_senderzz_label_error' );
    $pdf_url = (string) $order->get_meta( '_melhor_envio_pdf_local_url' );
    $pdf_path= (string) $order->get_meta( '_melhor_envio_pdf_local_path' );

    // Detecta estado real
    if (
        $pdf_url &&
        function_exists( 'senderzz_operator_is_valid_pdf_file' ) &&
        senderzz_operator_is_valid_pdf_file( $pdf_path )
    ) {
        $state = 'ready';
    } elseif ( $pending === 'yes' ) {
        $state = 'pending';
    } elseif ( $ready === 'yes' ) {
        $state = 'ready';
    } elseif ( $error ) {
        $state = 'error';
    } else {
        $state = 'none';
    }

    return new WP_REST_Response( [
        'success'  => true,
        'order_id' => $order_id,
        'state'    => $state,
        'url'      => $state === 'ready' ? $pdf_url : '',
        'error'    => $state === 'error'  ? $error  : '',
    ], 200 );
}

/* ═══════════════════════════════════════════════════════════════════════
 * 5. FRONTEND: injeção de lógica de polling e exibição de estado
 *    Adiciona script no portal do operador que:
 *      - Após aprovar/gerar etiqueta, faz polling a cada 3s no endpoint acima.
 *      - Atualiza a UI: "Gerando…" → "Baixar etiqueta" / "Tentar novamente"
 * ═══════════════════════════════════════════════════════════════════════ */

add_action( 'wp_footer', 'senderzz_async_inject_label_polling_script' );

function senderzz_async_inject_label_polling_script(): void {
    // Só injeta no portal do operador (página com shortcode tpc_painel ou URL /portal/)
    if (
        ! is_page() &&
        ! ( defined( 'DOING_AJAX' ) && DOING_AJAX )
    ) return;

    global $post;
    $is_portal = $post && (
        has_shortcode( $post->post_content, 'tpc_painel' ) ||
        str_contains( (string) $post->post_name, 'portal' ) ||
        str_contains( (string) get_permalink( $post ), '/portal' )
    );

    if ( ! $is_portal && ! apply_filters( 'senderzz_force_label_polling_script', false ) ) return;

    ?>
    <script id="senderzz-async-label-polling">
    (function () {
        'use strict';

        const API_BASE = '<?php echo esc_js( rest_url( 'senderzz/v1' ) ); ?>';
        const POLL_INTERVAL_MS = 3000;
        const MAX_POLLS = 40; // 2 minutos máximo

        /**
         * Inicia polling para um order_id específico.
         * Atualiza elementos com data-order-id="N" na tela.
         */
        function pollLabelStatus(orderId) {
            let polls = 0;

            function getToken() {
                const match = document.cookie.match(/senderzz_portal_session=([^;]+)/);
                return match ? match[1] : '';
            }

            function updateUI(state, url, errorMsg) {
                // Suporta múltiplos seletores para compatibilidade com o portal atual.
                const containers = document.querySelectorAll(
                    `[data-order-id="${orderId}"] .senderzz-label-status,
                     [data-order="${orderId}"] .senderzz-label-status,
                     .senderzz-order-${orderId} .senderzz-label-status`
                );

                containers.forEach(function (el) {
                    if (state === 'pending') {
                        el.innerHTML = '<span class="sz-label-generating">⏳ Gerando etiqueta…</span>';
                    } else if (state === 'ready' && url) {
                        el.innerHTML = `<a class="sz-label-download button" href="${url}" target="_blank">📄 Baixar etiqueta</a>`;
                    } else if (state === 'error') {
                        el.innerHTML = `<span class="sz-label-error" title="${errorMsg}">❌ Erro</span> `
                            + `<button class="sz-label-retry button-link" data-order-id="${orderId}">Tentar novamente</button>`;
                    }
                });

                // Dispara evento customizado para o app Vue/React/vanilla do portal.
                document.dispatchEvent(new CustomEvent('senderzz:labelStatusChanged', {
                    detail: { orderId: parseInt(orderId), state, url, error: errorMsg }
                }));
            }

            function poll() {
                if (polls >= MAX_POLLS) {
                    updateUI('error', '', 'Timeout: etiqueta não gerada em 2 minutos.');
                    return;
                }
                polls++;

                fetch(`${API_BASE}/operator/orders/${orderId}/label-status`, {
                    headers: {
                        'X-Senderzz-Token': getToken(),
                        'X-WP-Nonce': (window.wpApiSettings && window.wpApiSettings.nonce) || ''
                    },
                    credentials: 'include'
                })
                .then(function (r) { return r.json(); })
                .then(function (data) {
                    if (!data.success) return;

                    if (data.state === 'pending') {
                        updateUI('pending', '', '');
                        setTimeout(poll, POLL_INTERVAL_MS);
                    } else if (data.state === 'ready') {
                        updateUI('ready', data.url, '');
                    } else if (data.state === 'error') {
                        updateUI('error', '', data.error || 'Erro desconhecido');
                    }
                })
                .catch(function () {
                    // Rede falhou — tenta de novo
                    setTimeout(poll, POLL_INTERVAL_MS * 2);
                });
            }

            updateUI('pending', '', '');
            poll();
        }

        // Delegação: botão "Tentar novamente"
        document.addEventListener('click', function (e) {
            const btn = e.target.closest('.sz-label-retry');
            if (!btn) return;
            e.preventDefault();

            const orderId = btn.dataset.orderId;
            if (!orderId) return;

            const API_BASE_INNER = '<?php echo esc_js( rest_url( 'senderzz/v1' ) ); ?>';
            const match = document.cookie.match(/senderzz_portal_session=([^;]+)/);
            const token = match ? match[1] : '';

            btn.disabled = true;
            btn.textContent = 'Enfileirando…';

            fetch(`${API_BASE_INNER}/operator/labels/generate`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Senderzz-Token': token,
                    'X-WP-Nonce': (window.wpApiSettings && window.wpApiSettings.nonce) || ''
                },
                credentials: 'include',
                body: JSON.stringify({ order_ids: [parseInt(orderId)] })
            })
            .then(function (r) { return r.json(); })
            .then(function (data) {
                if (data.success) {
                    pollLabelStatus(orderId);
                } else {
                    btn.disabled = false;
                    btn.textContent = 'Tentar novamente';
                    alert('Erro ao enfileirar: ' + (data.message || 'desconhecido'));
                }
            })
            .catch(function () {
                btn.disabled = false;
                btn.textContent = 'Tentar novamente';
            });
        });

        // API pública: window.senderzz.pollLabel(orderId)
        window.senderzz = window.senderzz || {};
        window.senderzz.pollLabel = pollLabelStatus;

        // Auto-poll ao carregar: detecta pedidos com _senderzz_label_pending=yes
        // via atributo data-label-state="pending" nos elementos da lista.
        document.addEventListener('DOMContentLoaded', function () {
            document.querySelectorAll('[data-label-state="pending"]').forEach(function (el) {
                const oid = el.closest('[data-order-id]')?.dataset?.orderId
                         || el.closest('[data-order]')?.dataset?.order;
                if (oid) pollLabelStatus(oid);
            });
        });

    })();
    </script>
    <style id="senderzz-async-label-styles">
        .sz-label-generating { color: #777; font-style: italic; }
        .sz-label-download   { display: inline-block; padding: 4px 10px; background: #2271b1; color: #fff; border-radius: 3px; text-decoration: none; font-size: var(--sz-text-base); }
        .sz-label-download:hover { background: #135e96; color: #fff; }
        .sz-label-error      { color: #cc1818; font-weight: 600; }
        .sz-label-retry      { color: #2271b1; cursor: pointer; background: none; border: none; text-decoration: underline; font-size: var(--sz-text-meta); margin-left: 4px; }
        .sz-label-retry:hover { color: #135e96; }
    </style>
    <?php
}

/* ═══════════════════════════════════════════════════════════════════════
 * 6. HELPER: expõe estado da etiqueta nos dados do pedido para o portal
 *    Filtra a resposta do endpoint GET /operator/orders para incluir
 *    label_state em cada pedido.
 * ═══════════════════════════════════════════════════════════════════════ */

add_filter( 'senderzz_operator_order_data', 'senderzz_async_append_label_state', 10, 2 );

function senderzz_async_append_label_state( array $data, \WC_Order $order ): array {
    $pending  = (string) $order->get_meta( '_senderzz_label_pending' );
    $ready    = (string) $order->get_meta( '_senderzz_label_ready' );
    $error    = (string) $order->get_meta( '_senderzz_label_error' );
    $pdf_path = (string) $order->get_meta( '_melhor_envio_pdf_local_path' );
    $pdf_url  = (string) $order->get_meta( '_melhor_envio_pdf_local_url' );

    $has_valid_pdf = $pdf_url &&
        function_exists( 'senderzz_operator_is_valid_pdf_file' ) &&
        senderzz_operator_is_valid_pdf_file( $pdf_path );

    if ( $has_valid_pdf ) {
        $label_state = 'ready';
    } elseif ( $pending === 'yes' ) {
        $label_state = 'pending';
    } elseif ( $error ) {
        $label_state = 'error';
    } else {
        $label_state = 'none';
    }

    $data['label_state']       = $label_state;
    $data['label_error']       = $error;
    $data['label_pending']     = $pending === 'yes';
    return $data;
}

/*
 * Aplica o filtro acima na resposta do endpoint existente de orders.
 * senderzz_operator_get_orders() retorna array de arrays — injetamos label_state
 * pós-processando via hook woocommerce_order_data_store_cpt_get_orders_query
 * não é viável; mais simples: filtrar senderzz_rest_operator_orders via hook de saída.
 */
add_filter( 'senderzz_rest_operator_orders_response', function ( array $orders ): array {
    foreach ( $orders as &$item ) {
        $order_id = absint( $item['id'] ?? 0 );
        if ( ! $order_id ) continue;
        $order = wc_get_order( $order_id );
        if ( ! $order ) continue;
        $item = senderzz_async_append_label_state( $item, $order );
    }
    unset( $item );
    return $orders;
} );

/* ─── Aplica o filtro dentro do callback existente (monkey-patch não-invasivo) ─── */
add_action( 'rest_api_init', function () {
    // Wraps senderzz_rest_operator_orders para aplicar o filtro de label_state.
    register_rest_route( 'senderzz/v1', '/operator/orders', [
        'methods'             => 'GET',
        'callback'            => function ( WP_REST_Request $request ): WP_REST_Response {
            // Chama a função original se existir, senão retorna vazio.
            if ( ! function_exists( 'senderzz_rest_operator_orders' ) ) {
                return new WP_REST_Response( [ 'success' => false, 'message' => 'Handler não encontrado.' ], 500 );
            }
            $response = senderzz_rest_operator_orders( $request );
            $data     = $response->get_data();

            if ( ! empty( $data['orders'] ) && is_array( $data['orders'] ) ) {
                $orders_raw = $data['orders'];
                $enriched   = [];
                foreach ( $orders_raw as $item ) {
                    $order_id = absint( $item['id'] ?? 0 );
                    if ( $order_id ) {
                        $order = wc_get_order( $order_id );
                        if ( $order ) {
                            $item = senderzz_async_append_label_state( $item, $order );
                        }
                    }
                    $enriched[] = $item;
                }
                $data['orders'] = $enriched;
                $response->set_data( $data );
            }

            return $response;
        },
        'permission_callback' => 'senderzz_rest_operator_permission',
    ], /* override */ true );
}, 20 );
