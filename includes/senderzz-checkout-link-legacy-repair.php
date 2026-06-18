<?php
/**
 * Senderzz — reparo/migração de links legados de checkout.
 *
 * Objetivo: atualização do plugin nunca pode exigir recriação de links públicos.
 * Links antigos sem payload/token/tipo correto são normalizados mantendo o mesmo ?sz=.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'senderzz_checkout_legacy_table_name' ) ) {
    function senderzz_checkout_legacy_table_name(): string {
        global $wpdb;
        return $wpdb->prefix . 'senderzz_checkout_links';
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_table_exists' ) ) {
    function senderzz_checkout_legacy_table_exists(): bool {
        global $wpdb;
        $table = senderzz_checkout_legacy_table_name();
        return $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) === $table;
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_parse_money' ) ) {
    function senderzz_checkout_legacy_parse_money( $value ): float {
        $raw = trim( wp_strip_all_tags( (string) $value ) );
        if ( $raw === '' ) return 0.0;
        $raw = preg_replace( '/[^0-9,\.\-]/', '', $raw );
        if ( $raw === '' || $raw === '-' ) return 0.0;
        // pt-BR: 1.234,56 -> 1234.56
        if ( strpos( $raw, ',' ) !== false ) {
            $raw = str_replace( '.', '', $raw );
            $raw = str_replace( ',', '.', $raw );
        }
        return max( 0.0, (float) $raw );
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_token_from_url' ) ) {
    function senderzz_checkout_legacy_token_from_url( string $url ): string {
        if ( $url === '' ) return '';
        $query = wp_parse_url( $url, PHP_URL_QUERY );
        if ( ! $query ) return '';
        parse_str( $query, $args );
        return ! empty( $args['sz'] ) ? sanitize_text_field( (string) $args['sz'] ) : '';
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_infer_type' ) ) {
    function senderzz_checkout_legacy_infer_type( array $link ): string {
        $tipo = strtolower( trim( (string) ( $link['tipo'] ?? '' ) ) );
        $url  = strtolower( (string) ( $link['url'] ?? '' ) );
        $path = strtolower( (string) wp_parse_url( $url, PHP_URL_PATH ) );
        $query = strtolower( (string) wp_parse_url( $url, PHP_URL_QUERY ) );
        $name = strtolower( remove_accents( (string) ( $link['name'] ?? '' ) ) );

        $looks_motoboy = false;
        if ( strpos( $path, '/checkouts/cod' ) !== false || strpos( $path, '/codsfpc' ) !== false ) $looks_motoboy = true;
        if ( strpos( $query, 'szm=1' ) !== false || strpos( $query, 'frete=motoboy' ) !== false || strpos( $query, 'm=1' ) !== false ) $looks_motoboy = true;
        if ( strpos( $name, 'motoboy' ) !== false || strpos( $name, 'cod' ) !== false ) $looks_motoboy = true;

        // Requisição atual também serve como fallback para registros antigos com URL vazia/incorreta.
        if ( ! is_admin() || wp_doing_ajax() ) {
            $req_uri = strtolower( (string) ( $_SERVER['REQUEST_URI'] ?? '' ) );
            if ( strpos( $req_uri, '/checkouts/cod' ) !== false || strpos( $req_uri, 'szm=1' ) !== false || strpos( $req_uri, 'frete=motoboy' ) !== false ) {
                $looks_motoboy = true;
            }
        }

        if ( $looks_motoboy ) return 'motoboy';
        if ( $tipo === 'motoboy' ) return 'motoboy';
        return 'correio';
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_is_no_cpf' ) ) {
    function senderzz_checkout_legacy_is_no_cpf( array $link ): bool {
        $url  = strtolower( (string) ( $link['url'] ?? '' ) );
        $path = strtolower( (string) wp_parse_url( $url, PHP_URL_PATH ) );
        $req  = strtolower( (string) ( $_SERVER['REQUEST_URI'] ?? '' ) );
        return strpos( $path, 'codsfpc' ) !== false || strpos( $req, 'codsfpc' ) !== false;
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_default_post_id' ) ) {
    function senderzz_checkout_legacy_default_post_id( string $tipo, bool $no_cpf = false ): int {
        if ( $tipo === 'motoboy' ) {
            if ( $no_cpf ) return (int) get_option( 'senderzz_checkout_template_id_sem_cpf', 1075 );
            return (int) get_option( 'senderzz_checkout_template_id', 140 );
        }
        return (int) get_option( 'senderzz_checkout_template_id', 140 );
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_default_url' ) ) {
    function senderzz_checkout_legacy_default_url( string $tipo, string $token, bool $no_cpf = false ): string {
        if ( $tipo === 'motoboy' ) {
            $post_id = senderzz_checkout_legacy_default_post_id( $tipo, $no_cpf );
            $base = $post_id ? get_permalink( $post_id ) : '';
            if ( ! $base ) $base = home_url( $no_cpf ? '/checkouts/codsfpc/' : '/checkouts/cod/' );
            return add_query_arg( [ 'sz' => $token, 'szm' => '1' ], $base );
        }
        return add_query_arg( 'sz', $token, home_url( '/checkouts/lp/' ) );
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_payload_valid' ) ) {
    function senderzz_checkout_legacy_payload_valid( $payload ): bool {
        return is_array( $payload ) && ! empty( $payload['components'] ) && is_array( $payload['components'] );
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_normalize_component' ) ) {
    function senderzz_checkout_legacy_normalize_component( $raw ): ?array {
        if ( ! is_array( $raw ) ) return null;
        $pid = absint( $raw['pid'] ?? $raw['product_id'] ?? $raw['parent_product_id'] ?? 0 );
        $vid = absint( $raw['vid'] ?? $raw['variation_id'] ?? 0 );
        $id  = absint( $raw['id'] ?? 0 );

        if ( ! $pid && $id ) {
            $product = function_exists( 'wc_get_product' ) ? wc_get_product( $id ) : null;
            if ( $product && method_exists( $product, 'is_type' ) && $product->is_type( 'variation' ) ) {
                $vid = $id;
                $pid = method_exists( $product, 'get_parent_id' ) ? absint( $product->get_parent_id() ) : 0;
            } else {
                $pid = $id;
            }
        }
        if ( $vid && ! $pid && function_exists( 'wc_get_product' ) ) {
            $variation = wc_get_product( $vid );
            if ( $variation && method_exists( $variation, 'get_parent_id' ) ) $pid = absint( $variation->get_parent_id() );
        }
        $qty = max( 1, absint( $raw['qty'] ?? $raw['quantity'] ?? 1 ) );
        if ( ! $pid ) return null;
        return [ 'pid' => $pid, 'vid' => $vid, 'qty' => $qty ];
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_components_from_wfacp' ) ) {
    function senderzz_checkout_legacy_components_from_wfacp( int $post_id ): array {
        if ( ! $post_id ) return [];
        $sources = [];
        $selected = get_post_meta( $post_id, '_wfacp_selected_products', true );
        if ( is_array( $selected ) ) $sources[] = $selected;
        $switcher = get_post_meta( $post_id, '_wfacp_product_switcher_setting', true );
        if ( is_array( $switcher ) && ! empty( $switcher['products'] ) && is_array( $switcher['products'] ) ) $sources[] = $switcher['products'];

        $components = [];
        foreach ( $sources as $source ) {
            foreach ( $source as $raw ) {
                $comp = senderzz_checkout_legacy_normalize_component( $raw );
                if ( $comp ) $components[] = $comp;
            }
            if ( ! empty( $components ) ) break;
        }
        return $components;
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_payload_from_related' ) ) {
    function senderzz_checkout_legacy_payload_from_related( array $link ): array {
        global $wpdb;
        if ( ! senderzz_checkout_legacy_table_exists() ) return [];
        $table = senderzz_checkout_legacy_table_name();
        $id = absint( $link['id'] ?? 0 );
        $candidates = [];

        if ( $id ) {
            // Motoboy filho antigo: payload pode estar no link principal que aponta para ele.
            $parent = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM `{$table}` WHERE link_motoboy_id=%d AND payload IS NOT NULL AND payload<>'' LIMIT 1", $id ), ARRAY_A );
            if ( $parent ) $candidates[] = $parent;
        }
        $linked_id = absint( $link['link_motoboy_id'] ?? 0 );
        if ( $linked_id ) {
            $child = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM `{$table}` WHERE id=%d AND payload IS NOT NULL AND payload<>'' LIMIT 1", $linked_id ), ARRAY_A );
            if ( $child ) $candidates[] = $child;
        }

        foreach ( $candidates as $row ) {
            $payload = json_decode( (string) ( $row['payload'] ?? '' ), true );
            if ( senderzz_checkout_legacy_payload_valid( $payload ) ) return $payload;
        }
        return [];
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_components_value' ) ) {
    function senderzz_checkout_legacy_components_value( array $components ): float {
        if ( ! function_exists( 'wc_get_product' ) ) return 0.0;
        $total = 0.0;
        foreach ( $components as $comp ) {
            $pid = absint( $comp['pid'] ?? 0 );
            $vid = absint( $comp['vid'] ?? 0 );
            $qty = max( 1, absint( $comp['qty'] ?? 1 ) );
            $product = wc_get_product( $vid ?: $pid );
            if ( $product ) $total += (float) $product->get_price() * $qty;
        }
        return max( 0.0, $total );
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_build_payload_for_link' ) ) {
    function senderzz_checkout_legacy_build_payload_for_link( array $link ): array {
        $existing = ! empty( $link['payload'] ) ? json_decode( (string) $link['payload'], true ) : [];
        if ( senderzz_checkout_legacy_payload_valid( $existing ) ) {
            return $existing;
        }

        $related = senderzz_checkout_legacy_payload_from_related( $link );
        if ( senderzz_checkout_legacy_payload_valid( $related ) ) {
            $payload = $related;
            $payload['source'] = $payload['source'] ?? 'legacy_related_payload_v374';
        } else {
            $components = senderzz_checkout_legacy_components_from_wfacp( absint( $link['post_id'] ?? 0 ) );
            if ( empty( $components ) ) return [];
            $payload = [ 'components' => $components, 'source' => 'legacy_wfacp_postmeta_v374' ];
        }

        $valor = (float) ( $payload['valor'] ?? 0 );
        if ( $valor <= 0 && isset( $link['display_value'] ) ) $valor = (float) $link['display_value'];
        if ( $valor <= 0 && ! empty( $link['price_label'] ) ) $valor = senderzz_checkout_legacy_parse_money( $link['price_label'] );
        if ( $valor <= 0 ) $valor = senderzz_checkout_legacy_components_value( (array) ( $payload['components'] ?? [] ) );
        $payload['valor'] = max( 0.0, $valor );

        return senderzz_checkout_legacy_payload_valid( $payload ) ? $payload : [];
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_ensure_columns' ) ) {
    function senderzz_checkout_legacy_ensure_columns(): array {
        global $wpdb;
        if ( function_exists( 'senderzz_portal_ensure_checkout_links_table' ) ) senderzz_portal_ensure_checkout_links_table();
        if ( ! senderzz_checkout_legacy_table_exists() ) return [];
        $table = senderzz_checkout_legacy_table_name();
        $cols = $wpdb->get_col( "SHOW COLUMNS FROM `{$table}`", 0 );
        $cols = is_array( $cols ) ? $cols : [];
        $defs = [
            'token'          => "VARCHAR(255) NOT NULL DEFAULT ''",
            'tipo'           => "VARCHAR(20) NOT NULL DEFAULT 'correio'",
            'payload'        => "LONGTEXT NULL",
            'display_value'  => "DECIMAL(12,2) NOT NULL DEFAULT 0.00",
            'updated_at'     => "DATETIME NULL",
            'schema_version' => "VARCHAR(32) NOT NULL DEFAULT ''",
        ];
        foreach ( $defs as $col => $def ) {
            if ( ! in_array( $col, $cols, true ) ) {
                $wpdb->query( "ALTER TABLE `{$table}` ADD `{$col}` {$def}" );
                $cols[] = $col;
            }
        }
        return $cols;
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_repair_row' ) ) {
    function senderzz_checkout_legacy_repair_row( array $link, bool $force = false ): array {
        global $wpdb;
        if ( empty( $link['id'] ) || ! senderzz_checkout_legacy_table_exists() ) return $link;
        $table = senderzz_checkout_legacy_table_name();
        $cols = senderzz_checkout_legacy_ensure_columns();
        if ( empty( $cols ) ) return $link;

        $updates = [];
        $formats = [];
        $add = static function( string $key, $value, string $format ) use ( &$updates, &$formats, $cols ): void {
            if ( in_array( $key, $cols, true ) ) {
                $updates[ $key ] = $value;
                $formats[] = $format;
            }
        };

        $token = sanitize_text_field( (string) ( $link['token'] ?? '' ) );
        if ( ! $token ) $token = sanitize_text_field( (string) ( $link['slug'] ?? '' ) );
        if ( ! $token && ! empty( $link['url'] ) ) $token = senderzz_checkout_legacy_token_from_url( (string) $link['url'] );
        if ( $token ) {
            if ( empty( $link['token'] ) ) $add( 'token', $token, '%s' );
            if ( empty( $link['slug'] ) ) $add( 'slug', $token, '%s' );
        }

        $tipo = senderzz_checkout_legacy_infer_type( $link );
        $current_tipo = strtolower( trim( (string) ( $link['tipo'] ?? '' ) ) );
        if ( $tipo === 'motoboy' && $current_tipo !== 'motoboy' ) {
            $add( 'tipo', 'motoboy', '%s' );
            $link['tipo'] = 'motoboy';
        } elseif ( ! $current_tipo ) {
            $add( 'tipo', $tipo, '%s' );
            $link['tipo'] = $tipo;
        }

        $no_cpf = senderzz_checkout_legacy_is_no_cpf( $link );
        if ( empty( $link['post_id'] ) ) {
            $post_id = senderzz_checkout_legacy_default_post_id( $tipo, $no_cpf );
            if ( $post_id ) $add( 'post_id', $post_id, '%d' );
        }
        if ( empty( $link['url'] ) && $token ) {
            $add( 'url', senderzz_checkout_legacy_default_url( $tipo, $token, $no_cpf ), '%s' );
        }

        $payload = senderzz_checkout_legacy_build_payload_for_link( $link );
        if ( senderzz_checkout_legacy_payload_valid( $payload ) ) {
            $existing_payload = ! empty( $link['payload'] ) ? json_decode( (string) $link['payload'], true ) : [];
            if ( $force || ! senderzz_checkout_legacy_payload_valid( $existing_payload ) ) {
                $add( 'payload', wp_json_encode( $payload, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ), '%s' );
            }
            $valor = (float) ( $payload['valor'] ?? 0 );
            if ( $valor > 0 ) {
                if ( empty( $link['display_value'] ) ) $add( 'display_value', $valor, '%f' );
                if ( empty( $link['price_label'] ) ) $add( 'price_label', 'R$ ' . number_format( $valor, 2, ',', '.' ), '%s' );
            }
        }

        $add( 'schema_version', 'v374', '%s' );
        $add( 'updated_at', current_time( 'mysql' ), '%s' );

        if ( ! empty( $updates ) ) {
            $wpdb->update( $table, $updates, [ 'id' => absint( $link['id'] ) ], $formats, [ '%d' ] );
            $fresh = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM `{$table}` WHERE id=%d LIMIT 1", absint( $link['id'] ) ), ARRAY_A );
            if ( is_array( $fresh ) ) return $fresh;
        }
        return $link;
    }
}

if ( ! function_exists( 'senderzz_checkout_legacy_repair_single_token' ) ) {
    function senderzz_checkout_legacy_repair_single_token( string $token, ?array $link = null ): ?array {
        global $wpdb;
        $token = sanitize_text_field( $token );
        if ( ! $token || ! senderzz_checkout_legacy_table_exists() ) return $link;
        $table = senderzz_checkout_legacy_table_name();
        if ( ! $link ) {
            $link = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM `{$table}` WHERE slug=%s OR token=%s LIMIT 1", $token, $token ), ARRAY_A );
        }
        if ( ! is_array( $link ) ) return null;
        return senderzz_checkout_legacy_repair_row( $link, false );
    }
}

if ( ! function_exists( 'senderzz_checkout_repair_legacy_links' ) ) {
    function senderzz_checkout_repair_legacy_links( int $limit = 1000, bool $force = false ): array {
        global $wpdb;
        $stats = [ 'checked' => 0, 'repaired' => 0, 'with_payload' => 0, 'without_payload' => 0 ];
        if ( ! senderzz_checkout_legacy_table_exists() ) return $stats;
        $cols = senderzz_checkout_legacy_ensure_columns();
        if ( empty( $cols ) ) return $stats;
        $table = senderzz_checkout_legacy_table_name();
        $limit = max( 1, min( 5000, absint( $limit ) ) );
        $rows = $wpdb->get_results( "SELECT * FROM `{$table}` ORDER BY id DESC LIMIT {$limit}", ARRAY_A ) ?: [];
        foreach ( $rows as $row ) {
            $stats['checked']++;
            $before = wp_json_encode( [
                'token' => $row['token'] ?? '', 'slug' => $row['slug'] ?? '', 'tipo' => $row['tipo'] ?? '',
                'payload' => ! empty( $row['payload'] ) ? md5( (string) $row['payload'] ) : '',
                'schema_version' => $row['schema_version'] ?? '',
            ] );
            $fresh = senderzz_checkout_legacy_repair_row( $row, $force );
            $after = wp_json_encode( [
                'token' => $fresh['token'] ?? '', 'slug' => $fresh['slug'] ?? '', 'tipo' => $fresh['tipo'] ?? '',
                'payload' => ! empty( $fresh['payload'] ) ? md5( (string) $fresh['payload'] ) : '',
                'schema_version' => $fresh['schema_version'] ?? '',
            ] );
            if ( $before !== $after ) $stats['repaired']++;
            $payload = ! empty( $fresh['payload'] ) ? json_decode( (string) $fresh['payload'], true ) : [];
            if ( senderzz_checkout_legacy_payload_valid( $payload ) ) $stats['with_payload']++; else $stats['without_payload']++;
        }
        update_option( 'senderzz_checkout_legacy_repair_v374_last_stats', $stats, false );
        return $stats;
    }
}

// Migração automática: roda no admin após atualização. O fallback no checkout também repara sob demanda por token.
add_action( 'admin_init', function(): void {
    if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) return;
    if ( get_option( 'senderzz_checkout_legacy_repair_v374_done' ) ) return;
    $stats = senderzz_checkout_repair_legacy_links( 1500, false );
    update_option( 'senderzz_checkout_legacy_repair_v374_done', time(), false );
    if ( defined( 'WP_DEBUG' ) && WP_DEBUG ) {
        error_log( '[Senderzz] Reparo legado checkout v374 executado: ' . wp_json_encode( $stats ) );
    }
}, 30 );

add_action( 'admin_post_senderzz_repair_checkout_links', function(): void {
    if ( ! current_user_can( 'manage_options' ) && ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );
    check_admin_referer( 'senderzz_repair_checkout_links' );
    delete_option( 'senderzz_checkout_legacy_repair_v374_done' );
    $stats = senderzz_checkout_repair_legacy_links( 5000, true );
    update_option( 'senderzz_checkout_legacy_repair_v374_done', time(), false );
    $msg = sprintf( 'Links verificados: %d | Reparados: %d | Com payload: %d | Sem payload: %d', (int) $stats['checked'], (int) $stats['repaired'], (int) $stats['with_payload'], (int) $stats['without_payload'] );
    wp_safe_redirect( add_query_arg( [
        'page' => 'senderzz',
        'area' => 'config',
        'tab'  => 'cfg-saude',
        'sz_msg' => rawurlencode( $msg ),
    ], admin_url( 'admin.php' ) ) );
    exit;
} );

if ( ! function_exists( 'senderzz_checkout_legacy_repair_button_html' ) ) {
    function senderzz_checkout_legacy_repair_button_html(): string {
        $url = wp_nonce_url( admin_url( 'admin-post.php?action=senderzz_repair_checkout_links' ), 'senderzz_repair_checkout_links' );
        $stats = get_option( 'senderzz_checkout_legacy_repair_v374_last_stats', [] );
        $detail = '';
        if ( is_array( $stats ) && isset( $stats['checked'] ) ) {
            $detail = ' <span class="sz-muted">Última verificação: ' . esc_html( (int) $stats['checked'] . ' links, ' . (int) ( $stats['repaired'] ?? 0 ) . ' reparados.' ) . '</span>';
        }
        return '<a class="button button-primary" href="' . esc_url( $url ) . '">Reparar links antigos de checkout</a>' . $detail;
    }
}
