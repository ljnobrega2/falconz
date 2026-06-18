<?php
/**
 * Senderzz COD Wallet
 * Carteira COD Motoboy: regras de repasse, taxas, conta PIX e saques.
 */
defined('ABSPATH') || exit;

if (!defined('SZ_COD_FINANCE_OPTION')) define('SZ_COD_FINANCE_OPTION', 'senderzz_cod_finance_settings');

function sz_cod_money($v): string { return 'R$ ' . number_format((float)$v, 2, ',', '.'); }
function sz_cod_now_mysql(): string { return wp_date('Y-m-d H:i:s', time(), new DateTimeZone('America/Sao_Paulo')); }
function sz_cod_dt_mysql($ts): string { return wp_date('Y-m-d H:i:s', (int)$ts, new DateTimeZone('America/Sao_Paulo')); }

function sz_cod_default_rules(): array {
    return [
        'retention_days'       => 7,
        'withdraw_fee'         => 2.99,
        'anticipation_fee_pct' => 4.99,
    ];
}

function sz_cod_get_global_rules(): array {
    $opt = get_option(SZ_COD_FINANCE_OPTION, []);
    if (!is_array($opt)) $opt = [];
    $d = sz_cod_default_rules();
    return [
        'retention_days'       => max(0, (int)($opt['retention_days'] ?? $d['retention_days'])),
        'withdraw_fee'         => max(0, (float)($opt['withdraw_fee'] ?? $d['withdraw_fee'])),
        'anticipation_fee_pct' => max(0, (float)($opt['anticipation_fee_pct'] ?? $d['anticipation_fee_pct'])),
    ];
}

function sz_cod_get_rules_for_user(int $user_id): array {
    $r = sz_cod_get_global_rules();
    foreach (['retention_days','withdraw_fee','anticipation_fee_pct'] as $k) {
        $v = get_user_meta($user_id, '_senderzz_cod_' . $k, true);
        if ($v !== '' && $v !== null) $r[$k] = $k === 'retention_days' ? max(0, (int)$v) : max(0, (float)$v);
    }
    return $r;
}

function sz_cod_tables(): array { global $wpdb; return [
    'tx' => $wpdb->prefix . 'sz_cod_wallet_transactions',
    'wd' => $wpdb->prefix . 'sz_cod_withdrawals',
    'acct' => $wpdb->prefix . 'sz_cod_withdraw_accounts',
]; }

function sz_cod_install_tables(): void {
    global $wpdb; $t = sz_cod_tables(); require_once ABSPATH . 'wp-admin/includes/upgrade.php'; $charset = $wpdb->get_charset_collate();
    dbDelta("CREATE TABLE {$t['tx']} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        user_id BIGINT UNSIGNED NOT NULL,
        order_id BIGINT UNSIGNED NULL,
        type VARCHAR(32) NOT NULL DEFAULT 'credit',
        status VARCHAR(24) NOT NULL DEFAULT 'pending',
        gross DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        fee DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        net DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        release_at DATETIME NULL,
        description VARCHAR(255) NULL,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_order_type (order_id,type),
        KEY user_status (user_id,status),
        KEY release_at (release_at)
    ) $charset;");
    dbDelta("CREATE TABLE {$t['wd']} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        user_id BIGINT UNSIGNED NOT NULL,
        account_id BIGINT UNSIGNED NULL,
        amount DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        fee DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        net DECIMAL(10,2) NOT NULL DEFAULT 0.00,
        pix_key VARCHAR(190) NULL,
        pix_type VARCHAR(40) NULL,
        holder_name VARCHAR(190) NULL,
        holder_cpf VARCHAR(14) NULL,
        proof_url VARCHAR(255) NULL,
        admin_note TEXT NULL,
        completed_at DATETIME NULL,
        status VARCHAR(24) NOT NULL DEFAULT 'analysis',
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        KEY user_status (user_id,status),
        KEY account_id (account_id)
    ) $charset;");
    dbDelta("CREATE TABLE {$t['acct']} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        user_id BIGINT UNSIGNED NOT NULL,
        holder_name VARCHAR(190) NOT NULL,
        holder_cpf VARCHAR(14) NOT NULL,
        pix_type VARCHAR(40) NOT NULL DEFAULT 'cpf',
        pix_key VARCHAR(190) NOT NULL,
        bank_name VARCHAR(190) NULL,
        bank_code VARCHAR(40) NULL,
        agency VARCHAR(40) NULL,
        account_number VARCHAR(80) NULL,
        account_type VARCHAR(40) NULL,
        is_default TINYINT(1) NOT NULL DEFAULT 0,
        status VARCHAR(24) NOT NULL DEFAULT 'active',
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        KEY user_status (user_id,status)
    ) $charset;");
    $cols = $wpdb->get_col("DESC {$t['wd']}", 0);
    $maybe = [
        'account_id' => "ALTER TABLE {$t['wd']} ADD COLUMN account_id BIGINT UNSIGNED NULL AFTER user_id",
        'holder_name' => "ALTER TABLE {$t['wd']} ADD COLUMN holder_name VARCHAR(190) NULL AFTER pix_type",
        'holder_cpf' => "ALTER TABLE {$t['wd']} ADD COLUMN holder_cpf VARCHAR(14) NULL AFTER holder_name",
        'proof_url' => "ALTER TABLE {$t['wd']} ADD COLUMN proof_url VARCHAR(255) NULL AFTER holder_cpf",
        'admin_note' => "ALTER TABLE {$t['wd']} ADD COLUMN admin_note TEXT NULL AFTER proof_url",
        'completed_at' => "ALTER TABLE {$t['wd']} ADD COLUMN completed_at DATETIME NULL AFTER admin_note",
    ];
    foreach ($maybe as $col => $sql) { if (!in_array($col, $cols, true)) $wpdb->query($sql); }
    $acct_cols = $wpdb->get_col("DESC {$t['acct']}", 0);
    $acct_cols = is_array($acct_cols) ? $acct_cols : [];
    $acct_maybe = [
        'bank_name'      => "ALTER TABLE {$t['acct']} ADD COLUMN bank_name VARCHAR(190) NULL AFTER pix_key",
        'bank_code'      => "ALTER TABLE {$t['acct']} ADD COLUMN bank_code VARCHAR(40) NULL AFTER bank_name",
        'agency'         => "ALTER TABLE {$t['acct']} ADD COLUMN agency VARCHAR(40) NULL AFTER bank_code",
        'account_number' => "ALTER TABLE {$t['acct']} ADD COLUMN account_number VARCHAR(80) NULL AFTER agency",
        'account_type'   => "ALTER TABLE {$t['acct']} ADD COLUMN account_type VARCHAR(40) NULL AFTER account_number",
    ];
    foreach ($acct_maybe as $col => $sql) { if (!in_array($col, $acct_cols, true)) $wpdb->query($sql); }
}
// Só executa install/migration se a versão do DB mudou — evita dbDelta em todo request
add_action('init', function() {
    static $done = false;
    if ( $done ) return;
    $done = true;
    $current_ver = get_option( 'sz_cod_db_version', '' );
    $target_ver  = '1.0.2'; // Incrementar ao adicionar novas colunas/tabelas
    if ( $current_ver !== $target_ver ) {
        sz_cod_install_tables();
        update_option( 'sz_cod_db_version', $target_ver, false );
    }
}, 20);

function sz_cod_release_due_transactions(): void {
    global $wpdb; $t = sz_cod_tables();
    // status='pending' apenas — 'anticipation_pending' é ignorado intencionalmente (já foi antecipado)
    $wpdb->query($wpdb->prepare(
        "UPDATE {$t['tx']} SET status='available', updated_at=%s
          WHERE status='pending' AND release_at IS NOT NULL AND release_at <= %s",
        sz_cod_now_mysql(), sz_cod_now_mysql()
    ));
}
// Executa via cron horário — não mais em todo init (economiza 1 UPDATE pesado por request)
add_action( 'sz_cod_release_cron', 'sz_cod_release_due_transactions' );
add_action( 'init', function() {
    if ( ! wp_next_scheduled( 'sz_cod_release_cron' ) ) {
        wp_schedule_event( time(), 'hourly', 'sz_cod_release_cron' );
    }
}, 5 );

function sz_cod_order_owner_id(WC_Order $order): int {
    foreach (['_senderzz_owner_user_id','_senderzz_wp_user_id','_senderzz_customer_id'] as $k) { $v=(int)$order->get_meta($k,true); if($v>0) return $v; }
    $cid = (int)$order->get_customer_id(); return $cid > 0 ? $cid : 0;
}

function sz_cod_wallet_order_has_affiliate(WC_Order $order): bool {
    return (int) $order->get_meta('_sz_affiliate_id', true) > 0 || (int) $order->get_meta('_sz_affiliate_ref', true) > 0;
}

function sz_cod_wallet_get_producer_credit_amount(WC_Order $order, float $paid_gross = 0.0): float {
    global $wpdb;
    $order_id = (int) $order->get_id();
    $gross = $paid_gross > 0 ? $paid_gross : (float) $order->get_total();

    $producer_amount = (float) $order->get_meta('_sz_prod_commission', true);
    if ( $producer_amount > 0 ) return round( $producer_amount, 2 );

    // REGRA: produtor = gross - aff_BRUTO - taxas_senderzz
    // _sz_aff_commission é o valor LÍQUIDO do afiliado (após taxa 4,99%).
    // Usar _sz_aff_commission_gross para não creditar a taxa do afiliado ao produtor.
    $aff_gross = (float) $order->get_meta('_sz_aff_commission_gross', true);
    if ( $aff_gross <= 0 ) {
        // Fallback: calcular bruto pelo percentual
        $aff_pct = (float) $order->get_meta('_sz_aff_commission_pct', true);
        if ( $aff_pct > 0 ) {
            $aff_gross = round( $gross * $aff_pct / 100, 2 );
        }
    }
    if ( $aff_gross <= 0 && function_exists('sz_aff_table') ) {
        // Fallback: somar comissões + taxa do afiliado na tabela de transações (bruto = commission + tx_fee)
        $tx_table = sz_aff_table('sz_affiliate_transactions');
        $tx_gross = (float) $wpdb->get_var( $wpdb->prepare(
            "SELECT COALESCE(SUM(amount + COALESCE(transaction_fee,0)),0) FROM {$tx_table} WHERE order_id=%d AND type='commission' AND status IN ('pending','available','paid')",
            $order_id
        ) );
        if ( $tx_gross > 0 ) $aff_gross = $tx_gross;
    }

    $fees = (float) $order->get_meta('_sz_aff_fees', true);
    if ( $fees <= 0 ) $fees = class_exists( 'Senderzz_Order_Meta' )
        ? Senderzz_Order_Meta::get_fee_total( $order )
        : (float) $order->get_meta('_sz_mb_taxa_total', true);

    if ( $aff_gross > 0 || $fees > 0 ) {
        return round( max( 0, $gross - $aff_gross - $fees ), 2 );
    }

    return 0.0;
}

function sz_cod_wallet_record_delivery($order_or_id, $gross = null): void {
    global $wpdb; if (!function_exists('wc_get_order')) return;
    $order = $order_or_id instanceof WC_Order ? $order_or_id : wc_get_order($order_or_id);
    if (!$order) return;
    $user_id = sz_cod_order_owner_id($order); if (!$user_id) return;
    $gross_original = $gross === null ? (float)$order->get_total() : (float)$gross;
    $gross_original = round(max(0, $gross_original), 2); if ($gross_original <= 0) return;

    // Pedido de afiliado: a carteira COD do produtor deve receber apenas o líquido do produtor.
    // Antes ela recebia 100% do COD, enquanto o afiliado também recebia comissão em paralelo.
    if ( sz_cod_wallet_order_has_affiliate( $order ) ) {
        $producer_credit = sz_cod_wallet_get_producer_credit_amount( $order, $gross_original );
        if ( $producer_credit <= 0 ) {
            // Evita creditar o bruto quando o split ainda não foi calculado.
            return;
        }
        $gross = $producer_credit;
    } else {
        $gross = $gross_original;
    }

    $gross = round(max(0, $gross), 2); if ($gross <= 0) return;
    $rules = sz_cod_get_rules_for_user($user_id);
    $release_ts = strtotime('+' . (int)$rules['retention_days'] . ' days', time());
    $status = ((int)$rules['retention_days'] <= 0) ? 'available' : 'pending';
    $now = sz_cod_now_mysql();
    $t = sz_cod_tables();
    $wpdb->replace($t['tx'], [
        'user_id' => $user_id,
        'order_id' => (int)$order->get_id(),
        'type' => 'credit',
        'status' => $status,
        'gross' => $gross,
        'fee' => 0,
        'net' => $gross,
        'release_at' => sz_cod_dt_mysql($release_ts),
        'description' => 'Recebimento COD Motoboy do pedido #' . $order->get_id(),
        'created_at' => $now,
        'updated_at' => $now,
    ], ['%d','%d','%s','%s','%f','%f','%f','%s','%s','%s','%s']);
    $order->update_meta_data('_senderzz_cod_wallet_recorded', 'yes');
    $order->update_meta_data('_senderzz_cod_wallet_release_at', sz_cod_dt_mysql($release_ts));
    $order->update_meta_data('_senderzz_cod_wallet_status', $status);
    $order->save();
    // Invalida cache do summary para que o painel mostre o novo saldo imediatamente
    sz_cod_wallet_invalidate_summary_cache( $user_id );
}

function sz_cod_wallet_repair_affiliate_credits_once(): void {
    if ( ! function_exists('wc_get_order') ) return;
    global $wpdb;
    $flag = 'sz_cod_wallet_affiliate_credit_repair_v315';
    if ( get_option( $flag ) === 'done' ) return;
    $t = sz_cod_tables();
    $rows = $wpdb->get_results(
        "SELECT id,user_id,order_id,gross,net FROM {$t['tx']} WHERE type='credit' AND order_id IS NOT NULL AND order_id > 0 ORDER BY id DESC LIMIT 5000",
        ARRAY_A
    ) ?: [];
    $touched_users = [];
    foreach ( $rows as $r ) {
        $order = wc_get_order( (int) $r['order_id'] );
        if ( ! $order || ! sz_cod_wallet_order_has_affiliate( $order ) ) continue;
        $original_gross = max( (float) $r['gross'], (float) $order->get_total() );
        $producer_credit = sz_cod_wallet_get_producer_credit_amount( $order, $original_gross );
        if ( $producer_credit <= 0 ) continue;
        $current_net = round( (float) $r['net'], 2 );
        if ( abs( $current_net - $producer_credit ) < 0.01 ) continue;
        $wpdb->update( $t['tx'], [
            'gross' => $producer_credit,
            'fee' => 0,
            'net' => $producer_credit,
            'description' => 'Recebimento COD Motoboy do pedido #' . (int) $r['order_id'] . ' (líquido produtor)',
            'updated_at' => sz_cod_now_mysql(),
        ], [ 'id' => (int) $r['id'] ], [ '%f','%f','%f','%s','%s' ], [ '%d' ] );
        $touched_users[] = (int) $r['user_id'];
    }
    foreach ( array_unique( array_filter( $touched_users ) ) as $uid ) {
        sz_cod_wallet_invalidate_summary_cache( (int) $uid );
    }
    update_option( $flag, 'done', false );
}
add_action( 'init', 'sz_cod_wallet_repair_affiliate_credits_once', 35 );

function sz_cod_wallet_backfill_deliveries(int $user_id): void {
    global $wpdb;
    if (!$user_id || !function_exists('wc_get_order')) return;
    $t = sz_cod_tables();
    $mb_table = $wpdb->prefix . 'sz_motoboy_pedidos';
    $exists = $wpdb->get_var( $wpdb->prepare('SHOW TABLES LIKE %s', $mb_table) );
    if (!$exists) return;
    $rows = $wpdb->get_results(
        "SELECT wc_order_id, valor_pedido, pgto_dinheiro, pgto_pix, pgto_cartao FROM {$mb_table} WHERE status='entregue' ORDER BY COALESCE(ts_entregue, updated_at, created_at) DESC LIMIT 500",
        ARRAY_A
    ) ?: [];
    foreach ($rows as $r) {
        $oid = (int)($r['wc_order_id'] ?? 0);
        if (!$oid) continue;
        $already = (int)$wpdb->get_var($wpdb->prepare("SELECT COUNT(*) FROM {$t['tx']} WHERE order_id=%d AND type='credit'", $oid));
        if ($already) continue;
        $order = wc_get_order($oid);
        if (!$order || sz_cod_order_owner_id($order) !== $user_id) continue;
        $paid = round((float)($r['pgto_dinheiro'] ?? 0) + (float)($r['pgto_pix'] ?? 0) + (float)($r['pgto_cartao'] ?? 0), 2);
        $gross = $paid > 0 ? $paid : (float)($r['valor_pedido'] ?? $order->get_total());
        sz_cod_wallet_record_delivery($order, $gross);
    }
}


function sz_cod_wallet_cleanup_deleted_orders(int $user_id = 0): void {
    global $wpdb;
    if (!function_exists('wc_get_order')) return;
    $t = sz_cod_tables();
    $where = $user_id > 0 ? $wpdb->prepare('WHERE user_id=%d AND order_id IS NOT NULL AND order_id > 0', $user_id) : 'WHERE order_id IS NOT NULL AND order_id > 0';
    $rows = $wpdb->get_results("SELECT id, order_id FROM {$t['tx']} {$where} LIMIT 1000", ARRAY_A) ?: [];
    foreach ($rows as $r) {
        $oid = (int)($r['order_id'] ?? 0);
        $order = $oid > 0 ? wc_get_order($oid) : false;
        $delete = false;
        if (!$order) {
            $delete = true;
        } else {
            $st = method_exists($order, 'get_status') ? (string)$order->get_status() : '';
            if (in_array($st, ['trash','cancelled','refunded','failed'], true)) $delete = true;
        }
        if ($delete) {
            $wpdb->delete($t['tx'], ['id' => (int)$r['id']], ['%d']);
        }
    }
}

function sz_cod_wallet_summary(int $user_id, bool $force_refresh = false): array {
    // Cache por transient: evita backfill + cleanup + 3 queries a cada request do portal
    $cache_key = 'sz_cod_summary_' . $user_id;
    if ( ! $force_refresh ) {
        $cached = get_transient( $cache_key );
        if ( is_array( $cached ) && isset( $cached['available'] ) ) {
            return $cached;
        }
    }

    // Operações pesadas: backfill e cleanup só quando cache expirou (a cada 5 min)
    sz_cod_wallet_cleanup_deleted_orders($user_id);
    sz_cod_wallet_backfill_deliveries($user_id);

    global $wpdb; $t = sz_cod_tables();
    $available = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(net),0) FROM {$t['tx']} WHERE user_id=%d AND status='available'", $user_id
    ) );
    $pending = (float) $wpdb->get_var( $wpdb->prepare(
        // 'anticipation_pending' não conta como saldo disponível — já foi antecipado
        "SELECT COALESCE(SUM(net),0) FROM {$t['tx']} WHERE user_id=%d AND status='pending'", $user_id
    ) );
    $analysis = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(amount),0) FROM {$t['wd']} WHERE user_id=%d AND status IN ('analysis','pending')", $user_id
    ) );

    $result = ['available'=>round($available,2),'pending'=>round($pending,2),'analysis'=>round($analysis,2)];
    // TTL de 5 minutos — curto o suficiente para mostrar saques recentes, longo o suficiente para evitar N+1
    set_transient( $cache_key, $result, 5 * MINUTE_IN_SECONDS );
    return $result;
}

/** Invalida o cache do summary ao registrar entrega, saque ou antecipação. */
function sz_cod_wallet_invalidate_summary_cache(int $user_id): void {
    delete_transient( 'sz_cod_summary_' . $user_id );
}

function sz_cod_wallet_history(int $user_id, string $period='30d'): array {
    sz_cod_wallet_cleanup_deleted_orders($user_id);
    global $wpdb; $t=sz_cod_tables(); $now=time();
    switch($period){case 'all':$from=0;break;case 'today':$from=strtotime('today',$now);break;case 'yesterday':$from=strtotime('yesterday',$now);$to=strtotime('today',$now)-1;break;case '7d':$from=strtotime('-7 days',$now);break;case 'month':$from=strtotime('first day of this month',$now);break;default:$from=strtotime('-30 days',$now);} $to=$to??$now;
    if($period==='all'){
        $rows=$wpdb->get_results($wpdb->prepare("SELECT * FROM {$t['tx']} WHERE user_id=%d ORDER BY created_at DESC LIMIT 1000",$user_id),ARRAY_A)?:[];
    } else {
        $rows=$wpdb->get_results($wpdb->prepare("SELECT * FROM {$t['tx']} WHERE user_id=%d AND created_at BETWEEN %s AND %s ORDER BY created_at DESC LIMIT 200",$user_id,sz_cod_dt_mysql($from),sz_cod_dt_mysql($to)),ARRAY_A)?:[];
    }
    return array_map(function($r){return ['date'=>date_i18n('d/m/Y H:i',strtotime($r['created_at'])),'description'=>$r['description']?:'Recebimento COD','order'=>!empty($r['order_id'])?'#'.$r['order_id']:'—','movement'=>$r['status']==='available'?'Disponível':'Pendente','value'=>(float)$r['gross'],'fee'=>(float)$r['fee'],'net'=>(float)$r['net'],'status'=>$r['status']];},$rows);
}

function sz_cod_wallet_future(int $user_id, string $period='30d'): array {
    sz_cod_wallet_cleanup_deleted_orders($user_id);
    global $wpdb; $t=sz_cod_tables(); $now=time();
    switch($period){case 'all':$to=0;break;case 'today':$to=strtotime('today 23:59:59',$now);break;case 'tomorrow':$to=strtotime('tomorrow 23:59:59',$now);break;case '7d':$to=strtotime('+7 days',$now);break;case '15d':$to=strtotime('+15 days',$now);break;default:$to=strtotime('+30 days',$now);} 
    if($period==='all'){
        $rows=$wpdb->get_results($wpdb->prepare("SELECT * FROM {$t['tx']} WHERE user_id=%d AND status='pending' ORDER BY release_at ASC LIMIT 1000",$user_id),ARRAY_A)?:[];
    } else {
        $rows=$wpdb->get_results($wpdb->prepare("SELECT * FROM {$t['tx']} WHERE user_id=%d AND status='pending' AND release_at <= %s ORDER BY release_at ASC LIMIT 200",$user_id,sz_cod_dt_mysql($to)),ARRAY_A)?:[];
    }
    $total=0; $data=[]; foreach($rows as $r){$total+=(float)$r['net'];$data[]=['date'=>date_i18n('d/m/Y',strtotime($r['release_at'])),'description'=>$r['description']?:'Recebimento COD','order'=>!empty($r['order_id'])?'#'.$r['order_id']:'—','conclusion'=>date_i18n('d/m/Y',strtotime($r['created_at'])),'commission'=>(float)$r['net'],'release'=>date_i18n('d/m/Y',strtotime($r['release_at']))];}
    return ['data'=>$data,'total'=>round($total,2)];
}

function sz_cod_only_digits($v): string { return preg_replace('/\D+/', '', (string)$v); }
function sz_cod_mask_cpf($cpf): string { $d=sz_cod_only_digits($cpf); return strlen($d)===11 ? substr($d,0,3).'.'.substr($d,3,3).'.'.substr($d,6,3).'-'.substr($d,9,2) : (string)$cpf; }
function sz_cod_valid_cpf($cpf): bool { $cpf=sz_cod_only_digits($cpf); if(strlen($cpf)!==11 || preg_match('/^(\d)\1{10}$/',$cpf)) return false; for($t=9;$t<11;$t++){ $d=0; for($c=0;$c<$t;$c++) $d += (int)$cpf[$c] * (($t+1)-$c); $d=((10*$d)%11)%10; if((int)$cpf[$t]!==$d) return false; } return true; }
function sz_cod_get_accounts(int $user_id): array { global $wpdb; $t=sz_cod_tables(); return $wpdb->get_results($wpdb->prepare("SELECT * FROM {$t['acct']} WHERE user_id=%d AND status='active' ORDER BY is_default DESC, id ASC", $user_id), ARRAY_A) ?: []; }
function sz_cod_get_account(int $user_id, int $account_id=0): array { global $wpdb; $t=sz_cod_tables(); if($account_id>0){ $a=$wpdb->get_row($wpdb->prepare("SELECT * FROM {$t['acct']} WHERE id=%d AND user_id=%d AND status='active'", $account_id,$user_id), ARRAY_A); if($a) return $a; } $list=sz_cod_get_accounts($user_id); return $list ? $list[0] : []; }
function sz_cod_get_pix_account(int $user_id): array { $a=sz_cod_get_account($user_id); if($a) return ['id'=>(int)$a['id'],'pix_key'=>(string)$a['pix_key'],'pix_type'=>(string)$a['pix_type'],'holder'=>(string)$a['holder_name'],'holder_cpf'=>(string)$a['holder_cpf']]; return [ 'id'=>0, 'pix_key'=>(string)get_user_meta($user_id,'_senderzz_cod_pix_key',true), 'pix_type'=>(string)get_user_meta($user_id,'_senderzz_cod_pix_type',true), 'holder'=>(string)get_user_meta($user_id,'_senderzz_cod_pix_holder',true), 'holder_cpf'=>(string)get_user_meta($user_id,'_senderzz_cod_pix_cpf',true) ]; }
function sz_cod_render_pix_profile(int $user_id, string $n): string { $accounts=sz_cod_get_accounts($user_id); ob_start(); ?>
<div class="sz-card sz-account-pix-full sz-account-pix-section" style="grid-column:1/-1;margin-top:16px;padding:22px;border-radius:22px">
  <style>
    .sz-cod-pix-head{display:flex;align-items:center;gap:14px;margin-bottom:18px}
    .sz-cod-pix-head h3{margin:0;color:var(--tx);font-size:var(--sz-text-xl);font-weight:700;letter-spacing:-.015em}
    .sz-cod-pix-head p{margin:3px 0 0;color:var(--tx2);font-size:var(--sz-text-base);font-weight:700}
    .sz-cod-pix-form{display:grid;grid-template-columns:repeat(12,minmax(0,1fr));gap:12px;align-items:end;margin:16px 0 18px}
    .sz-cod-field label{display:block;margin:0 0 7px;font-size:var(--sz-text-xs);font-weight:700;letter-spacing:0;text-transform:none;color:var(--tx3)}
    .sz-cod-field input,.sz-cod-field select{width:100%;height:46px;border-radius:14px;border:1.5px solid var(--bd);background:var(--c1);color:var(--tx);font-family:var(--sz-font);font-weight:700;padding:0 14px;outline:none}
    .sz-cod-field input:focus,.sz-cod-field select:focus{border-color:var(--ac);box-shadow:0 0 0 4px rgba(255,91,19,.10)}
    .sz-cod-field input.is-valid{border-color:#16a34a!important;box-shadow:0 0 0 3px rgba(22,163,74,.12)!important}
    .sz-cod-field input.is-invalid{border-color:#dc2626!important;box-shadow:0 0 0 3px rgba(220,38,38,.12)!important}
    .sz-cod-field-hint{font-size:var(--sz-text-sm);font-weight:700;margin-top:5px;display:none}
    .sz-cod-field-hint.show{display:block}
    .sz-cod-field-hint.ok{color:#16a34a}
    .sz-cod-field-hint.err{color:#dc2626}
    .sz-cod-pix-list{display:grid;gap:10px;margin-top:8px}
    .sz-cod-pix-row{display:grid;grid-template-columns:1.15fr .7fr .85fr 1fr 1fr 170px;gap:10px;align-items:center;padding:14px;border:1px solid var(--bd);border-radius:16px;background:var(--c2)}
    .sz-cod-pix-row small{font-size:var(--sz-text-xs);font-weight:700;text-transform:none;letter-spacing:0;color:var(--tx3);display:block;margin-bottom:3px}
    .sz-cod-pix-row strong,.sz-cod-pix-row span{font-size:var(--sz-text-base);color:var(--tx)}
    .sz-cod-delete{height:30px;border:0;border-radius:999px;background:var(--ac,#ff5b13);color:#fff;font-size:var(--sz-text-sm);font-weight:700;padding:0 12px;cursor:pointer;margin-left:6px}.sz-cod-pill{display:inline-flex;align-items:center;justify-content:center;height:28px;padding:0 10px;border-radius:999px;background:rgba(255,91,19,.10);color:var(--ac);font-size:var(--sz-text-sm);font-weight:700}
    .sz-cod-col-2{grid-column:span 2}.sz-cod-col-3{grid-column:span 3}.sz-cod-col-4{grid-column:span 4}.sz-cod-col-5{grid-column:span 5}.sz-cod-col-6{grid-column:span 6}.sz-cod-col-9{grid-column:span 9}.sz-cod-col-12{grid-column:span 12}@media(max-width:980px){.sz-cod-pix-form,.sz-cod-pix-row{grid-template-columns:1fr}.sz-cod-col-2,.sz-cod-col-3,.sz-cod-col-4,.sz-cod-col-5,.sz-cod-col-6,.sz-cod-col-9,.sz-cod-col-12{grid-column:1/-1}.sz-cod-pix-form .sz-primary{width:100%}}
  </style>
  <div class="sz-cod-pix-head">
    <div class="sz-wh-ico sz-support-ico">💸</div>
    <div><h3>Contas para saque</h3><p>Cadastre até 3 contas PIX. O CPF deve pertencer ao titular da conta.</p></div>
  </div>
  <div id="sz-cod-pix-msg" class="sz-alert" style="display:none;margin-bottom:12px"></div>
  <div class="sz-cod-pix-form">
    <div class="sz-cod-field sz-cod-col-5"><label>Titular da conta</label><input id="sz-cod-pix-holder" placeholder="Nome completo do titular"></div>
    <div class="sz-cod-field sz-cod-col-4"><label>CPF do titular</label><input id="sz-cod-pix-cpf" placeholder="000.000.000-00" inputmode="numeric" maxlength="14" oninput="szCodApplyCpfMask(this)" onblur="szCodValidateCpfField(this)"></div>
    <div class="sz-cod-field sz-cod-col-3"><label>Banco</label><select id="sz-cod-bank-select">
      <option value="">Selecione o banco</option>
      <option value="001|Banco do Brasil S.A.">001 — Banco do Brasil S.A.</option>
      <option value="104|Caixa Econômica Federal">104 — Caixa Econômica Federal</option>
      <option value="341|Itaú Unibanco S.A.">341 — Itaú Unibanco S.A.</option>
      <option value="033|Banco Santander (Brasil) S.A.">033 — Banco Santander (Brasil) S.A.</option>
      <option value="237|Banco Bradesco S.A.">237 — Banco Bradesco S.A.</option>
      <option value="260|Nu Pagamentos S.A.">260 — Nu Pagamentos S.A.</option>
      <option value="336|Banco C6 S.A.">336 — Banco C6 S.A.</option>
      <option value="077|Banco Inter S.A.">077 — Banco Inter S.A.</option>
      <option value="290|PagSeguro Internet IP S.A.">290 — PagSeguro Internet IP S.A.</option>
      <option value="323|Mercado Pago IP Ltda.">323 — Mercado Pago IP Ltda.</option>
      <option value="380|PicPay Serviços S.A.">380 — PicPay Serviços S.A.</option>
      <option value="208|Banco BTG Pactual S.A.">208 — Banco BTG Pactual S.A.</option>
      <option value="212|Banco Original S.A.">212 — Banco Original S.A.</option>
      <option value="735|Banco Neon S.A.">735 — Banco Neon S.A.</option>
      <option value="422|Banco Safra S.A.">422 — Banco Safra S.A.</option>
      <option value="748|Banco Cooperativo Sicredi S.A.">748 — Banco Cooperativo Sicredi S.A.</option>
      <option value="756|Banco Cooperativo Sicoob S.A.">756 — Banco Cooperativo Sicoob S.A.</option>
      <option value="041|Banco do Estado do Rio Grande do Sul S.A.">041 — Banrisul S.A.</option>
      <option value="070|Banco de Brasília S.A.">070 — Banco de Brasília S.A.</option>
      <option value="389|Banco Mercantil do Brasil S.A.">389 — Banco Mercantil do Brasil S.A.</option>
    </select></div>
    <div class="sz-cod-field sz-cod-col-2"><label>Agência</label><input id="sz-cod-agency" placeholder="0001"></div>
    <div class="sz-cod-field sz-cod-col-3"><label>Conta</label><input id="sz-cod-account-number" placeholder="000000-0"></div>
    <div class="sz-cod-field sz-cod-col-3"><label>Tipo de conta</label><select id="sz-cod-account-type"><option value="corrente">Corrente</option><option value="poupanca">Poupança</option><option value="pagamento">Pagamento</option></select></div>
    <div class="sz-cod-field sz-cod-col-4"><label>Tipo de chave PIX</label><select id="sz-cod-pix-type" onchange="szCodUpdatePixKeyField()"><option value="cpf">CPF</option><option value="email">E-mail</option><option value="telefone">Telefone</option><option value="aleatoria">Chave aleatória</option></select></div>
    <div class="sz-cod-field sz-cod-col-9"><label>Conteúdo da chave PIX</label><input id="sz-cod-pix-key" placeholder="CPF, e-mail, telefone ou chave aleatória" oninput="szCodFormatPixKey(this)" onblur="szCodValidatePixKey(this)"></div>
    <button type="button" class="sz-primary sz-account-action-btn sz-cod-col-3" style="height:46px;border-radius:14px" onclick="szCodSavePix('<?php echo esc_js($n); ?>')">Adicionar conta</button>
  </div>
  <div class="sz-cod-pix-list" id="sz-cod-accounts-list">
    <?php if (!$accounts): ?><div class="sz-wallet-empty" style="padding:18px;border:1px dashed var(--bd);border-radius:16px"><strong>Nenhuma conta cadastrada</strong><span>Adicione uma conta PIX para solicitar saques.</span></div><?php endif; ?>
    <?php foreach($accounts as $a): ?>
      <div class="sz-cod-pix-row">
        <div><small>Titular</small><strong><?php echo esc_html($a['holder_name']); ?></strong></div>
        <div><small>CPF</small><span><?php echo esc_html(sz_cod_mask_cpf($a['holder_cpf'])); ?></span></div>
        <div><small>PIX</small><span><?php echo esc_html(strtoupper($a['pix_type']).' · '.$a['pix_key']); ?></span></div>
        <div><small>Banco</small><span><?php echo esc_html(trim(($a['bank_name'] ?? '').' '.(!empty($a['bank_code'])?'('.$a['bank_code'].')':'')) ?: '—'); ?></span></div>
        <div><small>Agência / Conta</small><span><?php echo esc_html(trim(($a['agency'] ?? '').' · '.($a['account_number'] ?? '').' · '.($a['account_type'] ?? '')) ?: '—'); ?></span></div>
        <div><span class="sz-cod-pill"><?php echo !empty($a['is_default'])?'Principal':'Ativa'; ?></span><button type="button" class="sz-cod-delete" onclick="szCodDeleteAccount('<?php echo esc_js($n); ?>',<?php echo (int)$a['id']; ?>)">Excluir</button></div>
      </div>
    <?php endforeach; ?>
  </div>
</div>
<script>
function szCodCpfMask(v){v=(v||'').replace(/\D/g,'').slice(0,11);return v.replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d{1,2})$/,'$1-$2')}
function szCodApplyCpfMask(el){el.value=szCodCpfMask(el.value);}
function szCodValidateCpf(v){v=(v||'').replace(/\D/g,'');if(v.length!==11||/^(\d)\1{10}$/.test(v))return false;var s1=0,s2=0;for(var i=0;i<9;i++)s1+=parseInt(v[i])*(10-i);var r1=(s1*10)%11;if(r1===10||r1===11)r1=0;if(r1!==parseInt(v[9]))return false;for(var j=0;j<10;j++)s2+=parseInt(v[j])*(11-j);var r2=(s2*10)%11;if(r2===10||r2===11)r2=0;return r2===parseInt(v[10]);}
function szCodValidateCpfField(el){var ok=szCodValidateCpf(el.value);el.classList.toggle('is-valid',ok);el.classList.toggle('is-invalid',!ok&&el.value.replace(/\D/g,'').length>0);var hint=el.parentNode.querySelector('.sz-cod-field-hint');if(!hint){hint=document.createElement('span');hint.className='sz-cod-field-hint';el.parentNode.appendChild(hint);}if(el.value.replace(/\D/g,'').length===0){hint.className='sz-cod-field-hint';return;}hint.textContent=ok?'✓ CPF válido':'✗ CPF inválido';hint.className='sz-cod-field-hint show '+(ok?'ok':'err');}
function szCodUpdatePixKeyField(){var type=(document.getElementById('sz-cod-pix-type')||{}).value||'cpf';var el=document.getElementById('sz-cod-pix-key');if(!el)return;var placeholders={cpf:'000.000.000-00',email:'seu@email.com',telefone:'+55 (11) 99999-9999',aleatoria:'Chave aleatória (UUID)'};el.placeholder=placeholders[type]||'';el.value='';el.classList.remove('is-valid','is-invalid');var hint=el.parentNode.querySelector('.sz-cod-field-hint');if(hint)hint.className='sz-cod-field-hint';}
function szCodFormatPixKey(el){var type=(document.getElementById('sz-cod-pix-type')||{}).value||'cpf';if(type==='cpf'){el.value=szCodCpfMask(el.value);}else if(type==='telefone'){var v=el.value.replace(/\D/g,'').slice(0,11);v=v.replace(/^(\d{2})(\d)/,'+$1 ($2').replace(/(\(\d{2})(\d)/,'$1) $2').replace(/(\d{5})(\d{4})$/,'$1-$2');el.value=v;}}
function szCodValidatePixKey(el){var type=(document.getElementById('sz-cod-pix-type')||{}).value||'cpf';var v=el.value.trim();var ok=false;if(type==='cpf')ok=szCodValidateCpf(v);else if(type==='email')ok=/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v);else if(type==='telefone')ok=v.replace(/\D/g,'').length>=10;else if(type==='aleatoria')ok=v.length>=10;el.classList.toggle('is-valid',ok&&v.length>0);el.classList.toggle('is-invalid',!ok&&v.length>0);var hint=el.parentNode.querySelector('.sz-cod-field-hint');if(!hint){hint=document.createElement('span');hint.className='sz-cod-field-hint';el.parentNode.appendChild(hint);}if(!v){hint.className='sz-cod-field-hint';return;}var msgs={cpf:['✓ Chave CPF válida','✗ CPF inválido'],email:['✓ E-mail válido','✗ E-mail inválido'],telefone:['✓ Telefone válido','✗ Telefone inválido'],aleatoria:['✓ Chave informada','✗ Chave muito curta']};var m=msgs[type]||['✓ OK','✗ Inválido'];hint.textContent=ok?m[0]:m[1];hint.className='sz-cod-field-hint show '+(ok?'ok':'err');}
function szCodMoneyMask(v){v=(v||'').replace(/\D/g,'');var n=(parseInt(v||'0',10)/100);return n.toLocaleString('pt-BR',{minimumFractionDigits:2,maximumFractionDigits:2});}
document.addEventListener('input',function(e){if(e.target&&e.target.id==='sz-cod-wd-amount'){e.target.value=szCodMoneyMask(e.target.value)}});
function szCodToast(msg,ok){var box=document.getElementById('sz-cod-modal-toast'); if(!box){box=document.createElement('div');box.id='sz-cod-modal-toast';box.style.cssText='position:fixed;right:22px;bottom:22px;z-index:999999;background:#111827;color:#fff;border-radius:14px;padding:14px 16px;font-weight:700;box-shadow:0 18px 40px rgba(15,23,42,.25)';document.body.appendChild(box);} box.textContent=msg; box.style.background=ok?'#111827':'#dc2626'; setTimeout(function(){box.remove()},3800);}
function szCodSavePix(n){var msg=document.getElementById('sz-cod-pix-msg');var bank=(document.getElementById('sz-cod-bank-select')||{}).value||'';var bp=bank.split('|');if(msg){msg.style.display='block';msg.textContent='Salvando…';msg.style.color='var(--tx2)'}fetch(window.location.origin+'/wp-json/sz-portal/v1/cod/pix-account',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({n:n,holder:document.getElementById('sz-cod-pix-holder').value,holder_cpf:document.getElementById('sz-cod-pix-cpf').value,type:document.getElementById('sz-cod-pix-type').value,key:document.getElementById('sz-cod-pix-key').value,bank_code:bp[0]||'',bank_name:bp.slice(1).join('|')||'',agency:document.getElementById('sz-cod-agency').value,account_number:document.getElementById('sz-cod-account-number').value,account_type:document.getElementById('sz-cod-account-type').value})}).then(r=>r.json()).then(d=>{if(msg){msg.textContent=d.message||(d.success?'Conta salva.':'Erro');msg.style.color=d.success?'#166534':'#dc2626'} if(d.success) setTimeout(()=>(window.szReloadSameSection?window.szReloadSameSection():location.reload()),700);}).catch(()=>{if(msg){msg.textContent='Erro ao salvar. Atualize a página e tente novamente.';msg.style.color='#dc2626'}});}function szCodDeleteAccount(n,id){if(!confirm('Excluir esta conta de saque?'))return;fetch(window.location.origin+'/wp-json/sz-portal/v1/cod/account-delete',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({n:n,account_id:id})}).then(r=>r.json()).then(function(d){szCodToast(d.message||'Conta excluída.',!!d.success);if(d.success)setTimeout(function(){(window.szReloadSameSection?window.szReloadSameSection():location.reload())},700);}).catch(function(){szCodToast('Erro ao excluir conta.',false)});}
function szCodWithdraw(n){fetch(window.location.origin+'/wp-json/sz-portal/v1/cod/accounts?n='+encodeURIComponent(n),{credentials:'same-origin'}).then(r=>r.json()).then(function(data){if(!data.success){szCodToast(data.message||'Cadastre uma conta PIX antes de sacar.',false);return;} var accounts=data.accounts||[]; if(!accounts.length){szCodToast('Cadastre uma conta PIX no Perfil antes de sacar.',false);return;} var options=accounts.map(function(a){return '<option value="'+a.id+'">'+a.holder_name+' · PIX '+a.pix_type.toUpperCase()+': '+a.pix_key+' · '+(a.bank_name||'Banco não informado')+' · '+(a.agency||'—')+'/'+(a.account_number||'—')+'</option>'}).join(''); var html='<div id="sz-cod-withdraw-modal" style="position:fixed;inset:0;z-index:999998;background:rgba(15,23,42,.55);display:flex;align-items:center;justify-content:center;padding:22px"><div style="width:min(540px,100%);background:#fff;border-radius:24px;padding:24px;box-shadow:0 25px 80px rgba(15,23,42,.28);font-family:var(--sz-font);align-items:center;gap:12px;margin-bottom:14px"><div class="sz-wh-ico">💸</div><div><h2 style="margin:0;color:#111827">Solicitar saque</h2><p style="margin:4px 0 0;color:#667085;font-weight:700">O pedido ficará em análise até confirmação da Senderzz.</p></div></div><label style="font-weight:700;font-size:var(--sz-text-sm);color:#98a2b3;text-transform:none;letter-spacing:0">Valor do saque</label><input id="sz-cod-wd-amount" class="sz-field-input" style="width:100%;margin:6px 0 14px" placeholder="0,00" inputmode="numeric"><label style="font-weight:700;font-size:var(--sz-text-sm);color:#98a2b3;text-transform:none;letter-spacing:0">Conta para receber</label><select id="sz-cod-wd-account" class="sz-field-input" style="width:100%;margin:6px 0 18px">'+options+'</select><div style="display:flex;gap:10px;justify-content:flex-end"><button type="button" class="sz-wallet-outline-btn" onclick="document.getElementById(\'sz-cod-withdraw-modal\').remove()">Cancelar</button><button type="button" class="sz-primary" onclick="szCodSubmitWithdraw(\''+n+'\')">Solicitar saque</button></div></div></div>'; document.body.insertAdjacentHTML('beforeend',html);}).catch(function(){szCodToast('Erro ao abrir saque.',false)});}
function szCodSubmitWithdraw(n){var amount=document.getElementById('sz-cod-wd-amount').value, account=document.getElementById('sz-cod-wd-account').value; fetch(window.location.origin+'/wp-json/sz-portal/v1/cod/withdraw',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({n:n,amount:amount,account_id:account})}).then(r=>r.json()).then(function(d){szCodToast(d.message||'Solicitação enviada.',!!d.success); if(d.success){document.getElementById('sz-cod-withdraw-modal').remove(); setTimeout(function(){(window.szReloadSameSection?window.szReloadSameSection():location.reload())},1000);}}).catch(function(){szCodToast('Erro ao solicitar saque.',false)});}
</script>
<?php return ob_get_clean(); }

add_action('rest_api_init', function(){
    register_rest_route('sz-portal/v1','/cod/pix-account',['methods'=>'POST','callback'=>'sz_cod_rest_save_pix','permission_callback'=>'__return_true']);
    register_rest_route('sz-portal/v1','/cod/accounts',['methods'=>'GET','callback'=>'sz_cod_rest_accounts','permission_callback'=>'__return_true']);
    register_rest_route('sz-portal/v1','/cod/withdraw',['methods'=>'POST','callback'=>'sz_cod_rest_withdraw','permission_callback'=>'__return_true']);
    register_rest_route('sz-portal/v1','/cod/account-delete',['methods'=>'POST','callback'=>'sz_cod_rest_delete_account','permission_callback'=>'__return_true']);
});
function sz_cod_portal_wp_user(?WP_REST_Request $r=null): int {
    $u = null;
    if (class_exists('\\WC_MelhorEnvio\\Portal\\Portal_Auth')) {
        $u = \WC_MelhorEnvio\Portal\Portal_Auth::get_current_user();
    }
    if (!$u && function_exists('sz_portal_auth_user')) {
        $u = sz_portal_auth_user();
    }
    if (!$u && $r && function_exists('sz_aff_get_portal_user_by_token')) {
        $n = sanitize_text_field((string)$r->get_param('n'));
        if ($n) $u = sz_aff_get_portal_user_by_token($n);
    }
    return $u ? (int)($u->wp_user_id ?? 0) : 0;
}
function sz_cod_parse_money($v): float {
    $s = trim((string)$v);
    $s = preg_replace('/[^0-9,.]/', '', $s);
    if ($s === '') return 0.0;
    if (strpos($s, ',') !== false) {
        $s = str_replace('.', '', $s);
        $s = str_replace(',', '.', $s);
    }
    return round((float)$s, 2);
}
function sz_cod_notify_admin_withdrawal(int $wd_id): void { $admin=get_option('admin_email'); if(!$admin) return; wp_mail($admin, 'Senderzz: novo pedido de saque COD #'.$wd_id, "Há um novo pedido de saque COD aguardando análise.\n\nAcesse WP Admin > Senderzz > Financeiro COD para conferir."); }
function sz_cod_notify_user(int $uid, string $subject, string $message): void { $u=get_userdata($uid); if($u && $u->user_email) wp_mail($u->user_email, $subject, $message); }
function sz_cod_rest_accounts(WP_REST_Request $r): WP_REST_Response { $uid=sz_cod_portal_wp_user($r); if(!$uid)return new WP_REST_Response(['success'=>false,'message'=>'Não autenticado.'],403); $accounts=sz_cod_get_accounts($uid); return new WP_REST_Response(['success'=>true,'accounts'=>array_map(function($a){return ['id'=>(int)$a['id'],'holder_name'=>$a['holder_name'],'holder_cpf'=>sz_cod_mask_cpf($a['holder_cpf']),'pix_type'=>$a['pix_type'],'pix_key'=>$a['pix_key'],'bank_name'=>$a['bank_name'] ?? '','bank_code'=>$a['bank_code'] ?? '','agency'=>$a['agency'] ?? '','account_number'=>$a['account_number'] ?? '','account_type'=>$a['account_type'] ?? ''];},$accounts)]); }
function sz_cod_rest_delete_account(WP_REST_Request $r): WP_REST_Response { global $wpdb; $uid=sz_cod_portal_wp_user($r); if(!$uid)return new WP_REST_Response(['success'=>false,'message'=>'Não autenticado.'],403); $id=absint($r->get_param('account_id')); if(!$id)return new WP_REST_Response(['success'=>false,'message'=>'Conta inválida.'],422); $t=sz_cod_tables(); $ok=$wpdb->update($t['acct'],['status'=>'deleted','updated_at'=>sz_cod_now_mysql()],['id'=>$id,'user_id'=>$uid]); if($ok===false)return new WP_REST_Response(['success'=>false,'message'=>'Não foi possível excluir.'],500); return new WP_REST_Response(['success'=>true,'message'=>'Conta de saque excluída.']); }
function sz_cod_rest_save_pix(WP_REST_Request $r): WP_REST_Response { global $wpdb; $uid=sz_cod_portal_wp_user($r); if(!$uid)return new WP_REST_Response(['success'=>false,'message'=>'Não autenticado.'],403); $holder=sanitize_text_field($r->get_param('holder')); $holder_cpf=sz_cod_only_digits($r->get_param('holder_cpf')); $type=sanitize_text_field($r->get_param('type')); $key=sanitize_text_field($r->get_param('key')); $bank_name=sanitize_text_field($r->get_param('bank_name')); $bank_code=sanitize_text_field($r->get_param('bank_code')); $agency=sanitize_text_field($r->get_param('agency')); $account_number=sanitize_text_field($r->get_param('account_number')); $account_type=sanitize_text_field($r->get_param('account_type')); if(strlen($holder)<5)return new WP_REST_Response(['success'=>false,'message'=>'Informe o nome completo do titular.'],422); if(!sz_cod_valid_cpf($holder_cpf))return new WP_REST_Response(['success'=>false,'message'=>'CPF do titular inválido.'],422); if(!in_array($type,['cpf','email','telefone','aleatoria'],true))$type='cpf'; if(strlen($key)<3)return new WP_REST_Response(['success'=>false,'message'=>'Informe o conteúdo da chave PIX.'],422); if(strlen($bank_name)<2)return new WP_REST_Response(['success'=>false,'message'=>'Informe o banco.'],422); if(strlen($agency)<1)return new WP_REST_Response(['success'=>false,'message'=>'Informe a agência.'],422); if(strlen($account_number)<2)return new WP_REST_Response(['success'=>false,'message'=>'Informe a conta.'],422); if(!in_array($account_type,['corrente','poupanca','pagamento'],true))$account_type='corrente'; if($type==='cpf' && sz_cod_only_digits($key)!==$holder_cpf)return new WP_REST_Response(['success'=>false,'message'=>'Quando a chave PIX for CPF, ela deve ser o mesmo CPF do titular.'],422); $t=sz_cod_tables(); $count=(int)$wpdb->get_var($wpdb->prepare("SELECT COUNT(*) FROM {$t['acct']} WHERE user_id=%d AND status='active'",$uid)); if($count>=3)return new WP_REST_Response(['success'=>false,'message'=>'Limite de 3 contas para saque atingido.'],422); $now=sz_cod_now_mysql(); $wpdb->insert($t['acct'],['user_id'=>$uid,'holder_name'=>$holder,'holder_cpf'=>sz_cod_mask_cpf($holder_cpf),'pix_type'=>$type,'pix_key'=>$key,'bank_name'=>$bank_name,'bank_code'=>$bank_code,'agency'=>$agency,'account_number'=>$account_number,'account_type'=>$account_type,'is_default'=>$count===0?1:0,'status'=>'active','created_at'=>$now,'updated_at'=>$now]); return new WP_REST_Response(['success'=>true,'message'=>'Conta PIX cadastrada.']); }
function sz_cod_rest_withdraw(WP_REST_Request $r): WP_REST_Response {
    global $wpdb;
    $uid = sz_cod_portal_wp_user($r);
    if ( ! $uid ) return new WP_REST_Response( ['success'=>false,'message'=>'Não autenticado.'], 403 );

    // Rate limiting: máximo 5 tentativas por hora por usuário
    $rl_key   = 'sz_cod_withdraw_rl_' . $uid;
    $attempts = (int) get_transient( $rl_key );
    if ( $attempts >= 5 ) {
        return new WP_REST_Response( ['success'=>false,'message'=>'Muitas tentativas. Aguarde 1 hora antes de tentar novamente.'], 429 );
    }
    set_transient( $rl_key, $attempts + 1, HOUR_IN_SECONDS );

    $tipo   = sanitize_key( (string) ( $r->get_param('tipo') ?? 'normal' ) );
    $is_ant = $tipo === 'antecipacao';
    $rules  = sz_cod_get_rules_for_user( $uid );
    $t      = sz_cod_tables();

    $account = sz_cod_get_account( $uid, (int) $r->get_param('account_id') );
    if ( empty( $account ) ) return new WP_REST_Response( ['success'=>false,'message'=>'Cadastre uma conta PIX no Perfil antes de sacar.'], 422 );

    // Toda a operação financeira dentro de uma transação com SELECT FOR UPDATE
    // para garantir que dois saques simultâneos não passem pela mesma verificação de saldo.
    $wpdb->query( 'START TRANSACTION' );
    try {
        $now = sz_cod_now_mysql();

        // Verifica saque pendente já dentro da transação
        $pending_wd = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COUNT(*) FROM {$t['wd']} WHERE user_id=%d AND status IN ('analysis','pending','approved')", $uid
        ) );
        if ( $pending_wd > 0 ) {
            $wpdb->query( 'ROLLBACK' );
            return new WP_REST_Response( ['success'=>false,'message'=>'Já existe um saque em análise. Aguarde a conclusão.'], 422 );
        }

        if ( $is_ant ) {
            // ANTECIPACAO: busca transações pending com FOR UPDATE para travar contra concurrent release
            $pending_txs = $wpdb->get_results( $wpdb->prepare(
                "SELECT id, net FROM {$t['tx']}
                  WHERE user_id=%d AND status='pending'
                    AND release_at IS NOT NULL
                  ORDER BY release_at ASC
                  FOR UPDATE",
                $uid
            ), ARRAY_A ) ?: [];

            $amount = round( (float) array_sum( array_column( $pending_txs, 'net' ) ), 2 );
            if ( $amount <= 0 || empty( $pending_txs ) ) {
                $wpdb->query( 'ROLLBACK' );
                return new WP_REST_Response( ['success'=>false,'message'=>'Não há saldo pendente para antecipar.'], 422 );
            }

            $ant_pct = max( 0, (float) ( $rules['anticipation_fee_pct'] ?? 4.99 ) );
            $fee     = round( $amount * ( $ant_pct / 100 ), 2 );
            $net     = max( 0, round( $amount - $fee, 2 ) );
            $desc    = 'Antecipação de recebíveis COD (' . number_format( $ant_pct, 2, '.', '' ) . '%)';

            // Marca as transações pending como 'anticipation_pending' para que o cron de release
            // NÃO as libere novamente — fix do duplo crédito (cron ignora status != 'pending')
            $pending_ids = implode( ',', array_map( 'absint', array_column( $pending_txs, 'id' ) ) );
            $wpdb->query( $wpdb->prepare(
                "UPDATE {$t['tx']} SET status='anticipation_pending', updated_at=%s WHERE id IN ({$pending_ids})",
                $now
            ) );

            $wpdb->insert( $t['wd'], [
                'user_id'     => $uid,
                'account_id'  => (int) $account['id'],
                'amount'      => $amount,
                'fee'         => $fee,
                'net'         => $net,
                'pix_key'     => $account['pix_key'],
                'pix_type'    => $account['pix_type'],
                'holder_name' => $account['holder_name'],
                'holder_cpf'  => $account['holder_cpf'],
                'status'      => 'approved',
                'created_at'  => $now,
                'updated_at'  => $now,
            ] );
            $wd_id = (int) $wpdb->insert_id;

            // Transação de débito contábil do pendente antecipado
            $wpdb->insert( $t['tx'], [
                'user_id'     => $uid,
                'type'        => 'anticipation',
                'status'      => 'approved',
                'gross'       => -$amount,
                'fee'         => $fee,
                'net'         => -$net,
                'description' => $desc . ' #' . $wd_id,
                'created_at'  => $now,
                'updated_at'  => $now,
            ] );

            // Crédito no disponível (líquido após taxa)
            $wpdb->insert( $t['tx'], [
                'user_id'     => $uid,
                'type'        => 'anticipation_credit',
                'status'      => 'available',
                'gross'       => $net,
                'fee'         => 0,
                'net'         => $net,
                'description' => 'Crédito de antecipação #' . $wd_id . ' (líquido após taxa de ' . number_format($ant_pct,2,'.',''). '%)',
                'created_at'  => $now,
                'updated_at'  => $now,
            ] );

            $wpdb->query( 'COMMIT' );
            delete_transient( $rl_key );
            sz_cod_wallet_invalidate_summary_cache( $uid );

            sz_cod_notify_user( $uid,
                'Senderzz: Antecipação aprovada — ' . sz_cod_money($net) . ' disponível',
                'Sua antecipação de ' . sz_cod_money($amount) . ' foi processada automaticamente.' .
                "\nTaxa: " . sz_cod_money($fee) . ' (' . number_format($ant_pct,2,'.',''). '%)' .
                "\nValor disponível para saque: " . sz_cod_money($net)
            );
            return new WP_REST_Response( ['success'=>true,'message'=>'Antecipação aprovada! ' . sz_cod_money($net) . ' já disponível para saque.','net'=>$net,'fee'=>$fee], 200 );

        } else {
            // SAQUE NORMAL: lê saldo disponível com FOR UPDATE para bloquear saques concorrentes
            $available = (float) $wpdb->get_var( $wpdb->prepare(
                "SELECT COALESCE(SUM(net),0) FROM {$t['tx']}
                  WHERE user_id=%d AND status='available'
                  FOR UPDATE",
                $uid
            ) );

            $amount = sz_cod_parse_money( $r->get_param('amount') );
            if ( $amount <= 0 || $amount > $available ) {
                $wpdb->query( 'ROLLBACK' );
                return new WP_REST_Response( ['success'=>false,'message'=>'Valor indisponível para saque.'], 422 );
            }

            $fee  = min( $amount, (float) ( $rules['withdraw_fee'] ?? 0 ) );
            $net  = max( 0, round( $amount - $fee, 2 ) );
            $desc = 'Solicitação de saque COD';

            $wpdb->insert( $t['wd'], [
                'user_id'     => $uid,
                'account_id'  => (int) $account['id'],
                'amount'      => $amount,
                'fee'         => $fee,
                'net'         => $net,
                'pix_key'     => $account['pix_key'],
                'pix_type'    => $account['pix_type'],
                'holder_name' => $account['holder_name'],
                'holder_cpf'  => $account['holder_cpf'],
                'status'      => 'analysis',
                'created_at'  => $now,
                'updated_at'  => $now,
            ] );
            $wd_id = (int) $wpdb->insert_id;

            $wpdb->insert( $t['tx'], [
                'user_id'     => $uid,
                'type'        => 'withdrawal',
                'status'      => 'analysis',
                'gross'       => -$amount,
                'fee'         => $fee,
                'net'         => -$amount,
                'description' => $desc . ' #' . $wd_id,
                'created_at'  => $now,
                'updated_at'  => $now,
            ] );

            $wpdb->query( 'COMMIT' );
            delete_transient( $rl_key );
            sz_cod_wallet_invalidate_summary_cache( $uid );

            $admin = get_option('admin_email');
            if ( $admin ) {
                wp_mail( $admin,
                    "Senderzz: nova solicitação de SAQUE COD #{$wd_id}",
                    "Nova solicitação de SAQUE recebida.\n\nID: #{$wd_id}\nValor: " . sz_cod_money($amount) .
                    "\nTaxa: " . sz_cod_money($fee) . "\nLíquido: " . sz_cod_money($net) .
                    "\nConta: " . ($account['holder_name']??'') . " · PIX " . strtoupper($account['pix_type']??'') .
                    ": " . ($account['pix_key']??'') . "\n\nAcesse WP Admin > Senderzz > Financeiro COD para processar."
                );
            }
            sz_cod_notify_user( $uid,
                "Senderzz: {$desc} #{$wd_id} recebido",
                "Recebemos sua solicitação #{$wd_id}.\nValor: " . sz_cod_money($amount) .
                "\nTaxa: " . sz_cod_money($fee) . "\nLíquido previsto: " . sz_cod_money($net) . ".\n\nProcessamento em até 1 dia útil."
            );

            return new WP_REST_Response( [
                'success' => true,
                'message' => 'Saque solicitado. Taxa: ' . sz_cod_money($fee) . '. Líquido: ' . sz_cod_money($net) . '.',
            ] );
        }

    } catch ( \Throwable $e ) {
        $wpdb->query( 'ROLLBACK' );
        error_log( '[Senderzz][sz_cod_rest_withdraw] Transação revertida uid=' . $uid . ': ' . $e->getMessage() );
        return new WP_REST_Response( ['success'=>false,'message'=>'Erro interno. Tente novamente.'], 500 );
    }
}
function sz_cod_user_withdrawals(int $user_id): array { global $wpdb; $t=sz_cod_tables(); return $wpdb->get_results($wpdb->prepare("SELECT * FROM {$t['wd']} WHERE user_id=%d ORDER BY id DESC LIMIT 50",$user_id),ARRAY_A)?:[]; }
function sz_cod_render_wallet_withdrawals(int $user_id): string { $rows=sz_cod_user_withdrawals($user_id); ob_start(); ?>
<div class="sz-card" style="margin-top:16px;padding:18px;border-radius:18px"><h3 style="margin:0 0 6px">Ordens de saque</h3><p class="sz-wallet-mock-sub">Acompanhe solicitações, taxas, comprovantes e conclusão.</p><div style="overflow:auto"><table style="width:100%;border-collapse:collapse;font-size:var(--sz-text-base)"><thead><tr><th>Data</th><th>Valor</th><th>Taxa</th><th>Líquido</th><th>Conta</th><th>Status</th><th>Comprovante</th></tr></thead><tbody><?php if(!$rows): ?><tr><td colspan="7" style="padding:14px;color:#98a2b3">Nenhum saque solicitado.</td></tr><?php endif; foreach($rows as $w): ?><tr><td><?php echo esc_html(date_i18n('d/m/Y H:i',strtotime($w['created_at']))); ?></td><td><?php echo esc_html(sz_cod_money($w['amount'])); ?></td><td><?php echo esc_html(sz_cod_money($w['fee'])); ?></td><td><?php echo esc_html(sz_cod_money($w['net'])); ?></td><td><?php echo esc_html(($w['holder_name']?:'Conta PIX').' · '.$w['pix_key']); ?></td><td><?php echo esc_html($w['status']==='paid'?'Concluído':'Em análise'); ?></td><td><?php echo !empty($w['proof_url'])?'<a href="'.esc_url($w['proof_url']).'" target="_blank">Ver comprovante</a>':'—'; ?></td></tr><?php endforeach; ?></tbody></table></div></div>
<?php return ob_get_clean(); }

// Senderzz v221: submenu legado removido da origem; aba Financeiro COD vive em Admin > Senderzz.
// add_action('admin_menu', function(){ add_submenu_page('senderzz','Financeiro COD','Financeiro COD','manage_woocommerce','senderzz-cod-finance','sz_cod_admin_page'); }, 99);

if (!function_exists('sz_cod_get_class_owner_id')) {
    /**
     * Resolve o dono/produtor de uma classe de entrega.
     * Fonte principal: portal_user ativo vinculado à classe, sem parent_user_id.
     * Fallback: option senderzz_shipping_class_wallet_owners.
     */
    function sz_cod_get_class_owner_id(int $class_id): int {
        global $wpdb;
        if ($class_id <= 0) return 0;

        if (function_exists('senderzz_get_portal_wallet_owner_by_class_id')) {
            $owner = (int) senderzz_get_portal_wallet_owner_by_class_id($class_id);
            if ($owner > 0 && get_user_by('id', $owner)) return $owner;
        }

        $pu_table = $wpdb->prefix . 'senderzz_portal_users';
        $mc_table = $wpdb->prefix . 'senderzz_portal_user_classes';
        $pu_exists = ($wpdb->get_var($wpdb->prepare('SHOW TABLES LIKE %s', $pu_table)) === $pu_table);
        if ($pu_exists) {
            $mc_exists = ($wpdb->get_var($wpdb->prepare('SHOW TABLES LIKE %s', $mc_table)) === $mc_table);
            if ($mc_exists) {
                $owner = (int) $wpdb->get_var($wpdb->prepare(
                    "SELECT COALESCE(pu.wp_user_id,0)
                       FROM {$mc_table} mc
                       INNER JOIN {$pu_table} pu ON pu.id = mc.portal_user_id
                      WHERE mc.shipping_class_id = %d
                        AND pu.status = 'active'
                        AND (pu.parent_user_id IS NULL OR pu.parent_user_id = 0)
                      ORDER BY pu.id ASC
                      LIMIT 1",
                    $class_id
                ));
                if ($owner > 0 && get_user_by('id', $owner)) return $owner;
            }

            $owner = (int) $wpdb->get_var($wpdb->prepare(
                "SELECT COALESCE(wp_user_id,0)
                   FROM {$pu_table}
                  WHERE shipping_class_id = %d
                    AND status = 'active'
                    AND (parent_user_id IS NULL OR parent_user_id = 0)
                  ORDER BY id ASC
                  LIMIT 1",
                $class_id
            ));
            if ($owner > 0 && get_user_by('id', $owner)) return $owner;
        }

        $map = get_option('senderzz_shipping_class_wallet_owners', []);
        if (is_array($map)) {
            $owner = 0;
            if (!empty($map[$class_id])) $owner = (int) $map[$class_id];
            if (!$owner && !empty($map[(string)$class_id])) $owner = (int) $map[(string)$class_id];
            if ($owner > 0 && get_user_by('id', $owner)) return $owner;
        }

        return 0;
    }
}

if (!function_exists('sz_cod_get_product_producer_ids')) {
    /**
     * Retorna somente produtores reais para os overrides COD:
     * donos de classe de entrega que está sendo usada por pelo menos um produto WooCommerce.
     *
     * Importante: produto não é identificado pelo post_author neste projeto.
     * A regra correta é produto -> classe de entrega -> dono da classe.
     * Afiliado puro não entra aqui; afiliado segue retenção própria/fixa configurada na tela de afiliados.
     */
    function sz_cod_get_product_producer_ids(): array {
        global $wpdb;

        $class_ids = $wpdb->get_col("\n            SELECT DISTINCT tt.term_id\n              FROM {$wpdb->posts} p\n              INNER JOIN {$wpdb->term_relationships} tr ON tr.object_id = p.ID\n              INNER JOIN {$wpdb->term_taxonomy} tt ON tt.term_taxonomy_id = tr.term_taxonomy_id\n             WHERE p.post_type = 'product'\n               AND p.post_status IN ('publish','private','draft','pending')\n               AND tt.taxonomy = 'product_shipping_class'\n               AND tt.term_id > 0\n             ORDER BY tt.term_id ASC\n        ") ?: [];

        $ids = [];
        foreach ($class_ids as $class_id) {
            $owner_id = sz_cod_get_class_owner_id((int) $class_id);
            if ($owner_id > 0) $ids[$owner_id] = $owner_id;
        }

        return array_values(array_unique(array_map('intval', $ids)));
    }
}
add_action('admin_init', function(){
    if(!current_user_can('manage_woocommerce')) return;
    if(!empty($_POST['sz_cod_finance_save'])){
        check_admin_referer('sz_cod_finance_save');
        update_option(SZ_COD_FINANCE_OPTION,[
            'retention_days'=>max(0,(int)($_POST['retention_days']??7)),
            'withdraw_fee'=>max(0,(float)str_replace(',','.',$_POST['withdraw_fee']??'2.99')),
            'anticipation_fee_pct'=>max(0,(float)str_replace(',','.',$_POST['anticipation_fee_pct']??'4.99'))
        ],false);
        update_option('sz_admin_motoboy_fee', max(0,(float)str_replace(',','.',$_POST['sz_admin_motoboy_fee']??get_option('sz_admin_motoboy_fee','18'))), false);
        update_option('sz_admin_operational_fund_fee', max(0,(float)str_replace(',','.',$_POST['sz_admin_operational_fund_fee']??get_option('sz_admin_operational_fund_fee','2'))), false);
        $overrides = $_POST['producer_rules'] ?? [];
        if (is_array($overrides)) {
            $allowed_producer_ids = array_flip(sz_cod_get_product_producer_ids());
            foreach ($overrides as $uid => $rule) {
                $uid = absint($uid);
                if (!$uid || !isset($allowed_producer_ids[$uid])) {
                    continue; // afiliado puro não recebe override COD de produtor
                }
                foreach (['retention_days','withdraw_fee','anticipation_fee_pct'] as $k) {
                    $raw = isset($rule[$k]) ? trim((string)$rule[$k]) : '';
                    if ($raw === '') { delete_user_meta($uid, '_senderzz_cod_' . $k); continue; }
                    $val = $k === 'retention_days' ? max(0,(int)$raw) : max(0,(float)str_replace(',','.', $raw));
                    update_user_meta($uid, '_senderzz_cod_' . $k, $val);
                }
            }
        }
        wp_safe_redirect(add_query_arg(['page'=>'senderzz','tab'=>'financeiro-cod','saved'=>'1'],admin_url('admin.php'))); exit;
    }
    if(!empty($_POST['sz_cod_complete_withdraw'])){ check_admin_referer('sz_cod_complete_withdraw'); sz_cod_admin_complete_withdrawal(); }
});
function sz_cod_admin_pending_count(): int { global $wpdb; $t=sz_cod_tables(); return (int)$wpdb->get_var("SELECT COUNT(*) FROM {$t['wd']} WHERE status IN ('analysis','pending')"); }
add_action('admin_notices', function(){ if(!current_user_can('manage_woocommerce')) return; $c=sz_cod_admin_pending_count(); if($c>0) echo '<div class="notice notice-warning"><p><strong>Senderzz:</strong> existem '.(int)$c.' saque(s) COD aguardando análise. <a href="'.esc_url(admin_url('admin.php?page=senderzz&tab=financeiro-cod')).'">Abrir financeiro COD</a></p></div>'; });
function sz_cod_admin_complete_withdrawal(): void { global $wpdb; $t=sz_cod_tables(); $id=absint($_POST['withdrawal_id']??0); if(!$id) return; $w=$wpdb->get_row($wpdb->prepare("SELECT * FROM {$t['wd']} WHERE id=%d",$id),ARRAY_A); if(!$w) return; $proof=''; if(!empty($_FILES['proof_file']['name'])){ require_once ABSPATH.'wp-admin/includes/file.php'; $up=wp_handle_upload($_FILES['proof_file'],['test_form'=>false]); if(empty($up['error'])&&!empty($up['url'])) $proof=esc_url_raw($up['url']); } if(!$proof && !empty($_POST['proof_url'])) $proof=esc_url_raw($_POST['proof_url']); $note=sanitize_textarea_field($_POST['admin_note']??''); $now=sz_cod_now_mysql(); $wpdb->update($t['wd'],['status'=>'paid','proof_url'=>$proof,'admin_note'=>$note,'completed_at'=>$now,'updated_at'=>$now],['id'=>$id]); $wpdb->update($t['tx'],['status'=>'paid','updated_at'=>$now],['user_id'=>(int)$w['user_id'],'type'=>'withdrawal','status'=>'analysis']); sz_cod_notify_user((int)$w['user_id'],'Senderzz: saque COD concluído','Seu saque COD #'.$id.' foi concluído. Líquido pago: '.sz_cod_money($w['net']).'.'.($proof?"\nComprovante: ".$proof:'')); wp_safe_redirect(add_query_arg(['page'=>'senderzz','tab'=>'financeiro-cod','withdrawal_done'=>'1'],admin_url('admin.php'))); exit; }
function sz_cod_admin_page(): void {
    global $wpdb;
    $r = sz_cod_get_global_rules();
    $t = sz_cod_tables();
    $rows = $wpdb->get_results("SELECT w.*, u.display_name, u.user_email FROM {$t['wd']} w LEFT JOIN {$wpdb->users} u ON u.ID=w.user_id ORDER BY w.id DESC LIMIT 100", ARRAY_A) ?: [];
    // Overrides por produtor: listar somente autores de pelo menos um produto WooCommerce.
    // Usuários afiliados puros não aparecem aqui e continuam seguindo a regra de afiliados.
    $producer_ids = sz_cod_get_product_producer_ids();
    $users = [];
    foreach ($producer_ids as $uid) { $u = get_userdata((int)$uid); if ($u) $users[] = $u; }
    usort($users, function($a, $b) { return strcasecmp($a->display_name ?: $a->user_email, $b->display_name ?: $b->user_email); });
    ?>
<div class="wrap"><h1>Senderzz · Financeiro COD</h1><?php if(!empty($_GET['saved'])) echo '<div class="notice notice-success"><p>Configurações salvas.</p></div>'; if(!empty($_GET['withdrawal_done'])) echo '<div class="notice notice-success"><p>Saque marcado como concluído.</p></div>'; ?>
<form method="post" style="background:#fff;border:1px solid #ccd0d4;border-radius:12px;padding:20px;max-width:980px;margin-bottom:20px"><?php wp_nonce_field('sz_cod_finance_save'); ?><input type="hidden" name="sz_cod_finance_save" value="1">
<h2>Regras padrão de repasse</h2>
<div style="display:grid;grid-template-columns:repeat(3,minmax(160px,1fr));gap:14px;max-width:760px">
<p><label>Retenção após entrega (dias)<br><input type="number" min="0" name="retention_days" value="<?php echo esc_attr($r['retention_days']); ?>"></label></p>
<p><label>Taxa de saque padrão (R$)<br><input name="withdraw_fee" value="<?php echo esc_attr(number_format((float)$r['withdraw_fee'],2,'.','')); ?>"></label></p>
<p><label>Taxa de antecipação padrão (%)<br><input name="anticipation_fee_pct" value="<?php echo esc_attr(number_format((float)$r['anticipation_fee_pct'],2,'.','')); ?>"></label></p>
</div>
<p class="description">Fallback global. Campos vazios no produtor usam estes valores automaticamente.</p>
<h2 style="margin-top:24px">Taxas administrativas Motoboy</h2>
<p class="description">Usadas nas variáveis de comissão administrativa do push: <code>{{comissao_admin_liquida}}</code> e <code>{{comissao_admin_liquida_total}}</code>.</p>
<div style="display:grid;grid-template-columns:repeat(2,minmax(160px,1fr));gap:14px;max-width:520px">
<p><label>Taxa motoboy admin (R$)<br><input name="sz_admin_motoboy_fee" value="<?php echo esc_attr(number_format((float)get_option('sz_admin_motoboy_fee',18),2,'.','')); ?>"></label></p>
<p><label>Fundo operacional (R$)<br><input name="sz_admin_operational_fund_fee" value="<?php echo esc_attr(number_format((float)get_option('sz_admin_operational_fund_fee',2),2,'.','')); ?>"></label></p>
</div>
<?php if($users): ?><h2 style="margin-top:24px">Overrides por produtor</h2><table class="widefat striped" style="max-width:940px"><thead><tr><th>Produtor</th><th>Retenção</th><th>Taxa saque</th><th>Taxa antecipação</th><th>Usando</th></tr></thead><tbody><?php foreach($users as $u): $uid=(int)$u->ID; $eff=sz_cod_get_rules_for_user($uid); $m1=get_user_meta($uid,'_senderzz_cod_retention_days',true); $m2=get_user_meta($uid,'_senderzz_cod_withdraw_fee',true); $m3=get_user_meta($uid,'_senderzz_cod_anticipation_fee_pct',true); ?><tr><td><strong><?php echo esc_html($u->display_name ?: ('#'.$uid)); ?></strong><br><small><?php echo esc_html($u->user_email); ?> · ID <?php echo $uid; ?></small></td><td><input type="number" min="0" name="producer_rules[<?php echo $uid; ?>][retention_days]" value="<?php echo esc_attr($m1); ?>" placeholder="<?php echo esc_attr($r['retention_days']); ?>" style="width:90px"></td><td><input name="producer_rules[<?php echo $uid; ?>][withdraw_fee]" value="<?php echo esc_attr($m2); ?>" placeholder="<?php echo esc_attr(number_format((float)$r['withdraw_fee'],2,'.','')); ?>" style="width:90px"></td><td><input name="producer_rules[<?php echo $uid; ?>][anticipation_fee_pct]" value="<?php echo esc_attr($m3); ?>" placeholder="<?php echo esc_attr(number_format((float)$r['anticipation_fee_pct'],2,'.','')); ?>" style="width:90px"></td><td><?php echo esc_html($eff['retention_days'].' dias · R$ '.number_format((float)$eff['withdraw_fee'],2,',','.').' · '.number_format((float)$eff['anticipation_fee_pct'],2,',','.').'%'); ?></td></tr><?php endforeach; ?></tbody></table><?php endif; ?>
<p style="margin-top:18px"><button class="button button-primary">Salvar regras</button></p></form>
<div style="background:#fff;border:1px solid #ccd0d4;border-radius:12px;padding:20px"><h2>Pedidos de saque COD</h2><table class="widefat striped"><thead><tr><th>ID</th><th>Usuário</th><th>Valor</th><th>Taxa</th><th>Líquido</th><th>Conta</th><th>Status</th><th>Ação</th></tr></thead><tbody><?php if(!$rows): ?><tr><td colspan="8">Nenhum saque.</td></tr><?php endif; foreach($rows as $w): ?><tr><td>#<?php echo (int)$w['id']; ?></td><td><?php echo esc_html(($w['display_name']?:'Usuário').' · '.$w['user_email']); ?></td><td><?php echo esc_html(sz_cod_money($w['amount'])); ?></td><td><?php echo esc_html(sz_cod_money($w['fee'])); ?></td><td><?php echo esc_html(sz_cod_money($w['net'])); ?></td><td><?php echo esc_html(($w['holder_name']?:'').' · '.($w['holder_cpf']?:'').' · '.($w['pix_key']?:'')); ?></td><td><?php echo esc_html($w['status']==='paid'?'Concluído':'Em análise'); ?></td><td><?php if($w['status']!=='paid'): ?><form method="post" enctype="multipart/form-data" style="display:grid;gap:6px;max-width:260px"><?php wp_nonce_field('sz_cod_complete_withdraw'); ?><input type="hidden" name="sz_cod_complete_withdraw" value="1"><input type="hidden" name="withdrawal_id" value="<?php echo (int)$w['id']; ?>"><input type="file" name="proof_file" accept="image/*,application/pdf"><input type="url" name="proof_url" placeholder="ou URL do comprovante"><textarea name="admin_note" placeholder="Observação interna"></textarea><button class="button button-primary">Confirmar pago</button></form><?php else: echo !empty($w['proof_url'])?'<a href="'.esc_url($w['proof_url']).'" target="_blank">Comprovante</a>':'—'; endif; ?></td></tr><?php endforeach; ?></tbody></table></div></div><?php }
