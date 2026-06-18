<?php
/**
 * Senderzz Logistics — Valor Declarado Universal
 *
 * Versão simplificada: UM ÚNICO valor de declaração, aplicado nas chamadas
 * pro Melhor Envio. Não tem mais por-carrier, porque a API do ME aceita
 * insurance_value único por cotação — fazer "por carrier" não muda nada.
 *
 * O QUE FAZ:
 *   Substitui dois pontos hardcoded:
 *
 *   1. src/Api/Calculator.php:97 — set_insurance_value() força "1" e ignora
 *      o valor passado. Por isso mudar valor declarado em qualquer config
 *      antes não afetava o frete cotado.
 *
 *   2. includes/tpc/painel.php:715 — unitary_value: 50 hardcoded na emissão
 *      manual de etiqueta.
 *
 * COMO INTERCEPTA:
 *   Hook em pre_http_request:
 *     - POST /api/v2/me/shipment/calculate → reescreve options.insurance_value
 *     - POST /api/v2/me/cart                → reescreve products[].unitary_value
 *
 *   Não toca em nenhum arquivo do plugin. Funciona INDEPENDENTE do código
 *   interno do Calculator/Method/Cart.
 *
 * UI:
 *   Integrada na aba "Senderzz > Configurações" (renderiza um bloco logo
 *   abaixo do form de configurações existente, sem aba separada).
 *
 * APLICAR:
 *   1. Sobe pra includes/senderzz-declared-value.php (substitui versão antiga)
 *   2. Já tem require declarado se você seguiu antes:
 *      'includes/senderzz-declared-value.php',
 *   3. Vai em "Senderzz > Configurações" e ajusta o valor.
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_DECLARED_VALUE_LOADED' ) ) return;
define( 'SENDERZZ_DECLARED_VALUE_LOADED', true );

// ═════════════════════════════════════════════════════════════════════════════
// CONFIG
// ═════════════════════════════════════════════════════════════════════════════

function senderzz_dv_default_config(): array {
    return [
        'enabled'        => true,
        'apply_to_quote' => true,
        'apply_to_label' => true,
        'value'          => 1.00,
    ];
}

function senderzz_dv_get_config(): array {
    $stored = get_option( 'senderzz_declared_value_v3_config', [] );
    if ( ! is_array( $stored ) || empty( $stored ) ) {
        return senderzz_dv_default_config();
    }
    return array_merge( senderzz_dv_default_config(), $stored );
}

// ═════════════════════════════════════════════════════════════════════════════
// INTERCEPTOR 1: cotação (POST /me/shipment/calculate)
// ═════════════════════════════════════════════════════════════════════════════

add_filter( 'pre_http_request', function ( $preempt, $args, $url ) {
    if ( $preempt !== false ) return $preempt;
    if ( ! is_string( $url ) ) return $preempt;
    if ( ! preg_match( '#^https?://(sandbox\.|www\.)?melhorenvio\.com\.br/api/v2/me/shipment/calculate\b#i', $url ) ) {
        return $preempt;
    }
    if ( strtoupper( (string) ( $args['method'] ?? 'POST' ) ) !== 'POST' ) return $preempt;
    if ( ! empty( $args['_senderzz_dv_quote_modified'] ) ) return $preempt;

    $cfg = senderzz_dv_get_config();
    if ( empty( $cfg['enabled'] ) || empty( $cfg['apply_to_quote'] ) ) return $preempt;

    $body = is_string( $args['body'] ?? '' ) ? json_decode( $args['body'], true ) : ( $args['body'] ?? null );
    if ( ! is_array( $body ) ) return $preempt;

    $new_value = max( 1.0, (float) $cfg['value'] );

    if ( ! isset( $body['options'] ) || ! is_array( $body['options'] ) ) {
        $body['options'] = [];
    }
    $body['options']['insurance_value'] = function_exists( 'wc_format_decimal' )
        ? wc_format_decimal( $new_value, 2 )
        : number_format( $new_value, 2, '.', '' );

    // Sem log por cotação: esse hook roda em toda simulação de frete e podia gerar arquivos gigantes.

    $new_args = $args;
    $new_args['body'] = wp_json_encode( $body );
    $new_args['_senderzz_dv_quote_modified'] = true;
    return wp_remote_request( $url, $new_args );
}, 5, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// INTERCEPTOR 2: emissão de etiqueta (POST /me/cart)
// ═════════════════════════════════════════════════════════════════════════════

add_filter( 'pre_http_request', function ( $preempt, $args, $url ) {
    if ( $preempt !== false ) return $preempt;
    if ( ! is_string( $url ) ) return $preempt;
    if ( ! preg_match( '#^https?://(sandbox\.|www\.)?melhorenvio\.com\.br/api/v2/me/cart\b#i', $url ) ) {
        return $preempt;
    }
    if ( strtoupper( (string) ( $args['method'] ?? 'POST' ) ) !== 'POST' ) return $preempt;
    if ( ! empty( $args['_senderzz_dv_label_modified'] ) ) return $preempt;

    $cfg = senderzz_dv_get_config();
    if ( empty( $cfg['enabled'] ) || empty( $cfg['apply_to_label'] ) ) return $preempt;

    $body = is_string( $args['body'] ?? '' ) ? json_decode( $args['body'], true ) : ( $args['body'] ?? null );
    if ( ! is_array( $body ) ) return $preempt;
    if ( empty( $body['products'] ) || ! is_array( $body['products'] ) ) return $preempt;

    $new_value = max( 1.0, (float) $cfg['value'] );

    $modified = false;
    foreach ( $body['products'] as $i => $product ) {
        if ( ! is_array( $product ) ) continue;
        $body['products'][ $i ]['unitary_value'] = $new_value;
        $modified = true;
    }
    if ( ! $modified ) return $preempt;

    if ( function_exists( 'tpc_log' ) ) {
        tpc_log( 'declared_value_label_applied', [ 'new_value' => $new_value ] );
    }

    $new_args = $args;
    $new_args['body'] = wp_json_encode( $body );
    $new_args['_senderzz_dv_label_modified'] = true;
    return wp_remote_request( $url, $new_args );
}, 5, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// SAVE handler — escuta o submit do nosso form
// ═════════════════════════════════════════════════════════════════════════════

add_action( 'admin_init', function () {
    if (
        ! isset( $_POST['senderzz_dv_nonce'] ) ||
        ! wp_verify_nonce( $_POST['senderzz_dv_nonce'], 'senderzz_dv_save' ) ||
        ! current_user_can( 'manage_woocommerce' )
    ) {
        return;
    }

    $cfg = senderzz_dv_get_config();
    $cfg['enabled']        = isset( $_POST['senderzz_dv_enabled'] );
    $cfg['apply_to_quote'] = isset( $_POST['senderzz_dv_apply_to_quote'] );
    $cfg['apply_to_label'] = isset( $_POST['senderzz_dv_apply_to_label'] );
    $cfg['value']          = max( 1.0, (float) ( $_POST['senderzz_dv_value'] ?? 1.0 ) );

    update_option( 'senderzz_declared_value_v3_config', $cfg, false );

    add_action( 'admin_notices', function () {
        echo '<div class="notice notice-success is-dismissible"><p>✓ Valor declarado salvo. Próxima cotação/etiqueta usa o novo valor.</p></div>';
    } );
} );

// ═════════════════════════════════════════════════════════════════════════════
// UI — injeta bloco na aba "Senderzz > Configurações"
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Renderiza nosso bloco no admin_footer apenas quando estamos na página
 * Senderzz aba Configurações. Um JS pequeno (3 linhas) move o bloco pra
 * dentro do conteúdo da aba, logo após o form principal.
 */
add_action( 'admin_footer', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;

    $page = $_GET['page'] ?? '';
    $tab  = $_GET['tab']  ?? '';
    if ( $page !== 'senderzz' || $tab !== 'configuracoes' ) return;

    $cfg = senderzz_dv_get_config();
    ?>
    <div id="senderzz-dv-block-wrapper" style="display:none">
        <div id="senderzz-dv-block" style="background:#fff;border:1px solid #c3c4c7;padding:16px 20px;border-radius:4px;margin:24px 0;max-width:680px">
            <h2 style="margin-top:0">📦 Valor Declarado (Senderzz)</h2>

            <p style="margin:0 0 12px;font-size:var(--sz-text-base);line-height:1.6">
                Valor único declarado pro Melhor Envio em <strong>cotações de frete</strong> e
                <strong>emissões de etiqueta</strong>. Substitui o <code>insurance_value=1</code>
                hardcoded em <code>Calculator.php</code> e o <code>unitary_value=50</code> hardcoded em
                <code>painel.php</code>.
            </p>

            <div style="background:#fff8e1;border-left:3px solid #f59e0b;padding:10px 14px;margin:12px 0;font-size:var(--sz-text-meta);line-height:1.5">
                <strong>Como afeta o frete:</strong>
                Correios faz seguro por fora — declarar R$ 1 mantém frete mínimo.
                Jadlog, Latam, Azul e JeT calculam seguro proporcional ao declarado —
                declarar mais = frete mais caro mas mercadoria coberta.
                Mínimo aceito pelo ME: R$ 1.
            </div>

            <form method="post" action="" style="margin-top:12px">
                <?php wp_nonce_field( 'senderzz_dv_save', 'senderzz_dv_nonce' ); ?>

                <table class="form-table" style="margin-top:0">
                    <tr>
                        <th style="width:200px">Módulo ativo</th>
                        <td>
                            <label>
                                <input type="checkbox" name="senderzz_dv_enabled" value="1" <?php checked( $cfg['enabled'] ); ?>>
                                Substituir valor declarado nas chamadas pro ME
                            </label>
                            <p class="description">Se desativar, volta ao comportamento original
                                (insurance_value=1 e unitary_value=50 hardcoded).</p>
                        </td>
                    </tr>
                    <tr>
                        <th>Aplicar em</th>
                        <td>
                            <label>
                                <input type="checkbox" name="senderzz_dv_apply_to_quote" value="1" <?php checked( $cfg['apply_to_quote'] ); ?>>
                                Cotação de frete (preço que o cliente final paga)
                            </label>
                            <br>
                            <label>
                                <input type="checkbox" name="senderzz_dv_apply_to_label" value="1" <?php checked( $cfg['apply_to_label'] ); ?>>
                                Emissão de etiqueta (POST /me/cart)
                            </label>
                        </td>
                    </tr>
                    <tr>
                        <th>Valor declarado (R$)</th>
                        <td>
                            <input type="number" name="senderzz_dv_value" value="<?php echo esc_attr( $cfg['value'] ); ?>"
                                   step="0.01" min="1" style="width:140px" required>
                            <p class="description">Valor fixo aplicado em todas as chamadas. Mínimo R$ 1.</p>
                        </td>
                    </tr>
                </table>

                <p>
                    <input type="submit" class="button-primary" value="Salvar valor declarado">
                </p>
            </form>
        </div>
    </div>

    <script>
    (function() {
        var src = document.getElementById('senderzz-dv-block-wrapper');
        if (!src) return;
        var block = src.firstElementChild;

        // Procura o form principal da tab Configurações e insere logo depois.
        var anchors = document.querySelectorAll('.sz-tab-body form, .sz-tab-content form, #sz-admin-body form');
        var target = null;
        for (var i = 0; i < anchors.length; i++) {
            if (anchors[i].querySelector('input[name="tpc_config_nonce"]')) {
                target = anchors[i];
                break;
            }
        }

        if (target && target.parentNode) {
            target.parentNode.insertBefore(block, target.nextSibling);
        } else {
            // Fallback: coloca no final do conteúdo da aba.
            var body = document.querySelector('.sz-tab-body, #sz-admin-body, .wrap');
            if (body) body.appendChild(block);
        }
        src.remove();
    })();
    </script>
    <?php
} );