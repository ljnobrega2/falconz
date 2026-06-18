<?php
/**
 * Senderzz — Carteira do Motoboy
 * Gerencia ganhos por entrega/frustrado, dados bancários e pagamento semanal (sextas).
 */
if ( ! defined( 'ABSPATH' ) ) exit;

// ─── Tabelas ─────────────────────────────────────────────────────────────────

function sz_mbw_tables(): array {
    global $wpdb;
    return [
        'ganhos'  => $wpdb->prefix . 'sz_motoboy_ganhos',
        'bancario'=> $wpdb->prefix . 'sz_motoboy_bancario',
        'pagto'   => $wpdb->prefix . 'sz_motoboy_pagamentos',
    ];
}

function sz_mbw_install(): void {
    global $wpdb;
    $charset = $wpdb->get_charset_collate();
    $t = sz_mbw_tables();

    require_once ABSPATH . 'wp-admin/includes/upgrade.php';

    dbDelta( "CREATE TABLE IF NOT EXISTS {$t['ganhos']} (
        id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        motoboy_id    BIGINT UNSIGNED NOT NULL,
        pedido_id     BIGINT UNSIGNED NOT NULL,
        wc_order_id   BIGINT UNSIGNED NOT NULL,
        tipo          ENUM('entrega','frustrado') NOT NULL,
        valor         DECIMAL(8,2)   NOT NULL DEFAULT 0.00,
        valor_pago    DECIMAL(8,2)   NOT NULL DEFAULT 0.00,
        status        ENUM('pendente','disponivel','pago','cancelado') NOT NULL DEFAULT 'pendente',
        pagamento_id  BIGINT UNSIGNED NULL,
        created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        UNIQUE KEY uk_pedido (pedido_id, tipo),
        KEY idx_mb (motoboy_id),
        KEY idx_status (status),
        KEY idx_created (created_at)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$t['bancario']} (
        id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        motoboy_id    BIGINT UNSIGNED NOT NULL,
        holder_name   VARCHAR(100) NOT NULL,
        holder_cpf    VARCHAR(14)  NOT NULL,
        bank_name     VARCHAR(100) NULL,
        bank_code     VARCHAR(10)  NULL,
        agency        VARCHAR(20)  NULL,
        account_number VARCHAR(30) NULL,
        account_type  ENUM('corrente','poupanca','pagamento') NOT NULL DEFAULT 'corrente',
        pix_type      ENUM('cpf','cnpj','email','telefone','aleatoria') NOT NULL DEFAULT 'cpf',
        pix_key       VARCHAR(150) NOT NULL,
        is_default    TINYINT(1)   NOT NULL DEFAULT 1,
        created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_mb (motoboy_id)
    ) $charset;" );

    // Garante compatibilidade em instalações antigas: permite CNPJ como tipo de chave PIX do motoboy.
    $wpdb->query( "ALTER TABLE {$t['bancario']} MODIFY pix_type ENUM('cpf','cnpj','email','telefone','aleatoria') NOT NULL DEFAULT 'cpf'" );

    // v343: carteira do motoboy passa a ter estado individual por pedido: pendente -> disponivel -> pago.
    // A conciliação deixa de ser por fechamento/data e passa a ser por pedido.
    $wpdb->query( "ALTER TABLE {$t['ganhos']} MODIFY status ENUM('pendente','disponivel','pago','cancelado') NOT NULL DEFAULT 'pendente'" );
    $has_valor_pago = $wpdb->get_var( $wpdb->prepare( "SHOW COLUMNS FROM {$t['ganhos']} LIKE %s", 'valor_pago' ) );
    if ( ! $has_valor_pago ) {
        $wpdb->query( "ALTER TABLE {$t['ganhos']} ADD COLUMN valor_pago DECIMAL(8,2) NOT NULL DEFAULT 0.00 AFTER valor" );
    }

    dbDelta( "CREATE TABLE IF NOT EXISTS {$t['pagto']} (
        id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        motoboy_id    BIGINT UNSIGNED NOT NULL,
        valor_total   DECIMAL(8,2)   NOT NULL DEFAULT 0.00,
        qtd_entregas  INT UNSIGNED   NOT NULL DEFAULT 0,
        qtd_frustrados INT UNSIGNED  NOT NULL DEFAULT 0,
        data_pagamento DATE          NOT NULL,
        status        ENUM('aguardando','pago','cancelado') NOT NULL DEFAULT 'aguardando',
        obs           TEXT           NULL,
        created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_mb (motoboy_id),
        KEY idx_data (data_pagamento)
    ) $charset;" );
}
add_action( 'sz_motoboy_tables_created', 'sz_mbw_install' );
add_action( 'init', function() {
    static $done = false;
    if ( $done ) return;
    $done = true;
    sz_mbw_install();
}, 20 );

if ( ! function_exists( 'sz_mbw_reset_wallet_v343_once' ) ) {
function sz_mbw_reset_wallet_v343_once(): void {
    if ( get_option( 'sz_mbw_reset_wallet_v343_done' ) ) { return; }
    global $wpdb;
    $p = $wpdb->prefix;
    $wpdb->query( "DELETE FROM {$p}sz_motoboy_ganhos" );
    $wpdb->query( "DELETE FROM {$p}sz_motoboy_fechamento" );
    update_option( 'sz_mbw_reset_wallet_v343_done', current_time( 'mysql' ), false );
    if ( function_exists( 'sz_mbw_rebuild_all_from_orders' ) ) { sz_mbw_rebuild_all_from_orders(); }
}
add_action( 'admin_init', 'sz_mbw_reset_wallet_v343_once', 30 );
}

// ─── Taxas configuráveis ─────────────────────────────────────────────────────

function sz_mbw_get_taxa_entrega( int $motoboy_id = 0 ): float {
    // Por motoboy, com fallback para o global
    $per_mb = $motoboy_id > 0 ? (float) get_option( 'sz_mbw_taxa_entrega_mb_' . $motoboy_id, 0 ) : 0;
    if ( $per_mb > 0 ) return $per_mb;
    return (float) get_option( 'sz_mbw_taxa_entrega', 18.00 );
}

function sz_mbw_get_taxa_frustrado( int $motoboy_id = 0 ): float {
    $per_mb = $motoboy_id > 0 ? (float) get_option( 'sz_mbw_taxa_frustrado_mb_' . $motoboy_id, 0 ) : 0;
    if ( $per_mb > 0 ) return $per_mb;
    return (float) get_option( 'sz_mbw_taxa_frustrado', 5.00 );
}

// ─── Registrar ganho ao mudar status ─────────────────────────────────────────

add_action( 'sz_motoboy_status_changed', function( int $pedido_id, string $de, string $para, object $pedido ) {
    if ( ! in_array( $para, [ 'entregue', 'frustrado' ], true ) ) return;
    if ( ! $pedido->motoboy_id ) return;

    global $wpdb;
    $t = sz_mbw_tables();
    $mb_id = (int) $pedido->motoboy_id;
    $tipo  = $para === 'entregue' ? 'entrega' : 'frustrado';
    $valor = $para === 'entregue'
        ? sz_mbw_get_taxa_entrega( $mb_id )
        : (float) ( $pedido->valor_taxa_frustrado ?? 0 );

    // Idempotente, mas corrigível: se o ganho já existe e o pedido foi
    // reatribuído/trocado de motoboy, atualiza a linha em vez de manter saldo
    // preso no motoboy anterior.
    $exists = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, motoboy_id, valor, wc_order_id FROM {$t['ganhos']} WHERE pedido_id=%d AND tipo=%s LIMIT 1",
        $pedido_id, $tipo
    ) );
    if ( $exists ) {
        $update = [];
        $formats = [];
        if ( (int) $exists->motoboy_id !== $mb_id ) {
            $update['motoboy_id'] = $mb_id;
            $formats[] = '%d';
        }
        if ( round( (float) $exists->valor, 2 ) !== round( $valor, 2 ) ) {
            $update['valor'] = round( $valor, 2 );
            $formats[] = '%f';
        }
        $wc_order_id = (int) ( $pedido->wc_order_id ?? 0 );
        if ( $wc_order_id > 0 && (int) $exists->wc_order_id !== $wc_order_id ) {
            $update['wc_order_id'] = $wc_order_id;
            $formats[] = '%d';
        }
        if ( $update ) {
            $wpdb->update( $t['ganhos'], $update, [ 'id' => (int) $exists->id ], $formats, [ '%d' ] );
        }
        return;
    }

    $wpdb->insert( $t['ganhos'], [
        'motoboy_id'  => $mb_id,
        'pedido_id'   => $pedido_id,
        'wc_order_id' => (int) ( $pedido->wc_order_id ?? 0 ),
        'tipo'        => $tipo,
        'valor'       => round( $valor, 2 ),
        'valor_pago'  => 0.00,
        'status'      => 'pendente',
    ] );
}, 15, 4 );

// ─── Saldo atual do motoboy ───────────────────────────────────────────────────

function sz_mbw_saldo( int $motoboy_id ): array {
    global $wpdb;
    $t = sz_mbw_tables();

    $pendente = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(valor),0) FROM {$t['ganhos']} WHERE motoboy_id=%d AND status='pendente'",
        $motoboy_id
    ) );
    $pago = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(valor),0) FROM {$t['ganhos']} WHERE motoboy_id=%d AND status='pago'",
        $motoboy_id
    ) );
    $qtd_entregas = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM {$t['ganhos']} WHERE motoboy_id=%d AND tipo='entrega' AND status='pendente'",
        $motoboy_id
    ) );
    $qtd_frustrados = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM {$t['ganhos']} WHERE motoboy_id=%d AND tipo='frustrado' AND status='pendente'",
        $motoboy_id
    ) );

    // Próxima sexta-feira
    $hoje  = new DateTime( 'now', new DateTimeZone( 'America/Sao_Paulo' ) );
    $dow   = (int) $hoje->format( 'N' ); // 1=seg … 5=sex … 7=dom
    $dias  = $dow <= 5 ? 5 - $dow : 5 + ( 7 - $dow );
    if ( $dias === 0 && $hoje->format( 'H' ) >= 18 ) $dias = 7; // sexta mas depois das 18h → próxima
    $sexta = ( clone $hoje )->modify( "+{$dias} days" )->format( 'd/m/Y' );

    return compact( 'pendente', 'pago', 'qtd_entregas', 'qtd_frustrados', 'sexta' );
}

// ─── Últimos ganhos ───────────────────────────────────────────────────────────

function sz_mbw_historico( int $motoboy_id, int $limit = 20 ): array {
    global $wpdb;
    $t = sz_mbw_tables();
    return $wpdb->get_results( $wpdb->prepare(
        "SELECT g.*, p.wc_order_id FROM {$t['ganhos']} g
         WHERE g.motoboy_id=%d ORDER BY g.created_at DESC LIMIT %d",
        $motoboy_id, $limit
    ), ARRAY_A ) ?: [];
}

// ─── Dados bancários ─────────────────────────────────────────────────────────

function sz_mbw_get_bancario( int $motoboy_id ): ?array {
    global $wpdb;
    $t = sz_mbw_tables();
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$t['bancario']} WHERE motoboy_id=%d ORDER BY is_default DESC, id DESC LIMIT 1",
        $motoboy_id
    ), ARRAY_A );
    return $row ?: null;
}

// ─── REST API para o PWA — rotas wallet registradas no bloco principal de rest-api.php ──

function sz_mbw_validate_cnpj( string $cnpj ): bool {
    $cnpj = preg_replace( '/\D+/', '', $cnpj );
    if ( strlen( $cnpj ) !== 14 || preg_match( '/^(\d)\1{13}$/', $cnpj ) ) return false;
    $w1 = [5,4,3,2,9,8,7,6,5,4,3,2];
    $w2 = [6,5,4,3,2,9,8,7,6,5,4,3,2];
    $sum = 0;
    for ( $i = 0; $i < 12; $i++ ) $sum += (int) $cnpj[$i] * $w1[$i];
    $d1 = $sum % 11 < 2 ? 0 : 11 - ( $sum % 11 );
    if ( (int) $cnpj[12] !== $d1 ) return false;
    $sum = 0;
    for ( $i = 0; $i < 13; $i++ ) $sum += (int) $cnpj[$i] * $w2[$i];
    $d2 = $sum % 11 < 2 ? 0 : 11 - ( $sum % 11 );
    return (int) $cnpj[13] === $d2;
}

function sz_mbw_rest_saldo( WP_REST_Request $r ): WP_REST_Response {
    $mb = sz_mb_get_motoboy_by_token();
    if ( ! $mb ) return new WP_REST_Response( ['ok'=>false], 401 );
    $saldo = sz_mbw_saldo( (int) $mb->id );
    return new WP_REST_Response( array_merge( ['ok'=>true], $saldo ) );
}

function sz_mbw_rest_historico( WP_REST_Request $r ): WP_REST_Response {
    $mb = sz_mb_get_motoboy_by_token();
    if ( ! $mb ) return new WP_REST_Response( ['ok'=>false], 401 );
    $hist = sz_mbw_historico( (int) $mb->id );
    $formatted = array_map( function( $g ) {
        return [
            'id'       => (int) $g['id'],
            'pedido'   => '#' . $g['wc_order_id'],
            'tipo'     => $g['tipo'],
            'valor'    => 'R$ ' . number_format( (float)$g['valor'], 2, ',', '.' ),
            'status'   => $g['status'],
            'data'     => $g['created_at'] ? sz_br_format( $g['created_at'], 'd/m/Y H:i' ) : '—',
        ];
    }, $hist );
    return new WP_REST_Response( ['ok'=>true, 'historico'=>$formatted] );
}

function sz_mbw_rest_get_bancario( WP_REST_Request $r ): WP_REST_Response {
    $mb = sz_mb_get_motoboy_by_token();
    if ( ! $mb ) return new WP_REST_Response( ['ok'=>false], 401 );
    $dados = sz_mbw_get_bancario( (int) $mb->id );
    return new WP_REST_Response( ['ok'=>true, 'bancario'=>$dados] );
}

function sz_mbw_rest_save_bancario( WP_REST_Request $r ): WP_REST_Response {
    $mb = sz_mb_get_motoboy_by_token();
    if ( ! $mb ) return new WP_REST_Response( ['ok'=>false,'msg'=>'Não autenticado.'], 401 );

    $holder      = sanitize_text_field( (string) $r->get_param('holder_name') );
    $cpf         = preg_replace('/\D/', '', (string) $r->get_param('holder_cpf') );
    $bank_name   = sanitize_text_field( (string) $r->get_param('bank_name') );
    $bank_code   = sanitize_text_field( (string) $r->get_param('bank_code') );
    $agency      = sanitize_text_field( (string) $r->get_param('agency') );
    $account     = sanitize_text_field( (string) $r->get_param('account_number') );
    $acc_type    = sanitize_key( (string) $r->get_param('account_type') );
    $pix_type    = sanitize_key( (string) $r->get_param('pix_type') );
    $pix_key     = sanitize_text_field( (string) $r->get_param('pix_key') );

    if ( strlen($holder) < 3 ) return new WP_REST_Response( ['ok'=>false,'msg'=>'Informe o nome completo.'], 422 );
    if ( strlen($cpf) !== 11 ) return new WP_REST_Response( ['ok'=>false,'msg'=>'CPF inválido.'], 422 );
    if ( strlen($pix_key) < 3 ) return new WP_REST_Response( ['ok'=>false,'msg'=>'Informe a chave PIX.'], 422 );
    if ( ! in_array($acc_type, ['corrente','poupanca','pagamento'], true) ) $acc_type = 'corrente';
    if ( ! in_array($pix_type, ['cpf','cnpj','email','telefone','aleatoria'], true) ) $pix_type = 'cpf';
    if ( $pix_type === 'cpf' && strlen( preg_replace('/\D/', '', $pix_key) ) !== 11 ) return new WP_REST_Response( ['ok'=>false,'msg'=>'Chave PIX CPF inválida.'], 422 );
    if ( $pix_type === 'cnpj' && ! sz_mbw_validate_cnpj( $pix_key ) ) return new WP_REST_Response( ['ok'=>false,'msg'=>'Chave PIX CNPJ inválida.'], 422 );

    global $wpdb;
    $t = sz_mbw_tables();
    $mb_id = (int) $mb->id;

    $existing = $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$t['bancario']} WHERE motoboy_id=%d LIMIT 1", $mb_id ) );
    $data = [
        'motoboy_id'   => $mb_id,
        'holder_name'  => $holder,
        'holder_cpf'   => $cpf,
        'bank_name'    => $bank_name,
        'bank_code'    => $bank_code,
        'agency'       => $agency,
        'account_number'=> $account,
        'account_type' => $acc_type,
        'pix_type'     => $pix_type,
        'pix_key'      => $pix_key,
        'is_default'   => 1,
    ];
    if ( $existing ) {
        $wpdb->update( $t['bancario'], $data, ['id'=>(int)$existing] );
    } else {
        $wpdb->insert( $t['bancario'], $data );
    }
    return new WP_REST_Response( ['ok'=>true,'msg'=>'Dados bancários salvos.'] );
}

// ─── Aba no admin de motoboy ─────────────────────────────────────────────────

add_filter( 'sz_motoboy_admin_tabs', function( array $tabs ): array {
    $tabs['wallet'] = 'Carteira';
    return $tabs;
} );

add_action( 'sz_motoboy_admin_tab_wallet', function() {
    global $wpdb;
    $mb_table = $wpdb->prefix . 'sz_motoboys';
    $t = sz_mbw_tables();

    // Salvar taxas globais
    if ( isset( $_POST['sz_mbw_save_taxas'] ) && check_admin_referer( 'sz_mbw_taxas' ) ) {
        update_option( 'sz_mbw_taxa_entrega',   (float) str_replace( ',', '.', $_POST['taxa_entrega'] ?? '18' ) );
        update_option( 'sz_mbw_taxa_frustrado', (float) str_replace( ',', '.', $_POST['taxa_frustrado'] ?? '5' ) );
        echo '<div class="notice notice-success is-dismissible"><p>Taxas salvas!</p></div>';
    }

    // Salvar taxa individual
    if ( isset( $_POST['sz_mbw_save_mb_taxa'] ) && check_admin_referer( 'sz_mbw_mb_taxa' ) ) {
        $mb_id = absint( $_POST['mb_id'] ?? 0 );
        if ( $mb_id > 0 ) {
            $v_ent = (float) str_replace( ',', '.', $_POST['mb_taxa_entrega'] ?? '0' );
            $v_fru = (float) str_replace( ',', '.', $_POST['mb_taxa_frustrado'] ?? '0' );
            if ( $v_ent > 0 ) update_option( 'sz_mbw_taxa_entrega_mb_' . $mb_id, $v_ent );
            else delete_option( 'sz_mbw_taxa_entrega_mb_' . $mb_id );
            if ( $v_fru > 0 ) update_option( 'sz_mbw_taxa_frustrado_mb_' . $mb_id, $v_fru );
            else delete_option( 'sz_mbw_taxa_frustrado_mb_' . $mb_id );
        }
        echo '<div class="notice notice-success is-dismissible"><p>Taxa individual salva!</p></div>';
    }

    // Marcar pagamento como pago
    if ( isset( $_POST['sz_mbw_pagar'] ) && check_admin_referer( 'sz_mbw_pagar' ) ) {
        $mb_id = absint( $_POST['mb_id_pagar'] ?? 0 );
        if ( $mb_id > 0 ) {
            $wpdb->update( $t['ganhos'], ['status'=>'pago'], ['motoboy_id'=>$mb_id,'status'=>'pendente'] );
            echo '<div class="notice notice-success is-dismissible"><p>Pagamento confirmado!</p></div>';
        }
    }

    $taxa_ent = number_format( (float) get_option( 'sz_mbw_taxa_entrega', 18 ), 2, ',', '.' );
    $taxa_fru = number_format( (float) get_option( 'sz_mbw_taxa_frustrado', 5 ), 2, ',', '.' );
    $motoboys = $wpdb->get_results( "SELECT id, nome FROM {$mb_table} WHERE ativo=1 ORDER BY nome" ) ?: [];
    ?>
    <div style="max-width:900px;margin-top:20px">
        <h2 style="margin-bottom:16px">Carteira dos Motoboys</h2><p style="color:#64748b;font-weight:700">Taxas ficam centralizadas em Financeiro COD &gt; Taxas de Entrega.</p>

        <!-- Taxas globais -->
        <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:20px;margin-bottom:20px">
            <h3 style="margin-bottom:14px">Taxas padrão centralizadas</h3><p style="color:#64748b">Use Financeiro COD &gt; Taxas de Entrega para alterar. Este bloco legado será ignorado na operação nova.</p>
            <form method="post">
                <?php wp_nonce_field('sz_mbw_taxas'); ?>
                <div style="display:grid;grid-template-columns:1fr 1fr auto;gap:14px;align-items:end">
                    <div>
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;margin-bottom:6px">Valor por entrega (R$)</label>
                        <input type="text" name="taxa_entrega" value="<?php echo esc_attr($taxa_ent); ?>" style="width:100%;height:38px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px">
                    </div>
                    <div>
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;margin-bottom:6px">Valor por frustrado (R$)</label>
                        <input type="text" name="taxa_frustrado" value="<?php echo esc_attr($taxa_fru); ?>" style="width:100%;height:38px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px">
                    </div>
                    <button type="submit" name="sz_mbw_save_taxas" value="1" class="button button-primary" style="height:38px">Salvar taxas</button>
                </div>
            </form>
        </div>

        <!-- Saldo por motoboy -->
        <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:20px;margin-bottom:20px">
            <h3 style="margin-bottom:14px">Saldo a pagar por motoboy</h3>
            <table class="widefat" style="border-radius:8px;overflow:hidden">
                <thead><tr>
                    <th>Motoboy</th>
                    <th>Entregas pend.</th>
                    <th>Frustrados pend.</th>
                    <th>Saldo pendente</th>
                    <th>Taxa entrega</th>
                    <th>Taxa frustrado</th>
                    <th>Ação</th>
                </tr></thead>
                <tbody>
                <?php foreach ( $motoboys as $mb ) :
                    $mb_id = (int) $mb->id;
                    $saldo = sz_mbw_saldo( $mb_id );
                    $t_ent = number_format( sz_mbw_get_taxa_entrega( $mb_id ), 2, ',', '.' );
                    $t_fru = number_format( sz_mbw_get_taxa_frustrado( $mb_id ), 2, ',', '.' );
                    $pendente_fmt = 'R$ ' . number_format( $saldo['pendente'], 2, ',', '.' );
                ?>
                <tr>
                    <td><strong><?php echo esc_html($mb->nome); ?></strong></td>
                    <td><?php echo (int) $saldo['qtd_entregas']; ?></td>
                    <td><?php echo (int) $saldo['qtd_frustrados']; ?></td>
                    <td><strong style="color:<?php echo $saldo['pendente'] > 0 ? '#16a34a' : '#6b7280'; ?>"><?php echo esc_html($pendente_fmt); ?></strong></td>
                    <td>R$ <?php echo esc_html($t_ent); ?></td>
                    <td>R$ <?php echo esc_html($t_fru); ?></td>
                    <td>
                        <?php if ( $saldo['pendente'] > 0 ) : ?>
                        <form method="post" style="display:inline">
                            <?php wp_nonce_field('sz_mbw_pagar'); ?>
                            <input type="hidden" name="mb_id_pagar" value="<?php echo $mb_id; ?>">
                            <button type="submit" name="sz_mbw_pagar" value="1" class="button" onclick="return confirm('Confirmar pagamento de <?php echo esc_js($pendente_fmt); ?> para <?php echo esc_js($mb->nome); ?>?')">
                                ✓ Pagar
                            </button>
                        </form>
                        <?php else : ?>
                        <span style="color:#9ca3af;font-size:var(--sz-text-meta)">Sem pendências</span>
                        <?php endif; ?>
                    </td>
                </tr>
                <?php endforeach; ?>
                </tbody>
            </table>
        </div>

        <!-- Taxa individual por motoboy -->
        <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:20px">
            <h3 style="margin-bottom:14px">Taxas individuais centralizadas</h3><p style="color:#64748b">Use Financeiro COD &gt; Taxas de Entrega para alterar por motoboy.</p>
            <form method="post">
                <?php wp_nonce_field('sz_mbw_mb_taxa'); ?>
                <div style="display:grid;grid-template-columns:1.5fr 1fr 1fr auto;gap:14px;align-items:end;margin-bottom:14px">
                    <div>
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;margin-bottom:6px">Motoboy</label>
                        <select name="mb_id" style="width:100%;height:38px;border:1px solid #d1d5db;border-radius:8px;padding:0 8px">
                            <?php foreach ( $motoboys as $mb ) : ?>
                            <option value="<?php echo (int)$mb->id; ?>"><?php echo esc_html($mb->nome); ?></option>
                            <?php endforeach; ?>
                        </select>
                    </div>
                    <div>
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;margin-bottom:6px">Entrega (R$) — 0 = padrão</label>
                        <input type="text" name="mb_taxa_entrega" value="0" style="width:100%;height:38px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px">
                    </div>
                    <div>
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:700;margin-bottom:6px">Frustrado (R$) — 0 = padrão</label>
                        <input type="text" name="mb_taxa_frustrado" value="0" style="width:100%;height:38px;border:1px solid #d1d5db;border-radius:8px;padding:0 10px">
                    </div>
                    <button type="submit" name="sz_mbw_save_mb_taxa" value="1" class="button button-primary" style="height:38px">Salvar</button>
                </div>
                <p style="color:#6b7280;font-size:var(--sz-text-meta)">Deixe 0 para usar o valor padrão global. Taxa individual tem prioridade.</p>
            </form>
        </div>
    </div>
    <?php
} );

// ─── v310: sincronização/reconstrução de carteira e fechamento ─────────────
if ( ! function_exists( 'sz_mbw_wc_meta_first' ) ) {
function sz_mbw_wc_meta_first( int $wc_order_id, array $keys, $default = '' ) {
    if ( $wc_order_id <= 0 || empty( $keys ) ) { return $default; }
    global $wpdb;
    $placeholders = implode( ',', array_fill( 0, count( $keys ), '%s' ) );
    $sql = "SELECT meta_value FROM {$wpdb->prefix}wc_orders_meta WHERE order_id=%d AND meta_key IN ($placeholders) ORDER BY FIELD(meta_key,$placeholders) LIMIT 1";
    $args = array_merge( [ $wc_order_id ], $keys, $keys );
    $value = $wpdb->get_var( $wpdb->prepare( $sql, $args ) );
    return ( $value !== null && $value !== '' ) ? $value : $default;
}}

if ( ! function_exists( 'sz_mbw_effective_motoboy_id' ) ) {
function sz_mbw_effective_motoboy_id( object $pedido ): int {
    // v335 — fonte de verdade para carteira: pedido WooCommerce/metas do pedido.
    // A tabela operacional acompanha o pedido; não pode manter motoboy antigo após transferência.
    $wc_order_id = (int) ( $pedido->wc_order_id ?? 0 );
    $from_order = (int) sz_mbw_wc_meta_first( $wc_order_id, [
        '_senderzz_motoboy_baixa_motoboy_id',
        '_senderzz_motoboy_responsavel_id',
        '_senderzz_motoboy_entregador_id',
        '_senderzz_motoboy_id',
        '_sz_motoboy_id',
        '_motoboy_user_id',
        '_senderzz_motoboy_assigned_id',
        '_senderzz_motoboy_atribuido_id',
    ], 0 );
    if ( $from_order > 0 ) { return $from_order; }

    $baixa = isset( $pedido->baixa_motoboy_id ) ? (int) $pedido->baixa_motoboy_id : 0;
    if ( $baixa > 0 ) { return $baixa; }

    $operacional = isset( $pedido->motoboy_id ) ? (int) $pedido->motoboy_id : 0;
    if ( $operacional > 0 ) { return $operacional; }

    return 0;
}}

if ( ! function_exists( 'sz_mbw_sync_pedido_motoboy_from_meta' ) ) {
function sz_mbw_sync_pedido_motoboy_from_meta( object $pedido ): int {
    global $wpdb;
    $p = $wpdb->prefix;
    $mb_id = sz_mbw_effective_motoboy_id( $pedido );
    if ( $mb_id <= 0 ) { return 0; }

    $update = [ 'motoboy_id' => $mb_id ];
    // Para pedido já entregue/frustrado, a carteira deve seguir o responsável atual da baixa.
    if ( in_array( (string) ( $pedido->status ?? '' ), [ 'entregue', 'frustrado' ], true ) ) {
        $update['baixa_motoboy_id'] = $mb_id;
    }
    $wpdb->update( $p . 'sz_motoboy_pedidos', $update, [ 'id' => (int) $pedido->id ] );
    return $mb_id;
}}



if ( ! function_exists( 'sz_mbw_backfill_operational_from_wc_meta' ) ) {
function sz_mbw_backfill_operational_from_wc_meta(): int {
    global $wpdb;
    $p = $wpdb->prefix;

    // v314: pedidos antigos/baixados fora do app podiam estar como wc-completo no Woo,
    // mas sem linha financeira completa em sz_motoboy_pedidos. A conciliação usa a tabela
    // operacional, então reconstruímos/atualizamos por meta do pedido antes de calcular carteira.
    $wc_orders = $p . 'wc_orders';
    $has_wc_orders = ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $wc_orders ) ) === $wc_orders );

    $join_orders = $has_wc_orders ? "LEFT JOIN {$wc_orders} wo ON wo.id = dm.order_id" : '';
    $status_cond = $has_wc_orders
        ? "OR wo.status IN ('wc-completo','wc-completed','wc-frustrado','wc-cancelled')"
        : '';

    $order_ids = $wpdb->get_col(
        "SELECT DISTINCT dm.order_id
           FROM {$p}wc_orders_meta dm
           {$join_orders}
          WHERE (
                (dm.meta_key = '_senderzz_delivery_mode' AND dm.meta_value = 'motoboy')
             OR dm.meta_key IN (
                    '_senderzz_motoboy_status','_senderzz_motoboy_flow_status',
                    '_senderzz_motoboy_id','_sz_motoboy_id','_motoboy_user_id',
                    '_senderzz_motoboy_responsavel_id','_senderzz_motoboy_entregador_id',
                    '_senderzz_motoboy_baixa_motoboy_id','_senderzz_motoboy_pgto_total',
                    '_senderzz_motoboy_pgto_dinheiro','_senderzz_motoboy_pgto_pix','_senderzz_motoboy_pgto_cartao'
                )
            )
            AND (
                EXISTS (
                    SELECT 1 FROM {$p}wc_orders_meta st
                     WHERE st.order_id = dm.order_id
                       AND st.meta_key IN ('_senderzz_motoboy_status','_senderzz_motoboy_flow_status')
                       AND st.meta_value IN ('entregue','frustrado','completo','completed')
                )
                {$status_cond}
            )"
    );

    $count = 0;
    foreach ( $order_ids as $order_id ) {
        $order_id = (int) $order_id;
        if ( $order_id <= 0 ) { continue; }

        $meta_rows = $wpdb->get_results( $wpdb->prepare(
            "SELECT meta_key, meta_value FROM {$p}wc_orders_meta WHERE order_id=%d",
            $order_id
        ) );
        if ( ! $meta_rows ) { continue; }
        $meta = [];
        foreach ( $meta_rows as $mr ) { $meta[ (string) $mr->meta_key ] = (string) $mr->meta_value; }
        $get = static function( array $keys, $default = '' ) use ( $meta ) {
            foreach ( $keys as $k ) {
                if ( isset( $meta[$k] ) && $meta[$k] !== '' ) { return $meta[$k]; }
            }
            return $default;
        };

        $wc_status = '';
        if ( $has_wc_orders ) {
            $wc_status = (string) $wpdb->get_var( $wpdb->prepare( "SELECT status FROM {$wc_orders} WHERE id=%d LIMIT 1", $order_id ) );
        }

        $flow = strtolower( (string) $get( [ '_senderzz_motoboy_status', '_senderzz_motoboy_flow_status' ], '' ) );
        if ( in_array( $flow, [ 'frustrado' ], true ) || $wc_status === 'wc-frustrado' ) {
            $status = 'frustrado';
        } elseif ( in_array( $flow, [ 'entregue', 'completo', 'completed' ], true ) || in_array( $wc_status, [ 'wc-completo', 'wc-completed' ], true ) ) {
            $status = 'entregue';
        } else {
            continue;
        }

        $motoboy_id = (int) $get( [
            '_senderzz_motoboy_responsavel_id',
            '_senderzz_motoboy_entregador_id',
            '_senderzz_motoboy_baixa_motoboy_id',
            '_senderzz_motoboy_id',
            '_sz_motoboy_id',
            '_motoboy_user_id',
            '_senderzz_motoboy_assigned_id',
            '_senderzz_motoboy_atribuido_id',
        ], 0 );
        if ( $motoboy_id <= 0 ) { continue; }

        $order = function_exists( 'wc_get_order' ) ? wc_get_order( $order_id ) : null;
        $valor_pedido = (float) $get( [ '_senderzz_motoboy_valor_pedido', '_senderzz_offer_value', '_order_total' ], 0 );
        if ( $valor_pedido <= 0 && $order ) { $valor_pedido = (float) $order->get_total(); }

        $dinheiro = (float) str_replace( ',', '.', $get( [ '_senderzz_motoboy_pgto_dinheiro' ], 0 ) );
        $pix      = (float) str_replace( ',', '.', $get( [ '_senderzz_motoboy_pgto_pix' ], 0 ) );
        $cartao   = (float) str_replace( ',', '.', $get( [ '_senderzz_motoboy_pgto_cartao' ], 0 ) );
        $total_pg = $dinheiro + $pix + $cartao;

        if ( $status === 'entregue' && $total_pg <= 0 ) {
            $total_meta = (float) str_replace( ',', '.', $get( [ '_senderzz_motoboy_pgto_total' ], 0 ) );
            if ( $total_meta <= 0 ) { $total_meta = $valor_pedido; }
            if ( $total_meta > 0 ) {
                $label = strtolower( (string) $get( [ '_senderzz_cod_payment_method_label', '_payment_method_title', '_payment_method' ], '' ) );
                if ( str_contains( $label, 'pix' ) ) {
                    $pix = $total_meta;
                } elseif ( str_contains( $label, 'cart' ) || str_contains( $label, 'card' ) ) {
                    $cartao = $total_meta;
                } else {
                    // Fallback para pedidos antigos entregues sem detalhamento de baixa:
                    // não deixar o pedido sumir da conciliação nem gerar fechamento zerado.
                    $dinheiro = $total_meta;
                }
            }
        }

        $when = $get( [
            '_senderzz_motoboy_baixa_at',
            '_senderzz_motoboy_entregue_at',
            '_senderzz_motoboy_entrega_confirmed_at',
            '_senderzz_motoboy_entrega_data_hora_br',
            '_senderzz_motoboy_frustrado_at',
        ], '' );
        if ( $when === '' ) {
            $delivery_date = $get( [ '_sz_delivery_date' ], '' );
            if ( preg_match( '/^\d{4}-\d{2}-\d{2}$/', $delivery_date ) ) {
                $when = $delivery_date . ' 12:00:00';
            }
        }
        if ( $when === '' || $when === '0000-00-00 00:00:00' ) { $when = current_time( 'mysql' ); }

        $dest_nome = $get( [ '_shipping_address_index', '_billing_address_index' ], '' );
        if ( $order ) {
            $dest_nome = trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() );
            if ( $dest_nome === '' ) { $dest_nome = trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() ); }
        }
        $cep = preg_replace( '/\D/', '', $get( [ '_senderzz_destination_postcode', '_shipping_postcode', '_billing_postcode' ], '' ) );
        if ( $cep === '' && $order ) { $cep = preg_replace( '/\D/', '', $order->get_shipping_postcode() ?: $order->get_billing_postcode() ); }
        if ( $cep === '' ) { $cep = '00000000'; }

        $existing_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$p}sz_motoboy_pedidos WHERE wc_order_id=%d LIMIT 1",
            $order_id
        ) );

        $row = [
            'wc_order_id'       => $order_id,
            'cd_id'             => 1,
            'zona_id'           => 0,
            'motoboy_id'        => $motoboy_id,
            'status'            => $status,
            'dest_nome'         => $dest_nome,
            'dest_cep'          => $cep,
            'valor_pedido'      => round( $valor_pedido, 2 ),
            'pgto_dinheiro'     => round( $dinheiro, 2 ),
            'pgto_pix'          => round( $pix, 2 ),
            'pgto_cartao'       => round( $cartao, 2 ),
            'baixa_motoboy_id'  => $motoboy_id,
            'baixa_at'          => $when,
            'updated_at'        => current_time( 'mysql' ),
        ];
        if ( $status === 'entregue' ) {
            $row['ts_entregue'] = $when;
        } else {
            $row['ts_frustrado'] = $when;
        }

        if ( $existing_id ) {
            $wpdb->update( $p . 'sz_motoboy_pedidos', $row, [ 'id' => $existing_id ] );
        } else {
            $row['created_at'] = $when;
            $wpdb->insert( $p . 'sz_motoboy_pedidos', $row );
        }
        $count++;
    }

    return $count;
}}

if ( ! function_exists( 'sz_mbw_sync_all_data' ) ) {
function sz_mbw_sync_all_data(): array {
    sz_mbw_install();
    global $wpdb;
    $p = $wpdb->prefix;
    $t = sz_mbw_tables();

    $backfilled = sz_mbw_backfill_operational_from_wc_meta();

    // Limpa lançamentos pendentes obsoletos antes de reconstruir.
    // Regra: um pedido só pode ter UM lançamento ativo, seguindo o status atual
    // e o motoboy responsável atual. Lançamentos pagos são preservados como histórico.
    $wpdb->query(
        "DELETE g FROM {$t['ganhos']} g
          LEFT JOIN {$p}sz_motoboy_pedidos ped ON ped.id = g.pedido_id
         WHERE g.status = 'pendente'
           AND (
                ped.id IS NULL
             OR ped.status NOT IN ('entregue','frustrado')
             OR (g.tipo = 'entrega' AND ped.status <> 'entregue')
             OR (g.tipo = 'frustrado' AND ped.status <> 'frustrado')
             OR g.motoboy_id <> COALESCE(NULLIF(ped.baixa_motoboy_id,0), ped.motoboy_id)
           )"
    );

    $pedidos = $wpdb->get_results(
        "SELECT * FROM {$p}sz_motoboy_pedidos WHERE status IN ('entregue','frustrado')"
    );

    $ganhos = 0;
    foreach ( $pedidos as $pedido ) {
        $mb_id = sz_mbw_sync_pedido_motoboy_from_meta( $pedido );
        if ( $mb_id <= 0 ) { continue; }

        $tipo  = $pedido->status === 'frustrado' ? 'frustrado' : 'entrega';
        // Taxa de frustrado é a que foi gravada no pedido na data da baixa.
        // Não recalcula pedidos antigos quando a configuração muda.
        $valor = $tipo === 'entrega' ? sz_mbw_get_taxa_entrega( $mb_id ) : (float) ( $pedido->valor_taxa_frustrado ?? 0 );
        $created_at = $pedido->baixa_at ?: ( $tipo === 'entrega' ? ( $pedido->ts_entregue ?: $pedido->updated_at ) : ( $pedido->ts_frustrado ?: $pedido->updated_at ) );
        if ( ! $created_at || $created_at === '0000-00-00 00:00:00' ) { $created_at = current_time( 'mysql' ); }

        $exists = $wpdb->get_row( $wpdb->prepare(
            "SELECT id,status FROM {$t['ganhos']} WHERE pedido_id=%d AND tipo=%s LIMIT 1",
            (int) $pedido->id, $tipo
        ) );

        $row = [
            'motoboy_id'  => $mb_id,
            'pedido_id'   => (int) $pedido->id,
            'wc_order_id' => (int) $pedido->wc_order_id,
            'tipo'        => $tipo,
            'valor'       => round( (float) $valor, 2 ),
            'created_at'  => $created_at,
        ];

        if ( $exists ) {
            $wpdb->update( $t['ganhos'], $row, [ 'id' => (int) $exists->id ], [ '%d','%d','%d','%s','%f','%s' ], [ '%d' ] );
        } else {
            $row['status'] = 'pendente';
            $wpdb->insert( $t['ganhos'], $row, [ '%d','%d','%d','%s','%f','%s','%s' ] );
        }
        $ganhos++;
    }

    // Recalcula fechamento diário por motoboy com base nos pedidos, não em cache antigo.
    $rows = $wpdb->get_results(
        "SELECT
            COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) AS motoboy_id,
            COALESCE(NULLIF(cd_id,0),1) AS cd_id,
            DATE(COALESCE(baixa_at, ts_entregue, ts_frustrado, updated_at, created_at)) AS data_fechamento,
            COUNT(*) AS total_pedidos,
            SUM(CASE WHEN status='entregue' THEN 1 ELSE 0 END) AS total_entregues,
            SUM(CASE WHEN status='frustrado' THEN 1 ELSE 0 END) AS total_frustrados,
            SUM(CASE WHEN status='entregue' THEN pgto_dinheiro ELSE 0 END) AS total_dinheiro,
            SUM(CASE WHEN status='entregue' THEN pgto_pix ELSE 0 END) AS total_pix,
            SUM(CASE WHEN status='entregue' THEN pgto_cartao ELSE 0 END) AS total_cartao,
            SUM(CASE WHEN status='entregue' THEN (pgto_dinheiro + pgto_pix + pgto_cartao) ELSE 0 END) AS total_a_repassar
         FROM {$p}sz_motoboy_pedidos
         WHERE status IN ('entregue','frustrado')
           AND COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) IS NOT NULL
           AND COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) > 0
         GROUP BY motoboy_id, cd_id, data_fechamento"
    );

    // Remove fechamentos fantasmas/duplicados que não possuem mais entrega atual.
    // Isso corrige casos em que o pedido foi reatribuído depois da baixa: o fechamento
    // antigo do motoboy anterior deve sumir, pois a conciliação é única por pedido.
    $wpdb->query(
        "DELETE f FROM {$p}sz_motoboy_fechamento f
          LEFT JOIN (
              SELECT
                  COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) AS motoboy_id,
                  DATE(COALESCE(baixa_at, ts_entregue, updated_at, created_at)) AS data_fechamento,
                  COUNT(*) AS qtd
                FROM {$p}sz_motoboy_pedidos
               WHERE status IN ('entregue','frustrado')
                 AND COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) IS NOT NULL
                 AND COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) > 0
               GROUP BY motoboy_id, data_fechamento
          ) cur ON cur.motoboy_id = f.motoboy_id AND cur.data_fechamento = f.data_fechamento
         WHERE cur.qtd IS NULL"
    );

    $fech = 0;
    foreach ( $rows as $r ) {
        if ( ! $r->data_fechamento || $r->data_fechamento === '0000-00-00' ) { continue; }
        $exists_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$p}sz_motoboy_fechamento WHERE motoboy_id=%d AND data_fechamento=%s LIMIT 1",
            (int) $r->motoboy_id, $r->data_fechamento
        ) );
        $data = [
            'motoboy_id'       => (int) $r->motoboy_id,
            'cd_id'            => (int) $r->cd_id,
            'data_fechamento'  => $r->data_fechamento,
            'total_pedidos'    => (int) $r->total_pedidos,
            'total_entregues'  => (int) $r->total_entregues,
            'total_frustrados' => (int) $r->total_frustrados,
            'total_dinheiro'   => round( (float) $r->total_dinheiro, 2 ),
            'total_pix'        => round( (float) $r->total_pix, 2 ),
            'total_cartao'     => round( (float) $r->total_cartao, 2 ),
            'total_a_repassar' => round( (float) $r->total_a_repassar, 2 ),
            'updated_at'       => current_time( 'mysql' ),
        ];
        if ( $exists_id ) {
            $wpdb->update( $p . 'sz_motoboy_fechamento', $data, [ 'id' => $exists_id ] );
        } else {
            $data['created_at'] = current_time( 'mysql' );
            $wpdb->insert( $p . 'sz_motoboy_fechamento', $data );
        }
        $fech++;
    }

    update_option( 'sz_mbw_last_sync_v335', current_time( 'mysql' ), false );
    return [ 'backfilled' => $backfilled, 'ganhos' => $ganhos, 'fechamentos' => $fech ];
}}

// v315: correção pontual dos frustrados antigos informados no suporte.
// Regra: 1380/1381/1394 devem ficar um para cada motoboy ativo, excluindo Lucas Teste.
// 1381 = Guilherme SJC (ID 4), 1394 = Ailson Pimentel (ID 3), 1380 = Alan (ID 1).
if ( ! function_exists( 'sz_mbw_fix_legacy_frustrados_v315' ) ) {
function sz_mbw_fix_legacy_frustrados_v315(): void {
    global $wpdb;
    $p = $wpdb->prefix;
    $map = [ 1380 => 1, 1381 => 4, 1394 => 3 ];
    foreach ( $map as $wc_order_id => $motoboy_id ) {
        $pedido = $wpdb->get_row( $wpdb->prepare(
            "SELECT id,status FROM {$p}sz_motoboy_pedidos WHERE wc_order_id=%d LIMIT 1",
            $wc_order_id
        ) );
        if ( ! $pedido || (string) $pedido->status !== 'frustrado' ) { continue; }
        $nome = (string) $wpdb->get_var( $wpdb->prepare( "SELECT nome FROM {$p}sz_motoboys WHERE id=%d", $motoboy_id ) );
        $tel  = (string) $wpdb->get_var( $wpdb->prepare( "SELECT telefone FROM {$p}sz_motoboys WHERE id=%d", $motoboy_id ) );
        $wpdb->update(
            $p . 'sz_motoboy_pedidos',
            [ 'motoboy_id' => $motoboy_id, 'baixa_motoboy_id' => $motoboy_id, 'updated_at' => current_time( 'mysql' ) ],
            [ 'id' => (int) $pedido->id ]
        );
        $wpdb->update(
            $p . 'sz_motoboy_ganhos',
            [ 'motoboy_id' => $motoboy_id ],
            [ 'pedido_id' => (int) $pedido->id, 'tipo' => 'frustrado', 'status' => 'pendente' ]
        );
        foreach ( [
            '_senderzz_motoboy_id','_sz_motoboy_id','_motoboy_user_id',
            '_senderzz_motoboy_responsavel_id','_senderzz_motoboy_entregador_id','_senderzz_motoboy_baixa_motoboy_id'
        ] as $k ) {
            $wpdb->query( $wpdb->prepare(
                "DELETE FROM {$p}wc_orders_meta WHERE order_id=%d AND meta_key=%s", $wc_order_id, $k
            ) );
            $wpdb->insert( $p . 'wc_orders_meta', [ 'order_id' => $wc_order_id, 'meta_key' => $k, 'meta_value' => (string) $motoboy_id ] );
        }
        foreach ( [
            '_senderzz_motoboy_name','_sz_motoboy_name','_motoboy_name',
            '_senderzz_motoboy_responsavel_nome','_senderzz_motoboy_entregador_nome','_senderzz_motoboy_baixa_motoboy_nome'
        ] as $k ) {
            $wpdb->query( $wpdb->prepare(
                "DELETE FROM {$p}wc_orders_meta WHERE order_id=%d AND meta_key=%s", $wc_order_id, $k
            ) );
            $wpdb->insert( $p . 'wc_orders_meta', [ 'order_id' => $wc_order_id, 'meta_key' => $k, 'meta_value' => $nome ] );
        }
        if ( $tel !== '' ) {
            $wpdb->query( $wpdb->prepare(
                "DELETE FROM {$p}wc_orders_meta WHERE order_id=%d AND meta_key='_senderzz_motoboy_telefone'", $wc_order_id
            ) );
            $wpdb->insert( $p . 'wc_orders_meta', [ 'order_id' => $wc_order_id, 'meta_key' => '_senderzz_motoboy_telefone', 'meta_value' => $tel ] );
        }
    }
}}


// v315: roda uma reconstrução única após atualização do plugin para corrigir saldos antigos.
add_action( 'admin_init', function() {
    if ( ! current_user_can( 'manage_options' ) ) { return; }
    if ( get_option( 'sz_mbw_auto_sync_version' ) === 'v315' ) { return; }
    if ( function_exists( 'sz_mbw_fix_legacy_frustrados_v315' ) ) {
        sz_mbw_fix_legacy_frustrados_v315();
    }
    if ( function_exists( 'sz_mbw_sync_all_data' ) ) {
        sz_mbw_sync_all_data();
        update_option( 'sz_mbw_auto_sync_version', 'v315', false );
    }
}, 25 );

// v329: reconstrói novamente usando a regra ampliada de fonte única por pedido.
// Captura pedidos COD/motoboy antigos que não tinham _senderzz_delivery_mode=motoboy,
// mas tinham metas de motoboy ou status operacional de entrega/frustração.
add_action( 'admin_init', function() {
    if ( ! current_user_can( 'manage_options' ) ) { return; }
    if ( get_option( 'sz_mbw_auto_sync_version_v329' ) === 'done' ) { return; }
    if ( function_exists( 'sz_mbw_sync_all_data' ) ) {
        sz_mbw_sync_all_data();
        update_option( 'sz_mbw_auto_sync_version_v329', 'done', false );
    }
}, 26 );

if ( ! function_exists( 'sz_mbw_mark_pending_paid' ) ) {
function sz_mbw_mark_pending_paid( int $motoboy_id ): bool {
    global $wpdb;
    $t = sz_mbw_tables();
    $total = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(valor),0) FROM {$t['ganhos']} WHERE motoboy_id=%d AND status='pendente'",
        $motoboy_id
    ) );
    if ( $total <= 0 ) { return false; }
    $wpdb->update( $t['ganhos'], [ 'status' => 'pago' ], [ 'motoboy_id' => $motoboy_id, 'status' => 'pendente' ] );
    $wpdb->insert( $t['pagto'], [
        'motoboy_id'     => $motoboy_id,
        'valor_total'    => round( $total, 2 ),
        'data_pagamento' => current_time( 'Y-m-d' ),
        'status'         => 'pago',
        'obs'            => 'Marcado como pago pelo painel Senderzz.',
    ] );
    return true;
}}

add_action( 'init', function() {
    if ( get_option( 'sz_mbw_sync_v312_done' ) === 'yes' ) { return; }
    if ( function_exists( 'sz_mbw_sync_all_data' ) ) {
        sz_mbw_sync_all_data();
        update_option( 'sz_mbw_sync_v312_done', 'yes', false );
    }
}, 60 );
