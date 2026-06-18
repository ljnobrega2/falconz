<?php
/**
 * Senderzz — Módulo Motoboy
 * Integração por produtor: toggle, taxas e método de frete no checkout
 */
if ( ! defined( 'ABSPATH' ) ) exit;

// Oculta Melhor Envio via CSS quando é link motoboy (evita flash)
add_action( 'wp_head', function() {
    if ( is_admin() ) return;
    if ( empty($_GET['frete']) || $_GET['frete'] !== 'motoboy' ) return;
    if ( ! function_exists('is_checkout') || ! is_checkout() ) return;
    echo '<style>.woocommerce-shipping-methods li:not([data-sz-motoboy]){display:none!important}</style>';
}, 1 );

define( 'SZ_MB_META_ATIVO',      '_sz_motoboy_ativo' );
define( 'SZ_MB_META_TAXA_ENT',   '_sz_motoboy_taxa_entrega' );
define( 'SZ_MB_META_TAXA_MAN',   '_sz_motoboy_taxa_manuseio' );
define( 'SZ_MB_META_TAXA_PERC',  '_sz_motoboy_taxa_percentual' );
define( 'SZ_MB_META_TAXA_TRANS_MODE', '_sz_motoboy_taxa_transacao_modo' );


if ( ! function_exists( 'sz_mb_taxa_transacao_modes' ) ) {
    function sz_mb_taxa_transacao_modes(): array {
        return [
            'split_producer_affiliate' => 'Replicar taxa por participante',
        ];
    }
}

if ( ! function_exists( 'sz_mb_sanitize_taxa_transacao_mode' ) ) {
    function sz_mb_sanitize_taxa_transacao_mode( $mode ): string {
        // v369: não existe mais escolha de "quem paga". A taxa percentual é sempre
        // aplicada à parte de cada participante. Sem afiliado, aplica uma vez ao produtor.
        return 'split_producer_affiliate';
    }
}

if ( ! function_exists( 'sz_mb_get_taxa_transacao_mode' ) ) {
    function sz_mb_get_taxa_transacao_mode( int $user_id ): string {
        return 'split_producer_affiliate';
    }
}


// v369: migração leve da regra de transação.
// Remove o seletor antigo de pagador e corrige o valor criado pelo build anterior
// quando estava em 10% para simular dois participantes. A regra nova usa 4,99%
// replicado por participante, e sem afiliado cobra uma única vez do produtor.
add_action( 'admin_init', function(): void {
    if ( get_option( 'senderzz_v369_tx_replicated_migrated', '' ) === '1' ) return;
    $pct = (float) get_option( 'sz_motoboy_taxa_percentual', 0 );
    if ( abs( $pct - 10.0 ) < 0.0001 ) {
        update_option( 'sz_motoboy_taxa_percentual', 4.99, false );
    }
    update_option( 'sz_motoboy_taxa_transacao_modo', 'split_producer_affiliate', false );
    global $wpdb;
    if ( $wpdb instanceof wpdb ) {
        $wpdb->delete( $wpdb->usermeta, [ 'meta_key' => SZ_MB_META_TAXA_TRANS_MODE ], [ '%s' ] );
    }
    update_option( 'senderzz_v369_tx_replicated_migrated', '1', false );
}, 5 );

if ( ! function_exists( 'sz_mb_get_taxa_percentual_produtor' ) ) {
    function sz_mb_get_taxa_percentual_produtor( int $user_id ): float {
        $value = get_user_meta( $user_id, SZ_MB_META_TAXA_PERC, true );
        if ( $value === '' ) {
            $value = get_option( 'sz_motoboy_taxa_percentual', 0 );
        }
        return max( 0.0, (float) $value );
    }
}

// ─── 1. Painel do usuário no WP Admin ────────────────────────────────────────
add_action( 'show_user_profile', 'sz_mb_producer_user_fields' );
add_action( 'edit_user_profile', 'sz_mb_producer_user_fields' );

function sz_mb_producer_user_fields( WP_User $user ): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    ?>
    <h2>🏍️ Senderzz Motoboy</h2>
    <table class="form-table">
        <tr>
            <th>Configurações Senderzz</th>
            <td>
                <p class="description">
                    As funções e taxas da Senderzz agora ficam centralizadas no menu <strong>Senderzz Admin → Financeiro COD → Taxas de Entrega</strong>.
                    Esta tela de usuário não deve mais ser usada para alterar taxas, ativação Motoboy ou cobrança de transação.
                </p>
            </td>
        </tr>
    </table>
    <?php
}

// ─── 2. Salvar campos ─────────────────────────────────────────────────────────
add_action( 'personal_options_update',  'sz_mb_producer_save_fields' );
add_action( 'edit_user_profile_update', 'sz_mb_producer_save_fields' );

function sz_mb_producer_save_fields( int $user_id ): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    if ( ! isset( $_POST['sz_mb_producer_nonce'] ) ) return;
    if ( ! wp_verify_nonce( $_POST['sz_mb_producer_nonce'], 'sz_mb_producer_save' ) ) return;

    // v368: campos Senderzz foram centralizados em Senderzz Admin > Financeiro COD > Taxas de Entrega.
    // Mantém compatibilidade: só salva aqui se algum build antigo ainda enviar esses campos.
    if ( isset( $_POST['sz_mb_ativo'], $_POST['sz_mb_taxa_entrega'], $_POST['sz_mb_taxa_manuseio'], $_POST['sz_mb_taxa_percentual'] ) ) {
        update_user_meta( $user_id, SZ_MB_META_ATIVO,     isset( $_POST['sz_mb_ativo'] ) ? '1' : '0' );
        update_user_meta( $user_id, SZ_MB_META_TAXA_ENT,  (float) ( $_POST['sz_mb_taxa_entrega']    ?? 0 ) );
        update_user_meta( $user_id, SZ_MB_META_TAXA_MAN,  (float) ( $_POST['sz_mb_taxa_manuseio']   ?? 0 ) );
        update_user_meta( $user_id, SZ_MB_META_TAXA_PERC, (float) ( $_POST['sz_mb_taxa_percentual'] ?? 0 ) );
        delete_user_meta( $user_id, SZ_MB_META_TAXA_TRANS_MODE );
        wp_cache_delete( 'sz_mb_portal_uid_' . $user_id, 'sz_motoboy' );
    }

    // Salvar template sem CPF (global — só admins)
    if ( current_user_can('manage_woocommerce') && isset( $_POST['sz_template_sem_cpf_nonce'] )
        && wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['sz_template_sem_cpf_nonce'] ) ), 'sz_template_sem_cpf_save' ) ) {
        $tmpl_id = absint( $_POST['sz_template_sem_cpf'] ?? 0 );
        if ( $tmpl_id > 0 ) {
            update_option( 'senderzz_checkout_template_id_sem_cpf', $tmpl_id );
        } else {
            delete_option( 'senderzz_checkout_template_id_sem_cpf' );
        }
    }
}

// ─── 3. Helpers com cache ─────────────────────────────────────────────────────

/**
 * Retorna WP user_id do produtor — com cache de objeto para não rodar múltiplas vezes por request.
 */
function sz_mb_get_portal_user_id(): int {
    static $cached_uid = null;
    if ( $cached_uid !== null ) return $cached_uid;

    // Se houver usuário WP logado, usa ele.
    $uid = get_current_user_id();
    if ( $uid ) {
        $cached_uid = (int) $uid;
        return $cached_uid;
    }

    // Checkout público Senderzz: resolve o produtor dono do link pelo token.
    $token = '';
    foreach ( [ 'sz', 'senderzz_offer_token' ] as $key ) {
        if ( ! $token && isset( $_REQUEST[ $key ] ) ) {
            $token = sanitize_text_field( wp_unslash( $_REQUEST[ $key ] ) );
        }
    }
    if ( ! $token && ! empty( $_COOKIE['senderzz_offer_token'] ) ) {
        $token = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_offer_token'] ) );
    }
    if ( ! $token && function_exists( 'WC' ) && WC()->session ) {
        $data = WC()->session->get( 'senderzz_sz_link_data' );
        if ( is_array( $data ) && ! empty( $data['token'] ) ) {
            $token = sanitize_text_field( (string) $data['token'] );
        }
    }
    if ( ! $token && ! empty( $_SERVER['HTTP_REFERER'] ) ) {
        $ref_query = wp_parse_url( esc_url_raw( wp_unslash( $_SERVER['HTTP_REFERER'] ) ), PHP_URL_QUERY );
        if ( $ref_query ) {
            parse_str( $ref_query, $ref_args );
            if ( ! empty( $ref_args['sz'] ) ) {
                $token = sanitize_text_field( wp_unslash( $ref_args['sz'] ) );
            }
        }
    }

    if ( $token ) {
        global $wpdb;
        $row = $wpdb->get_row( $wpdb->prepare(
            "SELECT l.user_id AS portal_user_id, u.wp_user_id, u.email
               FROM {$wpdb->prefix}senderzz_checkout_links l
          LEFT JOIN {$wpdb->prefix}senderzz_portal_users u ON u.id = l.user_id
              WHERE l.slug = %s OR l.token = %s
              LIMIT 1",
            $token,
            $token
        ) );
        if ( $row ) {
            $wp_user_id = absint( $row->wp_user_id ?? 0 );
            if ( ! $wp_user_id && ! empty( $row->email ) ) {
                $wp_user = get_user_by( 'email', sanitize_email( $row->email ) );
                if ( $wp_user ) {
                    $wp_user_id = (int) $wp_user->ID;
                    if ( ! empty( $row->portal_user_id ) ) {
                        $wpdb->update(
                            $wpdb->prefix . 'senderzz_portal_users',
                            [ 'wp_user_id' => $wp_user_id ],
                            [ 'id' => (int) $row->portal_user_id ],
                            [ '%d' ],
                            [ '%d' ]
                        );
                    }
                }
            }
            if ( $wp_user_id ) {
                $cached_uid = $wp_user_id;
                return $cached_uid;
            }
        }
    }

    $cached_uid = 0;
    return 0;
}

/**
 * Verifica se motoboy está ativo para um WP user_id — com cache de objeto.
 */
function sz_mb_produtor_ativo( int $user_id ): bool {
    if ( ! $user_id ) return false;

    static $cache = [];
    if ( isset( $cache[ $user_id ] ) ) return $cache[ $user_id ];

    $result = get_user_meta( $user_id, SZ_MB_META_ATIVO, true ) === '1';
    $cache[ $user_id ] = $result;
    return $result;
}

/**
 * Calcula taxas motoboy para um produtor.
 */
function sz_mb_calcular_taxa_produtor( int $user_id, float $valor_pedido ): array {
    $t_ent_raw = get_user_meta( $user_id, SZ_MB_META_TAXA_ENT, true );
    $t_man_raw = get_user_meta( $user_id, SZ_MB_META_TAXA_MAN, true );
    $t_ent  = $t_ent_raw !== '' ? (float) $t_ent_raw : (float) get_option( 'sz_motoboy_taxa_entrega', 25.00 );
    $t_man  = $t_man_raw !== '' ? (float) $t_man_raw : 0.0;
    $t_perc = function_exists( 'sz_mb_get_taxa_percentual_produtor' ) ? sz_mb_get_taxa_percentual_produtor( $user_id ) : (float) get_option( 'sz_motoboy_taxa_percentual', 0 );
    $t_mode = sz_mb_get_taxa_transacao_mode( $user_id );
    $adicional = $valor_pedido * ( $t_perc / 100 );
    $total     = $t_ent + $t_man + $adicional;
    return [
        'taxa_entrega'   => $t_ent,
        'taxa_manuseio'  => $t_man,
        'taxa_percentual'=> $t_perc,
        'taxa_transacao_modo' => $t_mode,
        'adicional'      => $adicional,
        'total'          => $total,
    ];
}

// ─── 4. Método de frete WooCommerce ──────────────────────────────────────────

add_action( 'woocommerce_shipping_init', 'sz_mb_register_shipping_method' );

function sz_mb_register_shipping_method(): void {
    if ( class_exists( 'SZ_Motoboy_Shipping_Method' ) ) return;

    class SZ_Motoboy_Shipping_Method extends WC_Shipping_Method {

        public function __construct( $instance_id = 0 ) {
            $this->id                 = 'sz_motoboy';
            $this->instance_id        = absint( $instance_id );
            $this->method_title       = '🏍️ Motoboy Senderzz';
            $this->method_description = 'Entrega por motoboy Senderzz para CEPs cobertos.';
            $this->supports           = [ 'shipping-zones', 'instance-settings' ];
            $this->title              = '🏍️ Motoboy Senderzz';
            $this->init();
        }

        public function init(): void {
            $this->init_form_fields();
            $this->init_settings();
        }

        public function init_form_fields(): void {
            $this->instance_form_fields = [
                'title' => [
                    'title'   => 'Título',
                    'type'    => 'text',
                    'default' => '🏍️ Motoboy Senderzz',
                ],
            ];
        }

        public function calculate_shipping( $package = [] ): void {
            // Só calcula em contexto de carrinho/checkout
            if ( is_admin() && ! wp_doing_ajax() ) return;

            $user_id = sz_mb_get_portal_user_id();
            if ( ! $user_id || ! sz_mb_produtor_ativo( $user_id ) ) return;

            // Verifica cobertura do CEP — com cache estático
            $cep = preg_replace( '/\D/', '', $package['destination']['postcode'] ?? '' );
            if ( ! $cep ) return;

            static $zona_cache = [];
            if ( ! isset( $zona_cache[ $cep ] ) ) {
                $zona_cache[ $cep ] = sz_motoboy_resolver_zona( $cep );
            }
            if ( ! $zona_cache[ $cep ] ) return;

            $subtotal = WC()->cart ? (float) WC()->cart->get_subtotal() : 0.0;
            $taxas    = sz_mb_calcular_taxa_produtor( $user_id, $subtotal );

            $this->add_rate( [
                'id'        => $this->get_rate_id(),
                'label'     => $this->get_option( 'title', '🏍️ Motoboy Senderzz' ),
                'cost'      => $taxas['total'],
                'meta_data' => [
                    'sz_taxa_entrega'   => $taxas['taxa_entrega'],
                    'sz_taxa_manuseio'  => $taxas['taxa_manuseio'],
                    'sz_taxa_adicional' => $taxas['adicional'],
                ],
            ] );
        }
    }
}

add_filter( 'woocommerce_shipping_methods', function( $methods ) {
    $methods['sz_motoboy'] = 'SZ_Motoboy_Shipping_Method';
    return $methods;
} );

// ─── 5. Ocultar Melhor Envio quando motoboy ativo + CEP coberto ───────────────
add_filter( 'woocommerce_package_rates', function( $rates, $package ) {
    // Só roda em frontend
    if ( is_admin() && ! wp_doing_ajax() ) return $rates;

    $user_id = sz_mb_get_portal_user_id();
    if ( ! $user_id || ! sz_mb_produtor_ativo( $user_id ) ) return $rates;

    $cep = preg_replace( '/\D/', '', $package['destination']['postcode'] ?? '' );
    if ( ! $cep ) return $rates;

    // Cache de zona por CEP
    static $zona_cache = [];
    if ( ! isset( $zona_cache[ $cep ] ) ) {
        $zona_cache[ $cep ] = sz_motoboy_resolver_zona( $cep );
    }
    if ( ! $zona_cache[ $cep ] ) return $rates;

    // Mantém só o método motoboy
    $motoboy = [];
    foreach ( $rates as $key => $rate ) {
        if ( strpos( $key, 'sz_motoboy' ) !== false ) {
            $motoboy[ $key ] = $rate;
        }
    }

    return ! empty( $motoboy ) ? $motoboy : $rates;
}, 999, 2 );

// ─── 6. Breakdown de taxas na página do pedido ───────────────────────────────
add_action( 'woocommerce_order_details_after_order_table', 'sz_mb_order_taxa_breakdown' );

function sz_mb_order_taxa_breakdown( WC_Order $order ): void {
    $method = $order->get_shipping_method();
    if ( strpos( strtolower( $method ), 'motoboy' ) === false ) return;

    $t_ent  = (float) $order->get_meta( '_sz_mb_taxa_entrega' );
    $t_man  = (float) $order->get_meta( '_sz_mb_taxa_manuseio' );
    $t_adic = (float) $order->get_meta( '_sz_mb_taxa_adicional' );

    if ( ! $t_ent && ! $t_man && ! $t_adic ) return;
    ?>
    <section class="woocommerce-order-motoboy-taxas" style="margin-top:20px">
        <h2 style="font-size:var(--sz-text-lg);margin-bottom:10px">🏍️ Detalhes da entrega Motoboy</h2>
        <table style="width:100%;border-collapse:collapse">
            <tbody>
                <?php if ( $t_ent ) : ?>
                <tr><td style="padding:6px 0;border-bottom:1px solid #f3f4f6">Taxa de entrega</td><td style="text-align:right">R$ <?php echo number_format($t_ent,2,',','.'); ?></td></tr>
                <?php endif; ?>
                <?php if ( $t_man ) : ?>
                <tr><td style="padding:6px 0;border-bottom:1px solid #f3f4f6">Taxa de manuseio</td><td style="text-align:right">R$ <?php echo number_format($t_man,2,',','.'); ?></td></tr>
                <?php endif; ?>
                <?php if ( $t_adic ) : ?>
                <tr><td style="padding:6px 0;border-bottom:1px solid #f3f4f6">Taxa adicional</td><td style="text-align:right">R$ <?php echo number_format($t_adic,2,',','.'); ?></td></tr>
                <?php endif; ?>
                <tr><td style="padding:8px 0;font-weight:700">Total frete</td><td style="text-align:right;font-weight:700">R$ <?php echo number_format($t_ent+$t_man+$t_adic,2,',','.'); ?></td></tr>
            </tbody>
        </table>
    </section>
    <?php
}

// ─── 7. Salvar taxas no pedido ────────────────────────────────────────────────
add_action( 'woocommerce_checkout_order_created', function( WC_Order $order ) {
    foreach ( $order->get_items('shipping') as $item ) {
        if ( strpos( $item->get_method_id(), 'sz_motoboy' ) === false ) continue;
        $sum = 0.0;
        foreach ( $item->get_meta_data() as $m ) {
            $data = $m->get_data();
            if ( in_array( $data['key'], ['sz_taxa_entrega','sz_taxa_manuseio','sz_taxa_adicional','sz_taxa_total'], true ) ) {
                $order->update_meta_data( '_' . $data['key'], $data['value'] );
                $compat = str_replace( '_sz_taxa_', '_sz_mb_taxa_', '_' . $data['key'] );
                $order->update_meta_data( $compat, $data['value'] );
                if ( $data['key'] !== 'sz_taxa_total' ) $sum += (float) $data['value'];
            }
        }
        if ( (float) $order->get_meta( '_sz_mb_taxa_total' ) <= 0 && $sum > 0 ) {
            $order->update_meta_data( '_sz_mb_taxa_total', round( $sum, 2 ) );
        }
    }
    $order->save();
} );

// ─── 8. Modularidade: has_motoboy / has_expedicao ─────────────────────────────

define( 'SZ_META_HAS_MOTOBOY',    '_sz_has_motoboy' );
define( 'SZ_META_HAS_EXPEDICAO',  '_sz_has_expedicao' );
define( 'SZ_META_DISPENSAR_CPF',  '_sz_dispensar_cpf_motoboy' );

/**
 * Verifica se produtor tem motoboy habilitado.
 * Padrão: true (não quebra produtores existentes).
 */
function sz_producer_has_motoboy( int $user_id ): bool {
    if ( ! $user_id ) return false;
    $v = get_user_meta( $user_id, SZ_META_HAS_MOTOBOY, true );
    return $v === '' ? true : ( $v === '1' );
}

/**
 * Verifica se produtor tem expedição habilitada.
 * Padrão: true.
 */
function sz_producer_has_expedicao( int $user_id ): bool {
    if ( ! $user_id ) return false;
    $v = get_user_meta( $user_id, SZ_META_HAS_EXPEDICAO, true );
    return $v === '' ? true : ( $v === '1' );
}

/**
 * Verifica se produtor dispensou CPF no checkout motoboy.
 * Padrão: false (CPF obrigatório).
 */
function sz_producer_dispensar_cpf_motoboy( int $user_id ): bool {
    if ( ! $user_id ) return false;
    return get_user_meta( $user_id, SZ_META_DISPENSAR_CPF, true ) === '1';
}

// ─── 9. Campos no WP Admin (perfil do usuário) ────────────────────────────────
add_action( 'show_user_profile', 'sz_modularidade_user_fields' );
add_action( 'edit_user_profile', 'sz_modularidade_user_fields' );

function sz_modularidade_user_fields( WP_User $user ): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;

    $has_mb  = sz_producer_has_motoboy( $user->ID );
    $has_exp = sz_producer_has_expedicao( $user->ID );

    // Detecta se é afiliado puro (sem acesso de produtor): expedição bloqueada
    $is_affiliate_only = false;
    if ( function_exists( 'sz_aff_table' ) ) {
        global $wpdb;
        $is_affiliate_only = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COUNT(*) FROM " . sz_aff_table('sz_affiliates') . " WHERE user_id=%d AND status='active' AND deleted_at IS NULL",
            $user->ID
        ) ) > 0 && ! $has_mb && ! $has_exp;
    }
    ?>
    <h2>⚙️ Senderzz — Modalidades</h2>
    <?php
    // Campo global: template de checkout sem CPF (mostrar só para admins)
    if ( current_user_can('manage_woocommerce') ) :
        $tmpl_sem_cpf = (int) get_option('senderzz_checkout_template_id_sem_cpf', 1075);
    ?>
    <h2 style="margin-top:20px">🏍️ Senderzz — Template Checkout Motoboy sem CPF</h2>
    <table class="form-table">
        <tr>
            <th><label for="sz_template_sem_cpf">ID do checkout sem CPF</label></th>
            <td>
                <input type="number" name="sz_template_sem_cpf" id="sz_template_sem_cpf"
                    value="<?php echo esc_attr($tmpl_sem_cpf ?: ''); ?>" class="small-text" min="1">
                <span class="description">
                    Post ID do template FunnelKit (wfacp_checkout) sem campo CPF.
                    Usado quando o produtor habilita "Dispensar CPF no checkout motoboy".
                    Deixe em branco para usar o template padrão.
                </span>
            </td>
        </tr>
    </table>
    <?php wp_nonce_field('sz_template_sem_cpf_save','sz_template_sem_cpf_nonce'); ?>
    <?php endif; ?>
    <table class="form-table">
        <tr>
            <th><label for="sz_has_motoboy">Motoboy habilitado</label></th>
            <td>
                <input type="checkbox" name="sz_has_motoboy" id="sz_has_motoboy" value="1" <?php checked( $has_mb ); ?>>
                <span class="description">Permite uso da modalidade motoboy por este produtor.</span>
            </td>
        </tr>
        <tr>
            <th><label for="sz_has_expedicao">Expedição habilitada</label></th>
            <td>
                <input type="checkbox" name="sz_has_expedicao" id="sz_has_expedicao" value="1" <?php checked( $has_exp ); ?> <?php echo $is_affiliate_only ? 'disabled title="Afiliados puros não podem usar expedição"' : ''; ?>>
                <span class="description">Permite uso de expedição via Melhor Envio por este produtor.<?php echo $is_affiliate_only ? ' <strong style="color:#b91c1c;">Bloqueado: usuário é afiliado puro.</strong>' : ''; ?></span>
            </td>
        </tr>
        <!-- CPF: gerenciado exclusivamente pelo portal do produtor -->
    </table>
    <?php wp_nonce_field( 'sz_modularidade_save', 'sz_modularidade_nonce' ); ?>
    <?php
}

// ─── 10. Salvar campos de modularidade ────────────────────────────────────────
add_action( 'personal_options_update',  'sz_modularidade_save_fields' );
add_action( 'edit_user_profile_update', 'sz_modularidade_save_fields' );

function sz_modularidade_save_fields( int $user_id ): void {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    if ( ! isset( $_POST['sz_modularidade_nonce'] ) ) return;
    if ( ! wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['sz_modularidade_nonce'] ) ), 'sz_modularidade_save' ) ) return;

    update_user_meta( $user_id, SZ_META_HAS_MOTOBOY,   isset( $_POST['sz_has_motoboy'] )            ? '1' : '0' );

    // Expedição: nunca habilitada para afiliados puros (sem acesso de produtor)
    $has_expedicao_requested = isset( $_POST['sz_has_expedicao'] ) ? '1' : '0';
    if ( $has_expedicao_requested === '1' && function_exists( 'sz_aff_get_affiliate_ids_for_portal_user' ) ) {
        global $wpdb;
        $is_affiliate_only = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COUNT(*) FROM " . ( function_exists('sz_aff_table') ? sz_aff_table('sz_affiliates') : $wpdb->prefix.'sz_affiliates' ) . " WHERE user_id=%d AND status='active' AND deleted_at IS NULL",
            $user_id
        ) ) > 0;
        $has_motoboy  = get_user_meta( $user_id, SZ_META_HAS_MOTOBOY, true ) === '1';
        $already_had_expedicao = get_user_meta( $user_id, SZ_META_HAS_EXPEDICAO, true ) === '1';
        // Bloqueia expedição se for afiliado sem nenhuma modalidade de produtor
        if ( $is_affiliate_only && ! $has_motoboy && ! $already_had_expedicao ) {
            $has_expedicao_requested = '0';
        }
    }
    update_user_meta( $user_id, SZ_META_HAS_EXPEDICAO, $has_expedicao_requested );

    wp_cache_delete( 'sz_mb_portal_uid_' . $user_id, 'sz_motoboy' );
}

// ─── 11. Bloquear método de frete motoboy se produtor não tem motoboy ─────────
add_filter( 'woocommerce_package_rates', function( $rates, $package ) {
    if ( is_admin() && ! wp_doing_ajax() ) return $rates;
    $user_id = sz_mb_get_portal_user_id();
    if ( ! $user_id ) return $rates;
    if ( sz_producer_has_motoboy( $user_id ) ) return $rates;
    // Remove método motoboy
    foreach ( array_keys( $rates ) as $key ) {
        if ( strpos( $key, 'sz_motoboy' ) !== false ) {
            unset( $rates[ $key ] );
        }
    }
    return $rates;
}, 1000, 2 );

// ─── 12. Bloquear geração de etiqueta Melhor Envio se sem expedição ───────────
add_filter( 'sz_can_generate_label', function( bool $can, int $order_id ): bool {
    if ( ! $can ) return false;
    $order   = wc_get_order( $order_id );
    if ( ! $order ) return false;
    $user_id = sz_mb_get_portal_user_id();
    if ( ! $user_id ) {
        // Tentar resolver pelo produtor do link do pedido
        $user_id = (int) $order->get_meta( '_sz_portal_user_id', true );
    }
    if ( ! $user_id ) return $can;
    return sz_producer_has_expedicao( $user_id ) ? $can : false;
}, 10, 2 );
