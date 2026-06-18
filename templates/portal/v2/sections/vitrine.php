<?php
/**
 * Senderzz V2 – Vitrine de produtos
 * Exibe produtos disponíveis para afiliação com stats de vendas.
 * Visível para produtores e afiliados.
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

global $wpdb;

$sz9vt_is_aff      = ( 'affiliate' === strtolower( trim( (string) ( $sz_v2_user->role ?? 'client' ) ) ) );
$sz9vt_nonce       = wp_create_nonce( 'sz_aff_panel' );
$sz9vt_ajax        = admin_url( 'admin-ajax.php' );
$sz9vt_wp_uid      = function_exists( 'sz_aff_portal_user_wp_id' )
    ? (int) sz_aff_portal_user_wp_id( $sz_v2_user )
    : (int) ( $sz_v2_user->wp_user_id ?? 0 );

$sz9vt_money = static fn( float $v ): string =>
    function_exists( 'senderzz_portal_money' )
        ? senderzz_portal_money( $v )
        : 'R$ ' . number_format( $v, 2, ',', '.' );

// ── Afiliações já existentes do usuário atual (como afiliado) ─────────────
$sz9vt_aff_table     = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliates' ) : $wpdb->prefix . 'sz_affiliates';
$sz9vt_my_affiliates = [];
if ( $sz9vt_wp_uid ) {
    $rows = (array) $wpdb->get_results( $wpdb->prepare(
        "SELECT producer_id, status FROM {$sz9vt_aff_table} WHERE user_id = %d AND deleted_at IS NULL",
        $sz9vt_wp_uid
    ), ARRAY_A );
    foreach ( $rows as $r ) {
        $sz9vt_my_affiliates[ (int) $r['producer_id'] ] = (string) $r['status'];
    }
}

// ── Produtos WC com shipping class ────────────────────────────────────────
$sz9vt_products = [];
if ( function_exists( 'wc_get_products' ) ) {
    $sz9vt_all = wc_get_products( [
        'limit'   => -1,
        'status'  => 'publish',
        'type'    => [ 'simple', 'variation', 'variable' ],
        'orderby' => 'title',
        'order'   => 'ASC',
        'return'  => 'objects',
    ] );
    foreach ( (array) $sz9vt_all as $sz9vt_p ) {
        if ( ! $sz9vt_p instanceof WC_Product ) continue;
        $cid = (int) $sz9vt_p->get_shipping_class_id();
        if ( $sz9vt_p->is_type( 'variation' ) ) {
            $par = wc_get_product( $sz9vt_p->get_parent_id() );
            if ( $par && ! (int) $cid ) $cid = (int) $par->get_shipping_class_id();
        }
        $pid  = (int) $sz9vt_p->get_id();
        $name = $sz9vt_p->get_name();
        $hay  = strtolower( remove_accents( $name ) );
        // Exclui produtos internos/frete e kits compostos
        if ( preg_match( '/\b(kit|combo|pacote|pack|bundle|conjunto)\b/i', $hay ) ) continue;
        if ( preg_match( '/recarga|carteira.?frete|frete.?interno|wallet.?reload/i', $hay ) ) continue;
        if ( isset( $sz9vt_products[ $pid ] ) ) continue;
        // Prioriza descrição customizada da vitrine; fallback para descrição WC
        $vt_desc_custom = trim( (string) get_post_meta( $pid, '_sz_vitrine_description', true ) );
        $vt_desc_wc     = wp_trim_words( wp_strip_all_tags( $sz9vt_p->get_description() ?: $sz9vt_p->get_short_description() ), 40, '…' );
        $sz9vt_products[ $pid ] = [
            'id'               => $pid,
            'name'             => $name,
            'description'      => $vt_desc_custom ?: $vt_desc_wc,
            'description_raw'  => $vt_desc_custom, // para edição no modal
            'image'            => wp_get_attachment_image_url( (int) $sz9vt_p->get_image_id(), 'medium' ) ?: '',
            'class_id'         => $cid,
            'qty_sold'         => 0,
            'revenue'          => 0.0,
            'comm_paid'        => 0.0,
            'producer_id'      => 0,
        ];
    }
}

// ── Produtor de cada produto via portal_users + shipping_class ────────────
$sz9vt_class_to_producer = [];
$sz9vt_pu_rows = (array) $wpdb->get_results(
    "SELECT id, wp_user_id, shipping_class_id FROM {$wpdb->prefix}senderzz_portal_users WHERE status='active' AND shipping_class_id > 0 LIMIT 500",
    ARRAY_A
);
foreach ( $sz9vt_pu_rows as $pu ) {
    $sz9vt_class_to_producer[ (int) $pu['shipping_class_id'] ] = (int) $pu['wp_user_id'];
}

// Multi-class: sz_user_class_ids
if ( $wpdb->get_var( "SHOW TABLES LIKE '{$wpdb->prefix}sz_user_class_ids'" ) ) {
    $mc = (array) $wpdb->get_results( "SELECT wp_user_id, class_id FROM {$wpdb->prefix}sz_user_class_ids LIMIT 1000", ARRAY_A );
    foreach ( $mc as $r ) {
        if ( ! isset( $sz9vt_class_to_producer[ (int) $r['class_id'] ] ) ) {
            $sz9vt_class_to_producer[ (int) $r['class_id'] ] = (int) $r['wp_user_id'];
        }
    }
}

foreach ( $sz9vt_products as $pid => &$prod ) {
    $prod['producer_id'] = $sz9vt_class_to_producer[ $prod['class_id'] ] ?? 0;
}
unset( $prod );

// ── Total vendido por produto (wc_order_items) ────────────────────────────
$sz9vt_oi = $wpdb->prefix . 'woocommerce_order_items';
$sz9vt_oim = $wpdb->prefix . 'woocommerce_order_itemmeta';
$sz9vt_orders_t = $wpdb->prefix . 'wc_orders';
$sz9vt_has_hpos = (bool) $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9vt_orders_t ) );

if ( ! empty( $sz9vt_products ) ) {
    $sz9vt_pids   = array_keys( $sz9vt_products );
    $sz9vt_ph     = implode( ',', array_fill( 0, count( $sz9vt_pids ), '%d' ) );
    $sz9vt_status = "'wc-completed','wc-processing','wc-enviado','wc-entregue'";

    $sz9vt_sales = (array) $wpdb->get_results( $wpdb->prepare(
        "SELECT im.meta_value AS product_id,
                SUM(CAST(im2.meta_value AS UNSIGNED)) AS qty,
                SUM(CAST(im3.meta_value AS DECIMAL(12,2))) AS total
         FROM {$sz9vt_oi} oi
         INNER JOIN {$sz9vt_oim} im  ON im.order_item_id  = oi.order_item_id AND im.meta_key  = '_product_id'
         INNER JOIN {$sz9vt_oim} im2 ON im2.order_item_id = oi.order_item_id AND im2.meta_key = '_qty'
         INNER JOIN {$sz9vt_oim} im3 ON im3.order_item_id = oi.order_item_id AND im3.meta_key = '_line_total'
         WHERE im.meta_value IN ({$sz9vt_ph})
         GROUP BY im.meta_value",
        ...$sz9vt_pids
    ), ARRAY_A ) ?: [];

    foreach ( $sz9vt_sales as $s ) {
        $spid = (int) $s['product_id'];
        if ( isset( $sz9vt_products[ $spid ] ) ) {
            $sz9vt_products[ $spid ]['qty_sold'] = (int) $s['qty'];
            $sz9vt_products[ $spid ]['revenue']  = (float) $s['total'];
        }
    }
}

// ── Comissão paga por produtor ────────────────────────────────────────────
// Calcula a partir de total_sales (campo atualizado pelo sistema de comissões)
// Fallback: revenue * comm_pct se total_sales = 0
$sz9vt_comm_by_producer = [];
$sz9vt_producer_ids_uniq = array_unique( array_filter( array_column( $sz9vt_products, 'producer_id' ) ) );
if ( ! empty( $sz9vt_producer_ids_uniq ) ) {
    $sz9vt_ph2 = implode( ',', array_fill( 0, count( $sz9vt_producer_ids_uniq ), '%d' ) );
    $sz9vt_comm_rows = (array) $wpdb->get_results( $wpdb->prepare(
        "SELECT producer_id,
                SUM(total_sales) AS total_vendas,
                AVG(commission_pct) AS avg_comm
         FROM {$sz9vt_aff_table}
         WHERE producer_id IN ({$sz9vt_ph2}) AND deleted_at IS NULL
         GROUP BY producer_id",
        ...$sz9vt_producer_ids_uniq
    ), ARRAY_A ) ?: [];
    foreach ( $sz9vt_comm_rows as $cr ) {
        $total_v = (float) $cr['total_vendas'];
        $avg_c   = (float) $cr['avg_comm'];
        $sz9vt_comm_by_producer[ (int) $cr['producer_id'] ] = $total_v > 0
            ? round( $total_v * $avg_c / 100, 2 )
            : 0.0;
    }
}
foreach ( $sz9vt_products as $pid => &$prod ) {
    $prod['comm_paid'] = $sz9vt_comm_by_producer[ $prod['producer_id'] ] ?? 0.0;
}
unset( $prod );

// ── Localidades por produtor (CDs) ────────────────────────────────────────
$sz9vt_locs_by_producer = [];
$sz9vt_cd_table = $wpdb->prefix . 'sz_motoboy_cds';
$sz9vt_has_cds  = (bool) $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9vt_cd_table ) );
// Simplificado: mostrar todos os CDs ativos (todos produtores compartilham o armazém Senderzz)
// Decodifica unicode escapado que pode vir do banco com ou sem backslash
// Ex: "São" → "São" (com backslash) ou "Su00e3o" → "São" (sem backslash)
$sz9vt_decode = static function( string $s ): string {
    // 1. \uXXXX com backslash literal
    $s = (string) preg_replace_callback( '/\\\\u([0-9a-fA-F]{4})/i',
        static fn( $m ) => mb_chr( hexdec( $m[1] ), 'UTF-8' ), $s );
    // 2. uXXXX sem backslash (backslash foi stripado pelo DB/PHP) — não precedido de hex digit
    $s = (string) preg_replace_callback( '/(?<![0-9a-fA-F])u([0-9a-fA-F]{4})(?![0-9a-fA-F])/i',
        static fn( $m ) => mb_chr( hexdec( $m[1] ), 'UTF-8' ), $s );
    return $s;
};

$sz9vt_cds = [];
if ( $sz9vt_has_cds ) {
    $raw_cds = (array) $wpdb->get_results(
        "SELECT id, nome, cidade, uf FROM {$sz9vt_cd_table} WHERE ativo=1 ORDER BY nome ASC LIMIT 50",
        ARRAY_A
    ) ?: [];
    foreach ( $raw_cds as $rcd ) {
        $sz9vt_cds[] = [
            'id'     => $rcd['id'],
            'nome'   => $sz9vt_decode( (string) $rcd['nome'] ),
            'cidade' => $sz9vt_decode( (string) $rcd['cidade'] ),
            'uf'     => $sz9vt_decode( (string) $rcd['uf'] ),
        ];
    }
}

// ── Checkouts disponíveis por produtor ────────────────────────────────────
$sz9vt_links_by_producer = [];
$sz9vt_lk_table = $wpdb->prefix . 'senderzz_checkout_links';
if ( $sz9vt_has_cds && $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9vt_lk_table ) ) ) {
    $sz9vt_cols = $wpdb->get_col( "SHOW COLUMNS FROM {$sz9vt_lk_table}", 0 ) ?: [];
    if ( ! empty( $sz9vt_producer_ids_uniq ) ) {
        $sz9vt_ph3 = implode( ',', array_fill( 0, count( $sz9vt_producer_ids_uniq ), '%d' ) );
        $owner_col = in_array( 'producer_id', $sz9vt_cols, true ) ? 'producer_id' : 'user_id';
        // Não filtra por affiliate_visible — mostra todas as ofertas do produtor na vitrine
        $af_where  = '';
        // Excluir apenas espelhos motoboy auxiliares
        $tp_where  = in_array( 'tipo', $sz9vt_cols, true ) ? " AND (tipo IS NULL OR tipo='' OR tipo='correio' OR tipo='expedicao' OR tipo='expedição')" : '';
        $sz9vt_links_all = (array) $wpdb->get_results( $wpdb->prepare(
            "SELECT * FROM {$sz9vt_lk_table} WHERE {$owner_col} IN ({$sz9vt_ph3}){$af_where}{$tp_where} ORDER BY id DESC LIMIT 500",
            ...$sz9vt_producer_ids_uniq
        ), ARRAY_A ) ?: [];
        foreach ( $sz9vt_links_all as $lk ) {
            $lpid = (int) ( $lk[ $owner_col ] ?? 0 );
            if ( ! $lpid ) continue;
            if ( ! isset( $sz9vt_links_by_producer[ $lpid ] ) ) $sz9vt_links_by_producer[ $lpid ] = [];
            $sz9vt_links_by_producer[ $lpid ][] = $lk;
        }
    }
}

// ── Nomes dos produtores ──────────────────────────────────────────────────
$sz9vt_producer_names = [];
foreach ( $sz9vt_producer_ids_uniq as $ppid ) {
    $u = get_userdata( $ppid );
    $sz9vt_producer_names[ $ppid ] = $u ? ( $u->display_name ?: $u->user_email ) : 'Produtor';
}
?>
<section id="sec-vitrine" class="sz-sec" data-szv2-label="Vitrine">

    <!-- Header vitrine premium — sem ícone -->
    <div style="margin-bottom:28px">
        <h2 style="margin:0 0 4px;font-size:22px;font-weight:800;letter-spacing:-.3px;color:var(--szv2-text)">Vitrine de produtos</h2>
        <p style="margin:0;font-size:13px;color:var(--szv2-text-muted)">Explore produtos disponíveis, veja estatísticas e afilie-se com um clique.</p>
    </div>

    <?php if ( empty( $sz9vt_products ) ) : ?>
        <?php echo sz_v2_empty_state( [ 'title' => 'Nenhum produto encontrado', 'text' => 'Produtos registrados na plataforma aparecerão aqui.' ] ); // phpcs:ignore ?>
    <?php else : ?>

    <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:20px" id="szv2-vitrine-grid">
        <?php foreach ( $sz9vt_products as $sz9vt_pid => $sz9vt_prod ) :
            $sz9vt_producer_id  = $sz9vt_prod['producer_id'];
            $sz9vt_comm_pct     = $sz9vt_producer_id
                ? ( function_exists( 'sz_aff_producer_default_commission_pct' )
                    ? sz_aff_producer_default_commission_pct( $sz9vt_producer_id )
                    : 0.0 )
                : 0.0;
            $sz9vt_aff_status   = $sz9vt_my_affiliates[ $sz9vt_producer_id ] ?? null;
            $sz9vt_links_prod   = $sz9vt_links_by_producer[ $sz9vt_producer_id ] ?? [];
            // Melhor oferta por preço (maior valor = maior comissão absoluta)
            $sz9vt_best_price   = 0.0;
            $sz9vt_best_comm    = 0.0;
            foreach ( $sz9vt_links_prod as $lk ) {
                $lk_price = (float) ( $lk['price'] ?? $lk['valor'] ?? 0 );
                if ( $lk_price > $sz9vt_best_price ) {
                    $sz9vt_best_price = $lk_price;
                    $sz9vt_best_comm  = $lk_price * $sz9vt_comm_pct / 100;
                }
            }
            $sz9vt_prod_links = array_slice( array_values( $sz9vt_links_prod ), 0, 8 );
        ?>
        <!-- Card premium -->
        <div style="background:var(--szv2-surface);border-radius:16px;overflow:hidden;box-shadow:0 2px 12px rgba(0,0,0,.1);border:1px solid var(--szv2-border);display:flex;flex-direction:column;transition:box-shadow .2s,transform .2s" onmouseenter="this.style.boxShadow='0 8px 32px rgba(0,0,0,.2)';this.style.transform='translateY(-2px)'" onmouseleave="this.style.boxShadow='0 2px 12px rgba(0,0,0,.1)';this.style.transform=''">
            <!-- Imagem hero -->
            <?php if ( $sz9vt_prod['image'] ) : ?>
            <div style="height:210px;overflow:hidden;background:var(--szv2-surface-alt);flex-shrink:0;position:relative">
                <img src="<?php echo esc_url( $sz9vt_prod['image'] ); ?>"
                     alt="<?php echo esc_attr( $sz9vt_prod['name'] ); ?>"
                     style="width:100%;height:100%;object-fit:cover;display:block">
                <?php if ( $sz9vt_aff_status === 'active' ) : ?>
                <span style="position:absolute;top:10px;right:10px;background:var(--szv2-brand);color:#fff;font-size:10px;font-weight:800;padding:3px 9px;border-radius:99px;letter-spacing:.04em">✓ AFILIADO</span>
                <?php elseif ( $sz9vt_aff_status === 'pending' ) : ?>
                <span style="position:absolute;top:10px;right:10px;background:rgba(0,0,0,.55);color:#fff;font-size:10px;font-weight:700;padding:3px 9px;border-radius:99px">PENDENTE</span>
                <?php endif; ?>
            </div>
            <?php else : ?>
            <div style="height:160px;background:var(--szv2-surface-alt);display:flex;align-items:center;justify-content:center">
                <svg viewBox="0 0 20 20" style="width:48px;height:48px;fill:var(--szv2-text-faint)"><path d="M4 4h5v5H4zM11 4h5v5h-5zM4 11h5v5H4zM11 11h5v5h-5z"/></svg>
            </div>
            <?php endif; ?>

            <!-- Info -->
            <div style="padding:16px 18px 18px;flex:1;display:flex;flex-direction:column">
                <h3 style="font-size:15px;font-weight:800;margin:0 0 10px;color:var(--szv2-text);line-height:1.3;letter-spacing:-.2px"><?php echo esc_html( $sz9vt_prod['name'] ); ?></h3>

                <!-- Stats do card: vendas + total distribuído -->
                <div style="display:flex;align-items:center;gap:16px;margin-bottom:12px;padding:10px 0;border-bottom:1px solid var(--szv2-divider)">
                    <div style="display:flex;align-items:baseline;gap:3px">
                        <span style="font-size:18px;font-weight:800;color:var(--szv2-text)"><?php echo esc_html( number_format( $sz9vt_prod['qty_sold'] ) ); ?></span>
                        <span style="font-size:11px;color:var(--szv2-text-muted);font-weight:500">vendas</span>
                    </div>
                    <div style="font-size:12px;color:var(--szv2-text-muted)">
                        Total distribuído: <strong style="color:<?php echo $sz9vt_prod['comm_paid'] > 0 ? 'var(--szv2-brand)' : 'var(--szv2-text-faint)'; ?>"><?php echo esc_html( $sz9vt_money( $sz9vt_prod['comm_paid'] ) ); ?></strong>
                    </div>
                </div>

                <!-- Ação -->
                <div style="margin-top:auto;display:flex;gap:8px">
                    <button type="button"
                            class="szv2-btn szv2-btn-secondary"
                            style="flex:1;border-radius:10px;padding:10px;font-size:13px;font-weight:600"
                            onclick="szV2VtOpenModal(<?php echo esc_js( wp_json_encode( [
                                'pid'             => $sz9vt_pid,
                                'name'            => $sz9vt_prod['name'],
                                'description'     => $sz9vt_prod['description'],
                                'description_raw' => $sz9vt_prod['description_raw'],
                                'image'           => $sz9vt_prod['image'],
                                'qty_sold'        => $sz9vt_prod['qty_sold'],
                                'revenue'         => $sz9vt_prod['revenue'],
                                'comm_paid'       => $sz9vt_prod['comm_paid'],
                                'comm_pct'        => $sz9vt_comm_pct,
                                'producer_id'     => $sz9vt_producer_id,
                                'is_own'          => ( $sz9vt_producer_id === $sz9vt_wp_uid ),
                                'aff_status'      => $sz9vt_aff_status,
                                'cds'             => array_values( $sz9vt_cds ),
                                'links'           => array_values( $sz9vt_prod_links ),
                            ] ) ); ?>)">
                        Ver detalhes
                    </button>
                    <?php if ( ! $sz9vt_aff_status && $sz9vt_producer_id && $sz9vt_producer_id !== $sz9vt_wp_uid ) : ?>
                    <button type="button"
                            style="flex:1;background:#ea580c;border:none;border-radius:10px;padding:10px;font-size:13px;font-weight:700;color:#fff;cursor:pointer;transition:background .15s"
                            onmouseenter="this.style.background='#c2410c'"
                            onmouseleave="this.style.background='#ea580c'"
                            data-producer-id="<?php echo esc_attr( (string) $sz9vt_producer_id ); ?>"
                            data-nonce="<?php echo esc_attr( $sz9vt_nonce ); ?>"
                            onclick="szV2VtAffiliate(this)">
                        Afiliar-me
                    </button>
                    <?php endif; ?>
                </div>
            </div>
        </div>
        <?php endforeach; ?>
    </div>

    <!-- Modal 3 telas -->
    <!-- Modal vitrine: tabs nomeados sempre visíveis -->
    <div id="szv2-vt-modal" style="display:none;position:fixed;top:0;left:0;width:100vw;height:100vh;background:rgba(0,0,0,.5);z-index:99999;align-items:center;justify-content:center">
        <div id="szv2-vt-modal-box" style="background:var(--szv2-surface);border-radius:16px;max-width:540px;width:94%;max-height:92vh;overflow:hidden;box-shadow:0 24px 64px rgba(0,0,0,.35);display:flex;flex-direction:column">
            <!-- Header fixo: nome + fechar -->
            <div style="padding:16px 20px 0;display:flex;align-items:center;justify-content:space-between;flex-shrink:0">
                <span id="szv2-vt-modal-title" style="font-size:16px;font-weight:800;color:var(--szv2-text)"></span>
                <button onclick="document.getElementById('szv2-vt-modal').style.display='none'" style="background:none;border:none;font-size:24px;cursor:pointer;color:var(--szv2-text-muted);line-height:1;padding:0 4px">×</button>
            </div>
            <!-- Tabs nomeados — sempre clicáveis -->
            <div style="display:flex;gap:0;padding:0 20px;margin-top:12px;border-bottom:2px solid var(--szv2-divider);flex-shrink:0">
                <button class="szv2-vt-tab szv2-vt-tab--active" data-step="0" onclick="szV2VtStep(0)" style="flex:1;background:none;border:none;border-bottom:2px solid transparent;padding:8px 4px;font-size:13px;font-weight:700;cursor:pointer;color:var(--szv2-text-muted);margin-bottom:-2px;transition:all .15s">Produto</button>
                <button class="szv2-vt-tab" data-step="1" onclick="szV2VtStep(1)" style="flex:1;background:none;border:none;border-bottom:2px solid transparent;padding:8px 4px;font-size:13px;font-weight:700;cursor:pointer;color:var(--szv2-text-muted);margin-bottom:-2px;transition:all .15s">Ofertas</button>
                <button class="szv2-vt-tab" data-step="2" onclick="szV2VtStep(2)" style="flex:1;background:none;border:none;border-bottom:2px solid transparent;padding:8px 4px;font-size:13px;font-weight:700;cursor:pointer;color:var(--szv2-text-muted);margin-bottom:-2px;transition:all .15s">Localidades</button>
            </div>
            <!-- Conteúdo rolável -->
            <div id="szv2-vt-modal-inner" style="overflow-y:auto;flex:1;padding:20px 24px 24px"></div>
            <!-- Footer ação (afiliação) -->
            <div id="szv2-vt-modal-footer" style="padding:12px 24px 16px;display:flex;align-items:center;justify-content:flex-end;flex-shrink:0;border-top:1px solid #f3f4f6;min-height:52px"></div>
        </div>
    </div>
    <style>
    .szv2-vt-tab--active { color:#ea580c !important; border-bottom-color:#ea580c !important; }
    </style>

    <?php endif; ?>
</section>
<script>
(function(){
var _nonce = <?php echo wp_json_encode( $sz9vt_nonce ); ?>;
var _ajax  = <?php echo wp_json_encode( $sz9vt_ajax ); ?>;
var _money = function(v){ return 'R$ ' + parseFloat(v||0).toFixed(2).replace('.',',').replace(/\B(?=(\d{3})+(?!\d))/g,'.'); };
var _cur = 0;
var _data = null;

function _affBtn() {
    if (_data.aff_status === 'active') return '<span style="font-size:13px;font-weight:700;color:var(--szv2-brand)">✓ Afiliado</span>';
    if (_data.aff_status === 'pending') return '<span style="font-size:13px;color:var(--szv2-text-muted)">Aguardando aprovação</span>';
    if (_data.producer_id && !_data.is_own) return '<button class="szv2-btn szv2-btn-brand" style="height:38px;padding:0 20px;font-size:13px" data-producer-id="'+_data.producer_id+'" data-nonce="'+_nonce+'" onclick="szV2VtAffiliate(this)">Afiliar-me</button>';
    return '';
}

function _renderStep(step) {
    var inner  = document.getElementById('szv2-vt-modal-inner');
    var footer = document.getElementById('szv2-vt-modal-footer');
    var title  = document.getElementById('szv2-vt-modal-title');
    if (!inner || !footer || !_data) return;
    _cur = step;
    inner.scrollTop = 0;

    // Tab highlight
    document.querySelectorAll('.szv2-vt-tab').forEach(function(t) {
        var active = parseInt(t.dataset.step) === step;
        t.classList.toggle('szv2-vt-tab--active', active);
    });

    if (title) title.textContent = _data.name;

    // ── Tela 0: Produto ──────────────────────────────────
    if (step === 0) {
        var kpis =
            '<div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:8px;margin-bottom:16px">' +
            '<div style="background:var(--szv2-surface-alt);border-radius:10px;padding:12px;text-align:center"><div style="font-size:9px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.06em;margin-bottom:3px">Vendidos</div><div style="font-size:20px;font-weight:800;color:var(--szv2-text)">'+(_data.qty_sold||0)+'</div></div>' +
            '<div style="background:var(--szv2-brand-light);border-radius:10px;padding:12px;text-align:center;border:1px solid var(--szv2-border)"><div style="font-size:9px;font-weight:700;color:var(--szv2-brand);text-transform:uppercase;letter-spacing:.06em;margin-bottom:3px">Comissão</div><div style="font-size:20px;font-weight:800;color:var(--szv2-brand)">'+(_data.comm_pct||0)+'%</div></div>' +
            '<div style="background:var(--szv2-surface-alt);border-radius:10px;padding:12px;text-align:center"><div style="font-size:9px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.06em;margin-bottom:3px">Distribuído</div><div style="font-size:14px;font-weight:800;color:var(--szv2-text)">'+(_data.comm_paid > 0 ? _money(_data.comm_paid) : '—')+'</div></div>' +
            '</div>';

        var desc = _data.description
            ? '<div style="background:var(--szv2-surface-alt);border-left:3px solid var(--szv2-brand);border-radius:0 8px 8px 0;padding:12px 14px;font-size:13px;color:var(--szv2-text-soft);line-height:1.6;margin-bottom:12px">'+_data.description+'</div>'
            : '<p style="font-size:13px;color:var(--szv2-text-muted);font-style:italic;margin-bottom:12px">Sem descrição cadastrada.</p>';

        // Edição inline de descrição (somente dono do produto)
        var editBtn = '';
        if (_data.is_own) {
            editBtn = '<button onclick="szV2VtEditDesc()" style="background:none;border:none;font-size:12px;color:#ea580c;cursor:pointer;padding:0;font-weight:600;margin-bottom:12px;display:block">✏ Editar descrição da vitrine</button>';
        }

        inner.innerHTML =
            (_data.image ? '<img src="'+_data.image+'" style="width:100%;height:170px;object-fit:cover;border-radius:10px;margin-bottom:14px;display:block">' : '') +
            kpis + editBtn + desc +
            '<div id="szv2-vt-desc-edit" style="display:none;margin-bottom:12px">' +
                '<textarea id="szv2-vt-desc-input" style="width:100%;box-sizing:border-box;border:1px solid #e5e7eb;border-radius:8px;padding:10px 12px;font-size:13px;line-height:1.5;resize:vertical;min-height:80px;font-family:inherit" placeholder="Descreva o produto para os afiliados: benefícios, público-alvo, diferenciais...">'+(_data.description_raw||'')+'</textarea>' +
                '<div style="display:flex;gap:8px;margin-top:6px">' +
                    '<button onclick="szV2VtSaveDesc()" style="background:#ea580c;color:#fff;border:none;border-radius:6px;padding:7px 16px;font-size:13px;font-weight:700;cursor:pointer">Salvar</button>' +
                    '<button onclick="document.getElementById(\'szv2-vt-desc-edit\').style.display=\'none\'" style="background:none;border:1px solid #e5e7eb;border-radius:6px;padding:7px 14px;font-size:13px;cursor:pointer;color:#6b7280">Cancelar</button>' +
                '</div>' +
                '<div id="szv2-vt-desc-msg" style="font-size:12px;margin-top:4px"></div>' +
            '</div>';

        footer.innerHTML = _affBtn();
    }

    // ── Tela 1: Ofertas ──────────────────────────────────
    if (step === 1) {
        var links = _data.links || [];
        var html = '';
        if (!links.length) {
            html = '<div style="padding:32px;text-align:center;color:var(--szv2-text-muted)"><div style="font-size:32px;margin-bottom:8px">🛒</div><p style="font-size:13px;margin:0">Nenhuma oferta configurada ainda.</p></div>';
        } else {
            links.forEach(function(lk) {
                var nm    = lk.name || lk.link_name || lk.display_name || 'Oferta';
                var price = parseFloat(lk.price || lk.valor || 0);
                var comm  = price > 0 ? price * (_data.comm_pct||0) / 100 : 0;
                var tk    = lk.token || lk.slug || '';
                var checkoutUrl = tk ? (window.location.origin + '/checkout/' + tk) : '';
                html +=
                    '<div style="border:1px solid var(--szv2-border);border-radius:10px;padding:14px 16px;margin-bottom:10px;background:var(--szv2-surface-alt)">' +
                        '<div style="display:flex;align-items:flex-start;justify-content:space-between;gap:10px;margin-bottom:10px">' +
                            '<span style="font-size:14px;font-weight:700;color:var(--szv2-text);line-height:1.3">'+nm+'</span>' +
                            (checkoutUrl ? '<a href="'+checkoutUrl+'" target="_blank" rel="noopener" style="font-size:11px;font-weight:600;color:var(--szv2-brand);white-space:nowrap;text-decoration:none;padding:3px 10px;border:1px solid var(--szv2-border);border-radius:99px;background:var(--szv2-brand-light);flex-shrink:0">Abrir ↗</a>' : '') +
                        '</div>' +
                        '<div style="display:flex;gap:20px">' +
                            '<div><div style="font-size:9px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.05em;margin-bottom:2px">Valor da oferta</div><div style="font-size:18px;font-weight:800;color:var(--szv2-text)">'+(price > 0 ? _money(price) : '—')+'</div></div>' +
                            '<div><div style="font-size:9px;font-weight:700;color:var(--szv2-text-muted);text-transform:uppercase;letter-spacing:.05em;margin-bottom:2px">Sua comissão</div><div style="font-size:18px;font-weight:800;color:var(--szv2-brand)">'+(comm > 0 ? _money(comm) : ((_data.comm_pct||0)+'%'))+'</div></div>' +
                        '</div>' +
                    '</div>';
            });
        }
        inner.innerHTML = html;
        footer.innerHTML = _affBtn();
    }

    // ── Tela 2: Localidades ──────────────────────────────
    if (step === 2) {
        var cds = _data.cds || [];
        var html2 = '';
        function _szDecodeU(s) {
            return (s||'').replace(/u([0-9a-fA-F]{4})/g, function(_, h) {
                var prev = s.charAt(s.indexOf('u'+h)-1);
                return /[0-9a-fA-F]/i.test(prev) ? ('u'+h) : String.fromCharCode(parseInt(h,16));
            });
        }
        if (!cds.length) {
            html2 = '<div style="padding:32px;text-align:center;color:var(--szv2-text-muted)"><div style="font-size:32px;margin-bottom:8px">📍</div><p style="font-size:13px;margin:0">Nenhuma localidade configurada.</p></div>';
        } else {
            cds.forEach(function(cd) {
                var nm   = _szDecodeU(cd.nome || cd.name || '');
                var uf   = _szDecodeU(cd.uf || '');
                var city = _szDecodeU(cd.cidade || '');
                var sub  = [city, uf].filter(Boolean).join(' – ');
                html2 +=
                    '<div style="display:flex;align-items:center;gap:12px;padding:12px 14px;border:1px solid var(--szv2-border);border-radius:10px;margin-bottom:8px;background:var(--szv2-surface-alt)">' +
                        '<div style="width:36px;height:36px;border-radius:8px;background:var(--szv2-brand-light);display:flex;align-items:center;justify-content:center;flex-shrink:0">' +
                            '<svg viewBox="0 0 20 20" style="width:16px;height:16px;fill:var(--szv2-brand)"><path d="M10 2a5 5 0 0 0-5 5c0 3.5 5 11 5 11s5-7.5 5-11a5 5 0 0 0-5-5zm0 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4z"/></svg>' +
                        '</div>' +
                        '<div style="min-width:0"><div style="font-size:13px;font-weight:700;color:var(--szv2-text)">'+nm+'</div>' +
                        (sub ? '<div style="font-size:12px;color:var(--szv2-text-muted)">'+sub+'</div>' : '') +
                        '</div>' +
                        '<span style="margin-left:auto;flex-shrink:0;font-size:11px;font-weight:700;color:var(--szv2-brand);background:rgba(234,88,12,.08);padding:3px 10px;border-radius:99px;border:1px solid rgba(234,88,12,.3)">Disponível</span>' +
                    '</div>';
            });
        }
        inner.innerHTML = html2;
        footer.innerHTML = _affBtn();
    }
}

window.szV2VtStep = function(n) { _renderStep(n); };

window.szV2VtEditDesc = function() {
    document.getElementById('szv2-vt-desc-edit').style.display = 'block';
};

window.szV2VtSaveDesc = function() {
    var ta  = document.getElementById('szv2-vt-desc-input');
    var msg = document.getElementById('szv2-vt-desc-msg');
    if (!ta || !_data) return;
    var fd = new FormData();
    fd.append('action', 'senderzz_portal');
    fd.append('szaction', 'save_vitrine_description');
    fd.append('product_id', _data.pid);
    fd.append('description', ta.value);
    fd.append('_ajax_nonce', _nonce);
    if (msg) { msg.style.color='#9ca3af'; msg.textContent='Salvando…'; }
    fetch(_ajax, { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            if (d && d.success) {
                _data.description = ta.value;
                _data.description_raw = ta.value;
                if (msg) { msg.style.color='var(--szv2-brand)'; msg.textContent='✓ Salvo'; }
                setTimeout(function(){ document.getElementById('szv2-vt-desc-edit').style.display='none'; _renderStep(0); }, 800);
            } else {
                if (msg) { msg.style.color='#dc2626'; msg.textContent=(d&&d.data&&d.data.message)||'Erro ao salvar.'; }
            }
        })
        .catch(function(){ if(msg){ msg.style.color='#dc2626'; msg.textContent='Erro de conexão.'; } });
};

window.szV2VtOpenModal = function(data) {
    _data = data;
    _cur  = 0;
    var m = document.getElementById('szv2-vt-modal');
    if (!m) return;
    _renderStep(0);
    m.style.display = 'flex';
};

document.getElementById('szv2-vt-modal').addEventListener('click', function(e) {
    if (e.target === this) this.style.display = 'none';
});

window.szV2VtAffiliate = function(btn) {
    var producerId = btn.dataset.producerId;
    if (!producerId) return;
    btn.disabled = true;
    var origText = btn.textContent;
    btn.textContent = 'Aguarde…';
    var fd = new FormData();
    fd.append('action', 'sz_aff_panel_action');
    fd.append('aff_act', 'request_affiliation');
    fd.append('producer_id', producerId);
    fd.append('_wpnonce', btn.dataset.nonce || _nonce);
    fetch(_ajax, { method:'POST', body:fd, credentials:'same-origin' })
        .then(function(r){ return r.json(); })
        .then(function(d){
            if (d && d.success) {
                if (_data) _data.aff_status = 'pending';
                document.querySelectorAll('[data-producer-id="'+producerId+'"]').forEach(function(b){
                    b.textContent = 'Pendente'; b.disabled = true;
                    b.classList.remove('szv2-btn-brand'); b.classList.add('szv2-btn-secondary');
                });
                _renderStep(_cur); // atualiza footer
            } else {
                btn.disabled = false; btn.textContent = origText;
                if (window.szV2Confirm) window.szV2Confirm({title:'Aviso',message:(d&&d.message)||(d&&d.data&&d.data.message)||'Erro.',btn:'OK',danger:false},function(){});
            }
        })
        .catch(function(){ btn.disabled = false; btn.textContent = origText; });
};
})();
</script>
