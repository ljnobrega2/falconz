<?php
/**
 * Senderzz Audit Engine
 *
 * Motor central de auditoria somente leitura. Ele classifica divergências reais,
 * avisos e itens ignorados sem alterar banco. Correções continuam nos handlers atuais.
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'senderzz_audit_meta_table' ) ) {
    function senderzz_audit_meta_table(): array {
        global $wpdb;
        $hpos = $wpdb->prefix . 'wc_orders_meta';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $hpos ) ) === $hpos ) return [ $hpos, 'order_id' ];
        return [ $wpdb->postmeta, 'post_id' ];
    }
}

if ( ! function_exists( 'senderzz_audit_table_exists' ) ) {
    function senderzz_audit_table_exists( string $table ): bool {
        global $wpdb;
        return $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) === $table;
    }
}

if ( ! function_exists( 'senderzz_audit_order_should_ignore_finance' ) ) {
    function senderzz_audit_order_should_ignore_finance( int $order_id ): bool {
        $ctx = function_exists( 'senderzz_order_context' ) ? senderzz_order_context( $order_id ) : [ 'exists' => false ];
        if ( empty( $ctx['exists'] ) ) return true;
        if ( ! empty( $ctx['frustrated'] ) ) return true;
        return empty( $ctx['financially_settled'] );
    }
}

if ( ! function_exists( 'senderzz_audit_problem_rows' ) ) {
    function senderzz_audit_problem_rows( int $limit = 100 ): array {
        global $wpdb;
        [ $meta, $order_col ] = senderzz_audit_meta_table();
        if ( ! senderzz_audit_table_exists( $meta ) ) return [];
        $limit = max( 1, min( 500, $limit ) );
        $rows = [];

        $split_candidates = $wpdb->get_results( "SELECT {$order_col} pedido,
                MAX(CASE WHEN meta_key IN ('_sz_aff_gross','_senderzz_offer_value') THEN CAST(meta_value AS DECIMAL(12,2)) END) bruto,
                MAX(CASE WHEN meta_key='_sz_aff_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) aff,
                MAX(CASE WHEN meta_key='_sz_mb_taxa_total' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) taxa,
                MAX(CASE WHEN meta_key='_sz_prod_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) prod
            FROM {$meta}
            WHERE meta_key IN ('_sz_aff_gross','_senderzz_offer_value','_sz_aff_commission','_sz_mb_taxa_total','_sz_prod_commission')
            GROUP BY {$order_col}
            HAVING bruto IS NOT NULL AND ABS(ROUND(bruto,2)-ROUND(aff+taxa+prod,2))>0.02
            ORDER BY {$order_col} DESC LIMIT 120", ARRAY_A ) ?: [];
        foreach ( $split_candidates as $r ) {
            $oid = absint( $r['pedido'] );
            if ( senderzz_audit_order_should_ignore_finance( $oid ) ) continue;
            $rows[] = [ 'pedido' => $oid, 'tipo' => 'Total divergente', 'tipo_key' => 'split', 'oficial' => (float) $r['bruto'], 'atual' => (float) $r['aff'] + (float) $r['taxa'] + (float) $r['prod'], 'severity' => 'error' ];
        }

        $aff_tx = function_exists( 'sz_aff_table' ) ? sz_aff_table( 'sz_affiliate_transactions' ) : $wpdb->prefix . 'sz_affiliate_transactions';
        if ( senderzz_audit_table_exists( $aff_tx ) ) {
            $missing = $wpdb->get_results( "SELECT x.order_id pedido,'Afiliado sem transação' tipo,'aff_missing' tipo_key,x.comissao oficial,0 atual FROM (SELECT {$order_col} order_id, MAX(CASE WHEN meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref') THEN CAST(meta_value AS UNSIGNED) ELSE 0 END) affiliate_id, MAX(CASE WHEN meta_key='_sz_aff_commission' THEN CAST(meta_value AS DECIMAL(12,2)) ELSE 0 END) comissao FROM {$meta} WHERE meta_key IN ('_sz_affiliate_id','_sz_affiliate_ref','_sz_aff_commission') GROUP BY {$order_col}) x LEFT JOIN {$aff_tx} t ON t.order_id=x.order_id AND t.affiliate_id=x.affiliate_id AND t.type='commission' AND t.status IN ('pending','available') WHERE x.affiliate_id>0 AND x.comissao>0 AND t.id IS NULL ORDER BY x.order_id DESC LIMIT 120", ARRAY_A ) ?: [];
            $bad = $wpdb->get_results( "SELECT t.order_id pedido,'Comissão afiliado divergente' tipo,'aff_bad' tipo_key,m.meta_value oficial,t.amount atual FROM {$aff_tx} t JOIN {$meta} m ON m.{$order_col}=t.order_id AND m.meta_key='_sz_aff_commission' WHERE t.type='commission' AND t.status IN ('pending','available') AND ROUND(CAST(t.amount AS DECIMAL(12,2)),2)<>ROUND(CAST(m.meta_value AS DECIMAL(12,2)),2) ORDER BY t.order_id DESC LIMIT 120", ARRAY_A ) ?: [];
            foreach ( array_merge( $missing, $bad ) as $r ) {
                if ( senderzz_audit_order_should_ignore_finance( absint( $r['pedido'] ) ) ) continue;
                $r['severity'] = 'error';
                $rows[] = $r;
            }
        }

        $cod_tx = $wpdb->prefix . 'sz_cod_wallet_transactions';
        if ( senderzz_audit_table_exists( $cod_tx ) ) {
            $wallet = $wpdb->get_results( "SELECT w.order_id pedido,'Produtor COD divergente' tipo,'wallet' tipo_key,m.meta_value oficial,w.net atual FROM {$cod_tx} w JOIN {$meta} m ON m.{$order_col}=w.order_id AND m.meta_key='_sz_prod_commission' WHERE w.type='credit' AND ROUND(CAST(w.net AS DECIMAL(12,2)),2)<>ROUND(CAST(m.meta_value AS DECIMAL(12,2)),2) ORDER BY w.order_id DESC LIMIT 120", ARRAY_A ) ?: [];
            foreach ( $wallet as $r ) {
                if ( senderzz_audit_order_should_ignore_finance( absint( $r['pedido'] ) ) ) continue;
                $r['severity'] = 'error';
                $rows[] = $r;
            }
        }

        return array_slice( $rows, 0, $limit );
    }
}

if ( ! function_exists( 'senderzz_audit_counts' ) ) {
    function senderzz_audit_counts(): array {
        $out = [ 'split' => 0, 'aff_bad' => 0, 'aff_missing' => 0, 'wallet' => 0 ];
        foreach ( senderzz_audit_problem_rows( 500 ) as $row ) {
            $key = (string) ( $row['tipo_key'] ?? '' );
            if ( isset( $out[ $key ] ) ) $out[ $key ]++;
        }
        return $out;
    }
}

if ( ! function_exists( 'senderzz_audit_order' ) ) {
    function senderzz_audit_order( int $order_id ): array {
        $ctx = function_exists( 'senderzz_order_context' ) ? senderzz_order_context( $order_id ) : [ 'exists' => false ];
        $problems = [];
        foreach ( senderzz_audit_problem_rows( 500 ) as $row ) {
            if ( absint( $row['pedido'] ?? 0 ) === $order_id ) $problems[] = $row;
        }
        return [ 'context' => $ctx, 'problems' => $problems, 'ok' => empty( $problems ) ];
    }
}
