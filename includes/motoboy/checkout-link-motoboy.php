<?php
/**
 * Senderzz — Módulo Motoboy
 * Checkout Links: adiciona coluna 'tipo' e gera link motoboy junto com correio
 *
 * Incluir em senderzz-logistics.php:
 *   'includes/motoboy/checkout-link-motoboy.php',
 */
if ( ! defined( 'ABSPATH' ) ) exit;

// ─── Helper global: identifica se o checkout atual é realmente Motoboy ──────
// Evita vazamento de sessão: link de Expedição nunca deve carregar bloco Motoboy.
if ( ! function_exists( 'sz_mb_current_checkout_is_motoboy' ) ) {
    function sz_mb_current_checkout_is_motoboy(): bool {
        if ( is_admin() && ! wp_doing_ajax() ) return false;

        $clear_forced_motoboy = static function(): void {
            if ( function_exists( 'WC' ) && WC()->session ) {
                WC()->session->__unset( 'sz_frete_forcado' );
                WC()->session->__unset( 'sz_motoboy_data' );
            }
        };

        $request_has_motoboy_flag = false;

        // Parâmetro público neutro: szm=1 (atual) ou m=1/frete=motoboy (legado).
        if ( isset( $_REQUEST['szm'] ) && sanitize_text_field( wp_unslash( $_REQUEST['szm'] ) ) === '1' ) {
            $request_has_motoboy_flag = true;
        }
        if ( isset( $_REQUEST['m'] ) && sanitize_text_field( wp_unslash( $_REQUEST['m'] ) ) === '1' ) {
            $request_has_motoboy_flag = true;
        }
        if ( isset( $_REQUEST['frete'] ) && sanitize_text_field( wp_unslash( $_REQUEST['frete'] ) ) === 'motoboy' ) {
            $request_has_motoboy_flag = true;
        }

        $sz_token = isset( $_REQUEST['sz'] ) ? sanitize_text_field( wp_unslash( $_REQUEST['sz'] ) ) : '';
        $referer_has_motoboy_flag = false;

        // FunnelKit pode renderizar partes do checkout via AJAX; nesses casos os
        // parâmetros originais costumam vir em REQUEST ou HTTP_REFERER.
        if ( ! empty( $_SERVER['HTTP_REFERER'] ) ) {
            $ref_query = wp_parse_url( esc_url_raw( wp_unslash( $_SERVER['HTTP_REFERER'] ) ), PHP_URL_QUERY );
            if ( $ref_query ) {
                parse_str( $ref_query, $ref_args );
                if ( ! empty( $ref_args['szm'] ) && sanitize_text_field( wp_unslash( $ref_args['szm'] ) ) === '1' ) {
                    $referer_has_motoboy_flag = true;
                }
                if ( ! empty( $ref_args['m'] ) && sanitize_text_field( wp_unslash( $ref_args['m'] ) ) === '1' ) {
                    $referer_has_motoboy_flag = true;
                }
                if ( ! empty( $ref_args['frete'] ) && sanitize_text_field( wp_unslash( $ref_args['frete'] ) ) === 'motoboy' ) {
                    $referer_has_motoboy_flag = true;
                }
                if ( ! $sz_token && ! empty( $ref_args['sz'] ) ) {
                    $sz_token = sanitize_text_field( wp_unslash( $ref_args['sz'] ) );
                }
            }
        }

        // Regra principal: quando existe token, o tipo do link no banco manda.
        // Isso evita vazamento de sessão: um acesso anterior ao Motoboy não pode
        // transformar o link de Expedição/Correio em Motoboy.
        if ( $sz_token ) {
            global $wpdb;
            $link = $wpdb->get_row( $wpdb->prepare(
                "SELECT * FROM {$wpdb->prefix}senderzz_checkout_links WHERE slug = %s OR token = %s LIMIT 1",
                $sz_token,
                $sz_token
            ), ARRAY_A );

            if ( is_array( $link ) && function_exists( 'senderzz_checkout_legacy_repair_single_token' ) ) {
                $repaired = senderzz_checkout_legacy_repair_single_token( $sz_token, $link );
                if ( is_array( $repaired ) ) $link = $repaired;
            }

            $tipo = is_array( $link ) ? strtolower( (string) ( $link['tipo'] ?? '' ) ) : '';
            if ( is_array( $link ) && function_exists( 'senderzz_checkout_legacy_infer_type' ) ) {
                $tipo = senderzz_checkout_legacy_infer_type( $link );
            }

            if ( $tipo === 'motoboy' ) {
                if ( function_exists( 'WC' ) && WC()->session ) {
                    WC()->session->set( 'sz_frete_forcado', 'motoboy' );
                }
                return true;
            }

            // Token existe, mas não é Motoboy: limpar sessão e bloquear bloco Motoboy.
            $clear_forced_motoboy();
            return false;
        }

        // Sem token, só aceita Motoboy por flag explícita da própria requisição/referer.
        // Nunca usar apenas sessão como fonte de verdade, porque ela vazava para links de Expedição.
        if ( $request_has_motoboy_flag || $referer_has_motoboy_flag ) {
            if ( function_exists( 'WC' ) && WC()->session ) {
                WC()->session->set( 'sz_frete_forcado', 'motoboy' );
            }
            return true;
        }

        $clear_forced_motoboy();
        return false;
    }
}


// ─── 1. Adiciona coluna 'tipo' na tabela de links ────────────────────────────
add_action( 'init', function() {
    global $wpdb;
    $t    = $wpdb->prefix . 'senderzz_checkout_links';
    $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$t}`", 0 );
    if ( is_array( $cols ) && ! in_array( 'tipo', $cols, true ) ) {
        $wpdb->query( "ALTER TABLE `{$t}` ADD `tipo` VARCHAR(20) NOT NULL DEFAULT 'correio' AFTER `url`" );
    }
    if ( is_array( $cols ) && ! in_array( 'link_motoboy_id', $cols, true ) ) {
        $wpdb->query( "ALTER TABLE `{$t}` ADD `link_motoboy_id` BIGINT UNSIGNED NULL AFTER `tipo`" );
    }
}, 15 );

// ─── 2. Intercepta geração do link e cria também o motoboy ──────────────────
add_filter( 'senderzz_checkout_link_generated', function( array $result, object $user, array $data ) {
    if ( ! $result['success'] ) return $result;

    global $wpdb;
    $t = $wpdb->prefix . 'senderzz_checkout_links';

    // Marca o link original como 'correio'
    $wpdb->update( $t, [ 'tipo' => 'correio' ], [ 'id' => $result['link_id'] ] );

    // Verifica se o produtor tem motoboy habilitado
    $wp_user_id = sz_mb_get_wp_user_id_for_portal( $user );
    // Fallback: tentar pelo ID direto do objeto user
    if ( ! $wp_user_id && ! empty( $user->id ) ) {
        $wp_uid_row = $wpdb->get_var( $wpdb->prepare(
            "SELECT wp_user_id FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d LIMIT 1",
            (int) $user->id
        ) );
        $wp_user_id = $wp_uid_row ? (int) $wp_uid_row : 0;
    }
    // Gerar link motoboy — só bloquear se _sz_has_motoboy = '0' explícito
    if ( $wp_user_id && get_user_meta( $wp_user_id, '_sz_has_motoboy', true ) === '0' ) return $result;

    // CPF: ler meta diretamente
    $token_mb    = bin2hex( random_bytes(16) );
    $disp_cpf_mb = $wp_user_id && get_user_meta( $wp_user_id, '_sz_dispensar_cpf_motoboy', true ) === '1';
    $template_key = $disp_cpf_mb ? 'senderzz_checkout_template_id_sem_cpf' : 'senderzz_checkout_template_id';
    $default_id   = $disp_cpf_mb ? 1075 : (int) get_option( 'senderzz_checkout_template_id', 140 );
    $template_id  = (int) get_option( $template_key, $default_id );
    $base_url     = get_permalink( $template_id );
    if ( ! $base_url ) { $base_url = home_url( $disp_cpf_mb ? '/checkouts/codsfpc/' : '/checkouts/cod/' ); }
    $url_mb       = add_query_arg( [ 'sz' => $token_mb, 'szm' => '1' ], $base_url );

    $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$t}`", 0 );
    $cols = is_array( $cols ) ? $cols : [];

    $payload_mb = wp_json_encode( [
        'components' => is_array( $data['components'] ?? null ) ? $data['components'] : [],
        'valor'      => (float) ( $data['valor'] ?? 0 ),
    ] );

    $insert_data = [
        'user_id'         => (int) $user->id,
        'post_id'         => $template_id,
        'name'            => $disp_cpf_mb ? 'COD - Senderzz' : ( $result['name'] . ' — Motoboy' ),
        'slug'            => $token_mb,
        'token'           => $token_mb,
        'url'             => $url_mb,
        'tipo'            => 'motoboy',
        'components_text' => $result['components'],
        'price_label'     => $result['price_label'],
        'created_at'      => sz_motoboy_now_mysql(),
        'updated_at'      => sz_motoboy_now_mysql(),
    ];

    if ( in_array( 'producer_id', $cols, true ) ) {
        $insert_data['producer_id'] = (int) $user->id;
    }
    if ( in_array( 'shipping_class_id', $cols, true ) ) {
        $_mb_user_classes = function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($user) : [];
        $insert_data['shipping_class_id'] = ! empty( $_mb_user_classes ) ? (int) $_mb_user_classes[0] : (int) ( $user->shipping_class_id ?? 0 );
    }
    if ( in_array( 'affiliate_visible', $cols, true ) ) {
        $insert_data['affiliate_visible'] = ! empty( $_POST['cl_affiliate_visible'] ) ? 1 : 0;
    }
    if ( in_array( 'display_value', $cols, true ) ) {
        $insert_data['display_value'] = (float) ( $data['valor'] ?? 0 );
    }
    if ( in_array( 'payload', $cols, true ) ) {
        $insert_data['payload'] = $payload_mb;
    }
    if ( in_array( 'affiliate_commission_pct', $cols, true ) ) {
        $insert_data['affiliate_commission_pct'] = (float) ( $data['checkout_commission_pct'] ?? $data['commission_pct'] ?? ( function_exists( 'sz_aff_producer_default_commission_pct' ) ? sz_aff_producer_default_commission_pct( (int) $user->id ) : 0 ) );
    }
    if ( in_array( 'schema_version', $cols, true ) ) {
        $insert_data['schema_version'] = 'v374';
    }

    $mb_id = $wpdb->insert( $t, $insert_data );

    if ( ! $mb_id ) {
        return $result;
    }

    $mb_insert_id = (int) $wpdb->insert_id;
    // Vincula o link motoboy ao link correio
    $wpdb->update( $t, [ 'link_motoboy_id' => $mb_insert_id ], [ 'id' => $result['link_id'] ] );
    $result['url_motoboy']     = $url_mb;
    $result['link_motoboy_id'] = $mb_insert_id;

    return $result;
}, 10, 3 );

// ─── 3. Filtro de rates consolidado — ver filtro principal sz-v98 abaixo (prioridade 10000)

// Persiste o parâmetro frete=motoboy na sessão WC
add_action( 'wp', function() {
    if ( ! function_exists( 'is_checkout' ) || ! is_checkout() || ! function_exists( 'WC' ) || ! WC()->session ) return;

    if ( sz_mb_current_checkout_is_motoboy() ) {
        WC()->session->set( 'sz_frete_forcado', 'motoboy' );
        return;
    }

    // Link de Expedição/Correio: limpa qualquer Motoboy antigo gravado na sessão.
    if ( isset( $_GET['sz'] ) || isset( $_GET['frete'] ) || isset( $_GET['szm'] ) || isset( $_GET['m'] ) ) {
        WC()->session->set( 'sz_frete_forcado', '' );
    }
} );

// Filtro duplicado removido — consolidado no filtro sz-v98 (prioridade 10000)

// Limpa sessão após pedido
add_action( 'woocommerce_thankyou', function() {
    WC()->session && WC()->session->set( 'sz_frete_forcado', '' );
} );

// ─── 4. Helper: resolve WP user_id a partir do user do portal ────────────────
function sz_mb_get_wp_user_id_for_portal( object $user ): int {
    if ( isset($user->wp_user_id) && $user->wp_user_id ) return (int) $user->wp_user_id;
    $email = sanitize_email( $user->email ?? '' );
    if ( ! $email ) return 0;
    $wp_user = get_user_by('email', $email);
    return $wp_user ? (int) $wp_user->ID : 0;
}

// ─── 5. Exibe link motoboy na lista de checkouts do portal ───────────────────
add_filter( 'senderzz_checkout_link_row_extra', function( string $html, array $link ): string {
    if ( empty($link['url_motoboy']) && empty($link['link_motoboy_id']) ) return $html;

    global $wpdb;
    $url_mb = $link['url_motoboy'] ?? '';

    if ( ! $url_mb && ! empty($link['link_motoboy_id']) ) {
        $mb = $wpdb->get_var( $wpdb->prepare(
            "SELECT url FROM {$wpdb->prefix}senderzz_checkout_links WHERE id = %d",
            $link['link_motoboy_id']
        ) );
        $url_mb = $mb ?: '';
    }

    if ( ! $url_mb ) return $html;

    $html .= '<div style="margin-top:6px;display:flex;gap:6px;flex-wrap:wrap">'
           . '<span style="font-size:var(--sz-text-sm);color:#6b7280;font-weight:600">🏍️ Motoboy:</span>'
           . '<a href="' . esc_url($url_mb) . '" target="_blank" style="font-size:var(--sz-text-sm);color:#E8650A">Abrir</a>'
           . '<button onclick="szCopyText(this,' . esc_js(wp_json_encode($url_mb)) . ')" style="font-size:var(--sz-text-sm);background:none;border:none;color:#E8650A;cursor:pointer;padding:0">Copiar</button>'
           . '</div>';
    return $html;
}, 10, 2 );

// ─── 6. Injeta variáveis JS no checkout (funciona em aba anônima também) ────
add_action( 'wp_enqueue_scripts', function() {
    if ( ! function_exists('is_checkout') || ! is_checkout() ) return;
    $is_motoboy = sz_mb_current_checkout_is_motoboy() ? '1' : '0';
    wp_localize_script( 'senderzz-checkout', 'sz_checkout_vars', [
        'rest_url'  => rest_url('sz-motoboy/v1'),
        'is_motoboy'=> $is_motoboy,
    ] );
    if ( $is_motoboy === '1' ) {
        wp_add_inline_script( 'senderzz-checkout',
            'window.sz_motoboy_checkout="1";window.sz_checkout_vars=window.sz_checkout_vars||{};window.sz_checkout_vars.rest_url=' . wp_json_encode(rest_url('sz-motoboy/v1')) . ';window.sz_checkout_vars.is_motoboy="1";',
            'before'
        );
    }
}, 20 );

// v383 — resumo da oferta disponível no primeiro paint, sem depender de hidratação tardia.
if ( ! function_exists( 'sz_mb_checkout_offer_summary' ) ) {
    function sz_mb_checkout_offer_summary(): array {
        $summary = [ 'name' => '', 'price_label' => '', 'thumb_url' => '' ];
        $sz_token = sanitize_text_field( $_GET['sz'] ?? '' );
        if ( $sz_token ) {
            global $wpdb;
            $link = $wpdb->get_row( $wpdb->prepare(
                "SELECT * FROM {$wpdb->prefix}senderzz_checkout_links WHERE slug = %s OR token = %s LIMIT 1",
                $sz_token,
                $sz_token
            ), ARRAY_A );
            if ( is_array( $link ) && function_exists( 'senderzz_checkout_legacy_repair_single_token' ) ) {
                $fixed = senderzz_checkout_legacy_repair_single_token( $sz_token, $link );
                if ( is_array( $fixed ) ) $link = $fixed;
            }
            if ( is_array( $link ) ) {
                $summary['name'] = preg_replace('/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', (string) ( $link['name'] ?? '' ) );
                $summary['price_label'] = (string) ( $link['price_label'] ?? '' );
                foreach ( [ 'product_id', 'wc_product_id', 'produto_id' ] as $pid_key ) {
                    if ( ! empty( $link[ $pid_key ] ) && function_exists( 'wp_get_attachment_image_url' ) ) {
                        $product_id = absint( $link[ $pid_key ] );
                        if ( function_exists( 'wc_get_product' ) ) {
                            $product = wc_get_product( $product_id );
                            if ( $product && method_exists( $product, 'get_image_id' ) ) {
                                $image_id = absint( $product->get_image_id() );
                                if ( $image_id ) {
                                    $summary['thumb_url'] = (string) wp_get_attachment_image_url( $image_id, 'thumbnail' );
                                    break;
                                }
                            }
                        }
                    }
                }
            }
        }
        if ( function_exists( 'WC' ) && WC()->cart ) {
            foreach ( WC()->cart->get_cart() as $item ) {
                if ( empty( $summary['thumb_url'] ) && ! empty( $item['data'] ) && is_object( $item['data'] ) && method_exists( $item['data'], 'get_image_id' ) ) {
                    $image_id = absint( $item['data']->get_image_id() );
                    if ( $image_id && function_exists( 'wp_get_attachment_image_url' ) ) {
                        $summary['thumb_url'] = (string) wp_get_attachment_image_url( $image_id, 'thumbnail' );
                    }
                }
                if ( empty( $summary['name'] ) && ! empty( $item['senderzz_offer_name'] ) ) {
                    $summary['name'] = preg_replace('/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', sanitize_text_field( (string) $item['senderzz_offer_name'] ) );
                }
                if ( empty( $summary['price_label'] ) && ! empty( $item['senderzz_offer_value'] ) ) {
                    $summary['price_label'] = function_exists( 'sz_mb_money_label' ) ? sz_mb_money_label( (float) $item['senderzz_offer_value'] ) : (string) $item['senderzz_offer_value'];
                }
            }
        }
        return $summary;
    }
}

if ( ! function_exists( 'sz_mb_parse_money_label' ) ) {
    function sz_mb_parse_money_label( $value ): float {
        if ( is_numeric( $value ) ) {
            return max( 0, (float) $value );
        }

        $raw = trim( wp_strip_all_tags( (string) $value ) );
        if ( $raw === '' ) return 0.0;

        $raw = preg_replace( '/[^0-9,\.\-]/', '', $raw );
        if ( $raw === '' ) return 0.0;

        $has_comma = strpos( $raw, ',' ) !== false;
        $has_dot   = strpos( $raw, '.' ) !== false;

        if ( $has_comma && $has_dot ) {
            $raw = str_replace( '.', '', $raw );
            $raw = str_replace( ',', '.', $raw );
        } elseif ( $has_comma ) {
            $raw = str_replace( ',', '.', $raw );
        }

        return max( 0, (float) $raw );
    }
}

if ( ! function_exists( 'sz_mb_checkout_offer_display_data' ) ) {
    function sz_mb_checkout_offer_display_data(): array {
        $summary = function_exists( 'sz_mb_checkout_offer_summary' ) ? sz_mb_checkout_offer_summary() : [ 'name' => '', 'price_label' => '', 'thumb_url' => '' ];

        $product_label     = trim( (string) ( $summary['name'] ?? '' ) );
        $product_thumb_url = trim( (string) ( $summary['thumb_url'] ?? '' ) );
        $order_total       = function_exists( 'sz_mb_parse_money_label' ) ? sz_mb_parse_money_label( $summary['price_label'] ?? '' ) : 0.0;

        $sz_offer_data = function_exists( 'senderzz_sz_get_data' ) ? senderzz_sz_get_data() : null;
        if ( is_array( $sz_offer_data ) ) {
            if ( ! empty( $sz_offer_data['name'] ) ) {
                $product_label = sanitize_text_field( (string) $sz_offer_data['name'] );
                $product_label = preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', $product_label );
            }
            if ( isset( $sz_offer_data['valor'] ) && (float) $sz_offer_data['valor'] > 0 ) {
                $order_total = (float) $sz_offer_data['valor'];
            }
        }

        if ( empty( $product_label ) && ! empty( $_COOKIE['senderzz_offer_name'] ) ) {
            $product_label = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_offer_name'] ) );
            $product_label = preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', $product_label );
        }
        if ( $order_total <= 0 && ! empty( $_COOKIE['senderzz_offer_value'] ) ) {
            $order_total = function_exists( 'sz_mb_parse_money_label' ) ? sz_mb_parse_money_label( wp_unslash( $_COOKIE['senderzz_offer_value'] ) ) : (float) $_COOKIE['senderzz_offer_value'];
        }

        if ( function_exists( 'WC' ) && WC()->cart ) {
            $items = WC()->cart->get_cart();
            $names = [];
            foreach ( $items as $item ) {
                if ( empty( $item['data'] ) || ! is_object( $item['data'] ) ) { continue; }
                if ( empty( $product_thumb_url ) && method_exists( $item['data'], 'get_image_id' ) ) {
                    $image_id = absint( $item['data']->get_image_id() );
                    if ( $image_id && function_exists( 'wp_get_attachment_image_url' ) ) {
                        $product_thumb_url = (string) wp_get_attachment_image_url( $image_id, 'thumbnail' );
                    }
                }
                if ( empty( $product_label ) && ! empty( $item['senderzz_offer_name'] ) ) {
                    $product_label = sanitize_text_field( (string) $item['senderzz_offer_name'] );
                    $product_label = preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', $product_label );
                }
                if ( $order_total <= 0 && ! empty( $item['senderzz_offer_value'] ) ) {
                    $order_total = (float) $item['senderzz_offer_value'];
                }
                if ( method_exists( $item['data'], 'get_name' ) ) {
                    $qty  = ! empty( $item['quantity'] ) ? (int) $item['quantity'] : 1;
                    $name = function_exists( 'senderzz_clean_product_label' ) ? senderzz_clean_product_label( $item['data']->get_name() ) : $item['data']->get_name();
                    $names[] = $qty > 1 ? $name . ' × ' . $qty : $name;
                }
            }
            if ( empty( $product_label ) && ! empty( $names ) ) {
                $product_label = implode( ', ', $names );
            }
            if ( $order_total <= 0 ) {
                $order_total = (float) WC()->cart->get_subtotal();
            }
        }

        if ( empty( $product_label ) ) {
            $product_label = 'Oferta selecionada';
        }

        $order_value = function_exists( 'sz_mb_money_label' ) ? sz_mb_money_label( $order_total ) : 'R$ ' . number_format( max( 0, $order_total ), 2, ',', '.' );

        return [
            'name'        => $product_label,
            'order_total' => $order_total,
            'order_value' => $order_value,
            'thumb_url'   => $product_thumb_url,
        ];
    }
}

if ( ! function_exists( 'sz_mb_render_checkout_offer_strip' ) ) {
    function sz_mb_render_checkout_offer_strip( array $data = [], string $context = 'first' ): string {
        if ( empty( $data ) && function_exists( 'sz_mb_checkout_offer_display_data' ) ) {
            $data = sz_mb_checkout_offer_display_data();
        }

        $product_label     = trim( (string) ( $data['name'] ?? 'Oferta selecionada' ) );
        $product_thumb_url = trim( (string) ( $data['thumb_url'] ?? '' ) );
        $order_total       = max( 0, (float) ( $data['order_total'] ?? 0 ) );
        $order_value       = trim( (string) ( $data['order_value'] ?? '' ) );

        if ( $order_value === '' && $order_total > 0 ) {
            $order_value = function_exists( 'sz_mb_money_label' ) ? sz_mb_money_label( $order_total ) : 'R$ ' . number_format( $order_total, 2, ',', '.' );
        }

        $installment_select = function_exists( 'sz_mb_render_installment_select' ) ? sz_mb_render_installment_select( $order_total, 1 ) : '';
        $is_first_context   = $context === 'first';

        ob_start();
        ?>
        <div <?php echo $is_first_context ? 'id="sz-oferta-bar"' : ''; ?> class="sz-mb-offer-strip senderzz-checkout-offer-bar <?php echo $is_first_context ? 'sz-mb-first-step-offer-strip' : ''; ?>" data-sz-mb-offer-strip>
            <div class="sz-mb-offer-strip-main">
                <span class="sz-mb-offer-thumb"><?php if ( ! empty( $product_thumb_url ) ) : ?><img src="<?php echo esc_url( $product_thumb_url ); ?>" alt="" loading="lazy"><?php else : ?>📦<?php endif; ?></span>
                <span class="sz-mb-offer-copy"><small>Oferta selecionada</small><strong class="sz-mb-offer-name"><?php echo esc_html( $product_label ?: 'Oferta selecionada' ); ?></strong></span>
            </div>
            <div class="sz-mb-offer-strip-price"><small>Valor</small><strong class="sz-mb-offer-value"><?php echo esc_html( $order_value ); ?></strong></div>
            <div class="sz-mb-offer-strip-installments"><small>Parcelamento</small><?php echo $installment_select; ?></div>
        </div>
        <?php
        return trim( ob_get_clean() );
    }
}


if ( ! function_exists( 'sz_mb_output_first_step_offer_bar' ) ) {
    function sz_mb_output_first_step_offer_bar(): void {
        static $printed = false;
        if ( $printed || ! function_exists( 'is_checkout' ) || ! is_checkout() || ! sz_mb_current_checkout_is_motoboy() ) return;

        $data = function_exists( 'sz_mb_checkout_offer_display_data' ) ? sz_mb_checkout_offer_display_data() : [];
        $name = trim( (string) ( $data['name'] ?? '' ) );
        $price = trim( (string) ( $data['order_value'] ?? '' ) );
        if ( $name === '' && $price === '' ) return;

        $printed = true;
        echo function_exists( 'sz_mb_render_checkout_offer_strip' ) ? sz_mb_render_checkout_offer_strip( $data, 'first' ) : '';
    }
}
// Senderzz: a barra da oferta deve aparecer somente na etapa 2 (card de datas).
// Não registrar os hooks da etapa 1 para evitar duplicidade/clone visual.
// add_action( 'woocommerce_before_checkout_billing_form', 'sz_mb_output_first_step_offer_bar', 1 );
// add_action( 'woocommerce_checkout_before_customer_details', 'sz_mb_output_first_step_offer_bar', 1 );
// add_action( 'wfacp_woocommerce_before_checkout_billing_form', 'sz_mb_output_first_step_offer_bar', 1 );

if ( ! function_exists( 'sz_mb_lock_checkout_fields_initially' ) ) {
    function sz_mb_lock_checkout_fields_initially( array $fields ): array {
        if ( ! sz_mb_current_checkout_is_motoboy() ) return $fields;
        $lock_keys = [
            'billing' => [ 'billing_phone', 'billing_postcode', 'billing_address_1', 'billing_address_2', 'billing_city', 'billing_state', 'billing_neighborhood', 'billing_bairro', 'billing_number', 'billing_numero', 'billing_address_number' ],
            'shipping' => [ 'shipping_phone', 'shipping_postcode', 'shipping_address_1', 'shipping_address_2', 'shipping_city', 'shipping_state' ],
        ];
        foreach ( $lock_keys as $group => $keys ) {
            if ( empty( $fields[ $group ] ) || ! is_array( $fields[ $group ] ) ) continue;
            foreach ( $keys as $key ) {
                if ( empty( $fields[ $group ][ $key ] ) || ! is_array( $fields[ $group ][ $key ] ) ) continue;
                $fields[ $group ][ $key ]['custom_attributes'] = $fields[ $group ][ $key ]['custom_attributes'] ?? [];
                $fields[ $group ][ $key ]['custom_attributes']['disabled'] = 'disabled';
                $fields[ $group ][ $key ]['custom_attributes']['aria-disabled'] = 'true';
                $fields[ $group ][ $key ]['custom_attributes']['tabindex'] = '-1';
            }
        }
        return $fields;
    }
}
add_filter( 'woocommerce_checkout_fields', 'sz_mb_lock_checkout_fields_initially', 30 );

// wp_head — injeta vars globais em todos os checkouts Senderzz
add_action( 'wp_head', function() {
    if ( is_admin() ) return;
    if ( ! function_exists('is_checkout') || ! is_checkout() ) return;

    $is_motoboy = sz_mb_current_checkout_is_motoboy() ? '1' : '0';

    // Lê nome e preço do link direto do banco (funciona para motoboy e expedição)
    $nome_oferta  = '';
    $preco_oferta = '';
    $sz_token = sanitize_text_field( $_GET['sz'] ?? '' );
    if ( $sz_token ) {
        global $wpdb;
        $link = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}senderzz_checkout_links WHERE slug = %s OR token = %s LIMIT 1",
            $sz_token,
            $sz_token
        ), ARRAY_A );
        if ( is_array( $link ) && function_exists( 'senderzz_checkout_legacy_repair_single_token' ) ) {
            $fixed = senderzz_checkout_legacy_repair_single_token( $sz_token, $link );
            if ( is_array( $fixed ) ) $link = $fixed;
        }
        if ( $link ) {
            $nome_oferta  = preg_replace('/ — (Motoboy|Correio|Expedição)$/i', '', $link['name'] ?? '');
            $preco_oferta = $link['price_label'] ?? '';
        }
    }

    $summary = function_exists( 'sz_mb_checkout_offer_summary' ) ? sz_mb_checkout_offer_summary() : [ 'name' => $nome_oferta, 'price_label' => $preco_oferta ];
    if ( ! empty( $summary['name'] ) ) $nome_oferta = (string) $summary['name'];
    if ( ! empty( $summary['price_label'] ) ) $preco_oferta = (string) $summary['price_label'];
    $thumb_oferta = ! empty( $summary['thumb_url'] ) ? (string) $summary['thumb_url'] : '';

    echo '<script id="sz-mb-critical-boot">
(function(){
  var doc=document.documentElement;
  if(' . wp_json_encode($is_motoboy) . '==="1") doc.classList.add("sz-mb-booting");
  window.sz_motoboy_checkout=' . wp_json_encode($is_motoboy) . ';
  window.sz_dispensar_cpf=' . wp_json_encode( $is_motoboy === '1' && sz_producer_dispensar_cpf_motoboy( sz_mb_get_portal_user_id() ) ) . ';
  window.sz_checkout_vars=window.sz_checkout_vars||{};
  window.sz_checkout_vars.rest_url=' . wp_json_encode(rest_url('sz-motoboy/v1')) . ';
  window.sz_checkout_vars.is_motoboy=' . wp_json_encode($is_motoboy) . ';
  window.sz_oferta_nome=' . wp_json_encode($nome_oferta) . ';
  window.sz_oferta_preco=' . wp_json_encode($preco_oferta) . ';
  window.sz_oferta_thumb=' . wp_json_encode($thumb_oferta) . ';
  window.szMbCheckoutReady=function(){document.documentElement.classList.remove("sz-mb-booting");document.documentElement.classList.add("sz-mb-ready");};
  // v418: revela o formulário assim que o DOM estiver pronto (e teto rígido de 700ms),
  // em vez de segurar escondido por 1,6s — era isso que deixava o card vazio na carga.
  if(document.readyState!=="loading"){ window.szMbCheckoutReady(); }
  else { document.addEventListener("DOMContentLoaded", function(){ window.szMbCheckoutReady(); }, {once:true}); }
  setTimeout(function(){ if(window.szMbCheckoutReady) window.szMbCheckoutReady(); }, 700);
})();
</script>
<style id="sz-mb-critical-no-fouc">
html.sz-mb-booting .wfacp_main_form,html.sz-mb-booting form.checkout{opacity:0!important;visibility:hidden!important}
html.sz-mb-ready .wfacp_main_form,html.sz-mb-ready form.checkout{opacity:1!important;visibility:visible!important}

#sz-oferta-bar.sz-mb-offer-strip{
  display:grid!important;
  grid-template-columns:minmax(230px,1fr) minmax(185px,205px) minmax(300px,1.08fr)!important;
  align-items:center!important;
  column-gap:20px!important;
  row-gap:0!important;
  margin:16px 0 18px!important;
  padding:14px 16px!important;
  border:0!important;
  border-radius:18px!important;
  background:linear-gradient(135deg,#E8650A 0%,#F97316 52%,#C8540A 100%)!important;
  color:#fff!important;
  box-shadow:none!important;
  overflow:hidden!important;
}

#sz-oferta-bar .sz-mb-offer-strip-main{
  display:grid!important;
  grid-template-columns:58px minmax(0,1fr)!important;
  align-items:center!important;
  column-gap:13px!important;
  min-width:0!important;
  height:58px!important;
}

#sz-oferta-bar .sz-mb-offer-thumb{
  width:58px!important;
  height:58px!important;
  border-radius:15px!important;
  background:rgba(255,255,255,.24)!important;
  display:flex!important;
  align-items:center!important;
  justify-content:center!important;
  overflow:hidden!important;
  font-size:24px;
  flex:0 0 auto!important;
}

#sz-oferta-bar .sz-mb-offer-thumb img{
  display:block!important;
  width:100%!important;
  height:100%!important;
  object-fit:cover!important;
  border-radius:15px!important;
}

#sz-oferta-bar .sz-mb-offer-copy,
#sz-oferta-bar .sz-mb-offer-strip-price,
#sz-oferta-bar .sz-mb-offer-strip-installments{
  min-width:0!important;
  align-self:center!important;
}

#sz-oferta-bar small{
  display:block!important;
  height:13px!important;
  margin:0 0 7px!important;
  color:#fff!important;
  font-size:13px;
  font-weight:700;
  line-height:13px;
  letter-spacing:0;
  text-transform:none;
  opacity:.96!important;
  text-align:left!important;
}

#sz-oferta-bar .sz-mb-offer-strip-main strong{
  display:-webkit-box!important;
  color:#fff!important;
  font-size:clamp(18px,2.05vw,24px);
  font-weight:700;
  line-height:1.03;
  white-space:normal!important;
  overflow:hidden!important;
  text-overflow:clip!important;
  text-align:left!important;
  -webkit-line-clamp:2!important;
  -webkit-box-orient:vertical!important;
  max-height:50px!important;
}

#sz-oferta-bar .sz-mb-offer-strip-price{
  position:relative!important;
  text-align:left!important;
  padding-left:20px!important;
  border-left:1px solid rgba(255,255,255,.26)!important;
  min-width:185px!important;
}

#sz-oferta-bar .sz-mb-offer-strip-price small{
  margin-bottom:6px!important;
}

#sz-oferta-bar .sz-mb-offer-strip-price strong{
  display:block!important;
  margin:0!important;
  padding:0!important;
  background:transparent!important;
  color:#fff!important;
  font-size:clamp(25px,2.65vw,31px);
  font-weight:700;
  line-height:1;
  white-space:nowrap!important;
  overflow:visible!important;
  text-overflow:clip!important;
  min-width:165px!important;
  letter-spacing:-.015em;
}

#sz-oferta-bar .sz-mb-offer-strip-installments{
  position:relative!important;
  padding-left:20px!important;
  border-left:1px solid rgba(255,255,255,.26)!important;
  min-width:300px!important;
}

#sz-oferta-bar .sz-mb-offer-strip-installments small{
  margin-bottom:7px!important;
}

#sz-oferta-bar .sz-mb-select-shell{
  position:relative!important;
  width:100%!important;
  display:block!important;
  margin:0!important;
}

#sz-oferta-bar .sz-mb-installment-select{
  appearance:none!important;
  -webkit-appearance:none!important;
  display:block!important;
  width:100%!important;
  min-width:0!important;
  min-height:48px!important;
  height:48px!important;
  padding:0 42px 0 14px!important;
  border:0!important;
  border-radius:13px!important;
  background:#fff!important;
  color:#111827!important;
  font-family:var(--sz-font);
  font-size:13px;
  font-weight:700;
  line-height:48px;
  box-shadow:none!important;
  outline:none!important;
  white-space:nowrap!important;
  overflow:hidden!important;
  text-overflow:ellipsis!important;
}

#sz-oferta-bar .sz-mb-select-arrow{
  position:absolute!important;
  right:15px!important;
  top:50%!important;
  transform:translateY(-52%)!important;
  color:#111827!important;
  font-size:22px;
  font-weight:700;
  line-height:1;
  pointer-events:none!important;
}

@media (max-width:1100px){
  #sz-oferta-bar.sz-mb-offer-strip{
    grid-template-columns:1fr!important;
    gap:12px!important;
  }

  #sz-oferta-bar .sz-mb-offer-strip-main{
    height:auto!important;
  }

  #sz-oferta-bar .sz-mb-offer-strip-price,
  #sz-oferta-bar .sz-mb-offer-strip-installments{
    padding-left:0!important;
    border-left:0!important;
    text-align:left!important;
    min-width:0!important;
  }
}

body.sz-mb-cep-ready #billing_postcode:not(:disabled),body.sz-mb-cep-ready input[name="billing_postcode"]:not(:disabled),body.sz-mb-cep-ready #shipping_postcode:not(:disabled),body.sz-mb-cep-ready input[name="shipping_postcode"]:not(:disabled){background:#fff!important;color:#111827!important;opacity:1!important;cursor:text!important}
body:not(.senderzz-cep-invalid) #billing_state,body:not(.senderzz-cep-invalid) select[name="billing_state"],body:not(.senderzz-cep-invalid) #shipping_state,body:not(.senderzz-cep-invalid) select[name="shipping_state"]{border-color:#d9e1ea!important;box-shadow:none!important}
</style>
<script id="sz-mb-offer-bar-fast">
(function(){
  // O card de oferta agora é renderizado no PHP com a mesma estrutura da etapa 2.
  // Este bloco remove qualquer tarjeta antiga que tenha sido injetada por cache/template legado.
  function cleanupLegacyBars(){
    document.querySelectorAll("#sz-oferta-bar:not(.sz-mb-offer-strip), .sz-oferta-bar:not(.sz-mb-offer-strip)").forEach(function(el){ el.remove(); });
    var bars=document.querySelectorAll("#sz-oferta-bar.sz-mb-offer-strip");
    bars.forEach(function(el,idx){ if(idx>0) el.remove(); });
  }
  if(document.readyState==="loading") document.addEventListener("DOMContentLoaded",cleanupLegacyBars,{once:true}); else cleanupLegacyBars();
})();
</script>' . "
";
}, 1 );




// Senderzz v396: ajuste fino da tarjeta de oferta da etapa 2.
add_action( 'wp_footer', function() {
    if ( is_admin() || ! function_exists( 'is_checkout' ) || ! is_checkout() || ! sz_mb_current_checkout_is_motoboy() ) return;
    echo '<style id="sz-mb-offer-strip-v396">
    #sz-oferta-bar.sz-mb-offer-strip,
    .sz-mb-date-checkout #sz-oferta-bar.sz-mb-offer-strip{
        grid-template-columns:minmax(200px,1.05fr) minmax(185px,.72fr) minmax(292px,1fr)!important;
        gap:18px!important;
        padding:14px 16px!important;
        align-items:center!important;
    }
    #sz-oferta-bar .sz-mb-offer-strip-main,
    .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-main{
        grid-template-columns:58px minmax(0,1fr)!important;
        gap:13px!important;
        align-items:center!important;
    }
    #sz-oferta-bar .sz-mb-offer-thumb,
    .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-thumb{
        width:58px!important;height:58px!important;border-radius:15px!important;
    }
    #sz-oferta-bar .sz-mb-offer-copy,
    .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-copy{
        display:flex!important;flex-direction:column!important;justify-content:center!important;align-items:flex-start!important;text-align:left!important;min-width:0!important;
    }
    #sz-oferta-bar small,
    .sz-mb-date-checkout #sz-oferta-bar small{
        margin:0 0 5px!important;font-size:12.5px;line-height:1;text-align:left!important;
    }
    #sz-oferta-bar .sz-mb-offer-name,
    .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-name{
        color:#fff!important;font-size:24px;font-weight:700;line-height:1.04;text-align:left!important;white-space:normal!important;overflow:hidden!important;text-overflow:clip!important;display:-webkit-box!important;-webkit-line-clamp:2!important;-webkit-box-orient:vertical!important;word-break:normal!important;hyphens:auto!important;
    }
    #sz-oferta-bar .sz-mb-offer-strip-price,
    .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-price{
        min-width:185px!important;padding-left:18px!important;text-align:left!important;
    }
    #sz-oferta-bar .sz-mb-offer-value,
    .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-value{
        display:block!important;min-width:176px!important;font-size:28px;line-height:1;white-space:nowrap!important;overflow:visible!important;text-overflow:clip!important;letter-spacing:-.015em;
    }
    #sz-oferta-bar .sz-mb-offer-strip-installments,
    .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments{
        min-width:292px!important;padding-left:18px!important;text-align:left!important;
    }
    #sz-oferta-bar .sz-mb-installment-select,
    .sz-mb-date-checkout #sz-oferta-bar .sz-mb-installment-select{
        min-height:48px!important;font-size:13px;font-weight:700;padding:0 42px 0 14px!important;line-height:48px;white-space:nowrap!important;
    }
    @media (max-width:1100px){
        #sz-oferta-bar.sz-mb-offer-strip,.sz-mb-date-checkout #sz-oferta-bar.sz-mb-offer-strip{grid-template-columns:1fr!important;gap:12px!important}
        #sz-oferta-bar .sz-mb-offer-strip-price,#sz-oferta-bar .sz-mb-offer-strip-installments,.sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-price,.sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments{min-width:0!important;padding-left:0!important;border-left:0!important}
        #sz-oferta-bar .sz-mb-offer-value,.sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-value{min-width:0!important}
    }
    </style>
    <script id="sz-mb-offer-strip-fit-v396">
    (function(){
        function fitOne(el,min,max){
            if(!el) return;
            el.style.fontSize=max+"px";
            var box=el.parentElement;
            if(!box) return;
            var size=max;
            while(size>min && (el.scrollWidth>box.clientWidth || el.scrollHeight>box.clientHeight+2)){
                size-=1;
                el.style.fontSize=size+"px";
            }
        }
        function fitOfferStrip(){
            document.querySelectorAll("#sz-oferta-bar .sz-mb-offer-name").forEach(function(el){fitOne(el,16,24);});
            document.querySelectorAll("#sz-oferta-bar .sz-mb-offer-value").forEach(function(el){fitOne(el,24,28);});
        }
        if(document.readyState==="loading") document.addEventListener("DOMContentLoaded",fitOfferStrip,{once:true}); else fitOfferStrip();
        window.addEventListener("resize",function(){clearTimeout(window.__szOfferFit);window.__szOfferFit=setTimeout(fitOfferStrip,80);});
        document.body&&document.body.addEventListener("change",function(e){if(e.target&&e.target.matches(".sz-mb-installment-select")) setTimeout(fitOfferStrip,30);},true);
    })();
    </script>';
}, 99 );


// ─── 7. Ocultar taxa de frete no resumo do checkout motoboy ─────────────────
add_action( 'wp_head', function() {
    if ( ! function_exists('is_checkout') || ! is_checkout() || ! sz_mb_current_checkout_is_motoboy() ) return;
    echo '<style>
    /* Motoboy: remove SOMENTE a linha nativa isolada do método de entrega.
       Não esconder wrappers/tabelas do FunnelKit, pois isso derruba o checkout inteiro.
       Mantém somente o card Senderzz detalhado que é injetado abaixo. */
    ul#shipping_method > li,
    .woocommerce-shipping-methods > li,
    .wfacp_single_shipping_method,
    tr.woocommerce-shipping-totals.shipping,
    .woocommerce-checkout-review-order-table tr.shipping,
    .shop_table tr.shipping {
        display: none !important;
    }

    /* Oculta valor do frete motoboy na tela de checkout */
    .woocommerce-shipping-totals td .amount,
    .wfacp_shipping_table .amount,
    ul.woocommerce-shipping-methods .amount,
    .woocommerce-checkout-review-order-table .shipping .amount,
    li.woocommerce-shipping-method .woocommerce-Price-amount {
        visibility: hidden !important;
        font-size: 0;
    }
    /* Oculta label "Incluso"/"Grátis" do frete motoboy */
    li.woocommerce-shipping-method::after,
    .wfacp_single_shipping_method::after {
        content: "" !important;
        display: none !important;
    }
    .wfacp-shipping-method-name ~ .wfacp-shipping-price,
    .wfacp_single_shipping_method .woocommerce-Price-amount,
    .wfacp_single_shipping_method .amount,
    .wfacp-coupon-code-wrap ~ * .amount,
    td.woocommerce-Price-amount.amount {
        display: none !important;
    }


    .woocommerce-no-shipping-available-html,
    .woocommerce-no-shipping-to-destination-html,
    .wfacp_no_shipping_method,
    .wfacp_main_form .woocommerce-error li:has(.woocommerce-no-shipping-available-html) {
        display:none!important;
    }

    /* ── Form compacto ── */
    .wfacp-form-control-wrapper { margin-bottom: 8px !important; }
    .wfacp-form-control-label { font-size: var(--sz-text-meta); margin-bottom: 2px !important; }
    .wfacp-form-control { font-size: var(--sz-text-base); }
    /* Datas: fundo branco sempre */
    .sz-date-opt { background: #ffffff !important; color: #111827 !important; }
    .sz-date-opt:hover { border-color: #E8650A !important; }
    .sz-date-opt.selected { border-color: #E8650A !important; background: #ffffff !important; box-shadow: 0 0 0 3px rgba(232,101,10,.15) !important; }
    .sz-date-opt .sz-date-day { color: #9ca3af !important; }
    .sz-date-opt .sz-date-num { color: #111827 !important; }
    .sz-date-opt .sz-date-mes { color: #9ca3af !important; }
    .sz-date-opt.selected .sz-date-day, .sz-date-opt.selected .sz-date-mes { color: #E8650A !important; }


        /* v383 — acabamento final do checkout: 5 datas reais, CTA acima das copies e seletor refinado */
        .sz-mb-date-checkout .sz-mb-delivery-panel{max-width:100%!important;padding:22px 28px 20px!important;border-radius:24px!important}
        /* Senderzz v395: a barra da oferta na etapa 2 usa o mesmo visual bonito do #sz-oferta-bar, sem centralizar preço e sem compactar o valor. */
        .sz-mb-date-checkout #sz-oferta-bar.sz-mb-offer-strip,.sz-mb-date-checkout .sz-mb-offer-strip{display:grid!important;grid-template-columns:minmax(200px,1.05fr) minmax(185px,.72fr) minmax(292px,1fr)!important;align-items:center!important;gap:18px!important;margin:0 0 18px!important;padding:14px 16px!important;border:0!important;border-radius:18px!important;background:linear-gradient(135deg,#E8650A 0%,#F97316 52%,#C8540A 100%)!important;color:#fff!important;box-shadow:none!important}
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-main,.sz-mb-date-checkout .sz-mb-offer-strip-main{display:grid!important;grid-template-columns:58px minmax(0,1fr)!important;align-items:center!important;gap:13px!important;min-width:0!important}
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-price,.sz-mb-date-checkout .sz-mb-offer-strip-price{position:relative!important;text-align:left!important;padding-left:22px!important;border-left:1px solid rgba(255,255,255,.24)!important;min-width:185px!important}
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-price strong,.sz-mb-date-checkout .sz-mb-offer-strip-price strong{display:block!important;margin:0!important;padding:0!important;background:transparent!important;color:#fff!important;font-size:28px;font-weight:700;line-height:1;white-space:nowrap!important;overflow:visible!important;text-overflow:clip!important;min-width:176px!important;overflow:visible!important;text-overflow:clip!important}
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments,.sz-mb-date-checkout .sz-mb-offer-strip-installments{position:relative!important;padding-left:22px!important;border-left:1px solid rgba(255,255,255,.24)!important;min-width:0!important}
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments .sz-mb-select-shell,.sz-mb-date-checkout .sz-mb-offer-strip-installments .sz-mb-select-shell{width:100%!important;display:block!important;margin:0!important}
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments .sz-mb-installment-select,.sz-mb-date-checkout .sz-mb-offer-strip-installments .sz-mb-installment-select{width:100%!important;min-height:48px!important;border:0!important;border-radius:13px!important;background:#fff!important;color:#111827!important;padding:0 42px 0 14px!important;font-size:13px;font-weight:700;line-height:48px;white-space:nowrap!important;overflow:hidden!important;text-overflow:ellipsis!important}
        .sz-mb-date-checkout .sz-mb-date-head{grid-template-columns:52px 1fr!important;margin:2px 0 14px!important}.sz-mb-date-checkout .sz-mb-date-icon{width:52px!important;height:52px!important;border-radius:15px!important}.sz-mb-date-checkout .sz-mb-date-head strong{font-size:22px;line-height:1.12}.sz-mb-date-checkout .sz-mb-date-head small{font-size:14px}
        .sz-mb-date-checkout .sz-mb-date-options{display:grid!important;grid-template-columns:repeat(5,minmax(0,1fr))!important;gap:10px!important;margin-top:10px!important}
        .sz-mb-date-checkout .sz-mb-date-option{min-width:0!important;min-height:126px!important;padding:34px 8px 12px!important;border-radius:17px!important;gap:5px!important;background:#fff!important;overflow:hidden!important;line-height:1.1;box-shadow:none!important}
        .sz-mb-date-checkout .sz-mb-date-option b{display:block!important;width:100%!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important;font-size:16px;line-height:1.12;color:#111827!important}.sz-mb-date-checkout .sz-mb-date-option em{font-size:14px;line-height:1.12;color:#667085!important}
        .sz-mb-date-checkout .sz-mb-date-option.is-active{border-color:#E8650A!important;background:linear-gradient(180deg,#fff 0%,#fffaf7 100%)!important;box-shadow:0 10px 22px rgba(232,101,10,.10)!important}.sz-mb-date-checkout .sz-mb-date-option.is-active b,.sz-mb-date-checkout .sz-mb-date-option.is-active em{color:#E8650A!important}
        .sz-mb-date-checkout .sz-mb-date-badge{top:10px!important;left:10px!important;right:auto!important;max-width:calc(100% - 48px)!important;width:auto!important;padding:6px 10px!important;border-radius:999px!important;background:linear-gradient(135deg,#E8650A,#C8540A)!important;color:#fff!important;font-size:10px;line-height:1;white-space:nowrap!important;text-align:center!important;box-shadow:0 8px 16px rgba(232,101,10,.15)!important}
        .sz-mb-date-checkout .sz-mb-date-check{top:9px!important;right:9px!important;width:28px!important;height:28px!important;font-size:15px;background:linear-gradient(135deg,#E8650A,#C8540A)!important;color:#fff!important}
        .sz-mb-date-checkout .sz-mb-calendar-icon{width:22px!important;height:22px!important;margin:2px auto 5px!important;border-width:2px!important;border-radius:4px!important;display:block!important;position:relative!important}.sz-mb-date-checkout .sz-mb-calendar-icon:before{top:6px!important;border-top-width:2px!important}.sz-mb-date-checkout .sz-mb-calendar-icon:after{top:-5px!important}
        .sz-mb-date-checkout .sz-mb-place-order-slot{display:block!important;margin:18px auto 0!important;max-width:620px!important;width:100%!important}.sz-mb-date-checkout .sz-mb-place-order-slot #place_order,.sz-mb-date-checkout .sz-mb-place-order-slot .wfacp_place_order,.sz-mb-date-checkout .sz-mb-place-order-slot .wfacp-submit-btn{margin:0 auto!important;max-width:620px!important;width:100%!important;min-height:58px!important;border-radius:14px!important;background:linear-gradient(135deg,#FF4B00,#FF9900)!important;box-shadow:none!important}
        .sz-mb-date-checkout .sz-mb-date-alert{margin:16px 0 0!important;padding:0 0 10px!important;background:transparent!important;border-bottom:1px solid #e8edf3!important;color:#667085!important}.sz-mb-date-checkout .sz-mb-cutoff-note{margin:10px 0 0!important;color:#667085!important}.sz-mb-date-checkout .sz-mb-notice-icon{background:#f1f5f9!important;color:#64748b!important}
        @media (max-width: 900px){.sz-mb-date-checkout .sz-mb-offer-strip{grid-template-columns:1fr!important}.sz-mb-date-checkout .sz-mb-offer-strip-price{text-align:left!important}.sz-mb-date-checkout .sz-mb-date-options{grid-template-columns:repeat(3,minmax(0,1fr))!important}}
        @media (max-width: 680px){.sz-mb-date-checkout .sz-mb-delivery-panel{padding:18px 14px!important}.sz-mb-date-checkout .sz-mb-date-options{grid-template-columns:repeat(2,minmax(0,1fr))!important}.sz-mb-date-checkout .sz-mb-date-option{min-height:118px!important}}
    </style>';
}, 99 );

// ─── 8. Checkout motoboy limpo ──────────────────────────────────────────────
// Removido o painel visual Motoboy injetado no checkout.
// O checkout deve permanecer somente com a estrutura nativa FunnelKit/WooCommerce
// e a experiência Motoboy rica fica restrita ao pós-pedido/painel.

// ─── 9. Salva data de entrega no pedido ──────────────────────────────────────


// Regra comercial Senderzz: checkout aceita somente nome composto.
// A validação fica no servidor para impedir avanço por JS/cache/tema.
if ( ! function_exists( 'sz_mb_is_compound_name' ) ) {
    function sz_mb_is_compound_name( string $name ): bool {
        $name = trim( preg_replace( '/\s+/', ' ', $name ) );
        if ( $name === '' ) return false;
        $parts = array_values( array_filter( explode( ' ', $name ), static function( $p ) {
            return mb_strlen( trim( (string) $p ), 'UTF-8' ) >= 2;
        } ) );
        return count( $parts ) >= 2;
    }
}

add_action( 'woocommerce_checkout_process', function() {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return;
    $first = sanitize_text_field( wp_unslash( $_POST['billing_first_name'] ?? '' ) );
    $last  = sanitize_text_field( wp_unslash( $_POST['billing_last_name'] ?? '' ) );
    $single = sanitize_text_field( wp_unslash( $_POST['billing_name'] ?? '' ) );
    $full = trim( $single ?: trim( $first . ' ' . $last ) );
    if ( ! sz_mb_is_compound_name( $full ) ) {
        wc_add_notice( 'Informe o nome completo do cliente, com nome e sobrenome.', 'error' );
    }
}, 1 );

// Blindagem server-side da agenda Motoboy: o cliente não consegue forçar data fora
// da regra da cidade alterando o input hidden do checkout.
add_action( 'woocommerce_checkout_process', function() {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return;

    $date = sanitize_text_field( wp_unslash( $_POST['sz_delivery_date'] ?? '' ) );
    if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date ) ) {
        wc_add_notice( 'Selecione uma data de entrega válida para o Motoboy Senderzz.', 'error' );
        return;
    }

    $cep = function_exists( 'sz_mb_current_checkout_postcode' ) ? sz_mb_current_checkout_postcode() : '';
    if ( strlen( $cep ) !== 8 ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }

    if ( strlen( $cep ) === 8 && function_exists( 'sz_mb_cep_exists' ) && sz_mb_cep_exists( $cep ) !== true ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }
    if ( strlen( $cep ) === 8 && function_exists( 'sz_motoboy_resolver_zona' ) && ! sz_motoboy_resolver_zona( $cep ) ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }

    if ( strlen( $cep ) === 8 && function_exists( 'sz_motoboy_resolver_zona' ) && function_exists( 'sz_motoboy_zone_date_is_allowed' ) ) {
        $zona = sz_motoboy_resolver_zona( $cep );
        if ( $zona && ! sz_motoboy_zone_date_is_allowed( $date, $zona['dias_funcionamento'] ?? '', $zona['cutoff_horarios'] ?? '' ) ) {
            wc_add_notice( 'Essa data não está disponível para o CEP informado. Escolha uma das datas mostradas pela Senderzz para sua região.', 'error' );
            return;
        }
    }

    $city = '';
    if ( isset( $_POST['shipping_city'] ) ) $city = sanitize_text_field( wp_unslash( $_POST['shipping_city'] ) );
    if ( ! $city && isset( $_POST['billing_city'] ) ) $city = sanitize_text_field( wp_unslash( $_POST['billing_city'] ) );

    if ( ! $cep && $city && function_exists( 'sz_dr_date_allowed_for_city' ) && ! sz_dr_date_allowed_for_city( $city, $date ) ) {
        $region = function_exists( 'sz_dr_region_label_for_city' ) ? sz_dr_region_label_for_city( $city ) : '';
        $msg = $region
            ? sprintf( 'A cidade %s pertence à rota %s. Escolha uma das datas permitidas exibidas no checkout.', $city, $region )
            : sprintf( 'A data selecionada não é permitida para a cidade %s.', $city );
        wc_add_notice( $msg, 'error' );
    }
}, 2 );

add_action( 'woocommerce_checkout_create_order', function( WC_Order $order ) {
    $date = sanitize_text_field( $_POST['sz_delivery_date'] ?? '' );
    if ( $date ) {
        $order->update_meta_data( '_sz_delivery_date', $date );
    }
}, 10, 1 );

add_action( 'woocommerce_admin_order_data_after_shipping_address', function( WC_Order $order ) {
    $date = (string) ( $order->get_meta('_sz_delivery_date') ?: $order->get_meta('_senderzz_delivery_date') ?: $order->get_meta('_sz_motoboy_entrega_data') );
    $date_value = '';
    $date_fmt   = '';
    if ( $date && preg_match('/^(\d{4}-\d{2}-\d{2})/', $date, $dm) ) {
        $date_value = $dm[1];
        // Fixa timezone: T12:00:00 evita perda de um dia em UTC-3
        $date_fmt = wp_date( 'd/m/Y', strtotime( $dm[1] . 'T12:00:00' ) );
    } elseif ( $date ) {
        $date_fmt = $date;
    }

    echo '<p><strong>📅 Data de entrega solicitada:</strong> ' . esc_html( $date_fmt ?: 'Não definida' ) . '</p>';
    echo '<p class="form-field form-field-wide" style="margin-top:8px">';
    echo '<label for="_senderzz_admin_delivery_date"><strong>Reagendar entrega Senderzz</strong></label>';
    echo '<input type="date" id="_senderzz_admin_delivery_date" name="_senderzz_admin_delivery_date" value="' . esc_attr( $date_value ) . '" style="max-width:180px" />';
    echo '<span class="description">Salve/atualize o pedido para gravar a nova data.</span>';
    echo '</p>';
} );

if ( ! function_exists( 'sz_senderzz_admin_save_delivery_date' ) ) {
    function sz_senderzz_admin_save_delivery_date( WC_Order $order ): void {
        if ( ! isset( $_POST['_senderzz_admin_delivery_date'] ) ) return;
        $date = sanitize_text_field( wp_unslash( $_POST['_senderzz_admin_delivery_date'] ) );
        if ( $date !== '' && ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date ) ) return;

        $old = (string) ( $order->get_meta( '_sz_delivery_date', true ) ?: $order->get_meta( '_senderzz_delivery_date', true ) ?: $order->get_meta( '_sz_motoboy_entrega_data', true ) );
        $old_norm = ( $old && preg_match( '/^(\d{4}-\d{2}-\d{2})/', $old, $om ) ) ? $om[1] : $old;
        if ( $date === $old_norm ) return;

        if ( $date === '' ) {
            $order->delete_meta_data( '_sz_delivery_date' );
            $order->delete_meta_data( '_senderzz_delivery_date' );
            $order->delete_meta_data( '_sz_motoboy_entrega_data' );
            $order->add_order_note( 'Senderzz: data de entrega removida pelo admin sem alterar status.' );
            return;
        }

        $order->update_meta_data( '_sz_delivery_date', $date );
        $order->update_meta_data( '_senderzz_delivery_date', $date );
        $order->update_meta_data( '_sz_motoboy_entrega_data', $date );
        $order->update_meta_data( '_senderzz_motoboy_reagendado_para', $date );
        $order->update_meta_data( '_senderzz_rescheduled_at', sz_motoboy_now_mysql() );
        $order->update_meta_data( '_senderzz_rescheduled_by', get_current_user_id() );

        global $wpdb;
        $pedido = $wpdb->get_row( $wpdb->prepare( "SELECT id, status FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1", $order->get_id() ) );
        if ( $pedido ) {
            // Admin WooCommerce: mantém exatamente o status atual, só troca a data.
            $wpdb->update(
                $wpdb->prefix . 'sz_motoboy_pedidos',
                [ 'reagendado_para' => $date, 'updated_at' => sz_motoboy_now_mysql() ],
                [ 'id' => (int) $pedido->id ],
                [ '%s', '%s' ],
                [ '%d' ]
            );
        }

        $old_fmt = $old_norm && preg_match( '/^(\d{4}-\d{2}-\d{2})/', $old_norm, $m ) ? wp_date( 'd/m/Y', strtotime( $m[1] . 'T12:00:00' ) ) : ( $old_norm ?: 'sem data' );
        $new_fmt = wp_date( 'd/m/Y', strtotime( $date . 'T12:00:00' ) );
        $order->add_order_note( 'Senderzz: data de entrega alterada pelo admin de ' . $old_fmt . ' para ' . $new_fmt . ' sem alterar status.' );
    }
}

add_action( 'woocommerce_admin_process_shop_order_object', 'sz_senderzz_admin_save_delivery_date', 10, 1 );
add_action( 'woocommerce_process_shop_order_meta', function( int $order_id ) {
    $order = wc_get_order( $order_id );
    if ( $order instanceof WC_Order ) {
        sz_senderzz_admin_save_delivery_date( $order );
        $order->save();
    }
}, 10, 1 );

// ─── 10. Status automático: motoboy vai direto para agendado ─────────────────
add_action( 'woocommerce_checkout_order_created', function( WC_Order $order ) {
    // Detecta pedido motoboy por múltiplas camadas
    $is_motoboy = false;

    // Camada 1: method_id do item de frete
    foreach ( $order->get_items('shipping') as $item ) {
        $mid  = strtolower( (string) $item->get_method_id() );
        $name = strtolower( (string) $item->get_name() );
        if ( strpos( $mid, 'sz_motoboy' ) !== false || strpos( $name, 'motoboy' ) !== false ) {
            $is_motoboy = true;
            break;
        }
    }

    // Camada 2: session sz_frete_forcado (definida no hook de init)
    if ( ! $is_motoboy && WC()->session ) {
        $is_motoboy = ( WC()->session->get('sz_frete_forcado') === 'motoboy' );
    }

    // Camada 3: parâmetro POST frete=motoboy (FunnelKit envia o POST do checkout)
    if ( ! $is_motoboy ) {
        $m_post     = sanitize_text_field( wp_unslash( $_POST['m'] ?? '' ) );
        $frete_post = sanitize_text_field( wp_unslash( $_POST['frete'] ?? '' ) ); // retrocompat
        $is_motoboy = ( $m_post === '1' || $frete_post === 'motoboy' );
    }

    // Camada 4: meta já salva anteriormente (ex: woocommerce_new_order mais cedo)
    if ( ! $is_motoboy ) {
        $is_motoboy = ( $order->get_meta( '_senderzz_delivery_mode', true ) === 'motoboy' );
    }

    if ( ! $is_motoboy ) return;

    // Muda para agendado imediatamente
    $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
    $order->update_meta_data( '_senderzz_motoboy_flow_status', 'agendado' );
    $statuses = function_exists( 'wc_get_order_statuses' ) ? wc_get_order_statuses() : [];
    if ( isset( $statuses['wc-agendado'] ) ) {
        $order->update_status( 'agendado', 'Pedido motoboy agendado automaticamente.' );
    } else {
        $order->save();
    }
}, 20 );

// ─── 11. Seletor de data Motoboy no checkout limpo ─────────────────────────
// Mantém o checkout nativo/FunnelKit sem painel pesado, mas devolve a seleção
// de data na etapa final do checkout motoboy pelo hook correto.
if ( ! function_exists( 'sz_mb_is_motoboy_checkout_request' ) ) {
    function sz_mb_is_motoboy_checkout_request(): bool {
        return sz_mb_current_checkout_is_motoboy();
    }
}

// Motoboy: a progressão visual já controla telefone completo. Removemos a validação
// nativa/FunnelKit de formato para não exibir a mensagem inglesa "The provided phone number is not valid".
add_filter( 'woocommerce_checkout_fields', function( array $fields ): array {
    if ( function_exists( 'sz_mb_is_motoboy_checkout_request' ) && sz_mb_is_motoboy_checkout_request() ) {
        foreach ( [ 'billing', 'shipping' ] as $section ) {
            foreach ( [ 'billing_phone', 'shipping_phone' ] as $key ) {
                if ( isset( $fields[ $section ][ $key ]['validate'] ) && is_array( $fields[ $section ][ $key ]['validate'] ) ) {
                    $fields[ $section ][ $key ]['validate'] = array_values( array_diff( $fields[ $section ][ $key ]['validate'], [ 'phone' ] ) );
                }
            }
        }
    }
    return $fields;
}, 9999 );


if ( ! function_exists( 'sz_mb_current_checkout_postcode' ) ) {
    function sz_mb_current_checkout_postcode(): string {
        $keys = [ 'shipping_postcode', 'billing_postcode', 'postcode', 'cep' ];
        foreach ( $keys as $key ) {
            if ( isset( $_POST[ $key ] ) ) {
                $cep = preg_replace( '/\D/', '', (string) wp_unslash( $_POST[ $key ] ) );
                if ( strlen( $cep ) === 8 ) return $cep;
            }
        }
        if ( function_exists( 'WC' ) && WC() && WC()->customer ) {
            $cep = preg_replace( '/\D/', '', (string) WC()->customer->get_shipping_postcode() );
            if ( strlen( $cep ) === 8 ) return $cep;
            $cep = preg_replace( '/\D/', '', (string) WC()->customer->get_billing_postcode() );
            if ( strlen( $cep ) === 8 ) return $cep;
        }
        return '';
    }
}



if ( ! function_exists( 'sz_mb_cep_exists' ) ) {
    /**
     * Validação real de CEP com proteção de CPU/rede.
     * - Cache estático por request para não consultar várias vezes na mesma tela.
     * - Transient por CEP para não repetir ViaCEP.
     * - Timeout curto para não travar checkout/hospedagem.
     * - Retorna null quando não conseguiu consultar; nesse caso a regra de zona decide.
     */
    function sz_mb_cep_exists( string $cep ): ?bool {
        static $local_cache = [];
        $cep = preg_replace( '/\D/', '', $cep );
        if ( strlen( $cep ) !== 8 || preg_match( '/^(\d)\1{7}$/', $cep ) ) return false;
        if ( array_key_exists( $cep, $local_cache ) ) return $local_cache[ $cep ];

        $key = 'sz_mb_cep_exists_' . $cep;
        $cached = get_transient( $key );
        if ( $cached === 'yes' ) return $local_cache[ $cep ] = true;
        if ( $cached === 'no' ) return $local_cache[ $cep ] = false;
        if ( $cached === 'fail' ) return $local_cache[ $cep ] = null;

        $res = wp_remote_get( 'https://viacep.com.br/ws/' . rawurlencode( $cep ) . '/json/', [
            'timeout'     => 0.8,
            'redirection' => 0,
            'headers'     => [ 'Accept' => 'application/json' ],
        ] );
        if ( is_wp_error( $res ) ) {
            set_transient( $key, 'fail', 10 * MINUTE_IN_SECONDS );
            return $local_cache[ $cep ] = null;
        }
        $code = (int) wp_remote_retrieve_response_code( $res );
        if ( $code < 200 || $code >= 300 ) {
            set_transient( $key, 'fail', 10 * MINUTE_IN_SECONDS );
            return $local_cache[ $cep ] = null;
        }
        $body = json_decode( (string) wp_remote_retrieve_body( $res ), true );
        if ( is_array( $body ) && ! empty( $body['erro'] ) ) {
            set_transient( $key, 'no', 30 * DAY_IN_SECONDS );
            return $local_cache[ $cep ] = false;
        }
        if ( is_array( $body ) && ! empty( $body['cep'] ) ) {
            set_transient( $key, 'yes', 30 * DAY_IN_SECONDS );
            return $local_cache[ $cep ] = true;
        }
        set_transient( $key, 'fail', 10 * MINUTE_IN_SECONDS );
        return $local_cache[ $cep ] = null;
    }
}

if ( ! function_exists( 'sz_mb_next_delivery_dates' ) ) {
    function sz_mb_next_delivery_dates( int $qty = 3 ): array {
        // Regra principal: CEP → zona cadastrada. A agenda usa dias e horários limite da zona.
        $cep = function_exists( 'sz_mb_current_checkout_postcode' ) ? sz_mb_current_checkout_postcode() : '';
        if ( strlen( $cep ) === 8 && function_exists( 'sz_motoboy_resolver_zona' ) && function_exists( 'sz_motoboy_zone_next_dates' ) ) {
            $zona = sz_motoboy_resolver_zona( $cep );
            if ( $zona && ! empty( $zona['dias_funcionamento'] ) ) {
                $zone_dates = sz_motoboy_zone_next_dates( $zona['dias_funcionamento'], $qty, $zona['cutoff_horarios'] ?? '' );
                if ( ! empty( $zone_dates ) ) return $zone_dates;
            }
        }

        // Fallback legado por cidade, apenas se ainda não houver CEP/zona resolvida.
        $city = '';
        if ( isset( $_POST['billing_city'] ) ) $city = sanitize_text_field( wp_unslash( $_POST['billing_city'] ) );
        elseif ( isset( $_POST['shipping_city'] ) ) $city = sanitize_text_field( wp_unslash( $_POST['shipping_city'] ) );
        elseif ( function_exists( 'WC' ) && WC() && WC()->customer ) {
            $city = (string) WC()->customer->get_shipping_city();
            if ( ! $city ) $city = (string) WC()->customer->get_billing_city();
        }
        if ( $city && function_exists( 'sz_dr_next_delivery_dates' ) ) {
            $regional = sz_dr_next_delivery_dates( $city, $qty );
            if ( ! empty( $regional ) ) return $regional;
        }
        // ── Regra padrão abaixo ──
        $dates = [];

        // Força timezone Brasília independente da configuração do WP
        try {
            $tz  = new DateTimeZone( 'America/Sao_Paulo' );
            $dt  = new DateTime( 'now', $tz );
            $now = $dt->getTimestamp();
            $hour = (int) $dt->format( 'G' );
        } catch ( Exception $e ) {
            $now  = current_time( 'timestamp' );
            $hour = (int) wp_date( 'G', $now );
        }

        // Regra Motoboy Senderzz:
        // - entrega sempre do dia seguinte em diante;
        // - segunda a sábado;
        // - após 21h (horário de Brasília), agenda somente a partir de dois dias à frente;
        // - após 21h, se o segundo dia disponível for domingo, pula para 3 dias à frente.
        $start_offset = $hour >= 21 ? 2 : 1;
        if ( $hour >= 21 && $start_offset === 2 ) {
            // Verifica se o dia resultante (hoje + 2) é domingo no fuso Brasília
            try {
                $tz2       = new DateTimeZone( 'America/Sao_Paulo' );
                $candidate = new DateTime( '+2 days', $tz2 );
                if ( (int) $candidate->format( 'w' ) === 0 ) { // 0 = domingo
                    $start_offset = 3;
                }
            } catch ( Exception $e ) {
                $candidate_ts = strtotime( '+2 day', $now );
                if ( wp_date( 'w', $candidate_ts ) === '0' ) {
                    $start_offset = 3;
                }
            }
        }

        $dias_short = [ 'Sun'=>'DOM', 'Mon'=>'SEG', 'Tue'=>'TER', 'Wed'=>'QUA', 'Thu'=>'QUI', 'Fri'=>'SEX', 'Sat'=>'SÁB' ];
        $dias_full  = [ 'Sun'=>'Domingo', 'Mon'=>'Segunda-feira', 'Tue'=>'Terça-feira', 'Wed'=>'Quarta-feira', 'Thu'=>'Quinta-feira', 'Fri'=>'Sexta-feira', 'Sat'=>'Sábado' ];
        $meses = [ 1=>'Jan', 2=>'Fev', 3=>'Mar', 4=>'Abr', 5=>'Mai', 6=>'Jun', 7=>'Jul', 8=>'Ago', 9=>'Set', 10=>'Out', 11=>'Nov', 12=>'Dez' ];

        $tz_br    = new DateTimeZone( 'America/Sao_Paulo' );
        $tomorrow = ( new DateTime( '+1 day', $tz_br ) )->format( 'Y-m-d' );

        $i = $start_offset;
        while ( count( $dates ) < $qty && $i < 21 ) {
            $dt_loop  = new DateTime( '+' . $i . ' day', $tz_br );
            $dow_num  = (int) $dt_loop->format( 'w' ); // 0=Dom, 6=Sáb
            // Domingo não é atendido.
            if ( $dow_num !== 0 ) {
                $ymd         = $dt_loop->format( 'Y-m-d' );
                $is_tomorrow = ( $ymd === $tomorrow );
                $dow_en      = $dt_loop->format( 'D' ); // Mon, Tue, ...
                $dates[] = [
                    'value' => $ymd,
                    'dow'   => $dias_short[ $dow_en ] ?? strtoupper( $dow_en ),
                    'full'  => $is_tomorrow ? 'Amanhã' : ( $dias_full[ $dow_en ] ?? $dt_loop->format( 'l' ) ),
                    'day'   => $dt_loop->format( 'd' ),
                    'month' => $meses[ (int) $dt_loop->format( 'n' ) ] ?? $dt_loop->format( 'M' ),
                    'label' => $is_tomorrow ? 'Amanhã' : ( $dias_full[ $dow_en ] ?? $dt_loop->format( 'l' ) ),
                ];
            }
            $i++;
        }
        return $dates;
    }
}

if ( ! function_exists( 'sz_mb_delivery_label' ) ) {
    function sz_mb_delivery_label( array $date ): string {
        $name = $date['label'] ?? ( $date['full'] ?? $date['dow'] );
        return trim( $name . ', ' . ltrim( (string) $date['day'], '0' ) . ' de ' . ( $date['month'] ?? '' ) );
    }
}


if ( ! function_exists( 'sz_mb_money_label' ) ) {
    function sz_mb_money_label( float $value ): string {
        return 'R$ ' . number_format( max( 0, $value ), 2, ',', '.' );
    }
}


if ( ! function_exists( 'sz_mb_installment_rate_config' ) ) {
    function sz_mb_installment_rate_config(): array {
        // Tabela oficial Senderzz para exibição no checkout.
        // Mantida fixa aqui para evitar herdar arrays antigos/deslocados salvos em option.
        $cfg = [
            'one_time_pct'   => 4.98,
            'multi_base_pct' => 5.31,
            'addons'         => [
                2  => 9.64,
                3  => 11.23,
                4  => 11.36,
                5  => 14.31,
                6  => 14.32,
                7  => 16.72,
                8  => 16.73,
                9  => 19.69,
                10 => 20.65,
                11 => 20.66,
                12 => 22.11,
            ],
        ];

        return apply_filters( 'senderzz_checkout_installment_rates', $cfg );
    }
}

if ( ! function_exists( 'sz_mb_installment_total_pct_map' ) ) {
    function sz_mb_installment_total_pct_map(): array {
        $cfg = sz_mb_installment_rate_config();
        $map = [ 1 => (float) ( $cfg['one_time_pct'] ?? 4.98 ) ];
        $addons = is_array( $cfg['addons'] ?? null ) ? $cfg['addons'] : [];
        foreach ( range( 2, 12 ) as $parcel ) {
            $map[ $parcel ] = (float) ( $cfg['multi_base_pct'] ?? 5.31 ) + (float) ( $addons[ $parcel ] ?? 0 );
        }
        return $map;
    }
}

if ( ! function_exists( 'sz_mb_installment_total_for_customer' ) ) {
    function sz_mb_installment_total_for_customer( float $net_offer_value, int $installments ): float {
        $installments = max( 1, min( 12, $installments ) );
        $pct_map = sz_mb_installment_total_pct_map();
        $pct = (float) ( $pct_map[ $installments ] ?? 0 );
        $factor = max( 0.0001, 1 - ( $pct / 100 ) );
        return round( $net_offer_value / $factor, 2 );
    }
}

if ( ! function_exists( 'sz_mb_render_installment_select' ) ) {
    function sz_mb_render_installment_select( float $order_total, int $selected = 1 ): string {
        if ( $order_total <= 0 ) {
            return '';
        }
        $selected = max( 1, min( 12, $selected ) );
        ob_start();
        ?>
        <div class="sz-mb-select-shell">
            <select name="sz_installments" id="sz_installments" class="sz-mb-installment-select" data-sz-installment-select>
                <?php for ( $i = 1; $i <= 12; $i++ ) : ?>
                    <?php $total = sz_mb_installment_total_for_customer( $order_total, $i ); ?>
                    <?php $parcel = round( $total / $i, 2 ); ?>
                    <?php $is_selected = $i === $selected; ?>
                    <option value="<?php echo esc_attr( (string) $i ); ?>" data-total="<?php echo esc_attr( sz_mb_money_label( $total ) ); ?>" data-parcel="<?php echo esc_attr( sz_mb_money_label( $parcel ) ); ?>" <?php selected( $is_selected ); ?>><?php echo esc_html( ( $i === 1 ? sprintf( '1x de %s', sz_mb_money_label( $parcel ) ) : sprintf( '%dx de %s • total %s', $i, sz_mb_money_label( $parcel ), sz_mb_money_label( $total ) ) ) ); ?></option>
                <?php endfor; ?>
            </select>
            <span class="sz-mb-select-arrow" aria-hidden="true">⌄</span>
        </div>
        <?php
        return trim( ob_get_clean() );
    }
}


if ( ! function_exists( 'sz_mb_render_delivery_date_selector' ) ) {
    function sz_mb_render_delivery_date_selector( bool $echo = true ): string {
        if ( ! sz_mb_is_motoboy_checkout_request() ) return '';

        $dates = sz_mb_next_delivery_dates( 5 );
        if ( empty( $dates ) ) return '';

        if ( count( $dates ) < 5 ) {
            $seen = [];
            foreach ( $dates as $d ) {
                if ( ! empty( $d['value'] ) ) {
                    $seen[ (string) $d['value'] ] = true;
                }
            }

            $meses_php = [ 1=>'Jan', 2=>'Fev', 3=>'Mar', 4=>'Abr', 5=>'Mai', 6=>'Jun', 7=>'Jul', 8=>'Ago', 9=>'Set', 10=>'Out', 11=>'Nov', 12=>'Dez' ];
            $full_php  = [ 0=>'Domingo', 1=>'Segunda-feira', 2=>'Terça-feira', 3=>'Quarta-feira', 4=>'Quinta-feira', 5=>'Sexta-feira', 6=>'Sábado' ];

            try {
                $tz_pad = new DateTimeZone( 'America/Sao_Paulo' );
            } catch ( Exception $e ) {
                $tz_pad = wp_timezone();
            }

            $base = null;

            if ( ! empty( $dates ) && ! empty( $dates[ count( $dates ) - 1 ]['value'] ) ) {
                try {
                    $base = new DateTime( (string) $dates[ count( $dates ) - 1 ]['value'], $tz_pad );
                } catch ( Exception $e ) {
                    $base = null;
                }
            }

            if ( ! $base ) {
                $base = new DateTime( 'tomorrow', $tz_pad );
            }

            $tomorrow_pad = ( new DateTime( 'tomorrow', $tz_pad ) )->format( 'Y-m-d' );
            $guard = 0;

            while ( count( $dates ) < 5 && $guard < 30 ) {
                $guard++;
                $base->modify( '+1 day' );

                if ( (int) $base->format( 'w' ) === 0 ) {
                    continue;
                }

                $ymd = $base->format( 'Y-m-d' );

                if ( isset( $seen[ $ymd ] ) ) {
                    continue;
                }

                $seen[ $ymd ] = true;
                $dow = (int) $base->format( 'w' );

                $dates[] = [
                    'value' => $ymd,
                    'dow'   => strtoupper( mb_substr( $full_php[ $dow ] ?? '', 0, 3 ) ),
                    'full'  => $ymd === $tomorrow_pad ? 'Amanhã' : ( $full_php[ $dow ] ?? '' ),
                    'label' => $ymd === $tomorrow_pad ? 'Amanhã' : ( $full_php[ $dow ] ?? '' ),
                    'day'   => $base->format( 'd' ),
                    'month' => $meses_php[ (int) $base->format( 'n' ) ] ?? $base->format( 'M' ),
                ];
            }
        }

        $dates = array_slice( $dates, 0, 5 );
        $first = $dates[0]['value'];

        $product_label = '';
        $product_thumb_url = '';
        $order_total = 0.0;

        $sz_offer_data = function_exists( 'senderzz_sz_get_data' ) ? senderzz_sz_get_data() : null;

        if ( is_array( $sz_offer_data ) ) {
            if ( ! empty( $sz_offer_data['name'] ) ) {
                $product_label = sanitize_text_field( (string) $sz_offer_data['name'] );
                $product_label = preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', $product_label );
            }

            if ( isset( $sz_offer_data['valor'] ) && (float) $sz_offer_data['valor'] > 0 ) {
                $order_total = (float) $sz_offer_data['valor'];
            }
        }

        if ( empty( $product_label ) && ! empty( $_COOKIE['senderzz_offer_name'] ) ) {
            $product_label = sanitize_text_field( wp_unslash( $_COOKIE['senderzz_offer_name'] ) );
            $product_label = preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', $product_label );
        }

        if ( $order_total <= 0 && ! empty( $_COOKIE['senderzz_offer_value'] ) ) {
            $order_total = (float) str_replace( ',', '.', sanitize_text_field( wp_unslash( $_COOKIE['senderzz_offer_value'] ) ) );
        }

        if ( function_exists( 'WC' ) && WC()->cart ) {
            $items = WC()->cart->get_cart();
            $names = [];

            foreach ( $items as $item ) {
                if ( empty( $item['data'] ) || ! is_object( $item['data'] ) ) {
                    continue;
                }

                if ( empty( $product_thumb_url ) && method_exists( $item['data'], 'get_image_id' ) ) {
                    $image_id = absint( $item['data']->get_image_id() );

                    if ( $image_id && function_exists( 'wp_get_attachment_image_url' ) ) {
                        $product_thumb_url = (string) wp_get_attachment_image_url( $image_id, 'thumbnail' );
                    }
                }

                if ( empty( $product_label ) && ! empty( $item['senderzz_offer_name'] ) ) {
                    $product_label = sanitize_text_field( (string) $item['senderzz_offer_name'] );
                    $product_label = preg_replace( '/\s+—\s+(Motoboy|Correio|Expedição)$/iu', '', $product_label );
                }

                if ( $order_total <= 0 && ! empty( $item['senderzz_offer_value'] ) ) {
                    $order_total = (float) $item['senderzz_offer_value'];
                }

                $qty = ! empty( $item['quantity'] ) ? (int) $item['quantity'] : 1;
                $name = function_exists( 'senderzz_clean_product_label' )
                    ? senderzz_clean_product_label( $item['data']->get_name() )
                    : $item['data']->get_name();

                $names[] = $qty > 1 ? $name . ' × ' . $qty : $name;
            }

            if ( empty( $product_label ) && ! empty( $names ) ) {
                $product_label = implode( ', ', $names );
            }

            if ( $order_total <= 0 ) {
                $order_total = (float) WC()->cart->get_subtotal();
            }
        }

        if ( empty( $product_label ) ) {
            $product_label = 'Oferta selecionada';
        }

        $order_value = sz_mb_money_label( $order_total );

        ob_start();
        ?>
        <div class="sz-mb-date-checkout sz-mb-date-checkout-premium" data-sz-mb-date-checkout>

            <?php
            /* Senderzz v418: a tarjeta de oferta agora vive SOMENTE na etapa 1,
             * acima do card (slot .senderzz-first-step-offer-slot do canvas).
             * Não renderizar uma segunda cópia dentro do card de datas (etapa 2),
             * pois isso gerava IDs #sz-oferta-bar duplicados e a movimentação de DOM
             * que esvaziava o formulário. Os valores ainda são calculados acima
             * ($product_label/$order_total/$order_value/$product_thumb_url) e seguem
             * disponíveis para o restante do card, sem alterar nenhum cálculo. */
            ?>

            <div class="sz-mb-delivery-panel">
                <div class="sz-mb-date-head">
                    <div class="sz-mb-date-icon" aria-hidden="true">
                        <svg viewBox="0 0 24 24" role="img" focusable="false">
                            <path d="M4 15.5h2.2l2.1-5.2h4.1l2.1 5.2H20"/>
                            <path d="M9 10.3l1.1-2.8h2.6"/>
                            <path d="M13.4 11.2l2.8-1.6 1.6 2.8"/>
                            <path d="M6 17.3a2.2 2.2 0 1 0 0-.1"/>
                            <path d="M18 17.3a2.2 2.2 0 1 0 0-.1"/>
                        </svg>
                    </div>

                    <div>
                        <strong>Escolha o melhor dia para receber</strong>
                        <small>A melhor data disponível para sua região já fica selecionada.</small>
                    </div>
                </div>

                <div class="sz-mb-date-options" role="radiogroup" aria-label="Data de entrega Motoboy">
                    <?php foreach ( $dates as $idx => $date ) : ?>
                        <?php $badge = $idx === 0 ? 'Mais próxima' : ''; ?>

                        <button
                            type="button"
                            class="sz-mb-date-option <?php echo $idx === 0 ? 'is-active' : ''; ?>"
                            data-sz-date-option="1"
                            data-date="<?php echo esc_attr( $date['value'] ); ?>"
                            data-label="<?php echo esc_attr( sz_mb_delivery_label( $date ) ); ?>"
                            aria-pressed="<?php echo $idx === 0 ? 'true' : 'false'; ?>"
                        >
                            <?php if ( $badge ) : ?>
                                <span class="sz-mb-date-badge"><?php echo esc_html( $badge ); ?></span>
                            <?php endif; ?>

                            <i class="sz-mb-calendar-icon" aria-hidden="true"></i>
                            <b><?php echo esc_html( $date['label'] ); ?></b>
                            <em><?php echo esc_html( ltrim( (string) $date['day'], '0' ) . ' ' . $date['month'] ); ?></em>

                            <?php if ( $idx === 0 ) : ?>
                                <strong class="sz-mb-date-check">✓</strong>
                            <?php endif; ?>
                        </button>
                    <?php endforeach; ?>
                </div>

                <input type="hidden" name="sz_delivery_date" id="sz_delivery_date" value="<?php echo esc_attr( $first ); ?>">

                <div class="sz-mb-place-order-slot" data-sz-mb-place-order-slot></div>

                <div class="sz-mb-date-alert">
                    <span class="sz-mb-notice-icon" aria-hidden="true">👤</span>
                    <span>É necessário informar o CPF no ato da entrega do pedido.</span>
                </div>

                <div class="sz-mb-cutoff-note">
                    <span class="sz-mb-notice-icon" aria-hidden="true">🔒</span>
                    <span>Pagamento 100% seguro — Pix, cartão de crédito ou débito. Seus dados são protegidos com criptografia SSL.</span>
                </div>
            </div>
        </div>
        <?php

        $html = trim( ob_get_clean() );

        if ( $echo ) {
            echo $html;
        }

        return $html;
    }
}

if ( ! function_exists( 'sz_mb_output_delivery_date_selector_once' ) ) {
    function sz_mb_output_delivery_date_selector_once(): void {
        static $printed = false;

        if ( $printed ) return;
        if ( ! sz_mb_is_motoboy_checkout_request() ) return;

        $printed = true;
        sz_mb_render_delivery_date_selector( true );
    }
}

// Hook nativo WooCommerce: antes do botão de finalizar pedido.
add_action( 'woocommerce_review_order_before_submit', 'sz_mb_output_delivery_date_selector_once', 20 );

// Hook correto do FunnelKit no template checkout/payment-3.3.0.php,
// executado imediatamente antes do botão #place_order.
add_action( 'wfacp_woocommerce_review_order_before_submit', 'sz_mb_output_delivery_date_selector_once', 20 );

add_action( 'wp_footer', function() {
    if ( ! function_exists( 'is_checkout' ) || ! is_checkout() || ! sz_mb_is_motoboy_checkout_request() ) return;
    $sz_mb_date_fallback_html = function_exists( 'sz_mb_render_delivery_date_selector' ) ? sz_mb_render_delivery_date_selector( false ) : '';
    if ( $sz_mb_date_fallback_html ) {
        echo '<template id="sz-mb-date-checkout-fallback-template">' . $sz_mb_date_fallback_html . '</template>';
    }
    $sz_mb_offer_bar_fallback_html = function_exists( 'sz_mb_render_checkout_offer_strip' ) && function_exists( 'sz_mb_checkout_offer_display_data' )
        ? sz_mb_render_checkout_offer_strip( sz_mb_checkout_offer_display_data(), 'first' )
        : '';
    if ( $sz_mb_offer_bar_fallback_html ) {
        echo '<template id="sz-mb-offer-bar-fallback-template">' . $sz_mb_offer_bar_fallback_html . '</template>';
    }
    ?>
    <script id="sz-mb-checkout-flow-fix">    window.sz_motoboy_checkout = "1";
    window.sz_checkout_vars = window.sz_checkout_vars || {};
    window.sz_checkout_vars.is_motoboy = "1";
    (function(){
      function isVisible(el){return !!(el && el.offsetParent !== null && getComputedStyle(el).display !== 'none' && getComputedStyle(el).visibility !== 'hidden');}
      function isFinalStep(){
        // v418: corrige ReferenceError (`active` não existia neste escopo), o que
        // lançava exceção dentro de tick() e impedia o checkout de ficar pronto.
        var active=document.querySelector('.wfacp_steps_wrap .wfacp_step.active, .wfacp-progress-step.active, .wfacp_step_active, .wfacp-current-step, .wfacp-active');
        var txt=(active&&active.textContent?active.textContent:'').toLowerCase();
        if(txt.indexOf('final')>=0||txt.indexOf('confirma')>=0)return true;
        // Etapa final = card de datas visível (etapa 2). Etapa 1 NÃO é final.
        var date=document.querySelector('[data-sz-mb-date-checkout]');
        return !!date && isVisible(date);
      }
      function normalizeText(s){return String(s||'').replace(/\s+/g,' ').trim();}
      function szMbEnsureFirstStepOfferBar(){
        // v418: garante UMA tarjeta de oferta, sempre ACIMA do formulário (etapa 1),
        // sem mover/clonar e sem remover filhos do form. Não toca na etapa 2.
        try{
          // 1) Remove qualquer cópia que tenha vazado para dentro do card de datas (etapa 2).
          var dateBlock=document.querySelector('[data-sz-mb-date-checkout]');
          if(dateBlock){
            dateBlock.querySelectorAll('.senderzz-checkout-offer-bar, #sz-oferta-bar').forEach(function(el){ el.remove(); });
          }
          // 2) Se o slot do canvas já tem a tarjeta, nada a fazer (caminho normal).
          var slot=document.querySelector('.senderzz-first-step-offer-slot');
          if(slot && slot.querySelector('.senderzz-checkout-offer-bar, #sz-oferta-bar')) return;
          // 3) Mantém só a primeira tarjeta existente fora do card; remove duplicatas.
          var bars=Array.prototype.slice.call(
            document.querySelectorAll('.senderzz-checkout-offer-bar, #sz-oferta-bar')
          ).filter(function(el){ return !el.closest('[data-sz-mb-date-checkout]'); });
          if(bars.length>1){ bars.slice(1).forEach(function(el){ el.remove(); }); }
          if(bars.length>=1) return;
          // 4) Fallback: nenhuma tarjeta na página → injeta a partir do <template>,
          //    como irmão imediatamente ANTES do formulário (nunca dentro dele).
          var tpl=document.getElementById('sz-mb-offer-bar-fallback-template');
          if(!tpl || !tpl.innerHTML) return;
          var form=document.querySelector('form.checkout, form.woocommerce-checkout, .wfacp_main_form');
          if(!form || !form.parentNode) return;
          var holder=document.createElement('div'); holder.innerHTML=tpl.innerHTML;
          var bar=holder.firstElementChild; if(!bar) return;
          var wrap=document.querySelector('.senderzz-first-step-offer-slot');
          if(!wrap){
            wrap=document.createElement('div');
            wrap.className='senderzz-first-step-offer-slot';
            form.parentNode.insertBefore(wrap, form);
          }
          wrap.appendChild(bar);
        }catch(e){}
      }
      function szMbToggleFirstStepOfferBar(){
        try{
          var finalVisible=false;
          var block=document.querySelector('[data-sz-mb-date-checkout]');
          if(block && isVisible(block)) finalVisible=true;
          var active=document.querySelector('.wfacp_steps_wrap .wfacp_step.active, .wfacp-progress-step.active, .wfacp_step_active, .wfacp-current-step, .wfacp-active');
          var txt=normalizeText(active&&active.textContent?active.textContent:'').toLowerCase();
          if(txt.indexOf('final')>=0 || txt.indexOf('confirma')>=0) finalVisible=true;
          if(document.body){ document.body.classList.toggle('sz-mb-final-step', !!finalVisible); }
          // v418: a tarjeta deve aparecer SOMENTE na etapa 1 e sumir na etapa 2.
          // finalVisible === true  -> etapa 2 -> ESCONDER a tarjeta.
          // finalVisible === false -> etapa 1 -> MOSTRAR a tarjeta.
          var showOnFirst = !finalVisible;
          document.querySelectorAll('.senderzz-checkout-offer-bar, #sz-oferta-bar').forEach(function(bar){
            if(bar.closest('[data-sz-mb-date-checkout]')) return; // ignora qualquer resíduo dentro do card
            if(showOnFirst){
              bar.style.removeProperty('display');
              bar.setAttribute('aria-hidden', 'false');
            } else {
              bar.style.setProperty('display', 'none', 'important');
              bar.setAttribute('aria-hidden', 'true');
            }
          });
        }catch(e){}
      }
      function szMbEnsureFinalExtras(){
        try{
          if(document.querySelector('[data-sz-mb-date-checkout]')) return;
          var tpl=document.getElementById('sz-mb-date-checkout-fallback-template');
          if(!tpl || !tpl.innerHTML) return;
          var btn=document.querySelector('#place_order, button[name="woocommerce_checkout_place_order"], .wfacp_place_order, .wfacp-submit-btn');
          if(!btn){
            Array.prototype.some.call(document.querySelectorAll('.wfacp_next_page_button, .wfacp_next_step_btn, button, input[type="submit"], input[type="button"]'), function(b){
              var t=String(b.textContent||b.value||'').toLowerCase();
              if(t.indexOf('finalizar')>=0 || t.indexOf('pedido')>=0){ btn=b; return true; }
              return false;
            });
          }
          if(!btn) return;
          var btnText=String(btn.textContent||btn.value||'').toLowerCase();
          if(btn.id!=='place_order' && btn.getAttribute('name')!=='woocommerce_checkout_place_order' && btnText.indexOf('finalizar')<0 && btnText.indexOf('pedido')<0) return;
          var form=document.querySelector('form.checkout, form.woocommerce-checkout, .wfacp_main_form');
          var holder=document.createElement('div');
          holder.innerHTML=tpl.innerHTML;
          var block=holder.firstElementChild;
          if(!block) return;
          var anchor=btn && (btn.closest('.wfacp_form_button_wrapper,.place-order,.form-row,div') || btn);
          if(anchor && anchor.parentNode){ anchor.parentNode.insertBefore(block, anchor); }
          else if(form){ form.appendChild(block); }
          else { document.body.appendChild(block); }
          szMbEnsureFirstStepOfferBar();
          setTimeout(function(){try{szMbEnsureFirstStepOfferBar();document.dispatchEvent(new Event('sz_mb_date_block_injected'));}catch(e){}},10);
        }catch(e){}
      }
      function hideDeliveryHeading(){
        var needles=['método de entrega','metodo de entrega','método de envio','metodo de envio'];
        function isDeliveryTitle(el){
          var txt=normalizeText(el && el.textContent ? el.textContent : '').toLowerCase();
          return needles.indexOf(txt) >= 0;
        }
        document.querySelectorAll('h1,h2,h3,h4,h5,h6,legend,.wfacp_heading_wrap,.wfacp_section_title,.wfacp-step-title,.wfacp_shipping_heading,.wfacp_shipping_title,.woocommerce-shipping-totals th').forEach(function(el){
          if(!isDeliveryTitle(el)) return;
          var wrap=el.closest('.wfacp_heading_wrap,.wfacp_section_title,.wfacp_shipping_heading,.wfacp_shipping_title,.woocommerce-shipping-totals,.shop_table,.wfacp_shipping_table') || el;
          wrap.style.display='none';
          wrap.setAttribute('aria-hidden','true');
          wrap.classList.add('sz-mb-native-shipping-title-hidden');
          var prev=wrap.previousElementSibling;
          var next=wrap.nextElementSibling;
          [prev,next].forEach(function(n){
            if(!n) return;
            var cs=getComputedStyle(n);
            var txt=normalizeText(n.textContent||'');
            var looksLine=(n.offsetHeight<=8 || txt==='') && (cs.borderTopColor.indexOf('232')>=0 || cs.borderBottomColor.indexOf('232')>=0 || cs.backgroundColor.indexOf('232')>=0 || cs.borderTopWidth!=='0px' || cs.borderBottomWidth!=='0px');
            if(looksLine){ n.style.display='none'; n.setAttribute('aria-hidden','true'); }
          });
        });
        document.querySelectorAll('tr.shipping, tr.woocommerce-shipping-totals.shipping, .wfacp_shipping_table, .wfacp_shipping_options').forEach(function(el){
          var txt=normalizeText(el.textContent||'').toLowerCase();
          if(txt.indexOf('método de entrega')>=0 || txt.indexOf('metodo de entrega')>=0){
            el.style.display='none'; el.setAttribute('aria-hidden','true');
          }
        });
      }

      function purgeExpeditionArtifacts(){
        var bad=/expediç|expedicao|correio|correios|sedex|pac|melhor envio|transportadora/i;
        var good=/motoboy|senderzz/i;
        document.querySelectorAll('#shipping_method li,.woocommerce-shipping-methods li,.wfacp_shipping_options li,.wfacp_shipping_table tr,.woocommerce-shipping-totals tr,.shipping_method').forEach(function(el){
          var txt=normalizeText(el.textContent||el.value||'');
          if(bad.test(txt) && !good.test(txt)){
            var row=el.closest('li,tr,.wfacp_single_shipping_method,.woocommerce-shipping-method')||el;
            row.style.display='none'; row.setAttribute('aria-hidden','true');
            row.classList.add('sz-mb-expedition-hidden');
          }
        });
        document.querySelectorAll('a,button,input[type=button],input[type=submit]').forEach(function(el){
          var txt=normalizeText(el.textContent||el.value||'');
          if(bad.test(txt)){
            if(el.tagName==='INPUT') el.value='Finalizar Pedido'; else el.textContent='Finalizar Pedido';
            el.classList.remove('senderzz-expedition-redirect');
            el.removeAttribute('data-sz-expedition-redirect');
          }
        });
      }
      function fixButtons(){
        var btns=document.querySelectorAll('#place_order,.wfacp_place_order,.wfacp-submit-btn,.wfacp_next_page_button,.wfacp_next_step_btn,button[name="woocommerce_checkout_place_order"]');
        var final=isFinalStep();
        btns.forEach(function(btn){
          if(btn.closest && btn.closest('[data-sz-mb-date-checkout], .sz-mb-date-checkout')) return;
          var txt=normalizeText(btn.textContent||btn.value||'');
          if(!txt)return;
          if(/expediç|expedicao|fazer pedido|finalizar pedido|próxima|proxima/i.test(txt)){
            var label=final?'Finalizar Pedido':'Próxima Etapa';
            if(btn.tagName==='INPUT')btn.value=label; else btn.textContent=label;
            btn.setAttribute('data-sz-mb-button-fixed','1');
            btn.classList.remove('sz-mb-next-button','sz-mb-final-button');
            btn.classList.add(final?'sz-mb-final-button':'sz-mb-next-button');
          }
        });
      }
      function szMbGetFinalPlaceOrderButton(){
        var candidates=document.querySelectorAll('#place_order, button[name="woocommerce_checkout_place_order"], .wfacp_place_order, .wfacp-submit-btn');
        for(var i=0;i<candidates.length;i++){
          var btn=candidates[i];
          if(!btn) continue;
          if(btn.classList.contains('wfacp_next_page_button') || btn.classList.contains('wfacp_next_step_btn')) continue;
          var txt=normalizeText(btn.textContent||btn.value||'').toLowerCase();
          if(/próxima|proxima|next/.test(txt)) continue;
          if(btn.id==='place_order' || btn.getAttribute('name')==='woocommerce_checkout_place_order' || btn.classList.contains('wfacp_place_order') || /finalizar|pedido|comprar/.test(txt)) return btn;
        }
        return null;
      }
      function szMbSyncFinalButtonState(){
        var btn=szMbGetFinalPlaceOrderButton();
        if(!btn) return;
        var block=document.querySelector('[data-sz-mb-date-checkout]');
        var date=document.querySelector('#sz_delivery_date');
        var ready=!!(block && isVisible(block) && date && date.value);
        btn.classList.toggle('senderzz-next-disabled', !ready);
        btn.setAttribute('aria-disabled', ready ? 'false' : 'true');
        if('disabled' in btn) btn.disabled=!ready;
      }
      function movePlaceOrderIntoCard(){
        var panel=document.querySelector('.sz-mb-delivery-panel');
        var slot=document.querySelector('[data-sz-mb-place-order-slot]');
        var block=document.querySelector('[data-sz-mb-date-checkout]');
        if(!panel || !slot || !block || !isVisible(block)) return;
        var btn=szMbGetFinalPlaceOrderButton();
        if(!btn) return;
        var wrap=btn.closest('.wfacp_form_button_wrapper,.place-order,.form-row') || btn;
        if(!slot.contains(wrap)) slot.appendChild(wrap);
        slot.style.display='block';
        szMbSyncFinalButtonState();
      }
      function removeFinalNoise(){
        document.querySelectorAll('.wfacp_billing_phone_field_error,.wfacp_inline_field_error').forEach(function(el){
          var txt=normalizeText(el.textContent||'').toLowerCase();
          if(el.classList.contains('wfacp_billing_phone_field_error') || txt.indexOf('phone number')>=0 || txt.indexOf('telefone')>=0){ el.remove(); }
        });
        document.querySelectorAll('.senderzz-hide-payment-headline,.wfacp-comm-title').forEach(function(el){
          var txt=normalizeText(el.textContent||'').toLowerCase();
          if(!txt || txt==='forma de pagamento' || txt.indexOf('forma de pagamento')>=0){ el.remove(); }
        });
        document.querySelectorAll('#shipping_calculator_field,.wfacp_shipping_calculator,.wfacp_shipping_options,.form_section_two_step_0_elementor-optic').forEach(function(el){
          if(!el) return;
          var section=el.closest('.form_section_two_step_0_elementor-optic,.wfacp-section[data-field-count="1"]')||el;
          section.remove();
        });
      }
      function neutralizePrematureValidation(){
        try{
          if(document.body && document.body.classList.contains('senderzz-submit-attempted')) return;
          if(document.body && document.body.classList.contains('senderzz-cep-invalid')) return;
          document.querySelectorAll('.woocommerce-invalid,.woocommerce-invalid-required-field,.wfacp_error,.wfacp-invalid,.input-text-error').forEach(function(el){
            el.classList.remove('woocommerce-invalid','woocommerce-invalid-required-field','wfacp_error','wfacp-invalid','input-text-error');
          });
          document.querySelectorAll('input,select,textarea').forEach(function(el){
            el.removeAttribute('aria-invalid');
            el.classList.remove('woocommerce-invalid','woocommerce-invalid-required-field','wfacp_error','wfacp-invalid','input-text-error','sz-mb-cep-invalid');
            if(el.style){
              var b=(el.style.borderColor||'').toLowerCase();
              if(b && (b.indexOf('red')>=0 || b.indexOf('239')>=0 || b.indexOf('ef4444')>=0 || b.indexOf('dc2626')>=0)){ el.style.borderColor=''; }
              el.style.boxShadow='';
            }
          });
          document.querySelectorAll('.wfacp_inline_field_error,.woocommerce-error li,.woocommerce-error').forEach(function(el){
            var txt=normalizeText(el.textContent||'').toLowerCase();
            if(txt.indexOf('required')>=0 || txt.indexOf('obrigat')>=0 || txt.indexOf('provided phone number')>=0 || txt.indexOf('phone number')>=0){
              if(el.classList.contains('wfacp_inline_field_error')) el.remove();
            }
          });
        }catch(e){}
      }
      function safe(fn){ try{ fn(); }catch(e){} }
      function tick(){
        // v418: revela o formulário PRIMEIRO e isola cada passo. Antes, um erro em
        // qualquer função abortava o tick e o form ficava escondido (card vazio).
        if(window.szMbCheckoutReady) safe(window.szMbCheckoutReady);
        safe(szMbEnsureFinalExtras);
        safe(szMbEnsureFirstStepOfferBar);
        safe(hideDeliveryHeading);
        safe(purgeExpeditionArtifacts);
        safe(fixButtons);
        safe(removeFinalNoise);
        safe(movePlaceOrderIntoCard);
        safe(szMbSyncFinalButtonState);
        safe(neutralizePrematureValidation);
        safe(szMbToggleFirstStepOfferBar);
      }
      function scheduleTick(){ clearTimeout(window.__szMbTick); window.__szMbTick=setTimeout(tick,80); }
      document.addEventListener('updated_checkout',tick);
      document.addEventListener('click',function(){setTimeout(tick,60);},true);
      // Só reage a "change" relevante (etapa/seleção), nunca à digitação de campos
      // de texto/telefone/CEP — isso é o que causava flicker e desformatação.
      document.addEventListener('change',function(e){
        var t=e.target;
        if(t && /^(text|tel|number|email|search|password)$/i.test(t.type||'')) return;
        setTimeout(tick,80);
      },true);
      safe(tick);
      if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',function(){tick();setTimeout(tick,120);setTimeout(tick,500);});
      else{tick();setTimeout(tick,120);setTimeout(tick,500);}
      if(window.MutationObserver){
        new MutationObserver(function(muts){
          // Ignora mutações causadas por digitação (campo de texto/tel/CEP em foco)
          // e por mudanças de classe/estilo nos próprios inputs — evita reprocessar
          // o checkout a cada tecla.
          var ae=document.activeElement;
          if(ae && /^(INPUT|TEXTAREA|SELECT)$/.test(ae.tagName) && /^(text|tel|number|email|search|password)$/i.test(ae.type||'text')){
            // mudança estrutural real (nós adicionados/removidos) ainda passa
            var structural=false;
            for(var i=0;i<muts.length;i++){ if(muts[i].type==='childList' && (muts[i].addedNodes.length||muts[i].removedNodes.length)){ structural=true; break; } }
            if(!structural) return;
          }
          scheduleTick();
        }).observe(document.body,{childList:true,subtree:true});
      }
    })();
    </script>
    <?php
}, 150 );

add_action( 'wp_head', function() {
    if ( ! function_exists( 'is_checkout' ) || ! is_checkout() || ! sz_mb_is_motoboy_checkout_request() ) return;
    ?>
    <style id="sz-mb-date-checkout-css">
        body:not(.senderzz-cep-invalid) .wfacp_main_form input,body:not(.senderzz-cep-invalid) .wfacp_main_form select,body:not(.senderzz-cep-invalid) .wfacp_main_form textarea,body:not(.senderzz-cep-invalid) .woocommerce-checkout input,body:not(.senderzz-cep-invalid) .woocommerce-checkout select,body:not(.senderzz-cep-invalid) .woocommerce-checkout textarea{border-color:#d9e1ea!important;box-shadow:none!important;outline:none!important}
        body:not(.senderzz-cep-invalid) .wfacp_main_form .woocommerce-invalid,body:not(.senderzz-cep-invalid) .wfacp_main_form .woocommerce-invalid-required-field,body:not(.senderzz-cep-invalid) .wfacp_main_form .wfacp_error,body:not(.senderzz-cep-invalid) .wfacp_main_form .wfacp-invalid,body:not(.senderzz-cep-invalid) .woocommerce-checkout .woocommerce-invalid,body:not(.senderzz-cep-invalid) .woocommerce-checkout .woocommerce-invalid-required-field,body:not(.senderzz-cep-invalid) .woocommerce-checkout .wfacp_error,body:not(.senderzz-cep-invalid) .woocommerce-checkout .wfacp-invalid{border-color:transparent!important;box-shadow:none!important}
        body:not(.senderzz-cep-invalid) .wfacp_main_form .woocommerce-invalid input,body:not(.senderzz-cep-invalid) .wfacp_main_form .woocommerce-invalid select,body:not(.senderzz-cep-invalid) .wfacp_main_form .woocommerce-invalid-required-field input,body:not(.senderzz-cep-invalid) .wfacp_main_form .woocommerce-invalid-required-field select,body:not(.senderzz-cep-invalid) .wfacp_main_form .wfacp_error input,body:not(.senderzz-cep-invalid) .wfacp_main_form .wfacp_error select{border-color:#d9e1ea!important;box-shadow:none!important}
        .sz-mb-date-checkout{box-sizing:border-box;clear:both;margin:8px auto 14px;padding:0;border:0!important;border-radius:0;background:transparent!important;box-shadow:none!important;font-family:var(--sz-font);max-width:100%}
        .wfacp_billing_phone_field_error,.senderzz-hide-payment-headline,.form_section_two_step_0_elementor-optic,#shipping_calculator_field,.wfacp_shipping_options[aria-hidden="true"]{display:none!important}
        .woocommerce-checkout input:disabled,.woocommerce-checkout select:disabled,.wfacp_main_form input:disabled,.wfacp_main_form select:disabled{background:#f8fafc!important;color:#94a3b8!important;cursor:not-allowed!important;opacity:1!important}
        .woocommerce-checkout .sz-mb-field-locked,.wfacp_main_form .sz-mb-field-locked{background:#f8fafc!important;color:#64748b!important;cursor:not-allowed!important;opacity:1!important}
        .woocommerce-checkout select.sz-mb-field-locked,.wfacp_main_form select.sz-mb-field-locked{cursor:default!important}
        .wfacp-comm-title:empty{display:none!important}
        .sz-mb-select-shell{position:relative;width:100%;margin-top:10px}
        .sz-mb-installment-select{appearance:none;-webkit-appearance:none;display:block;width:100%;min-height:56px;padding:0 42px 0 16px;border:1.5px solid #f4c5aa;border-radius:18px;background:#fff!important;color:#111827!important;font-family:var(--sz-font);font-size:var(--sz-text-base);font-weight:700;box-shadow:none!important;outline:none!important;cursor:pointer}
        .sz-mb-installment-select:focus{border-color:#ff6b00!important;box-shadow:0 0 0 3px rgba(255,107,0,.12)!important}
        .sz-mb-installment-select option{font-weight:700}
        .sz-mb-select-arrow{position:absolute;right:16px;top:50%;transform:translateY(-50%);pointer-events:none;color:#667085;font-size:14px;font-weight:700;line-height:1}
        .sz-mb-payment-amount{display:block;margin-top:6px;color:#111827;font-size:var(--sz-text-xl);font-weight:700;line-height:1.1}
        .sz-mb-payment-card.is-featured .sz-mb-payment-amount{color:#ff4b00}
        .sz-mb-payment-total{display:block;margin-top:6px;color:#667085;font-size:var(--sz-text-meta);font-style:normal;font-weight:700;line-height:1.25}
        .sz-mb-date-checkout *{box-sizing:border-box}.sz-mb-delivery-panel{box-sizing:border-box;margin:14px 0 0;padding:22px 28px 18px;border:1px solid #e6edf5;border-radius:26px;background:#fff!important;box-shadow:none}
        .sz-mb-date-head{display:grid;grid-template-columns:50px 1fr;align-items:center;gap:14px;text-align:left;margin:0 0 16px;color:#151515}
        .sz-mb-date-icon{width:50px;height:50px;border-radius:16px;display:inline-flex;align-items:center;justify-content:center;background:#fff!important;border:1px solid #e8eef6;color:#E8650A;font-size:var(--sz-text-xl);box-shadow:0 8px 18px rgba(15,23,42,.035)}.sz-mb-date-icon svg{width:29px;height:29px;display:block;fill:none;stroke:currentColor;stroke-width:1.75;stroke-linecap:round;stroke-linejoin:round}
        .sz-mb-date-head strong{display:block;font-size:var(--sz-text-2xl);font-weight:700;color:#111827;line-height:1.12;letter-spacing:-.015em}.sz-mb-date-head small{display:block;margin-top:5px;font-size:var(--sz-text-sm);color:#667085;font-weight:600;line-height:1.25}
        .sz-mb-date-options{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:14px;margin-top:8px}.sz-mb-date-option{appearance:none;font-family:var(--sz-font);position:relative;min-height:112px;border:1.5px solid #e5e7eb!important;background:#fff!important;border-radius:18px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:6px;cursor:pointer;color:#111827!important;font-weight:700;transition:transform .18s ease,border-color .18s ease,box-shadow .18s ease,background .18s ease;text-align:center;box-shadow:none!important;overflow:hidden}.sz-mb-date-option b{font-size:var(--sz-text-lg);line-height:1.1;text-transform:none;color:#0f172a!important}.sz-mb-date-option em{font-size:var(--sz-text-md);font-style:normal;color:#667085!important;font-weight:700}.sz-mb-date-option:hover{border-color:#E8650A!important;transform:translateY(-1px);box-shadow:0 12px 24px rgba(232,101,10,.08)!important}.sz-mb-date-option.is-active{background:#fff!important;border-color:#E8650A!important;box-shadow:0 10px 22px rgba(232,101,10,.10)!important}.sz-mb-date-option.is-active b,.sz-mb-date-option.is-active em{color:#E8650A!important}.sz-mb-date-badge{position:absolute;top:10px;left:12px;padding:5px 12px;border-radius:999px;background:linear-gradient(135deg,#E8650A,#c8540a)!important;border:0!important;color:#fff!important;font-size:var(--sz-text-meta);letter-spacing:0;text-transform:none;font-weight:700;box-shadow:0 8px 18px rgba(232,101,10,.16)}.sz-mb-date-check{position:absolute;top:11px;right:12px;width:30px;height:30px;border-radius:999px;display:flex!important;align-items:center;justify-content:center;background:linear-gradient(135deg,#E8650A,#c8540a)!important;color:#fff!important;font-size:var(--sz-text-lg);box-shadow:0 10px 22px rgba(232,101,10,.22)}.sz-mb-calendar-icon{width:24px;height:24px;display:block;border:2px solid #334155;border-radius:6px;position:relative;margin-top:4px}.sz-mb-calendar-icon:before{content:'';position:absolute;left:3px;right:3px;top:6px;border-top:2px solid currentColor}.sz-mb-calendar-icon:after{content:'';position:absolute;width:4px;height:4px;border-radius:999px;background:currentColor;left:5px;top:-5px;box-shadow:10px 0 0 currentColor}.sz-mb-date-option.is-active .sz-mb-calendar-icon{color:#E8650A;border-color:#E8650A}.sz-mb-date-option:not(.is-active) .sz-mb-calendar-icon{color:#334155;border-color:#334155}
        .sz-mb-offer-strip{display:grid!important;grid-template-columns:minmax(0,1.35fr) auto minmax(220px,.9fr)!important;align-items:center!important;gap:14px!important;margin:0 0 14px!important;padding:12px 16px!important;border-radius:18px!important;background:linear-gradient(135deg,#E8650A,#c8540a)!important;box-shadow:none!important;color:#fff!important}.sz-mb-offer-strip-main{display:flex;align-items:center;gap:12px;min-width:0}.sz-mb-offer-thumb{width:42px;height:42px;display:inline-flex;align-items:center;justify-content:center;flex:0 0 42px;border-radius:14px;background:rgba(255,255,255,.18);font-size:22px;overflow:hidden}.sz-mb-offer-thumb img{width:100%;height:100%;object-fit:cover;display:block}.sz-mb-offer-copy{display:flex;min-width:0;flex-direction:column}.sz-mb-offer-strip small{display:block;margin:0 0 2px;color:rgba(255,255,255,.76);font-size:11px;font-weight:700;text-transform:none;letter-spacing:0;line-height:1.1}.sz-mb-offer-strip strong{display:block;color:#fff!important;font-size:var(--sz-text-md);font-weight:700;line-height:1.15;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.sz-mb-offer-strip-price{min-width:120px;text-align:right}.sz-mb-offer-strip-price strong{display:inline-flex;align-items:center;justify-content:center;padding:7px 14px;border-radius:999px;background:rgba(255,255,255,.20);font-size:var(--sz-text-lg)}.sz-mb-offer-strip-installments{min-width:0}.sz-mb-offer-strip-installments .sz-mb-select-shell{margin-top:4px}.sz-mb-offer-strip-installments .sz-mb-installment-select{min-height:42px;border:0!important;border-radius:12px!important;background:rgba(255,255,255,.95)!important;font-size:var(--sz-text-sm);padding-left:12px}.sz-mb-offer-strip-installments .sz-mb-select-arrow{color:#334155}.sz-mb-place-order-slot{margin:12px auto 0;max-width:520px;width:100%;display:block}.sz-mb-place-order-slot .wfacp_form_button_wrapper,.sz-mb-place-order-slot .place-order,.sz-mb-place-order-slot .form-row{margin:0!important;padding:0!important;width:100%!important}.sz-mb-place-order-slot #place_order,.sz-mb-place-order-slot .wfacp_place_order,.sz-mb-place-order-slot .wfacp-submit-btn{margin:0 auto!important;width:100%!important;max-width:520px!important}
        .sz-mb-date-alert{margin:14px 0 0;border:0!important;background:transparent!important;color:#667085;border-radius:0;padding:0 0 10px;font-size:var(--sz-text-sm);font-weight:600;line-height:1.35;text-align:left;display:flex;align-items:center;gap:10px;border-bottom:1px solid #e8edf3!important}.sz-mb-notice-icon{width:26px;height:26px;min-width:26px;border-radius:999px;background:#f1f5f9;color:#64748b;display:inline-flex;align-items:center;justify-content:center;font-style:normal;font-size:13px;font-weight:700}.sz-mb-cutoff-note{margin:10px 0 0!important;color:#667085!important;font-size:var(--sz-text-sm);font-weight:500;line-height:1.35;text-align:left!important;white-space:normal!important;display:flex;align-items:center;gap:10px}.sz-mb-cutoff-note:before{content:none!important}.sz-mb-cep-feedback{display:none!important}.woocommerce-checkout input.sz-mb-cep-invalid,.wfacp_main_form input.sz-mb-cep-invalid{border-color:#ef4444!important;box-shadow:0 0 0 2px rgba(239,68,68,.12)!important}.woocommerce-checkout input.sz-mb-cep-ok,.wfacp_main_form input.sz-mb-cep-ok{border-color:inherit!important;box-shadow:none!important}.wfacp_main_form .sz-mb-date-checkout{box-sizing:border-box}.wfacp_main_form #place_order,.wfacp_main_form .wfacp_place_order,.wfacp_main_form .wfacp-submit-btn,.woocommerce-checkout #place_order{display:block!important;margin:14px auto 0!important;max-width:520px!important;width:100%!important;background:#E8650A!important;border:0!important;border-radius:13px!important;box-shadow:none!important;color:#fff!important;font-weight:700;min-height:58px!important}.woocommerce-checkout-review-order .sz-mb-date-checkout{margin-bottom:18px!important}

        .wfacp_main_form .sz-mb-next-button,.woocommerce-checkout .sz-mb-next-button,.wfacp_main_form .sz-mb-final-button,.woocommerce-checkout .sz-mb-final-button{font-family:var(--sz-font);background:#E8650A!important;color:#fff!important;border:0!important;border-radius:13px!important;box-shadow:none!important;min-height:58px!important;font-weight:700;max-width:520px!important;width:100%!important;margin:22px auto 0!important}.wfacp_main_form .sz-mb-next-button:disabled,.woocommerce-checkout .sz-mb-next-button:disabled,.wfacp_main_form .sz-mb-final-button:disabled,.woocommerce-checkout .sz-mb-final-button:disabled{opacity:.45!important;cursor:not-allowed!important;filter:saturate(.85)!important}

        .wfacp_main_form .woocommerce-shipping-fields,.wfacp_main_form .woocommerce-shipping-methods,.wfacp_main_form .wfacp_shipping_options,.wfacp_main_form .wfacp_shipping_table,.wfacp_main_form .shop_table.woocommerce-checkout-review-order-table{margin-bottom:0!important;padding-bottom:0!important}
        .sz-mb-date-option{font-family:var(--sz-font)}
        .sz-mb-date-option,.sz-mb-date-option:hover,.sz-mb-date-option:focus{background:#fff!important;color:#111827!important}
        .sz-mb-date-option.is-active{background:#fff!important;color:#111827!important}
        .wfacp_main_form #place_order:disabled,.wfacp_main_form .wfacp_place_order:disabled,.wfacp_main_form .wfacp-submit-btn:disabled,.woocommerce-checkout #place_order:disabled,.wfacp_main_form .sz-mb-next-button:disabled,.woocommerce-checkout .sz-mb-next-button:disabled,.wfacp_main_form .sz-mb-final-button:disabled,.woocommerce-checkout .sz-mb-final-button:disabled{background:#E8650A!important;color:#fff!important;opacity:.58!important;filter:none!important;border:0!important}
        .wfacp_main_form .senderzz-expedition-redirect,.woocommerce-checkout .senderzz-expedition-redirect{background:#E8650A!important;color:#fff!important;opacity:1!important;filter:none!important;pointer-events:auto!important;cursor:pointer!important}

        /* v383 — Checkout Motoboy: remove título nativo de frete, sobe o bloco Senderzz e suaviza pesos */
        .woocommerce-checkout h1,.woocommerce-checkout h2,.woocommerce-checkout h3,.woocommerce-checkout h4,.wfacp_main_form h1,.wfacp_main_form h2,.wfacp_main_form h3,.wfacp_main_form h4{font-weight:700;letter-spacing:-.015em}
        .wfacp_main_form .wfacp_shipping_heading,.wfacp_main_form .wfacp_shipping_title,.wfacp_main_form .woocommerce-shipping-totals.shipping th,.wfacp_main_form tr.shipping th,.wfacp_main_form .wfacp_shipping_table > h3,.wfacp_main_form .wfacp_section_title:has(h3),.wfacp_main_form .sz-mb-native-shipping-title-hidden{display:none!important}
        .wfacp_main_form .woocommerce-shipping-totals.shipping,.wfacp_main_form tr.shipping,.wfacp_main_form .wfacp_shipping_table{margin:0!important;padding:0!important;border:0!important}
        .wfacp_main_form .sz-mb-date-checkout{margin-top:0!important}
        .sz-mb-payment-topline,.sz-mb-date-topline{font-weight:700;letter-spacing:-.015em}.sz-mb-payment-head strong,.sz-mb-date-head strong,.sz-mb-order-card strong{font-weight:700}.sz-mb-payment-head small,.sz-mb-date-head small,.sz-mb-order-card span,.sz-mb-payment-total{font-weight:500}.sz-mb-payment-term,.sz-mb-date-badge{font-weight:700;letter-spacing:0}.sz-mb-date-alert{font-size:var(--sz-text-sm);font-weight:600}

        @media (max-width: 1100px){.sz-mb-date-options{grid-template-columns:repeat(3,minmax(0,1fr))}}
        @media (max-width: 900px){.sz-mb-offer-strip{grid-template-columns:1fr!important}.sz-mb-offer-strip-price{text-align:left}.sz-mb-cutoff-note{white-space:normal}}
        @media (max-width: 680px){.sz-mb-date-checkout{padding:0}.sz-mb-delivery-panel{padding:20px 18px}.sz-mb-date-options{grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.sz-mb-date-head{grid-template-columns:48px 1fr;gap:12px}.sz-mb-date-topline{font-size:var(--sz-text-xl)}.sz-mb-date-head strong{font-size:var(--sz-text-lg)}.sz-mb-date-option{min-height:118px}.sz-mb-date-option b{font-size:var(--sz-text-lg)}.sz-mb-date-option em{font-size:var(--sz-text-lg)}}

        @media(max-width:780px){.sz-mb-date-checkout{padding:18px 14px}.sz-mb-date-head{grid-template-columns:46px 1fr;gap:13px}.sz-mb-date-icon{width:46px;height:46px;border-radius:15px;font-size:var(--sz-text-xl)}.sz-mb-date-topline{font-size:var(--sz-text-xl)}.sz-mb-offer-strip{grid-template-columns:1fr!important}.sz-mb-date-options{grid-template-columns:1fr}.sz-mb-date-option{min-height:112px}.sz-mb-date-option b{font-size:var(--sz-text-lg)}}


        /* v383 — checkout final com cabeçalho acima, CTA acima das copies e ajustes finos */
        .sz-mb-date-checkout{margin:0 auto 12px!important;padding:0!important;background:transparent!important;border:0!important;box-shadow:none!important}
        .sz-mb-date-checkout .sz-mb-delivery-panel{box-sizing:border-box;width:100%!important;padding:22px 28px 20px!important;border:1px solid #e6edf5!important;border-radius:24px!important;background:#fff!important;box-shadow:none!important;overflow:hidden!important}
        .sz-mb-date-checkout .sz-mb-offer-strip{display:grid!important;grid-template-columns:minmax(200px,1.05fr) minmax(185px,.72fr) minmax(292px,1fr)!important;align-items:center!important;gap:18px!important;margin:0 0 18px!important;padding:14px 16px!important;border:0!important;border-radius:18px!important;background:linear-gradient(135deg,#E8650A 0%,#F97316 52%,#C8540A 100%)!important;color:#fff!important;box-shadow:none!important}
        .sz-mb-date-checkout .sz-mb-offer-strip-main{display:grid!important;grid-template-columns:58px minmax(0,1fr)!important;align-items:center!important;gap:13px!important;min-width:0!important}
        .sz-mb-date-checkout .sz-mb-offer-thumb{width:58px!important;height:58px!important;border-radius:15px!important;background:rgba(255,255,255,.24)!important;display:flex!important;align-items:center!important;justify-content:center!important;overflow:hidden!important;font-size:24px;flex:0 0 auto!important}
        .sz-mb-date-checkout .sz-mb-offer-thumb img{display:block!important;width:100%!important;height:100%!important;object-fit:cover!important;border-radius:15px!important}
        .sz-mb-date-checkout .sz-mb-offer-copy,.sz-mb-date-checkout .sz-mb-offer-strip-price,.sz-mb-date-checkout .sz-mb-offer-strip-installments{min-width:0!important}
        .sz-mb-date-checkout .sz-mb-offer-strip small{display:block!important;margin:0 0 5px!important;color:#fff!important;font-size:13px;font-weight:700;line-height:1;letter-spacing:0;text-transform:none;opacity:.96!important}
        .sz-mb-date-checkout .sz-mb-offer-strip-main strong{display:block!important;color:#fff!important;font-size:24px;font-weight:700;line-height:1.05;white-space:normal!important;overflow:hidden!important;text-overflow:clip!important;text-align:left!important;display:-webkit-box!important;-webkit-line-clamp:2!important;-webkit-box-orient:vertical!important}
        .sz-mb-date-checkout .sz-mb-offer-strip-price{position:relative!important;text-align:left!important;padding-left:22px!important;border-left:1px solid rgba(255,255,255,.24)!important}
        .sz-mb-date-checkout .sz-mb-offer-strip-price strong{display:block!important;margin:0!important;padding:0!important;background:transparent!important;color:#fff!important;font-size:28px;font-weight:700;line-height:1;white-space:nowrap!important;overflow:visible!important;text-overflow:clip!important;min-width:176px!important}
        .sz-mb-date-checkout .sz-mb-offer-strip-installments{position:relative!important;padding-left:22px!important;border-left:1px solid rgba(255,255,255,.24)!important}
        .sz-mb-date-checkout .sz-mb-select-shell{position:relative!important;width:100%!important;display:block!important}
        .sz-mb-date-checkout .sz-mb-installment-select{appearance:none!important;-webkit-appearance:none!important;display:block!important;width:100%!important;min-height:48px!important;padding:0 42px 0 14px!important;border:0!important;border-radius:13px!important;background:#fff!important;color:#111827!important;font-family:var(--sz-font);font-size:13px;font-weight:700;line-height:48px;box-shadow:none!important;outline:none!important;white-space:nowrap!important;overflow:hidden!important;text-overflow:ellipsis!important}
        .sz-mb-date-checkout .sz-mb-select-arrow{position:absolute!important;right:15px!important;top:50%!important;transform:translateY(-52%)!important;color:#111827!important;font-size:22px;font-weight:700;line-height:1;pointer-events:none!important}
        #sz-oferta-bar.sz-mb-offer-strip{display:grid!important;grid-template-columns:minmax(200px,1.05fr) minmax(185px,.72fr) minmax(292px,1fr)!important;align-items:center!important;gap:18px!important;margin:16px 0 18px!important;padding:14px 16px!important;border:0!important;border-radius:18px!important;background:linear-gradient(135deg,#E8650A 0%,#F97316 52%,#C8540A 100%)!important;color:#fff!important;box-shadow:none!important}
        #sz-oferta-bar .sz-mb-offer-strip-main{display:grid!important;grid-template-columns:58px minmax(0,1fr)!important;align-items:center!important;gap:13px!important;min-width:0!important}
        #sz-oferta-bar .sz-mb-offer-thumb{width:58px!important;height:58px!important;border-radius:15px!important;background:rgba(255,255,255,.24)!important;display:flex!important;align-items:center!important;justify-content:center!important;overflow:hidden!important;font-size:24px;flex:0 0 auto!important}
        #sz-oferta-bar .sz-mb-offer-thumb img{display:block!important;width:100%!important;height:100%!important;object-fit:cover!important;border-radius:15px!important}
        #sz-oferta-bar .sz-mb-offer-copy,#sz-oferta-bar .sz-mb-offer-strip-price,#sz-oferta-bar .sz-mb-offer-strip-installments{min-width:0!important}
        #sz-oferta-bar small{display:block!important;margin:0 0 5px!important;color:#fff!important;font-size:13px;font-weight:700;line-height:1;letter-spacing:0;text-transform:none;opacity:.96!important}
        #sz-oferta-bar .sz-mb-offer-strip-main strong{display:block!important;color:#fff!important;font-size:24px;font-weight:700;line-height:1.05;white-space:normal!important;overflow:hidden!important;text-overflow:clip!important;text-align:left!important;display:-webkit-box!important;-webkit-line-clamp:2!important;-webkit-box-orient:vertical!important}
        #sz-oferta-bar .sz-mb-offer-strip-price{position:relative!important;text-align:left!important;padding-left:22px!important;border-left:1px solid rgba(255,255,255,.24)!important}
        #sz-oferta-bar .sz-mb-offer-strip-price strong{display:block!important;margin:0!important;padding:0!important;background:transparent!important;color:#fff!important;font-size:28px;font-weight:700;line-height:1;white-space:nowrap!important;overflow:visible!important;text-overflow:clip!important;min-width:176px!important}
        #sz-oferta-bar .sz-mb-offer-strip-installments{position:relative!important;padding-left:22px!important;border-left:1px solid rgba(255,255,255,.24)!important}
        #sz-oferta-bar .sz-mb-select-shell{position:relative!important;width:100%!important;display:block!important;margin:0!important}
        #sz-oferta-bar .sz-mb-installment-select{appearance:none!important;-webkit-appearance:none!important;display:block!important;width:100%!important;min-height:48px!important;padding:0 42px 0 14px!important;border:0!important;border-radius:13px!important;background:#fff!important;color:#111827!important;font-family:var(--sz-font);font-size:13px;font-weight:700;line-height:48px;box-shadow:none!important;outline:none!important;white-space:nowrap!important;overflow:hidden!important;text-overflow:ellipsis!important}
        #sz-oferta-bar .sz-mb-select-arrow{position:absolute!important;right:15px!important;top:50%!important;transform:translateY(-52%)!important;color:#111827!important;font-size:22px;font-weight:700;line-height:1;pointer-events:none!important}
        .sz-mb-date-checkout .sz-mb-date-head{display:grid!important;grid-template-columns:54px minmax(0,1fr)!important;align-items:center!important;gap:16px!important;margin:0 0 14px!important;text-align:left!important;color:#111827!important}
        .sz-mb-date-checkout .sz-mb-date-icon{width:54px!important;height:54px!important;border-radius:15px!important;border:1px solid #e8edf3!important;background:#fff!important;color:#E8650A!important;display:flex!important;align-items:center!important;justify-content:center!important;box-shadow:none!important}
        .sz-mb-date-checkout .sz-mb-date-icon svg{width:30px!important;height:30px!important;display:block!important;stroke:#E8650A!important;stroke-width:1.9!important;fill:none!important;stroke-linecap:round!important;stroke-linejoin:round!important}
        .sz-mb-date-checkout .sz-mb-date-head strong{display:block!important;margin:0 0 5px!important;color:#111827!important;font-size:22px;font-weight:700;line-height:1.12;letter-spacing:-.015em}
        .sz-mb-date-checkout .sz-mb-date-head small{display:block!important;color:#667085!important;font-size:15px;font-weight:500;line-height:1.25}
        .sz-mb-date-checkout .sz-mb-date-options{display:grid!important;grid-template-columns:repeat(5,minmax(0,1fr))!important;gap:12px!important;margin:8px 0 0!important}
        .sz-mb-date-checkout .sz-mb-date-option{appearance:none!important;position:relative!important;min-width:0!important;min-height:126px!important;padding:39px 10px 13px!important;border:1px solid #e2e8f0!important;border-radius:15px!important;background:#fff!important;color:#111827!important;display:flex!important;flex-direction:column!important;align-items:center!important;justify-content:center!important;gap:6px!important;text-align:center!important;box-shadow:none!important;overflow:hidden!important;cursor:pointer!important;line-height:1.1;font-family:var(--sz-font)}
        .sz-mb-date-checkout .sz-mb-date-option:hover{border-color:#E8650A!important;box-shadow:0 10px 22px rgba(232,101,10,.08)!important}
        .sz-mb-date-checkout .sz-mb-date-option.is-active{border:1.5px solid #E8650A!important;background:#fff!important;box-shadow:0 10px 22px rgba(232,101,10,.08)!important}
        .sz-mb-date-checkout .sz-mb-date-option b{display:block!important;width:100%!important;margin:0!important;color:#111827!important;font-size:16px;font-weight:700;line-height:1.12;white-space:nowrap!important;overflow:hidden!important;text-overflow:ellipsis!important}
        .sz-mb-date-checkout .sz-mb-date-option em{display:block!important;margin:0!important;color:#667085!important;font-size:14px;font-style:normal;font-weight:700;line-height:1.12;white-space:nowrap!important}
        .sz-mb-date-checkout .sz-mb-date-option.is-active b,.sz-mb-date-checkout .sz-mb-date-option.is-active em{color:#E8650A!important}
        .sz-mb-date-checkout .sz-mb-date-badge{position:absolute!important;top:10px!important;left:10px!important;right:10px!important;width:auto!important;max-width:none!important;padding:5px 8px!important;border:0!important;border-radius:999px!important;background:linear-gradient(135deg,#E8650A,#C8540A)!important;color:#fff!important;font-size:9px;font-weight:700;letter-spacing:0;line-height:1;text-transform:none;white-space:nowrap!important;box-shadow:none!important;overflow:visible!important;z-index:1!important}
        .sz-mb-date-checkout .sz-mb-date-check{position:absolute!important;top:10px!important;right:10px!important;width:28px!important;height:28px!important;border-radius:999px!important;background:linear-gradient(135deg,#E8650A,#C8540A)!important;color:#fff!important;display:flex!important;align-items:center!important;justify-content:center!important;font-size:15px;font-weight:700;line-height:1;box-shadow:none!important}
        .sz-mb-date-checkout .sz-mb-calendar-icon{display:block!important;position:relative!important;width:23px!important;height:23px!important;margin:0 auto 5px!important;border:2px solid currentColor!important;border-radius:5px!important;color:#1f2937!important;background:transparent!important;box-shadow:none!important;font-style:normal}
        .sz-mb-date-checkout .sz-mb-date-option.is-active .sz-mb-calendar-icon{color:#E8650A!important}
        .sz-mb-date-checkout .sz-mb-calendar-icon:before{content:''!important;position:absolute!important;left:-2px!important;right:-2px!important;top:6px!important;border-top:2px solid currentColor!important}
        .sz-mb-date-checkout .sz-mb-calendar-icon:after{content:''!important;position:absolute!important;left:5px!important;right:5px!important;top:-6px!important;height:8px!important;border-left:2px solid currentColor!important;border-right:2px solid currentColor!important}
        .sz-mb-date-checkout .sz-mb-place-order-slot{display:block!important;margin:18px auto 10px!important;max-width:none!important;width:100%!important}
        .sz-mb-date-checkout .sz-mb-place-order-slot #place_order,.sz-mb-date-checkout .sz-mb-place-order-slot button[name="woocommerce_checkout_place_order"],.sz-mb-date-checkout .sz-mb-place-order-slot .wfacp_place_order,.sz-mb-date-checkout .sz-mb-place-order-slot .wfacp-submit-btn{display:flex!important;align-items:center!important;justify-content:center!important;margin:0 auto!important;max-width:680px!important;width:100%!important;min-height:58px!important;border:0!important;border-radius:14px!important;background:linear-gradient(135deg,#FF4B00,#FF9900)!important;color:#fff!important;font-size:21px;font-weight:700;box-shadow:none!important;text-align:center!important}
        .sz-mb-date-checkout .sz-mb-place-order-slot #place_order.senderzz-next-disabled,.sz-mb-date-checkout .sz-mb-place-order-slot button[name="woocommerce_checkout_place_order"].senderzz-next-disabled,.sz-mb-date-checkout .sz-mb-place-order-slot .wfacp_place_order.senderzz-next-disabled,.sz-mb-date-checkout .sz-mb-place-order-slot .wfacp-submit-btn.senderzz-next-disabled{background:#d9d9d9!important;color:#fff!important;opacity:1!important;cursor:not-allowed!important}
        .sz-mb-date-checkout .sz-mb-date-alert,.sz-mb-date-checkout .sz-mb-cutoff-note{display:flex!important;align-items:center!important;gap:10px!important;margin:0!important;padding:10px 0!important;background:transparent!important;border:0!important;color:#667085!important;text-align:left!important;font-size:15px;font-weight:600;line-height:1.35}
        .sz-mb-date-checkout .sz-mb-date-alert{border-bottom:1px solid #e8edf3!important}
        .sz-mb-date-checkout .sz-mb-notice-icon{width:24px!important;height:24px!important;min-width:24px!important;border-radius:999px!important;background:#f1f5f9!important;color:#64748b!important;display:flex!important;align-items:center!important;justify-content:center!important;font-size:13px;font-weight:700;line-height:1}
        @media (max-width: 1100px){.sz-mb-date-checkout .sz-mb-date-options{grid-template-columns:repeat(3,minmax(0,1fr))!important}.sz-mb-date-checkout .sz-mb-offer-strip,#sz-oferta-bar.sz-mb-offer-strip{grid-template-columns:1fr!important}.sz-mb-date-checkout .sz-mb-offer-strip-price,.sz-mb-date-checkout .sz-mb-offer-strip-installments,#sz-oferta-bar .sz-mb-offer-strip-price,#sz-oferta-bar .sz-mb-offer-strip-installments{padding-left:0!important;border-left:0!important;text-align:left!important}}
        @media (max-width: 680px){.sz-mb-date-checkout .sz-mb-delivery-panel{padding:18px 14px!important}.sz-mb-date-checkout .sz-mb-date-options{grid-template-columns:repeat(2,minmax(0,1fr))!important}.sz-mb-date-checkout .sz-mb-date-option{min-height:118px!important}}

    </style>

    <script id="sz-mb-date-checkout-js">
    window.SZ_MB_ZONA_CEP_ENDPOINT = <?php echo wp_json_encode( esc_url_raw( rest_url( 'sz-motoboy/v1/zona-cep' ) ) ); ?>;
    (function(){
        var lastCep = '', lastCoverageOk = null, lastCoverageMessage = '', validating = false;
        var lastRealCep = '', lastRealOk = null, allowNextClick = false;
        function cookieVal(name){
            var m=document.cookie.match(new RegExp('(?:^|; )'+name.replace(/[.$?*|{}()\[\]\\/+^]/g,'\\$&')+'=([^;]*)'));
            return m ? decodeURIComponent(m[1].replace(/\+/g,' ')) : '';
        }
        function money(v){
            var n=parseFloat(String(v||'').replace(/[^0-9,.-]/g,'').replace('R$','').replace(/\./g,'').replace(',','.'));
            if(!isFinite(n)||n<0)n=0;
            return n.toLocaleString('pt-BR',{style:'currency',currency:'BRL'});
        }
        function getStored(key){try{return sessionStorage.getItem(key)||localStorage.getItem(key)||'';}catch(e){return '';}}
        function cleanName(v){return String(v||'').replace(/\s+—\s+(Motoboy|Correio|Expedição)$/i,'').trim();}
        function onlyDigits(v){return String(v||'').replace(/\D/g,'');}
        function getFirstField(selectors){
            for(var i=0;i<selectors.length;i++){var el=document.querySelector(selectors[i]); if(el) return el;}
            return null;
        }
        function getNameValue(){
            var first=getFirstField(['#billing_first_name','input[name="billing_first_name"]','#shipping_first_name','input[name="shipping_first_name"]','#billing_name','input[name="billing_name"]','input[name="billing_full_name"]','input[name="billing_fullname"]','input[name="full_name"]','input[name="nome"]']);
            var last=getFirstField(['#billing_last_name','input[name="billing_last_name"]','#shipping_last_name','input[name="shipping_last_name"]']);
            var firstVal=first?String(first.value||'').trim():'';
            var lastVal=last?String(last.value||'').trim():'';
            if(last) return (firstVal+' '+lastVal).trim();
            return firstVal;
        }
        function hasFullName(){
            var name=getNameValue().replace(/\s+/g,' ').trim();
            var parts=name.split(' ').filter(function(p){return p.length>=2;});
            return parts.length>=2;
        }
        function getPhoneInput(){return getFirstField(['#billing_phone','input[name="billing_phone"]','#shipping_phone','input[name="shipping_phone"]','input[type="tel"]']);}
        function normalizePhoneDigits(v){var d=onlyDigits(v); if(d.length>11 && d.indexOf('55')===0)d=d.slice(2); return d;}
        function hasFullPhone(){var input=getPhoneInput(); var d=normalizePhoneDigits(input?input.value:''); return (d.length===10 || d.length===11) && !/^(\d)\1+$/.test(d);}
		function szMbFormatPhoneBR(v){
            var d=onlyDigits(v);
            if(d.length>11 && d.indexOf('55')===0) d=d.slice(2);
            d=d.slice(0,11);
            if(d.length<=2) return d;
            if(d.length<=6) return '('+d.slice(0,2)+') '+d.slice(2);
            if(d.length<=10) return '('+d.slice(0,2)+') '+d.slice(2,6)+'-'+d.slice(6);
            return '('+d.slice(0,2)+') '+d.slice(2,7)+'-'+d.slice(7);
        }
        function savePhoneState(){
            var input=getPhoneInput();
            if(!input) return;
            var d=normalizePhoneDigits(input.value||'');
            if(d.length>=10){
                try{sessionStorage.setItem('sz_mb_phone_digits',d);}catch(e){}
            }
        }
        function restorePhoneState(){
            var input=getPhoneInput();
            if(!input) return;
            try{
                var saved=sessionStorage.getItem('sz_mb_phone_digits')||'';
                var cur=normalizePhoneDigits(input.value||'');
                if(saved.length>=10 && cur.length<10){
                    input.value=szMbFormatPhoneBR(saved);
                }else if(cur.length>=10){
                    input.value=szMbFormatPhoneBR(cur);
                }
            }catch(e){}
        }
        function lockField(input, locked){
            if(!input) return;
            var tag=String(input.tagName||'').toLowerCase();
            input.classList.toggle('sz-mb-field-locked', !!locked);
            input.setAttribute('aria-disabled', locked ? 'true' : 'false');

            // Nunca usar disabled no checkout: FunnelKit/Woo recria campo e quebra máscara/estado.
            input.disabled=false;

            if(tag==='select'){
                input.removeAttribute('tabindex');
                return;
            }
            if(locked){
                input.setAttribute('readonly','readonly');
                input.setAttribute('tabindex','-1');
            }else{
                input.removeAttribute('readonly');
                input.removeAttribute('tabindex');
            }
        }
        function uniqueFields(list){
            var out=[];
            list.forEach(function(el){ if(el && out.indexOf(el)===-1) out.push(el); });
            return out;
        }
        function getAddressFields(){
            return uniqueFields([
                getFirstField(['#billing_address_1','input[name="billing_address_1"]','#shipping_address_1','input[name="shipping_address_1"]','input[name="billing_endereco"]','input[name="endereco"]']),
                getFirstField(['#billing_number','input[name="billing_number"]','#billing_numero','input[name="billing_numero"]','input[name="billing_address_number"]','input[name="billing_address_number"]','input[name="numero"]']),
                getFirstField(['#billing_address_2','input[name="billing_address_2"]','#shipping_address_2','input[name="shipping_address_2"]','input[name="complemento"]']),
                getFirstField(['#billing_neighborhood','input[name="billing_neighborhood"]','#billing_bairro','input[name="billing_bairro"]','input[name="bairro"]']),
                getFirstField(['#billing_city','input[name="billing_city"]','#shipping_city','input[name="shipping_city"]','input[name="cidade"]']),
                getFirstField(['#billing_state','select[name="billing_state"]','#shipping_state','select[name="shipping_state"]','select[name="estado"]'])
            ]);
        }
        function getAddressMap(){
            return {
                address:getFirstField(['#billing_address_1','input[name="billing_address_1"]','#shipping_address_1','input[name="shipping_address_1"]','input[name="billing_endereco"]','input[name="endereco"]']),
                number:getFirstField(['#billing_number','input[name="billing_number"]','#billing_numero','input[name="billing_numero"]','input[name="billing_address_number"]','input[name="billing_address_number"]','input[name="numero"]']),
                complement:getFirstField(['#billing_address_2','input[name="billing_address_2"]','#shipping_address_2','input[name="shipping_address_2"]','input[name="complemento"]']),
                neighborhood:getFirstField(['#billing_neighborhood','input[name="billing_neighborhood"]','#billing_bairro','input[name="billing_bairro"]','input[name="bairro"]']),
                city:getFirstField(['#billing_city','input[name="billing_city"]','#shipping_city','input[name="shipping_city"]','input[name="cidade"]']),
                state:getFirstField(['#billing_state','select[name="billing_state"]','#shipping_state','select[name="shipping_state"]','select[name="estado"]'])
            };
        }
        function fieldVal(el){return String(el&&el.value||'').trim();}
        function setFieldValue(el,val){
            if(!el) return;
            val=String(val||'');
            if(el.value===val) return;
            el.value=val;
            try{el.dispatchEvent(new Event('input',{bubbles:true}));}catch(e){}
            try{el.dispatchEvent(new Event('change',{bubbles:true}));}catch(e){}
        }
        function clearCepAddressFields(clearNumber){
            var m=getAddressMap();
            [m.address,m.neighborhood,m.city,m.state].forEach(function(el){setFieldValue(el,'');});
            if(clearNumber) setFieldValue(m.number,'');
        }
        function hasResolvedCepAddress(){
            var m=getAddressMap();
            return fieldVal(m.address).length>=3 && fieldVal(m.neighborhood).length>=2 && fieldVal(m.city).length>=2 && fieldVal(m.state).length>=2;
        }
        function applyViaCepData(data){
            if(!data || data.erro) return false;
            var m=getAddressMap();
            setFieldValue(m.address,data.logradouro||'');
            setFieldValue(m.neighborhood,data.bairro||'');
            setFieldValue(m.city,data.localidade||'');
            setFieldValue(m.state,data.uf||'');
            return hasResolvedCepAddress();
        }
        function fetchCepAddress(cep){
            cep=onlyDigits(cep);
            if(cep.length!==8) return Promise.resolve(false);
            if(window.__szMbViaCepInFlight===cep) return Promise.resolve(false);
            window.__szMbViaCepInFlight=cep;
            return fetch('https://viacep.com.br/ws/'+encodeURIComponent(cep)+'/json/',{credentials:'omit'})
                .then(function(r){return r.json().catch(function(){return{};});})
                .then(function(data){
                    window.__szMbViaCepInFlight='';
                    var ok=applyViaCepData(data||{});
                    setTimeout(applyProgressiveFieldLocks,0);
                    return ok;
                })
                .catch(function(){window.__szMbViaCepInFlight='';setTimeout(applyProgressiveFieldLocks,0);return false;});
        }
        function lockAddressFields(locked){
            getAddressFields().forEach(function(el){ lockField(el, locked); });
        }
        function removePhoneValidationNoise(){
            document.querySelectorAll('.wfacp_billing_phone_field_error,.wfacp_inline_field_error').forEach(function(el){
                var txt=String(el.textContent||'').toLowerCase();
                if(el.classList.contains('wfacp_billing_phone_field_error') || txt.indexOf('phone number')>=0 || txt.indexOf('telefone')>=0){ el.remove(); }
            });
        }
        function applyProgressiveFieldLocks(){
            removePhoneValidationNoise();
            if(!document.body) return {nameOk:false,phoneOk:false,cepOk:false,addressOk:false};

            var nameOk=hasFullName();
            var phone=getPhoneInput();
            lockField(phone,!nameOk);
            document.body.classList.toggle('sz-mb-phone-ready', !!nameOk);

            var phoneOk=nameOk && hasFullPhone();
            var cep=getCepInput();
            lockField(cep,!phoneOk);
            document.body.classList.toggle('sz-mb-cep-ready', !!phoneOk);

            var cepDigits=getCep();
            var cepOk=phoneOk && cepDigits.length===8 && (lastCoverageOk===true || lastRealOk===true);
            var addressOk=cepOk && hasResolvedCepAddress();
            var m=getAddressMap();

            // Endereço/cidade/bairro/estado só liberam visualmente depois do CEP aprovado.
            [m.address,m.neighborhood,m.city,m.state].forEach(function(el){lockField(el,!cepOk);});
            // Complemento não deve travar o fluxo.
            lockField(m.complement,!cepOk);
            // Número só libera depois que o CEP trouxe endereço/bairro/cidade/estado.
            lockField(m.number,!addressOk);

            document.body.classList.toggle('sz-mb-address-ready', !!addressOk);

            if(!phoneOk){
                lastCoverageOk=null;
                lastRealOk=null;
                markCepInput('');
                showCepFeedback('', '');
            }

            return {nameOk:nameOk,phoneOk:phoneOk,cepOk:cepOk,addressOk:addressOk};
        }
        function getCepInput(){return document.querySelector('#billing_postcode,input[name="billing_postcode"],#shipping_postcode,input[name="shipping_postcode"],input[autocomplete="postal-code"]');}
        function getCep(){var i=getCepInput();return onlyDigits(i?i.value:'');}
        function showCepFeedback(type,msg){
            document.querySelectorAll('.sz-mb-cep-feedback').forEach(function(box){ if(box && box.parentNode) box.parentNode.removeChild(box); });
            if(type==='error'){
                var wrapper=document.querySelector('.woocommerce-notices-wrapper,.wfacp-notices-wrapper,.woocommerce-error');
                var text=msg||'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.';
                if(wrapper && !document.querySelector('.sz-mb-cep-global-error')){
                    var div=document.createElement('div');
                    div.className='woocommerce-error sz-mb-cep-global-error';
                    div.textContent=text;
                    wrapper.appendChild(div);
                    setTimeout(function(){ if(div && div.parentNode) div.parentNode.removeChild(div); }, 5500);
                }
            }
        }
        function markCepInput(type){
            var input=getCepInput(); if(!input)return;
            input.classList.remove('sz-mb-cep-invalid','sz-mb-cep-ok');
            if(type==='error')input.classList.add('sz-mb-cep-invalid');
        }
        function setZoneMessage(msg){ document.querySelectorAll('[data-sz-zone-message],.sz-mb-date-footnote').forEach(function(el){if(msg)el.textContent=msg;}); }
        function dateLabel(d){ if(!d)return''; var day=String(d.day||'').replace(/^0/,''); var month=d.month||''; return String(d.label||d.full||'') + (day&&month ? ', '+day+' de '+month : ''); }
        function szMbPadDates(dates, qty){
            qty = qty || 5;
            if(!Array.isArray(dates)) dates=[];
            dates = dates.filter(function(d){return d && (d.value || d.label || d.full);}).slice(0, qty);
            var meses=['Jan','Fev','Mar','Abr','Mai','Jun','Jul','Ago','Set','Out','Nov','Dez'];
            var full=['Domingo','Segunda-feira','Terça-feira','Quarta-feira','Quinta-feira','Sexta-feira','Sábado'];
            var seen={};
            dates.forEach(function(d){ if(d.value) seen[String(d.value)] = true; });
            function parseYmd(v){
                var m=String(v||'').match(/^(\d{4})-(\d{2})-(\d{2})$/);
                return m ? new Date(Number(m[1]), Number(m[2])-1, Number(m[3])) : null;
            }
            var base = dates.length ? parseYmd(dates[dates.length-1].value) : null;
            if(!base || isNaN(base.getTime())) base = new Date();
            var tomorrow = new Date(); tomorrow.setDate(tomorrow.getDate()+1);
            var guard=0;
            while(dates.length < qty && guard < 30){
                guard++;
                base = new Date(base.getFullYear(), base.getMonth(), base.getDate()+1);
                if(base.getDay()===0) continue;
                var ymd = base.getFullYear()+'-'+String(base.getMonth()+1).padStart(2,'0')+'-'+String(base.getDate()).padStart(2,'0');
                if(seen[ymd]) continue;
                seen[ymd]=true;
                var isTomorrow = base.toDateString() === tomorrow.toDateString();
                dates.push({value:ymd,label:isTomorrow?'Amanhã':full[base.getDay()],full:isTomorrow?'Amanhã':full[base.getDay()],day:String(base.getDate()).padStart(2,'0'),month:meses[base.getMonth()]});
            }
            return dates;
        }
        function renderDates(dates){
            if(!Array.isArray(dates)||!dates.length)return;
            dates = szMbPadDates(dates, 5);
            document.querySelectorAll('[data-sz-mb-date-checkout]').forEach(function(wrap){
                var options=wrap.querySelector('.sz-mb-date-options'); if(!options)return;
                options.innerHTML='';
                dates.forEach(function(d,idx){
                    var btn=document.createElement('button'); btn.type='button'; btn.className='sz-mb-date-option'+(idx===0?' is-active':''); btn.setAttribute('data-sz-date-option','1'); btn.setAttribute('data-date',d.value||''); btn.setAttribute('data-label',dateLabel(d)); btn.setAttribute('aria-pressed',idx===0?'true':'false');
                    btn.innerHTML=(idx===0?'<span class="sz-mb-date-badge">Mais próxima</span>':'')+'<i class="sz-mb-calendar-icon" aria-hidden="true"></i><b>'+(d.label||d.full||'')+'</b><em>'+String(d.day||'').replace(/^0/,'')+' '+(d.month||'')+'</em>'+(idx===0?'<strong class="sz-mb-date-check">✓</strong>':'');
                    options.appendChild(btn);
                });
                var first=dates[0]; var input=wrap.querySelector('#sz_delivery_date'); if(input)input.value=first.value||''; var label=wrap.querySelector('.sz-mb-selected-label'); if(label)label.textContent=dateLabel(first);
            });
        }
        function syncOfferCards(){
            var name=cleanName(window.sz_oferta_nome||getStored('senderzz_offer_name')||cookieVal('senderzz_offer_name'));
            var price=window.sz_oferta_preco||getStored('senderzz_offer_value')||cookieVal('senderzz_offer_value');
            document.querySelectorAll('.sz-mb-offer-name').forEach(function(el){if(name)el.textContent=name;});
            document.querySelectorAll('.sz-mb-offer-value').forEach(function(el){if(price)el.textContent=/R\$/.test(String(price))?String(price):money(price);});
        }
        function blockMsg(){return 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.';}
        function validateCepFormat(forceMessage){
            var cep=getCep();
            if(!cep || cep.length!==8 || /^(\d)\1{7}$/.test(cep)){
                if(forceMessage){showCepFeedback('error',blockMsg());markCepInput('error');}
                return false;
            }
            return true;
        }
        function applyCepData(data,forceMessage,realCheck,cep){
            validating=false;
            var ok=!!(data&&data.ok);
            var msg=(data&&(data.mensagem||data.erro))||blockMsg();
            lastCep=cep; lastCoverageOk=ok; lastCoverageMessage=msg;
            if(realCheck){ lastRealCep=cep; lastRealOk=ok; }

            if(ok){
                markCepInput('');
                showCepFeedback('', '');
                if(data.checkout_message) setZoneMessage(data.checkout_message);
                if(Array.isArray(data.next_dates)&&data.next_dates.length) renderDates(data.next_dates);

                fetchCepAddress(cep).then(function(){
                    applyProgressiveFieldLocks();
                    try{document.body.dispatchEvent(new CustomEvent('sz_motoboy_zone_ok',{detail:data}));}catch(e){}
                });

                setTimeout(applyProgressiveFieldLocks,0);
                return true;
            }

            clearCepAddressFields(true);
            markCepInput('error');
            setTimeout(applyProgressiveFieldLocks,0);
            if(forceMessage) showCepFeedback('error', msg);
            return false;
        }
        function checkCoverage(forceMessage, realCheck){
            var cep=getCep(); realCheck=!!realCheck;
            if(!validateCepFormat(forceMessage)){
                lastCep=cep; lastCoverageOk=false; lastCoverageMessage=blockMsg();
                if(realCheck){lastRealCep=cep; lastRealOk=false;}
                return Promise.resolve(false);
            }
            if(realCheck && cep===lastRealCep && lastRealOk!==null){
                if(!lastRealOk && forceMessage){showCepFeedback('error',lastCoverageMessage||blockMsg());markCepInput('error');}
                return Promise.resolve(!!lastRealOk);
            }
            if(!realCheck && cep===lastCep && lastCoverageOk!==null){
                if(lastCoverageOk){markCepInput('');}
                else if(forceMessage){showCepFeedback('error',lastCoverageMessage||blockMsg());markCepInput('error');}
                return Promise.resolve(!!lastCoverageOk);
            }
            validating=true;
            var cacheKey='sz_mb_cep_v391_'+cep+'_'+(realCheck?'real':'zone');
            try{
                var cached=sessionStorage.getItem(cacheKey);
                if(cached){
                    var cachedData=JSON.parse(cached);
                    if(cachedData && cachedData.expires && cachedData.expires>Date.now()){
                        return Promise.resolve(applyCepData(cachedData.data||{},forceMessage,realCheck,cep));
                    }
                }
            }catch(e){}
            if(typeof fetch !== 'function'){
                return Promise.resolve(applyCepData({ok:false,mensagem:blockMsg()},forceMessage,realCheck,cep));
            }
            var url=(window.SZ_MB_ZONA_CEP_ENDPOINT||'/wp-json/sz-motoboy/v1/zona-cep')+'?cep='+encodeURIComponent(cep)+'&real='+(realCheck?'1':'0');
            return fetch(url,{credentials:'same-origin'}).then(function(r){return r.json().catch(function(){return{};});}).then(function(data){
                try{sessionStorage.setItem(cacheKey,JSON.stringify({expires:Date.now()+(realCheck?43200000:21600000),data:data||{}}));}catch(e){}
                return applyCepData(data||{},forceMessage,realCheck,cep);
            }).catch(function(){
                return applyCepData({ok:false,mensagem:blockMsg()},forceMessage,realCheck,cep);
            });
        }
        function shouldValidateClick(el){
            if(!el)return false; var txt=String(el.textContent||el.value||'').toLowerCase();
            return /próxima|proxima|finalizar|pedido|continuar/.test(txt) || el.matches('#place_order,.wfacp_next_page_button,.wfacp_next_step_btn,.wfacp-submit-btn,.wfacp_place_order');
        }
      function initCep(){
            var input=getCepInput();
            if(!input || input.__szMbInit) return;
            input.__szMbInit=true;

            function resetCepState(clearAddress){
                lastCep='';
                lastCoverageOk=null;
                lastCoverageMessage='';
                lastRealCep='';
                lastRealOk=null;
                markCepInput('');
                showCepFeedback('', '');
                if(clearAddress) clearCepAddressFields(true);
            }

            input.addEventListener('blur',function(){
                var cep=getCep();
                if(cep.length===8) checkCoverage(false,false);
                else applyProgressiveFieldLocks();
            });

            input.addEventListener('change',function(){
                var cep=getCep();
                resetCepState(cep.length!==8);
                if(cep.length===8) checkCoverage(false,false);
                else applyProgressiveFieldLocks();
            });

            input.addEventListener('input',function(){
                var cep=getCep();
                resetCepState(cep.length!==8);
                clearTimeout(window.__szMbCepTimer);
                applyProgressiveFieldLocks();
                if(cep.length===8){
                    window.__szMbCepTimer=setTimeout(function(){checkCoverage(false,false);},350);
                }
            });
        }

document.addEventListener('click', function(e){
    var btn = e.target.closest('button,input[type="button"],input[type="submit"],a');
    if(!shouldValidateClick(btn)) return;

    if(allowNextClick){
        allowNextClick = false;
        return;
    }

    if(!document.querySelector('[data-sz-mb-date-checkout]') && !/próxima|proxima/i.test(String(btn.textContent || btn.value || ''))) {
        return;
    }

    e.preventDefault();
    e.stopPropagation();

    savePhoneState();

    var locks = applyProgressiveFieldLocks();

    if(!locks.nameOk || !locks.phoneOk) {
        restorePhoneState();
        return false;
    }

    if(!validateCepFormat(true)) {
        restorePhoneState();
        return false;
    }

    Promise.resolve(checkCoverage(true, true)).then(function(ok){
        restorePhoneState();

        if(!ok){
            applyProgressiveFieldLocks();
            return;
        }

        fetchCepAddress(getCep()).then(function(){
            var afterLocks = applyProgressiveFieldLocks();
            if(!afterLocks.addressOk){
                showCepFeedback('error', 'Não foi possível carregar os dados do CEP. Digite um CEP válido para liberar o número.');
                markCepInput('error');
                return;
            }
            allowNextClick = true;
            setTimeout(function(){
                try {
                    btn.click();
                } catch(ex) {}
            }, 20);
        });
    });

    return false;
}, true);

syncOfferCards();
restorePhoneState();
applyProgressiveFieldLocks();

function szMbPostWooRefresh(){
    restorePhoneState();
    initCep();
    applyProgressiveFieldLocks();
    syncOfferCards();
    var cep=getCep();
    if(cep.length===8){
        checkCoverage(false,false);
    }
}

document.addEventListener('input', function(e){
    var phone=getPhoneInput();

    if(e.target && phone && e.target===phone){
        clearTimeout(window.__szMbPhoneMaskTimer);
        window.__szMbPhoneMaskTimer=setTimeout(function(){
            var d=normalizePhoneDigits(phone.value||'');
            phone.value=szMbFormatPhoneBR(d);
            savePhoneState();
            applyProgressiveFieldLocks();
        },20);
        return;
    }

    clearTimeout(window.__szMbLockTick);
    window.__szMbLockTick=setTimeout(function(){
        applyProgressiveFieldLocks();
    },30);
}, true);

document.addEventListener('change', function(e){
    var phone=getPhoneInput();
    if(e.target && phone && e.target===phone){
        restorePhoneState();
        savePhoneState();
    }
    setTimeout(applyProgressiveFieldLocks,20);
}, true);

document.addEventListener('DOMContentLoaded', function(){
    initCep();
    szMbPostWooRefresh();
    setTimeout(szMbPostWooRefresh,120);
    setTimeout(syncOfferCards,300);
    setTimeout(syncOfferCards,900);
});

document.addEventListener('updated_checkout', function(){
    savePhoneState();
    setTimeout(szMbPostWooRefresh,180);
    setTimeout(szMbPostWooRefresh,520);
});

/*
 * Removido o MutationObserver agressivo.
 * Ele era uma das causas do pisca no CEP/Estado e da reexecução indevida.
 */
		})(); document.addEventListener('click', function(e){
    var btn = e.target.closest('.sz-mb-date-option');
    if(!btn) return;

    var wrap = btn.closest('[data-sz-mb-date-checkout]');
    if(!wrap) return;

    wrap.querySelectorAll('.sz-mb-date-option').forEach(function(b){
        b.classList.remove('is-active');
        b.setAttribute('aria-pressed', 'false');

        var c = b.querySelector('.sz-mb-date-check');
        if(c) c.remove();
    });

    btn.classList.add('is-active');
    btn.setAttribute('aria-pressed', 'true');

    if(!btn.querySelector('.sz-mb-date-check')){
        var ck = document.createElement('strong');
        ck.className = 'sz-mb-date-check';
        ck.textContent = '✓';
        btn.appendChild(ck);
    }

    var input = wrap.querySelector('#sz_delivery_date');
    if(input) input.value = btn.getAttribute('data-date') || '';

    var label = wrap.querySelector('.sz-mb-selected-label');
    if(label){
        label.textContent = btn.getAttribute('data-label') || '';
    }
}, false);
    function szMbTightenCheckout(){try{var wrap=document.querySelector('[data-sz-mb-date-checkout]'); if(!wrap)return; var p=wrap.parentElement; for(var i=0;i<4&&p;i++,p=p.parentElement){p.style.marginBottom='0';p.style.paddingBottom='0';} var prev=wrap.previousElementSibling; while(prev){var txt=(prev.textContent||'').toLowerCase(); if(txt.indexOf('método')>=0||txt.indexOf('metodo')>=0||txt.indexOf('entrega')>=0){prev.style.marginBottom='6px';prev.style.paddingBottom='0';} prev=prev.previousElementSibling;}}catch(e){}}
    document.addEventListener('DOMContentLoaded',function(){setTimeout(szMbTightenCheckout,120);setTimeout(szMbTightenCheckout,600);});
    document.addEventListener('updated_checkout',function(){setTimeout(szMbTightenCheckout,160);});
    document.addEventListener('submit',function(e){
        var wrap=document.querySelector('[data-sz-mb-date-checkout]');
        if(!wrap)return;
        var input=wrap.querySelector('#sz_delivery_date');
        if(!input||!input.value){e.preventDefault();alert('Selecione a data de entrega para continuar.');}
    },true);
    </script>
    <?php
}, 120 );







// ─── V100. Helpers: taxas Motoboy do produtor sem somar no total do cliente ─
if ( ! function_exists( 'sz_mb_current_offer_value_for_fee' ) ) {
    function sz_mb_current_offer_value_for_fee( ?WC_Order $order = null ): float {
        if ( $order instanceof WC_Order ) {
            $v = (float) $order->get_meta( '_senderzz_offer_value', true );
            if ( $v > 0 ) return $v;
            $sum = 0.0;
            foreach ( $order->get_items( 'line_item' ) as $item ) {
                $sum += (float) $item->get_total();
            }
            if ( $sum > 0 ) return $sum;
        }

        $data = function_exists( 'senderzz_sz_get_data' ) ? senderzz_sz_get_data() : null;
        if ( is_array( $data ) && isset( $data['valor'] ) && (float) $data['valor'] > 0 ) {
            return (float) $data['valor'];
        }
        if ( function_exists( 'WC' ) && WC()->cart ) {
            $subtotal = (float) WC()->cart->get_subtotal();
            if ( $subtotal > 0 ) return $subtotal;
        }
        return 0.0;
    }
}

if ( ! function_exists( 'sz_mb_calculate_fee_for_current_checkout' ) ) {
    function sz_mb_calculate_fee_for_current_checkout( ?WC_Order $order = null ): array {
        $producer_id = function_exists( 'sz_mb_get_portal_user_id' ) ? (int) sz_mb_get_portal_user_id() : 0;
        if ( ! $producer_id && $order instanceof WC_Order ) {
            $producer_id = (int) $order->get_meta( '_senderzz_owner_user_id', true );
        }
        if ( ! $producer_id && $order instanceof WC_Order && function_exists( 'senderzz_get_order_wallet_owner_id' ) ) {
            $producer_id = (int) senderzz_get_order_wallet_owner_id( $order );
        }
        $valor = sz_mb_current_offer_value_for_fee( $order );
        if ( $producer_id && function_exists( 'sz_mb_calcular_taxa_produtor' ) ) {
            $taxas = sz_mb_calcular_taxa_produtor( $producer_id, $valor );
        } else {
            $ent = (float) get_option( 'sz_motoboy_taxa_entrega', 25.00 );
            $taxas = [ 'taxa_entrega' => $ent, 'taxa_manuseio' => 0.0, 'taxa_percentual' => 0.0, 'taxa_transacao_modo' => 'split_producer_affiliate', 'adicional' => 0.0, 'total' => $ent ];
        }
        foreach ( [ 'taxa_entrega', 'taxa_manuseio', 'taxa_percentual', 'adicional', 'total' ] as $k ) {
            $taxas[ $k ] = round( (float) ( $taxas[ $k ] ?? 0 ), 2 );
        }
        return $taxas;
    }
}

if ( ! function_exists( 'sz_mb_store_order_fees' ) ) {
    function sz_mb_store_order_fees( WC_Order $order ): void {
        $is_motoboy = (string) $order->get_meta( '_senderzz_delivery_mode', true ) === 'motoboy';
        if ( ! $is_motoboy ) {
            foreach ( $order->get_items( 'shipping' ) as $item ) {
                $mid = strtolower( (string) $item->get_method_id() );
                $name = strtolower( (string) $item->get_name() );
                if ( strpos( $mid, 'sz_motoboy' ) !== false || strpos( $name, 'motoboy' ) !== false ) { $is_motoboy = true; break; }
            }
        }
        if ( ! $is_motoboy && function_exists( 'sz_mb_current_checkout_is_motoboy' ) ) {
            $is_motoboy = sz_mb_current_checkout_is_motoboy();
        }
        if ( ! $is_motoboy ) return;

        $taxas = sz_mb_calculate_fee_for_current_checkout( $order );
        $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
        $order->update_meta_data( '_sz_mb_taxa_entrega', $taxas['taxa_entrega'] );
        $order->update_meta_data( '_sz_mb_taxa_manuseio', $taxas['taxa_manuseio'] );
        $order->update_meta_data( '_sz_mb_taxa_adicional', $taxas['adicional'] );
        $order->update_meta_data( '_sz_mb_taxa_percentual', $taxas['taxa_percentual'] );
        $order->update_meta_data( '_sz_mb_taxa_transacao_modo', function_exists( 'sz_mb_sanitize_taxa_transacao_mode' ) ? sz_mb_sanitize_taxa_transacao_mode( $taxas['taxa_transacao_modo'] ?? 'split_producer_affiliate' ) : sanitize_key( (string) ( $taxas['taxa_transacao_modo'] ?? 'split_producer_affiliate' ) ) );
        $order->update_meta_data( '_sz_mb_taxa_total', $taxas['total'] );
        // Compatibilidade com metas antigas/itens de frete.
        // _sz_taxa_total agora espelha _sz_mb_taxa_total (entrega + manuseio + percentual).
        $order->update_meta_data( '_sz_taxa_entrega', $taxas['taxa_entrega'] );
        $order->update_meta_data( '_sz_taxa_manuseio', $taxas['taxa_manuseio'] );
        $order->update_meta_data( '_sz_taxa_adicional', $taxas['adicional'] );
        $order->update_meta_data( '_sz_taxa_total', $taxas['total'] );
        $order->update_meta_data( '_senderzz_shipping_charged', $taxas['total'] );
        $order->update_meta_data( '_senderzz_shipping_real_cost', $taxas['total'] );
        $order->update_meta_data( '_senderzz_service_fee', 0 );
        $order->update_meta_data( '_senderzz_margin', 0 );

        foreach ( $order->get_items( 'shipping' ) as $item ) {
            $mid = strtolower( (string) $item->get_method_id() );
            $name = strtolower( (string) $item->get_name() );
            if ( strpos( $mid, 'sz_motoboy' ) === false && strpos( $name, 'motoboy' ) === false ) continue;
            $item->update_meta_data( 'sz_taxa_entrega', $taxas['taxa_entrega'] );
            $item->update_meta_data( 'sz_taxa_manuseio', $taxas['taxa_manuseio'] );
            $item->update_meta_data( 'sz_taxa_adicional', $taxas['adicional'] );
            $item->update_meta_data( 'sz_taxa_total', $taxas['total'] ); // total real: entrega + manuseio + percentual
            $item->save();
        }
    }
}



// ─── V281. Motoboy checkout: não deixa Melhor Envio/outros métodos calcularem ─
// O filtro woocommerce_package_rates acontece tarde demais: métodos como Melhor Envio
// já podem ter tentado cotar no update_order_review e gerar 500/timeout vazio.
// Em link Motoboy, a zona já é validada pelo CEP; então mantemos apenas o método
// sz_motoboy antes do WooCommerce chamar calculate_shipping().
if ( ! function_exists( 'sz_mb_filter_zone_methods_for_public_checkout' ) ) {
    function sz_mb_filter_zone_methods_for_public_checkout( $methods ) {
        if ( ! function_exists( 'sz_mb_current_checkout_is_motoboy' ) || ! sz_mb_current_checkout_is_motoboy() ) {
            return $methods;
        }
        if ( ! function_exists( 'is_checkout' ) && ! wp_doing_ajax() ) {
            return $methods;
        }

        $kept = [];
        foreach ( (array) $methods as $key => $method ) {
            $id          = is_object( $method ) && isset( $method->id ) ? strtolower( (string) $method->id ) : '';
            $method_id   = is_object( $method ) && isset( $method->method_id ) ? strtolower( (string) $method->method_id ) : '';
            $instance_id = is_object( $method ) && isset( $method->instance_id ) ? (string) $method->instance_id : '';
            $title       = '';
            if ( is_object( $method ) && method_exists( $method, 'get_title' ) ) {
                $title = strtolower( (string) $method->get_title() );
            } elseif ( is_object( $method ) && isset( $method->title ) ) {
                $title = strtolower( (string) $method->title );
            }

            if ( strpos( $id, 'sz_motoboy' ) !== false
              || strpos( $method_id, 'sz_motoboy' ) !== false
              || strpos( $title, 'motoboy' ) !== false
              || $instance_id === '2' ) {
                $kept[ $key ] = $method;
            }
        }

        return ! empty( $kept ) ? $kept : $methods;
    }
}
add_filter( 'woocommerce_shipping_zone_shipping_methods', 'sz_mb_filter_zone_methods_for_public_checkout', 100000, 1 );

// ─── V99. Helper: cria rate Motoboy virtual para checkout público ──────────
if ( ! function_exists( 'sz_mb_build_public_rate_if_covered' ) ) {
    function sz_mb_build_public_rate_if_covered( $package ): array {
        if ( ! sz_mb_current_checkout_is_motoboy() ) return [];

        $cep = preg_replace( '/\D/', '', $package['destination']['postcode'] ?? '' );
        if ( strlen( $cep ) !== 8 || ! function_exists( 'sz_motoboy_resolver_zona' ) ) return [];

        $zona = sz_motoboy_resolver_zona( $cep );
        if ( ! $zona ) return [];

        $user_id = function_exists( 'sz_mb_get_portal_user_id' ) ? sz_mb_get_portal_user_id() : 0;
        // Em link motoboy válido, não deixa a ausência de login do comprador matar o rate.
        if ( $user_id && function_exists( 'sz_mb_produtor_ativo' ) && ! sz_mb_produtor_ativo( $user_id ) ) return [];

        if ( ! class_exists( 'WC_Shipping_Rate' ) ) return [];

        $taxas = sz_mb_calculate_fee_for_current_checkout( null );
        $rate_id = 'sz_motoboy:senderzz';
        $rate = new WC_Shipping_Rate( $rate_id, '🏍️ Motoboy Senderzz', 0, [], 'sz_motoboy' );
        if ( method_exists( $rate, 'add_meta_data' ) ) {
            $rate->add_meta_data( 'sz_motoboy_checkout', '1', true );
            $rate->add_meta_data( 'sz_motoboy_zona_id', (string) ( $zona['zona_id'] ?? '' ), true );
            $rate->add_meta_data( 'sz_motoboy_cd_id', (string) ( $zona['cd_id'] ?? '' ), true );
            $rate->add_meta_data( 'sz_taxa_entrega', (string) $taxas['taxa_entrega'], true );
            $rate->add_meta_data( 'sz_taxa_manuseio', (string) $taxas['taxa_manuseio'], true );
            $rate->add_meta_data( 'sz_taxa_adicional', (string) $taxas['adicional'], true );
            $rate->add_meta_data( 'sz_taxa_total', (string) $taxas['total'], true );
        }
        return [ $rate_id => $rate ];
    }
}

// ─── V98. Isolamento forte: link Motoboy nunca usa Expedição/Jadlog ─────────
if ( ! function_exists( 'sz_mb_extract_motoboy_rates_only' ) ) {
    function sz_mb_extract_motoboy_rates_only( $rates ): array {
        $motoboy = [];
        foreach ( (array) $rates as $key => $rate ) {
            $method_id = is_object( $rate ) && isset( $rate->method_id ) ? (string) $rate->method_id : '';
            $rate_id   = is_object( $rate ) && method_exists( $rate, 'get_id' ) ? (string) $rate->get_id() : (string) $key;
            $label     = is_object( $rate ) && method_exists( $rate, 'get_label' ) ? strtolower( (string) $rate->get_label() ) : '';
            if ( strpos( $key, 'sz_motoboy' ) !== false || strpos( $method_id, 'sz_motoboy' ) !== false || strpos( $rate_id, 'sz_motoboy' ) !== false || strpos( $label, 'motoboy' ) !== false ) {
                $motoboy[ $key ] = $rate;
            }
        }
        return $motoboy;
    }
}

add_filter( 'woocommerce_package_rates', function( $rates, $package ) {
    if ( is_admin() && ! wp_doing_ajax() ) return $rates;
    if ( ! sz_mb_current_checkout_is_motoboy() ) return $rates;

    $motoboy = sz_mb_extract_motoboy_rates_only( $rates );

    // O checkout motoboy não deve herdar Jadlog, mas também não pode ficar sem rate
    // quando o comprador está anônimo e o método WooCommerce não calculou sozinho.
    if ( empty( $motoboy ) ) {
        $motoboy = sz_mb_build_public_rate_if_covered( $package );
    }

    // Cliente não paga a taxa Motoboy no checkout público: total fica o valor do link.
    foreach ( $motoboy as $rate ) {
        if ( is_object( $rate ) ) {
            if ( method_exists( $rate, 'set_cost' ) ) $rate->set_cost( 0 );
            if ( method_exists( $rate, 'set_taxes' ) ) $rate->set_taxes( [] );
        }
    }

    if ( function_exists( 'WC' ) && WC()->session ) {
        if ( ! empty( $motoboy ) ) {
            $first_key = array_key_first( $motoboy );
            WC()->session->set( 'chosen_shipping_methods', [ $first_key ] );
            WC()->session->set( 'sz_frete_forcado', 'motoboy' );
        } else {
            WC()->session->set( 'chosen_shipping_methods', [] );
        }
    }

    return $motoboy;
}, 10000, 2 );

add_action( 'woocommerce_before_checkout_process', function() {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return;
    if ( ! function_exists( 'WC' ) || ! WC()->session ) return;

    $cep = '';
    if ( isset( $_POST['billing_postcode'] ) ) {
        $cep = preg_replace( '/\D/', '', sanitize_text_field( wp_unslash( $_POST['billing_postcode'] ) ) );
    }
    if ( ! $cep && isset( $_POST['shipping_postcode'] ) ) {
        $cep = preg_replace( '/\D/', '', sanitize_text_field( wp_unslash( $_POST['shipping_postcode'] ) ) );
    }

    // Guarda de servidor: mesmo que o front ignore botão disabled/aba, pedido motoboy sem cobertura não passa.
    if ( $cep !== '' && strlen( $cep ) !== 8 ) {
        WC()->session->set( 'chosen_shipping_methods', [] );
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }
    if ( strlen( $cep ) === 8 && function_exists( 'sz_motoboy_resolver_zona' ) && ! sz_motoboy_resolver_zona( $cep ) ) {
        WC()->session->set( 'chosen_shipping_methods', [] );
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }

    $chosen = (array) WC()->session->get( 'chosen_shipping_methods', [] );
    foreach ( $chosen as $method ) {
        if ( strpos( (string) $method, 'sz_motoboy' ) !== false || stripos( (string) $method, 'motoboy' ) !== false ) {
            return;
        }
    }

    // Não adiciona notice vermelho aqui. O próprio filtro de rates cria/seleciona
    // sz_motoboy quando o CEP está coberto; se não estiver, o WooCommerce bloqueia.
}, 5 );


// Blindagem server-side da agenda Motoboy: o cliente não consegue forçar data fora
// da regra da cidade alterando o input hidden do checkout.
add_action( 'woocommerce_checkout_process', function() {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return;

    $date = sanitize_text_field( wp_unslash( $_POST['sz_delivery_date'] ?? '' ) );
    if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date ) ) {
        wc_add_notice( 'Selecione uma data de entrega válida para o Motoboy Senderzz.', 'error' );
        return;
    }

    $cep = function_exists( 'sz_mb_current_checkout_postcode' ) ? sz_mb_current_checkout_postcode() : '';
    if ( strlen( $cep ) !== 8 ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }

    if ( strlen( $cep ) === 8 && function_exists( 'sz_mb_cep_exists' ) && sz_mb_cep_exists( $cep ) !== true ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }
    if ( strlen( $cep ) === 8 && function_exists( 'sz_motoboy_resolver_zona' ) && ! sz_motoboy_resolver_zona( $cep ) ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }

    if ( strlen( $cep ) === 8 && function_exists( 'sz_motoboy_resolver_zona' ) && function_exists( 'sz_motoboy_zone_date_is_allowed' ) ) {
        $zona = sz_motoboy_resolver_zona( $cep );
        if ( $zona && ! sz_motoboy_zone_date_is_allowed( $date, $zona['dias_funcionamento'] ?? '', $zona['cutoff_horarios'] ?? '' ) ) {
            wc_add_notice( 'Essa data não está disponível para o CEP informado. Escolha uma das datas mostradas pela Senderzz para sua região.', 'error' );
            return;
        }
    }

    $city = '';
    if ( isset( $_POST['shipping_city'] ) ) $city = sanitize_text_field( wp_unslash( $_POST['shipping_city'] ) );
    if ( ! $city && isset( $_POST['billing_city'] ) ) $city = sanitize_text_field( wp_unslash( $_POST['billing_city'] ) );

    if ( ! $cep && $city && function_exists( 'sz_dr_date_allowed_for_city' ) && ! sz_dr_date_allowed_for_city( $city, $date ) ) {
        $region = function_exists( 'sz_dr_region_label_for_city' ) ? sz_dr_region_label_for_city( $city ) : '';
        $msg = $region
            ? sprintf( 'A cidade %s pertence à rota %s. Escolha uma das datas permitidas exibidas no checkout.', $city, $region )
            : sprintf( 'A data selecionada não é permitida para a cidade %s.', $city );
        wc_add_notice( $msg, 'error' );
    }
}, 2 );

add_action( 'woocommerce_checkout_create_order', function( WC_Order $order ) {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return;

    $has_motoboy = false;
    foreach ( $order->get_shipping_methods() as $item ) {
        $mid   = strtolower( (string) $item->get_method_id() );
        $title = strtolower( (string) $item->get_method_title() );
        if ( strpos( $mid, 'sz_motoboy' ) !== false || strpos( $title, 'motoboy' ) !== false ) {
            $has_motoboy = true;
            break;
        }
    }

    $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
    if ( ! $has_motoboy ) {
        $order->update_meta_data( '_senderzz_shipping_isolation_warning', 'Motoboy checkout sem método sz_motoboy no pedido; bloqueado no checkout_process.' );
    }
}, 1 );

add_action( 'wp_head', function() {
    if ( ! function_exists( 'is_checkout' ) || ! is_checkout() || ! sz_mb_current_checkout_is_motoboy() ) return;
    ?>
    <script id="sz-v98-motoboy-isolated-js">
    window.SZ_MOTOBOY_NO_EXPEDITION_FALLBACK = true;
    document.addEventListener('DOMContentLoaded', function(){
      function killExpeditionFallback(){
        document.querySelectorAll('.woocommerce-error li, .woocommerce-notice, .woocommerce-message, .woocommerce-info, .woocommerce-error').forEach(function(el){ var t=(el.textContent||'').toLowerCase(); if(t.indexOf('não existem métodos de entrega')>=0||t.indexOf('nao existem métodos de entrega')>=0||t.indexOf('nenhum método de entrega')>=0||t.indexOf('este link é exclusivo para motoboy')>=0){ el.style.display='none'; } });
        document.querySelectorAll('[data-sz-expedition-redirect], .senderzz-expedition-redirect').forEach(function(btn){
          btn.removeAttribute('data-sz-expedition-redirect');
          btn.classList.remove('senderzz-expedition-redirect');
          btn.onclick = null;
          if (/expedi/i.test((btn.textContent||''))) btn.textContent = 'Próxima Etapa';
        });
      }
      killExpeditionFallback();
      document.addEventListener('updated_checkout', killExpeditionFallback);
      if (window.MutationObserver) new MutationObserver(killExpeditionFallback).observe(document.body,{childList:true,subtree:true,attributes:true,attributeFilter:['class','data-sz-expedition-redirect']});
    });
    </script>
    <?php
}, 250 );


// V100: registra taxas Motoboy no pedido e na carteira, sem somar ao total pago pelo comprador.

// Blindagem server-side da agenda Motoboy: o cliente não consegue forçar data fora
// da regra da cidade alterando o input hidden do checkout.
add_action( 'woocommerce_checkout_process', function() {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return;

    $date = sanitize_text_field( wp_unslash( $_POST['sz_delivery_date'] ?? '' ) );
    if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date ) ) {
        wc_add_notice( 'Selecione uma data de entrega válida para o Motoboy Senderzz.', 'error' );
        return;
    }

    $cep = function_exists( 'sz_mb_current_checkout_postcode' ) ? sz_mb_current_checkout_postcode() : '';
    if ( strlen( $cep ) !== 8 ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }

    if ( strlen( $cep ) === 8 && function_exists( 'sz_mb_cep_exists' ) && sz_mb_cep_exists( $cep ) !== true ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }
    if ( strlen( $cep ) === 8 && function_exists( 'sz_motoboy_resolver_zona' ) && ! sz_motoboy_resolver_zona( $cep ) ) {
        wc_add_notice( 'CEP fora da área de entrega do Motoboy Senderzz. Informe um CEP atendido para continuar.', 'error' );
        return;
    }

    if ( strlen( $cep ) === 8 && function_exists( 'sz_motoboy_resolver_zona' ) && function_exists( 'sz_motoboy_zone_date_is_allowed' ) ) {
        $zona = sz_motoboy_resolver_zona( $cep );
        if ( $zona && ! sz_motoboy_zone_date_is_allowed( $date, $zona['dias_funcionamento'] ?? '', $zona['cutoff_horarios'] ?? '' ) ) {
            wc_add_notice( 'Essa data não está disponível para o CEP informado. Escolha uma das datas mostradas pela Senderzz para sua região.', 'error' );
            return;
        }
    }

    $city = '';
    if ( isset( $_POST['shipping_city'] ) ) $city = sanitize_text_field( wp_unslash( $_POST['shipping_city'] ) );
    if ( ! $city && isset( $_POST['billing_city'] ) ) $city = sanitize_text_field( wp_unslash( $_POST['billing_city'] ) );

    if ( ! $cep && $city && function_exists( 'sz_dr_date_allowed_for_city' ) && ! sz_dr_date_allowed_for_city( $city, $date ) ) {
        $region = function_exists( 'sz_dr_region_label_for_city' ) ? sz_dr_region_label_for_city( $city ) : '';
        $msg = $region
            ? sprintf( 'A cidade %s pertence à rota %s. Escolha uma das datas permitidas exibidas no checkout.', $city, $region )
            : sprintf( 'A data selecionada não é permitida para a cidade %s.', $city );
        wc_add_notice( $msg, 'error' );
    }
}, 2 );

add_action( 'woocommerce_checkout_create_order', function( WC_Order $order ) { sz_mb_store_order_fees( $order ); }, 999, 1 );
add_action( 'woocommerce_checkout_order_processed', function( $order_id ) { $order = wc_get_order( $order_id ); if ( $order instanceof WC_Order ) { sz_mb_store_order_fees( $order ); $order->save(); } }, 999 );
add_filter( 'senderzz_wallet_required_value', function( $required, WC_Order $order, array $snap ) {
    $is_motoboy = (string) $order->get_meta( '_senderzz_delivery_mode', true ) === 'motoboy';
    if ( ! $is_motoboy ) {
        foreach ( $order->get_items( 'shipping' ) as $item ) {
            if ( stripos( (string) $item->get_method_id(), 'sz_motoboy' ) !== false || stripos( (string) $item->get_name(), 'motoboy' ) !== false ) { $is_motoboy = true; break; }
        }
    }
    if ( ! $is_motoboy ) return $required;
    $total = (float) $order->get_meta( '_sz_mb_taxa_total', true );
    if ( $total <= 0 ) {
        $total = (float) $order->get_meta( '_sz_mb_taxa_entrega', true ) + (float) $order->get_meta( '_sz_mb_taxa_manuseio', true ) + (float) $order->get_meta( '_sz_mb_taxa_adicional', true );
    }
    return $total > 0 ? round( $total, 2 ) : $required;
}, 20, 3 );

// ── Remove obrigatoriedade do CPF no checkout motoboy ────────────────────────
// O checkout "sem cpf" não exibe o campo mas o plugin wcbcf ainda valida.
// Quando é motoboy, desativamos a validação e o campo.
add_filter( 'woocommerce_checkout_fields', function( array $fields ): array {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return $fields;
    // Remove billing_cpf e billing_cnpj do checkout motoboy
    foreach ( [ 'billing_cpf', 'billing_cnpj', 'billing_persontype' ] as $field ) {
        if ( isset( $fields['billing'][ $field ] ) ) {
            $fields['billing'][ $field ]['required'] = false;
            $fields['billing'][ $field ]['class'][]  = 'hidden';
            $fields['billing'][ $field ]['type']     = 'hidden';
        }
    }
    return $fields;
}, 9999 );

// FunnelKit usa wfacp_checkout_fields
add_filter( 'wfacp_checkout_fields', function( array $fields ): array {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return $fields;
    foreach ( [ 'billing_cpf', 'billing_cnpj', 'billing_persontype' ] as $field ) {
        if ( isset( $fields['billing'][ $field ] ) ) {
            $fields['billing'][ $field ]['required'] = false;
        }
        // Remove completamente do array do FunnelKit
        unset( $fields['billing'][ $field ] );
    }
    return $fields;
}, 9999 );

// Remove validação do CPF/CNPJ pelo wcbcf no checkout motoboy
// woocommerce_after_checkout_validation — consolidado no hook de prioridade 99999 abaixo

// Garante que billing_cpf não é required na validação interna do WC
add_filter( 'woocommerce_billing_fields', function( array $fields ): array {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return $fields;
    if ( isset( $fields['billing_cpf'] ) ) {
        $fields['billing_cpf']['required'] = false;
    }
    if ( isset( $fields['billing_cnpj'] ) ) {
        $fields['billing_cnpj']['required'] = false;
    }
    return $fields;
}, 9999 );

// ── Suprime validação de CPF do wcbcf no checkout motoboy ────────────────────
// O plugin wcbcf adiciona erro em woocommerce_checkout_process priority 10.
// Estratégia: injeta CPF válido no $_POST antes da validação do wcbcf rodar,
// e remove o campo required para que nenhum erro seja adicionado.
add_action( 'woocommerce_checkout_process', function() {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return;

    // Injeta CPF fake válido se não veio nenhum (evita erro de campo obrigatório)
    if ( empty( $_POST['billing_cpf'] ) ) {
        $_POST['billing_cpf'] = '000.000.000-00'; // não válido mas preenche o campo
    }

    // Remove required do campo CPF dinamicamente
    add_filter( 'woocommerce_checkout_fields', function( $fields ) {
        if ( isset( $fields['billing']['billing_cpf'] ) ) {
            $fields['billing']['billing_cpf']['required'] = false;
        }
        return $fields;
    }, 99999 );

    // ── Validação: telefone com pedido ativo na mesma classe de entrega ────
    $telefone = preg_replace( '/\D/', '', sanitize_text_field( wp_unslash( $_POST['billing_phone'] ?? '' ) ) );
    if ( strlen($telefone) >= 8 ) {
        global $wpdb;
        $mb_table = $wpdb->prefix . 'sz_motoboy_pedidos';
        $status_ativos = [ 'agendado', 'aprovado', 'embalado', 'em_rota', 'a_caminho', 'reagendado' ];
        $placeholders  = implode( ',', array_fill( 0, count($status_ativos), '%s' ) );

        // Busca por sufixo de 8+ dígitos para cobrir (11)99xxx-xxxx vs 11999xxx
        $like = '%' . $wpdb->esc_like( substr( $telefone, -8 ) );
        $query = $wpdb->prepare(
            "SELECT mp.wc_order_id FROM {$mb_table} mp
             WHERE mp.dest_telefone LIKE %s
               AND mp.status IN ({$placeholders})
             ORDER BY mp.id DESC LIMIT 1",
            array_merge( [ $like ], $status_ativos )
        );
        $existing_order_id = (int) $wpdb->get_var( $query );

        if ( $existing_order_id > 0 ) {
            // Segurança v278: a tabela do motoboy pode manter linhas órfãs com wc_order_id
            // de pedidos que não existem mais no WooCommerce/HPOS. Linha órfã não pode
            // bloquear checkout novo.
            $existing_order = wc_get_order( $existing_order_id );
            if ( ! $existing_order ) {
                // Marca o registro logístico como cancelado para não voltar a bloquear.
                $wpdb->update(
                    $mb_table,
                    [ 'status' => 'cancelado' ],
                    [ 'wc_order_id' => $existing_order_id ],
                    [ '%s' ],
                    [ '%d' ]
                );
                return;
            }

            $wc_status = $existing_order->get_status();
            $wc_status = is_string( $wc_status ) ? preg_replace( '/^wc-/', '', $wc_status ) : '';
            $wc_statuses_ativos = [ 'processing', 'on-hold', 'pending', 'agendado', 'aprovado', 'embalado', 'em_rota', 'a_caminho', 'reagendado' ];
            if ( $wc_status && ! in_array( $wc_status, $wc_statuses_ativos, true ) ) {
                $wpdb->update(
                    $mb_table,
                    [ 'status' => 'cancelado' ],
                    [ 'wc_order_id' => $existing_order_id ],
                    [ '%s' ],
                    [ '%d' ]
                );
                return;
            }

            // Verificar se é a mesma classe de entrega do carrinho atual
            $cart_class = '';
            if ( WC()->cart ) {
                foreach ( WC()->cart->get_cart() as $item ) {
                    $product = $item['data'] ?? null;
                    if ( $product ) {
                        $cart_class = $product->get_shipping_class();
                        break;
                    }
                }
            }
            $existing_class = '';
            foreach ( $existing_order->get_items() as $item ) {
                $product = $item->get_product();
                if ( $product ) { $existing_class = $product->get_shipping_class(); break; }
            }
            // Bloquear somente quando o pedido existe, está ativo e é da mesma classe.
            // Se não identificar classe, não bloqueia por segurança para não travar checkout.
            if ( $cart_class !== '' && $existing_class !== '' && $cart_class === $existing_class ) {
                wc_add_notice(
                    sprintf(
                        '⚠️ O telefone %s já possui um pedido ativo (#%d) aguardando entrega. Não é possível criar um novo pedido com o mesmo telefone para a mesma classe de entrega. Aguarde a conclusão do pedido anterior.',
                        preg_replace( '/(\d{2})(\d{4,5})(\d{4})/', '($1) $2-$3', $telefone ),
                        $existing_order_id
                    ),
                    'error'
                );
            }
        }
    }

}, 1 ); // priority 1 — antes do wcbcf (priority 10)

// Remove erros de CPF/CNPJ adicionados pelo wcbcf após o checkout_process
add_action( 'woocommerce_after_checkout_validation', function( $data, $errors ) {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return;
    if ( ! $errors instanceof WP_Error ) return;

    foreach ( $errors->get_error_codes() as $code ) {
        foreach ( $errors->get_error_messages( $code ) as $msg ) {
            if ( stripos( $msg, 'CPF' ) !== false
              || stripos( $msg, 'CNPJ' ) !== false
              || stripos( $msg, 'billing_cpf' ) !== false ) {
                $errors->remove( $code );
            }
        }
    }
}, 99999, 2 );

// Filtra notices de erro do WC para remover CPF no motoboy
add_filter( 'woocommerce_add_error', function( $error ) {
    if ( ! sz_mb_current_checkout_is_motoboy() ) return $error;
    if ( stripos( $error, 'CPF' ) !== false || stripos( $error, 'CNPJ' ) !== false ) {
        return ''; // suprime completamente
    }
    return $error;
}, 99999 );

// Senderzz v397 — alinhamento final da barra da oferta na etapa 2.
// Mantém as três colunas seguindo o alinhamento visual de Parcelamento, sem cortar valores até R$ 999,99.
add_action( 'wp_head', function() {
    if ( is_admin() ) return;
    if ( ! function_exists( 'is_checkout' ) || ! is_checkout() ) return;
    if ( ! function_exists( 'sz_mb_current_checkout_is_motoboy' ) || ! sz_mb_current_checkout_is_motoboy() ) return;
    ?>
    <style id="sz-mb-offer-strip-align-v397">
        .sz-mb-date-checkout #sz-oferta-bar.sz-mb-offer-strip{
            display:grid!important;
            grid-template-columns:minmax(225px,1.08fr) minmax(174px,.70fr) minmax(286px,1fr)!important;
            align-items:stretch!important;
            column-gap:18px!important;
            row-gap:0!important;
            width:100%!important;
            max-width:100%!important;
            min-height:76px!important;
            margin:0 0 18px!important;
            padding:10px 14px!important;
            border:0!important;
            border-radius:18px!important;
            background:linear-gradient(135deg,#E8650A 0%,#F97316 52%,#C8540A 100%)!important;
            color:#fff!important;
            box-shadow:none!important;
            overflow:hidden!important;
            box-sizing:border-box!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-main,
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-price,
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments{
            min-width:0!important;
            height:100%!important;
            box-sizing:border-box!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-main{
            display:grid!important;
            grid-template-columns:58px minmax(0,1fr)!important;
            align-items:center!important;
            gap:13px!important;
            padding:0!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-price,
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments{
            display:flex!important;
            flex-direction:column!important;
            justify-content:center!important;
            align-items:stretch!important;
            text-align:left!important;
            padding:0 0 0 18px!important;
            border-left:1px solid rgba(255,255,255,.34)!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-copy{
            display:flex!important;
            flex-direction:column!important;
            justify-content:center!important;
            align-items:flex-start!important;
            min-width:0!important;
            height:100%!important;
            text-align:left!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-thumb{
            width:58px!important;
            height:58px!important;
            flex:0 0 58px!important;
            border-radius:15px!important;
            overflow:hidden!important;
            align-self:center!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-thumb img{
            width:100%!important;
            height:100%!important;
            object-fit:cover!important;
            display:block!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar small{
            display:block!important;
            height:14px!important;
            line-height:14px;
            margin:0 0 6px!important;
            padding:0!important;
            color:#fff!important;
            opacity:1!important;
            font-size:12px;
            font-weight:700;
            letter-spacing:0;
            text-transform:none;
            text-align:left!important;
            white-space:nowrap!important;
            overflow:visible!important;
            text-overflow:clip!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-name{
            display:block!important;
            max-width:100%!important;
            color:#fff!important;
            font-size:23px;
            font-weight:700;
            line-height:1.02;
            text-align:left!important;
            white-space:normal!important;
            overflow:hidden!important;
            text-overflow:clip!important;
            display:-webkit-box!important;
            -webkit-line-clamp:2!important;
            -webkit-box-orient:vertical!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-value{
            display:block!important;
            width:100%!important;
            min-width:0!important;
            max-width:none!important;
            margin:0!important;
            padding:0!important;
            background:transparent!important;
            color:#fff!important;
            font-size:30px;
            font-weight:700;
            line-height:1;
            letter-spacing:-.015em;
            text-align:left!important;
            white-space:nowrap!important;
            overflow:visible!important;
            text-overflow:clip!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments .sz-mb-select-shell{
            display:block!important;
            width:100%!important;
            margin:0!important;
            min-width:0!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments .sz-mb-installment-select{
            display:block!important;
            width:100%!important;
            min-width:0!important;
            max-width:100%!important;
            height:48px!important;
            min-height:48px!important;
            margin:0!important;
            border:0!important;
            border-radius:13px!important;
            background:#fff!important;
            color:#111827!important;
            padding:0 42px 0 14px!important;
            font-size:13px;
            font-weight:700;
            line-height:48px;
            white-space:nowrap!important;
            overflow:hidden!important;
            text-overflow:ellipsis!important;
            box-sizing:border-box!important;
        }
        .sz-mb-date-checkout #sz-oferta-bar .sz-mb-select-arrow{
            right:15px!important;
            color:#111827!important;
        }
        @media (max-width: 980px){
            .sz-mb-date-checkout #sz-oferta-bar.sz-mb-offer-strip{
                grid-template-columns:1fr!important;
                gap:12px!important;
                padding:14px!important;
            }
            .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-price,
            .sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-strip-installments{
                padding-left:0!important;
                border-left:0!important;
            }
        }
    </style>
    <script id="sz-mb-offer-strip-align-fit-v397">
    (function(){
        function fitText(el, min, max){
            if(!el) return;
            el.style.fontSize = max + 'px';
            var size = max;
            var guard = 0;
            while(size > min && guard < 30 && (el.scrollWidth > el.clientWidth || el.scrollHeight > el.clientHeight + 2)){
                size -= 1;
                el.style.fontSize = size + 'px';
                guard++;
            }
        }
        function run(){
            document.querySelectorAll('.sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-name').forEach(function(el){ fitText(el, 15, 23); });
            document.querySelectorAll('.sz-mb-date-checkout #sz-oferta-bar .sz-mb-offer-value').forEach(function(el){ fitText(el, 25, 30); });
        }
        if(document.readyState === 'loading') document.addEventListener('DOMContentLoaded', run, {once:true}); else run();
        window.addEventListener('resize', function(){ clearTimeout(window.__szMbOfferAlignFit); window.__szMbOfferAlignFit = setTimeout(run, 80); });
        document.addEventListener('change', function(e){ if(e.target && e.target.matches('.sz-mb-installment-select')) setTimeout(run, 30); }, true);
    })();
    </script>
    <?php
}, 1000 );
