<?php
/**
 * Analytics.php
 *
 * Dashboard de receita e margem para o admin.
 * Consome os metas de expedição e exclui pedidos Motoboy/COD.
 * O relatório de expedição não pode usar pedidos COD como base.
 */

namespace WC_MelhorEnvio\Analytics;

defined( 'ABSPATH' ) || exit;

class Margin_Dashboard {

    public function __construct() {
        // Senderzz v221: menu legado removido da origem; render pelo menu unificado.
        // add_action( 'admin_menu', [ $this, 'register_menu' ] );
        add_action( 'admin_init', [ $this, 'handle_export' ] );
    }

    public function register_menu(): void {
        // Senderzz v221: registro legado cortado; renderização pela aba Relatórios do menu unificado.
        return;
    }

    /* ── Exportação CSV ──────────────────────────────────────────────── */

    public function handle_export(): void {
        if (
            ! isset( $_GET['senderzz_export_csv'] ) ||
            ! wp_verify_nonce( $_GET['_wpnonce'] ?? '', 'senderzz_export' ) ||
            ! current_user_can( 'manage_woocommerce' )
        ) return;

        $data = $this->fetch_data(
            sanitize_text_field( $_GET['date_from'] ?? '' ),
            sanitize_text_field( $_GET['date_to'] ?? '' )
        );

        header( 'Content-Type: text/csv; charset=utf-8' );
        header( 'Content-Disposition: attachment; filename="senderzz-margem-' . date( 'Y-m-d' ) . '.csv"' );
        $out = fopen( 'php://output', 'w' );
        fputcsv( $out, [ 'Pedido', 'Data', 'Classe Entrega', 'Transportadora', 'Cobrado R$', 'Custo Real R$', 'Margem R$', 'Markup %', 'Status' ], ';' );
        foreach ( $data['rows'] as $row ) {
            fputcsv( $out, [
                $row['number'],
                $row['date'],
                $row['class_name'],
                $row['carrier'],
                number_format( $row['charged'], 2, ',', '' ),
                number_format( $row['real_cost'], 2, ',', '' ),
                number_format( $row['margin'], 2, ',', '' ),
                $row['real_cost'] > 0 ? number_format( ( $row['margin'] / $row['real_cost'] ) * 100, 1, ',', '' ) . '%' : '—',
                $row['status'],
            ], ';' );
        }
        fclose( $out );
        exit;
    }

    /* ── Consulta de dados ───────────────────────────────────────────── */

    private function fetch_data( string $date_from = '', string $date_to = '' ): array {
        global $wpdb;

        $date_from = $date_from ?: date( 'Y-m-01' );
        $date_to   = $date_to   ?: date( 'Y-m-d' );

        // SQL direto, HPOS-aware, sem limite arbitrário de 500 pedidos.
        //
        // Estratégia: detecta se HPOS está ativo (tabela wp_wc_orders existe) e
        // usa a tabela correta. Agrega os 5 campos que interessam numa só passagem
        // via MAX(CASE WHEN) — evita N+1 queries de wc_get_order() por pedido.
        $hpos_table = $wpdb->prefix . 'wc_orders';
        $hpos_active = (bool) $wpdb->get_var(
            "SELECT COUNT(*) FROM information_schema.TABLES
             WHERE TABLE_SCHEMA = DATABASE()
               AND TABLE_NAME = '{$hpos_table}'"
        );

        if ( $hpos_active ) {
            $meta_table = $wpdb->prefix . 'wc_orders_meta';
            $sql = $wpdb->prepare(
                "SELECT
                    o.id                                                                AS order_id,
                    o.id                                                                AS order_number,
                    DATE(o.date_created_gmt)                                            AS order_date,
                    REPLACE(o.status, 'wc-', '')                                       AS order_status,
                    MAX(CASE WHEN m.meta_key = '_senderzz_service_fee'                THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS margin,
                    MAX(CASE WHEN m.meta_key = '_senderzz_shipping_charged'            THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS charged,
                    MAX(CASE WHEN m.meta_key = '_senderzz_shipping_real_cost'          THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS real_cost,
                    MAX(CASE WHEN m.meta_key = '_senderzz_product_shipping_class_name' THEN m.meta_value END)                       AS class_name,
                    MAX(CASE WHEN m.meta_key = '_senderzz_carrier_name'                THEN m.meta_value END)                       AS carrier,
                    MAX(CASE WHEN m.meta_key = '_senderzz_delivery_mode'               THEN m.meta_value END)                       AS delivery_mode,
                    MAX(CASE WHEN m.meta_key = '_senderzz_motoboy_flow_status'         THEN m.meta_value END)                       AS motoboy_flow_status,
                    MAX(CASE WHEN m.meta_key = '_senderzz_motoboy_status'              THEN m.meta_value END)                       AS motoboy_status
                FROM {$hpos_table} o
                INNER JOIN {$meta_table} m ON m.order_id = o.id
                    AND m.meta_key IN (
                        '_senderzz_service_fee',
                        '_senderzz_shipping_charged',
                        '_senderzz_shipping_real_cost',
                        '_senderzz_product_shipping_class_name',
                        '_senderzz_carrier_name',
                        '_senderzz_delivery_mode',
                        '_senderzz_motoboy_flow_status',
                        '_senderzz_motoboy_status'
                    )
                WHERE o.type = 'shop_order'
                  AND DATE(o.date_created_gmt) BETWEEN %s AND %s
                GROUP BY o.id
                HAVING margin IS NOT NULL
                ORDER BY o.date_created_gmt DESC",
                $date_from,
                $date_to
            );
        } else {
            $sql = $wpdb->prepare(
                "SELECT
                    p.ID                                                                AS order_id,
                    p.ID                                                                AS order_number,
                    DATE(p.post_date)                                                   AS order_date,
                    REPLACE(p.post_status, 'wc-', '')                                  AS order_status,
                    MAX(CASE WHEN m.meta_key = '_senderzz_service_fee'                THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS margin,
                    MAX(CASE WHEN m.meta_key = '_senderzz_shipping_charged'            THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS charged,
                    MAX(CASE WHEN m.meta_key = '_senderzz_shipping_real_cost'          THEN CAST(m.meta_value AS DECIMAL(10,2)) END) AS real_cost,
                    MAX(CASE WHEN m.meta_key = '_senderzz_product_shipping_class_name' THEN m.meta_value END)                       AS class_name,
                    MAX(CASE WHEN m.meta_key = '_senderzz_carrier_name'                THEN m.meta_value END)                       AS carrier,
                    MAX(CASE WHEN m.meta_key = '_senderzz_delivery_mode'               THEN m.meta_value END)                       AS delivery_mode,
                    MAX(CASE WHEN m.meta_key = '_senderzz_motoboy_flow_status'         THEN m.meta_value END)                       AS motoboy_flow_status,
                    MAX(CASE WHEN m.meta_key = '_senderzz_motoboy_status'              THEN m.meta_value END)                       AS motoboy_status
                FROM {$wpdb->posts} p
                INNER JOIN {$wpdb->postmeta} m ON m.post_id = p.ID
                    AND m.meta_key IN (
                        '_senderzz_service_fee',
                        '_senderzz_shipping_charged',
                        '_senderzz_shipping_real_cost',
                        '_senderzz_product_shipping_class_name',
                        '_senderzz_carrier_name',
                        '_senderzz_delivery_mode',
                        '_senderzz_motoboy_flow_status',
                        '_senderzz_motoboy_status'
                    )
                WHERE p.post_type = 'shop_order'
                  AND DATE(p.post_date) BETWEEN %s AND %s
                GROUP BY p.ID
                HAVING margin IS NOT NULL
                ORDER BY p.post_date DESC",
                $date_from,
                $date_to
            );
        }

        $result_rows = $wpdb->get_results( $sql, ARRAY_A ) ?: [];

        $rows          = [];
        $total_charged = 0.0;
        $total_real    = 0.0;
        $total_margin  = 0.0;
        $by_class      = [];
        $by_carrier    = [];

        foreach ( $result_rows as $r ) {
            $charged    = round( (float) ( $r['charged']   ?? 0 ), 2 );
            $real_cost  = round( (float) ( $r['real_cost'] ?? 0 ), 2 );
            $margin     = round( (float) ( $r['margin']    ?? 0 ), 2 );
            $class_name = $r['class_name'] ?: 'Sem classe';
            $carrier    = $r['carrier']    ?: '';
            $status     = $r['order_status'] ?? '';

            // Senderzz v333: relatório de expedição deve ignorar Motoboy/COD.
            // Antes, pedidos COD com taxa _senderzz_service_fee entravam como se fossem expedição,
            // gerando classe/transportadora "Motoboy Senderzz" e contaminando os totais.
            $delivery_mode = strtolower( trim( (string) ( $r['delivery_mode'] ?? '' ) ) );
            $flow_status   = trim( (string) ( $r['motoboy_flow_status'] ?? '' ) );
            $mb_status     = trim( (string) ( $r['motoboy_status'] ?? '' ) );
            $carrier_lc    = strtolower( (string) $carrier );
            $class_lc      = strtolower( (string) $class_name );
            if (
                $delivery_mode === 'motoboy'
                || $flow_status !== ''
                || $mb_status !== ''
                || strpos( $carrier_lc, 'motoboy' ) !== false
                || strpos( $carrier_lc, 'moto boy' ) !== false
                || strpos( $class_lc, 'motoboy' ) !== false
                || strpos( $class_lc, 'cod' ) !== false
            ) {
                continue;
            }

            $rows[] = [
                'number'     => $r['order_number'],
                'date'       => $r['order_date'],
                'class_name' => $class_name,
                'carrier'    => $carrier,
                'charged'    => $charged,
                'real_cost'  => $real_cost,
                'margin'     => $margin,
                'status'     => $status,
            ];

            $total_charged += $charged;
            $total_real    += $real_cost;
            $total_margin  += $margin;

            if ( ! isset( $by_class[ $class_name ] ) ) {
                $by_class[ $class_name ] = [ 'orders' => 0, 'charged' => 0.0, 'real' => 0.0, 'margin' => 0.0 ];
            }
            $by_class[ $class_name ]['orders']++;
            $by_class[ $class_name ]['charged'] += $charged;
            $by_class[ $class_name ]['real']    += $real_cost;
            $by_class[ $class_name ]['margin']  += $margin;

            $c = $carrier ?: 'Sem transportadora';
            if ( ! isset( $by_carrier[ $c ] ) ) {
                $by_carrier[ $c ] = [ 'orders' => 0, 'margin' => 0.0 ];
            }
            $by_carrier[ $c ]['orders']++;
            $by_carrier[ $c ]['margin'] += $margin;
        }

        $total_charged = round( $total_charged, 2 );
        $total_real    = round( $total_real, 2 );
        $total_margin  = round( $total_margin, 2 );

        arsort( $by_class );
        arsort( $by_carrier );

        return compact( 'rows', 'total_charged', 'total_real', 'total_margin', 'by_class', 'by_carrier', 'date_from', 'date_to' );
    }

    /* ── Render ──────────────────────────────────────────────────────── */

    public function render_inline(): void {
        echo '<div class="sz-tab-content active" style="display:block">';
        $this->render_body();
        echo '</div>';
    }

    public function render(): void {
        $this->render_body();
    }

    private function render_body(): void {
        $date_from = sanitize_text_field( $_GET['date_from'] ?? date( 'Y-m-01' ) );
        $date_to   = sanitize_text_field( $_GET['date_to']   ?? date( 'Y-m-d' ) );
        $data      = $this->fetch_data( $date_from, $date_to );
        extract( $data );
        $is_motoboy_report = ! empty( $rows );
        foreach ( $rows as $sz_r ) {
            if ( stripos( (string) ( $sz_r['carrier'] ?? '' ), 'motoboy' ) === false ) { $is_motoboy_report = false; break; }
        }
        $export_url = wp_nonce_url(
            add_query_arg( [ 'senderzz_export_csv' => 1, 'date_from' => $date_from, 'date_to' => $date_to ], admin_url( 'admin.php?page=senderzz&tab=relatorios' ) ),
            'senderzz_export'
        );
        ?>
        <div class="wrap">
            <h1>📊 Relatório de Margem — Senderzz</h1>

            <!-- Filtros -->
            <form method="get" style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;margin:20px 0 24px;">
                <input type="hidden" name="page" value="senderzz-analytics">
                <div>
                    <label style="display:block;font-size:var(--sz-text-meta);color:#666;margin-bottom:3px;">De</label>
                    <input type="date" name="date_from" value="<?php echo esc_attr( $date_from ); ?>" style="height:32px;border-radius:4px;border:1px solid #ccc;padding:0 8px;">
                </div>
                <div>
                    <label style="display:block;font-size:var(--sz-text-meta);color:#666;margin-bottom:3px;">Até</label>
                    <input type="date" name="date_to" value="<?php echo esc_attr( $date_to ); ?>" style="height:32px;border-radius:4px;border:1px solid #ccc;padding:0 8px;">
                </div>
                <button type="submit" class="button button-primary">Filtrar</button>
                <a href="<?php echo esc_url( $export_url ); ?>" class="button">⬇ Exportar CSV</a>
            </form>

            <!-- KPIs -->
            <div style="display:grid;grid-template-columns:repeat(<?php echo $is_motoboy_report ? 3 : 4; ?>,1fr);gap:16px;max-width:900px;margin-bottom:28px;">
                <?php
                $kpis = [
                    [ 'Pedidos', count( $rows ), '' ],
                    [ 'Total cobrado', 'R$ ' . number_format( $total_charged, 2, ',', '.' ), '' ],
                ];
                if ( ! $is_motoboy_report ) {
                    $kpis[] = [ 'Custo real ME', 'R$ ' . number_format( $total_real, 2, ',', '.' ), '' ];
                }
                $kpis[] = [ 'Margem retida', 'R$ ' . number_format( $total_margin, 2, ',', '.' ), '#16a34a' ];
                foreach ( $kpis as [ $label, $value, $color ] ) : ?>
                    <div style="background:#f9f9f9;border:1px solid #e5e7eb;border-radius:8px;padding:16px 18px;">
                        <div style="font-size:var(--sz-text-meta);color:#6b7280;margin-bottom:4px;"><?php echo esc_html( $label ); ?></div>
                        <div style="font-size:var(--sz-text-xl);font-weight:600;color:<?php echo $color ?: '#111827'; ?>;"><?php echo esc_html( $value ); ?></div>
                    </div>
                <?php endforeach; ?>
            </div>

            <!-- Por classe -->
            <?php if ( $by_class ) : ?>
                <h3 style="margin-bottom:10px;">Por classe de entrega (produtor)</h3>
                <table class="widefat" style="max-width:780px;margin-bottom:28px;">
                    <thead><tr><th>Classe</th><th>Pedidos</th><th>Cobrado</th><?php if ( ! $is_motoboy_report ) : ?><th>Custo ME</th><?php endif; ?><th>Margem</th><?php if ( ! $is_motoboy_report ) : ?><th>% Margem</th><?php endif; ?></tr></thead>
                    <tbody>
                    <?php foreach ( $by_class as $cls => $d ) :
                        $pct = $d['real'] > 0 ? round( ( $d['margin'] / $d['real'] ) * 100, 1 ) : 0;
                    ?>
                        <tr>
                            <td><strong><?php echo esc_html( $cls ); ?></strong></td>
                            <td><?php echo (int) $d['orders']; ?></td>
                            <td>R$ <?php echo number_format( $d['charged'], 2, ',', '.' ); ?></td>
                            <?php if ( ! $is_motoboy_report ) : ?><td>R$ <?php echo number_format( $d['real'], 2, ',', '.' ); ?></td><?php endif; ?>
                            <td style="color:#16a34a;font-weight:600;">R$ <?php echo number_format( $d['margin'], 2, ',', '.' ); ?></td>
                            <?php if ( ! $is_motoboy_report ) : ?><td><?php echo $pct; ?>%</td><?php endif; ?>
                        </tr>
                    <?php endforeach; ?>
                    </tbody>
                </table>
            <?php endif; ?>

            <!-- Por transportadora -->
            <?php if ( $by_carrier ) : ?>
                <h3 style="margin-bottom:10px;">Por transportadora</h3>
                <table class="widefat" style="max-width:500px;margin-bottom:28px;">
                    <thead><tr><th>Transportadora</th><th>Pedidos</th><th>Margem</th></tr></thead>
                    <tbody>
                    <?php foreach ( $by_carrier as $car => $d ) : ?>
                        <tr>
                            <td><?php echo esc_html( $car ); ?></td>
                            <td><?php echo (int) $d['orders']; ?></td>
                            <td style="color:#16a34a;font-weight:600;">R$ <?php echo number_format( $d['margin'], 2, ',', '.' ); ?></td>
                        </tr>
                    <?php endforeach; ?>
                    </tbody>
                </table>
            <?php endif; ?>

            <!-- Tabela de pedidos -->
            <?php if ( $rows ) : ?>
                <h3 style="margin-bottom:10px;">Detalhamento por pedido (<?php echo count( $rows ); ?> pedidos)</h3>
                <div style="overflow-x:auto;max-width:100%;">
                <table class="widefat">
                    <thead><tr><th>Pedido</th><th>Data</th><th>Classe</th><th>Transportadora</th><th>Cobrado</th><?php if ( ! $is_motoboy_report ) : ?><th>Custo ME</th><?php endif; ?><th>Margem</th><th>Status</th></tr></thead>
                    <tbody>
                    <?php foreach ( $rows as $row ) : ?>
                        <tr>
                            <td><a href="<?php echo get_edit_post_link( wc_get_order( $row['number'] ) ); ?>">#<?php echo esc_html( $row['number'] ); ?></a></td>
                            <td><?php echo esc_html( $row['date'] ); ?></td>
                            <td><?php echo esc_html( $row['class_name'] ); ?></td>
                            <td><?php echo esc_html( $row['carrier'] ); ?></td>
                            <td>R$ <?php echo number_format( $row['charged'], 2, ',', '.' ); ?></td>
                            <?php if ( ! $is_motoboy_report ) : ?><td>R$ <?php echo number_format( $row['real_cost'], 2, ',', '.' ); ?></td><?php endif; ?>
                            <td style="color:#16a34a;font-weight:600;">R$ <?php echo number_format( $row['margin'], 2, ',', '.' ); ?></td>
                            <td><?php echo esc_html( $row['status'] ); ?></td>
                        </tr>
                    <?php endforeach; ?>
                    </tbody>
                </table>
                </div>
            <?php else : ?>
                <div style="background:#fffbeb;border:1px solid #fcd34d;padding:14px 18px;border-radius:6px;max-width:600px;">
                    ⚠️ Nenhum pedido com dados de margem encontrado no período. Verifique se o plugin está processando pedidos normalmente.
                </div>
            <?php endif; ?>
        </div>
        <?php
    }

}
