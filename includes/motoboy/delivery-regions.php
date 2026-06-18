<?php
/**
 * delivery-regions.php
 * Regiões de entrega com cutoff por dia/hora e lista de cidades.
 *
 * Regra: quando o cliente está em uma cidade mapeada, as datas disponíveis
 * no seletor do checkout respeitam o cutoff dessa região.
 *
 * Exemplo:
 *   Região "Baixada Santista" → cidades: Santos, Cubatão, São Vicente, Praia Grande
 *   Cutoff: quinta-feira 13:00 → entrega: sexta-feira
 */
defined( 'ABSPATH' ) || exit;

// ── Instalação da tabela ─────────────────────────────────────────────────────
function sz_dr_install(): void {
    global $wpdb;
    $charset = $wpdb->get_charset_collate();
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';

    dbDelta( "CREATE TABLE IF NOT EXISTS {$wpdb->prefix}sz_delivery_regions (
        id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        nome            VARCHAR(100)    NOT NULL,
        cutoff_dow      TINYINT         NOT NULL DEFAULT 4 COMMENT '0=Dom,1=Seg,2=Ter,3=Qua,4=Qui,5=Sex,6=Sáb',
        cutoff_hour     TINYINT         NOT NULL DEFAULT 13 COMMENT 'Hora do cutoff (0-23)',
        cutoff_minute   TINYINT         NOT NULL DEFAULT 0,
        delivery_offset TINYINT         NOT NULL DEFAULT 1 COMMENT 'Dias após cutoff para entrega (normalmente 1)',
        delivery_dow    TINYINT         NOT NULL DEFAULT 5 COMMENT 'Dia da semana da entrega (0=Dom...6=Sáb)',
        cidades         TEXT            NOT NULL COMMENT 'JSON array de nomes de cidades (lowercase normalizado)',
        ativo           TINYINT(1)      NOT NULL DEFAULT 1,
        created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id)
    ) $charset;" );

    if ( get_option( 'sz_dr_version' ) !== '2' ) {
        sz_dr_seed_defaults();
        sz_dr_ensure_saturday_region();
        update_option( 'sz_dr_version', '2' );
    }
}
add_action( 'init', function() {
    if ( get_option( 'sz_dr_version' ) !== '2' ) sz_dr_install();
}, 4 );

// ── Seed: regiões iniciais ───────────────────────────────────────────────────
function sz_dr_seed_defaults(): void {
    global $wpdb;
    $t = $wpdb->prefix . 'sz_delivery_regions';
    if ( (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$t}" ) > 0 ) return;

    $regions = [
        [
            'nome'            => 'Baixada Santista',
            'cutoff_dow'      => 4, // quinta
            'cutoff_hour'     => 13,
            'cutoff_minute'   => 0,
            'delivery_dow'    => 5, // sexta
            'cidades'         => [ 'santos', 'cubatao', 'sao vicente', 'praia grande', 'guaruja', 'bertioga', 'itanhaem', 'mongagua', 'peruibe' ],
        ],
        [
            'nome'            => 'Vale do Paraíba',
            'cutoff_dow'      => 5, // sexta
            'cutoff_hour'     => 13,
            'cutoff_minute'   => 0,
            'delivery_dow'    => 6, // sábado
            'cidades'         => [ 'sao jose dos campos', 'taubate', 'jacarei', 'pindamonhangaba', 'caçapava', 'cacapava', 'guaratingueta', 'lorena', 'aparecida', 'cruzeiro', 'ubatuba', 'caraguatatuba', 'ilhabela', 'sao sebastiao' ],
        ],
    ];

    foreach ( $regions as $r ) {
        $wpdb->insert( $t, [
            'nome'            => $r['nome'],
            'cutoff_dow'      => $r['cutoff_dow'],
            'cutoff_hour'     => $r['cutoff_hour'],
            'cutoff_minute'   => $r['cutoff_minute'],
            'delivery_offset' => 1,
            'delivery_dow'    => $r['delivery_dow'],
            'cidades'         => wp_json_encode( $r['cidades'] ),
            'ativo'           => 1,
        ] );
    }
}

/**
 * Região fixa semanal de sábado.
 * Blindagem: garante que cidades como Jundiaí nunca caiam na regra padrão diária,
 * mesmo em instalações antigas onde a tabela já existia antes desta lista.
 */
function sz_dr_weekly_saturday_cities(): array {
    return [
        'mairipora', 'franco da rocha', 'francisco morato', 'cajamar', 'caieiras', 'jundiai',
        'santos', 'cubatao', 'praia grande', 'sao vicente',
        'jacarei', 'sao jose dos campos', 'taubate', 'pindamonhangaba',
    ];
}

function sz_dr_ensure_saturday_region(): void {
    global $wpdb;
    $t = $wpdb->prefix . 'sz_delivery_regions';
    $cities = sz_dr_weekly_saturday_cities();
    $existing = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$t} WHERE nome = %s LIMIT 1", 'Semanal Sábado' ), ARRAY_A );
    $data = [
        'nome'            => 'Semanal Sábado',
        'cutoff_dow'      => 5, // sexta-feira
        'cutoff_hour'     => 13,
        'cutoff_minute'   => 0,
        'delivery_offset' => 1,
        'delivery_dow'    => 6, // sábado
        'cidades'         => wp_json_encode( $cities ),
        'ativo'           => 1,
    ];
    if ( $existing ) {
        $wpdb->update( $t, $data, [ 'id' => (int) $existing['id'] ] );
    } else {
        $wpdb->insert( $t, $data );
    }
}
add_action( 'init', 'sz_dr_ensure_saturday_region', 6 );

// ── Helpers ──────────────────────────────────────────────────────────────────
function sz_dr_normalize( string $s ): string {
    $s = mb_strtolower( trim( $s ), 'UTF-8' );
    $s = preg_replace( '/[áàãâä]/u', 'a', $s );
    $s = preg_replace( '/[éèêë]/u',  'e', $s );
    $s = preg_replace( '/[íìîï]/u',  'i', $s );
    $s = preg_replace( '/[óòõôö]/u', 'o', $s );
    $s = preg_replace( '/[úùûü]/u',  'u', $s );
    $s = preg_replace( '/[ç]/u',     'c', $s );
    return (string) $s;
}

/** Retorna a região ativa para uma cidade, ou null se não mapeada. */
function sz_dr_get_region_for_city( string $city ): ?array {
    global $wpdb;
    $city_n = sz_dr_normalize( $city );
    if ( ! $city_n ) return null;

    // Cidades de atendimento semanal aos sábados têm precedência sobre regiões antigas
    // que possam ter sido semeadas antes da regra operacional atual.
    if ( function_exists( 'sz_dr_weekly_saturday_cities' ) && in_array( $city_n, sz_dr_weekly_saturday_cities(), true ) ) {
        $sat = $wpdb->get_row( $wpdb->prepare(
            "SELECT * FROM {$wpdb->prefix}sz_delivery_regions WHERE ativo = 1 AND nome = %s LIMIT 1",
            'Semanal Sábado'
        ), ARRAY_A );
        if ( $sat ) return $sat;
    }

    $rows = $wpdb->get_results(
        "SELECT * FROM {$wpdb->prefix}sz_delivery_regions WHERE ativo = 1",
        ARRAY_A
    ) ?: [];

    foreach ( $rows as $row ) {
        $cities = json_decode( $row['cidades'], true ) ?: [];
        foreach ( $cities as $c ) {
            if ( sz_dr_normalize( $c ) === $city_n ) return $row;
        }
    }
    return null;
}

/**
 * Dado uma cidade, retorna as datas de entrega disponíveis respeitando
 * o cutoff da região. Se cidade não mapeada, usa regra padrão (21h, dia seguinte).
 */
function sz_dr_next_delivery_dates( string $city, int $qty = 3 ): array {
    $region = sz_dr_get_region_for_city( $city );

    try {
        $tz  = new DateTimeZone( 'America/Sao_Paulo' );
        $now = new DateTime( 'now', $tz );
    } catch ( Exception $e ) {
        return sz_mb_next_delivery_dates( $qty ); // fallback
    }

    $dow_now  = (int) $now->format( 'w' ); // 0=Dom
    $hour_now = (int) $now->format( 'G' );
    $min_now  = (int) $now->format( 'i' );

    if ( ! $region ) {
        // Sem região especial: deixa o chamador usar a regra padrão.
        // Não chama sz_mb_next_delivery_dates() aqui para evitar recursão quando a cidade vem no POST.
        return [];
    }

    $cutoff_dow  = (int) $region['cutoff_dow'];
    $cutoff_h    = (int) $region['cutoff_hour'];
    $cutoff_m    = (int) $region['cutoff_minute'];
    $delivery_dow = (int) $region['delivery_dow'];

    // Passou do cutoff desta semana?
    $past_cutoff = false;
    if ( $dow_now === $cutoff_dow ) {
        $past_cutoff = ( $hour_now > $cutoff_h ) || ( $hour_now === $cutoff_h && $min_now >= $cutoff_m );
    } elseif ( $dow_now > $cutoff_dow ) {
        $past_cutoff = true;
    }

    $dates = [];
    $dias_short = [ 0=>'DOM', 1=>'SEG', 2=>'TER', 3=>'QUA', 4=>'QUI', 5=>'SEX', 6=>'SÁB' ];
    $dias_full  = [ 0=>'Domingo', 1=>'Segunda-feira', 2=>'Terça-feira', 3=>'Quarta-feira', 4=>'Quinta-feira', 5=>'Sexta-feira', 6=>'Sábado' ];
    $meses      = [ 1=>'Jan', 2=>'Fev', 3=>'Mar', 4=>'Abr', 5=>'Mai', 6=>'Jun', 7=>'Jul', 8=>'Ago', 9=>'Set', 10=>'Out', 11=>'Nov', 12=>'Dez' ];
    $tomorrow   = ( new DateTime( '+1 day', $tz ) )->format( 'Y-m-d' );

    // Gera próximas ocorrências do dia de entrega
    $week_offset = $past_cutoff ? 1 : 0; // se passou do cutoff, próxima semana
    $added = 0;
    for ( $w = $week_offset; $w < $week_offset + $qty + 2; $w++ ) {
        // Calcula quantos dias faltam para o delivery_dow desta semana (offset w semanas)
        $days_until = ( $delivery_dow - $dow_now + 7 ) % 7 + ( $w * 7 );
        if ( $days_until === 0 && ! $past_cutoff ) $days_until = 0;
        if ( $days_until <= 0 ) $days_until += 7;

        $dt = new DateTime( '+' . $days_until . ' days', $tz );
        $ymd = $dt->format( 'Y-m-d' );
        $dow = (int) $dt->format( 'w' );
        if ( $dow === 0 ) { continue; } // pula domingo (não deve acontecer mas por segurança)

        $is_tomorrow = ( $ymd === $tomorrow );
        $dates[] = [
            'value'  => $ymd,
            'dow'    => $dias_short[$dow] ?? 'N/A',
            'full'   => $is_tomorrow ? 'Amanhã' : ( $dias_full[$dow] ?? '' ),
            'day'    => $dt->format('d'),
            'month'  => $meses[(int) $dt->format('n')] ?? '',
            'label'  => $is_tomorrow ? 'Amanhã' : ( $dias_full[$dow] ?? '' ),
            'region' => $region['nome'],
        ];
        $added++;
        if ( $added >= $qty ) break;
    }

    return $dates;
}

// ── Intercepta o seletor de datas para usar a região do cliente ──────────────
add_filter( 'sz_mb_delivery_dates', function( array $dates, int $qty ) {
    // Tenta pegar a cidade do POST (checkout) ou da sessão WC
    $city = '';
    if ( isset( $_POST['billing_city'] ) ) {
        $city = sanitize_text_field( wp_unslash( $_POST['billing_city'] ) );
    } elseif ( isset( $_POST['shipping_city'] ) ) {
        $city = sanitize_text_field( wp_unslash( $_POST['shipping_city'] ) );
    } elseif ( function_exists( 'WC' ) && WC()->customer ) {
        $city = (string) WC()->customer->get_shipping_city();
        if ( ! $city ) $city = (string) WC()->customer->get_billing_city();
    }
    if ( $city && sz_dr_get_region_for_city( $city ) ) {
        $regional = sz_dr_next_delivery_dates( $city, $qty );
        if ( ! empty( $regional ) ) return $regional;
    }
    return $dates;
}, 10, 2 );

// ── Modifica sz_mb_next_delivery_dates para aplicar filtro ───────────────────
// Hook no render do seletor para passar cidade
add_filter( 'sz_mb_render_delivery_dates_qty', function( $qty ) { return $qty; } );


function sz_dr_date_allowed_for_city( string $city, string $date ): bool {
    $date = trim( $date );
    if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $date ) ) return false;
    $region = sz_dr_get_region_for_city( $city );
    if ( ! $region ) return true;
    $dow = (int) wp_date( 'w', strtotime( $date . 'T12:00:00' ) );
    if ( $dow !== (int) $region['delivery_dow'] ) return false;
    $allowed = array_map( static fn( $d ) => (string) ( $d['value'] ?? '' ), sz_dr_next_delivery_dates( $city, 8 ) );
    return in_array( $date, $allowed, true );
}

function sz_dr_region_label_for_city( string $city ): string {
    $region = sz_dr_get_region_for_city( $city );
    return $region ? (string) $region['nome'] : '';
}

// ── Interface Admin ──────────────────────────────────────────────────────────
// Menu integrado ao Senderzz via Unified_Menu (cod-regioes)

add_action( 'admin_init', 'sz_dr_handle_save' );
function sz_dr_handle_save(): void {
    if ( ! isset( $_POST['sz_dr_nonce'] ) ) return;
    if ( ! wp_verify_nonce( $_POST['sz_dr_nonce'], 'sz_dr_save' ) ) return;
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;

    global $wpdb;
    $t = $wpdb->prefix . 'sz_delivery_regions';
    $action = sanitize_key( $_POST['sz_dr_action'] ?? '' );

    if ( $action === 'delete' ) {
        $wpdb->delete( $t, [ 'id' => absint( $_POST['sz_dr_id'] ) ] );
        wp_redirect( add_query_arg( 'msg', 'deleted', admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-regioes' ) ) ); exit;
    }

    $data = [
        'nome'            => sanitize_text_field( $_POST['nome'] ?? '' ),
        'cutoff_dow'      => absint( $_POST['cutoff_dow'] ?? 4 ),
        'cutoff_hour'     => min( 23, absint( $_POST['cutoff_hour'] ?? 13 ) ),
        'cutoff_minute'   => min( 59, absint( $_POST['cutoff_minute'] ?? 0 ) ),
        'delivery_offset' => 1,
        'delivery_dow'    => absint( $_POST['delivery_dow'] ?? 5 ),
        'ativo'           => isset( $_POST['ativo'] ) ? 1 : 0,
        'cidades'         => wp_json_encode( array_values( array_filter( array_map(
            'sz_dr_normalize',
            explode( "\n", sanitize_textarea_field( $_POST['cidades'] ?? '' ) )
        ) ) ) ),
    ];

    if ( ! $data['nome'] ) {
        wp_redirect( add_query_arg( 'msg', 'error', admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-regioes' ) ) ); exit;
    }

    $id = absint( $_POST['sz_dr_id'] ?? 0 );
    if ( $id ) {
        $wpdb->update( $t, $data, [ 'id' => $id ] );
    } else {
        $wpdb->insert( $t, $data );
    }
    wp_redirect( add_query_arg( 'msg', 'saved', admin_url( 'admin.php?page=senderzz&area=cod&tab=cod-regioes' ) ) ); exit;
}

function sz_dr_admin_page(): void {
    global $wpdb;
    $t = $wpdb->prefix . 'sz_delivery_regions';

    $edit_id = absint( $_GET['edit'] ?? 0 );
    $editing = $edit_id ? $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$t} WHERE id=%d", $edit_id ), ARRAY_A ) : null;
    $regions = $wpdb->get_results( "SELECT * FROM {$t} ORDER BY id", ARRAY_A ) ?: [];

    $dow_labels = [ 0=>'Domingo', 1=>'Segunda', 2=>'Terça', 3=>'Quarta', 4=>'Quinta', 5=>'Sexta', 6=>'Sábado' ];
    $msg = sanitize_key( $_GET['msg'] ?? '' );
    ?>
    <div class="wrap">
    <h1>🗺 Regiões de Entrega COD</h1>
    <p style="color:#6b7280">Define horário de corte e dia de entrega por região. Cidades não mapeadas usam a regra padrão (cutoff 21h, entrega no próximo dia útil).</p>

    <?php if ( $msg === 'saved' ) echo '<div class="notice notice-success is-dismissible"><p>✅ Região salva.</p></div>'; ?>
    <?php if ( $msg === 'deleted' ) echo '<div class="notice notice-success is-dismissible"><p>🗑 Região removida.</p></div>'; ?>
    <?php if ( $msg === 'error' ) echo '<div class="notice notice-error"><p>❌ Nome obrigatório.</p></div>'; ?>

    <div style="display:grid;grid-template-columns:1fr 400px;gap:24px;margin-top:20px;align-items:start">

    <!-- Lista -->
    <div>
    <table class="wp-list-table widefat striped" style="font-size:var(--sz-text-base)">
        <thead><tr>
            <th>Região</th><th>Corte</th><th>Entrega</th><th>Cidades</th><th>Status</th><th></th>
        </tr></thead>
        <tbody>
        <?php foreach ( $regions as $r ) :
            $cities = json_decode( $r['cidades'], true ) ?: [];
            $cutoff_label = $dow_labels[$r['cutoff_dow']] . ' ' . str_pad($r['cutoff_hour'],2,'0',STR_PAD_LEFT) . ':' . str_pad($r['cutoff_minute'],2,'0',STR_PAD_LEFT) . 'h';
            $delivery_label = $dow_labels[$r['delivery_dow']];
        ?>
        <tr>
            <td><strong><?php echo esc_html($r['nome']); ?></strong></td>
            <td style="color:#E8650A;font-weight:700"><?php echo esc_html($cutoff_label); ?></td>
            <td style="color:#16a34a;font-weight:700"><?php echo esc_html($delivery_label); ?></td>
            <td style="font-size:var(--sz-text-sm);color:#6b7280"><?php echo esc_html(implode(', ', array_slice($cities, 0, 4)).(count($cities)>4?' +'.( count($cities)-4).'…':'')); ?></td>
            <td><?php echo $r['ativo'] ? '<span style="color:#16a34a;font-weight:700">✅ Ativo</span>' : '<span style="color:#9ca3af">Inativo</span>'; ?></td>
            <td style="white-space:nowrap">
                <a href="<?php echo esc_url(add_query_arg(['page'=>'senderzz','area'=>'cod','tab'=>'cod-regioes','edit'=>$r['id']],admin_url('admin.php'))); ?>" class="button button-small">✏️ Editar</a>
                <form method="post" style="display:inline" onsubmit="return confirm('Remover esta região?')">
                    <?php wp_nonce_field('sz_dr_save','sz_dr_nonce'); ?>
                    <input type="hidden" name="sz_dr_action" value="delete">
                    <input type="hidden" name="sz_dr_id" value="<?php echo (int)$r['id']; ?>">
                    <button type="submit" class="button button-small button-link-delete">🗑</button>
                </form>
            </td>
        </tr>
        <?php endforeach; ?>
        <?php if ( empty($regions) ) echo '<tr><td colspan="6" style="text-align:center;color:#9ca3af;padding:20px">Nenhuma região cadastrada.</td></tr>'; ?>
        </tbody>
    </table>
    </div>

    <!-- Formulário -->
    <div style="background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:20px">
    <h2 style="font-size:var(--sz-text-lg);margin-bottom:16px"><?php echo $editing ? '✏️ Editar região' : '➕ Nova região'; ?></h2>
    <form method="post">
        <?php wp_nonce_field('sz_dr_save','sz_dr_nonce'); ?>
        <input type="hidden" name="sz_dr_action" value="save">
        <input type="hidden" name="sz_dr_id" value="<?php echo (int)($editing['id']??0); ?>">

        <table class="form-table" style="font-size:var(--sz-text-base)">
        <tr><th style="padding:8px 0">Nome da região</th><td>
            <input type="text" name="nome" value="<?php echo esc_attr($editing['nome']??''); ?>" class="regular-text" required>
        </td></tr>

        <tr><th style="padding:8px 0">Dia do corte</th><td>
            <select name="cutoff_dow" style="height:32px">
            <?php foreach($dow_labels as $v=>$l): ?>
                <option value="<?php echo $v; ?>" <?php selected((int)($editing['cutoff_dow']??4),$v); ?>><?php echo esc_html($l); ?></option>
            <?php endforeach; ?>
            </select>
        </td></tr>

        <tr><th style="padding:8px 0">Hora do corte</th><td style="display:flex;gap:6px;align-items:center">
            <input type="number" name="cutoff_hour" min="0" max="23" value="<?php echo (int)($editing['cutoff_hour']??13); ?>" style="width:60px;height:32px;padding:0 6px"> h
            <input type="number" name="cutoff_minute" min="0" max="59" value="<?php echo (int)($editing['cutoff_minute']??0); ?>" style="width:60px;height:32px;padding:0 6px"> min
        </td></tr>

        <tr><th style="padding:8px 0">Dia de entrega</th><td>
            <select name="delivery_dow" style="height:32px">
            <?php foreach($dow_labels as $v=>$l): ?>
                <option value="<?php echo $v; ?>" <?php selected((int)($editing['delivery_dow']??5),$v); ?>><?php echo esc_html($l); ?></option>
            <?php endforeach; ?>
            </select>
        </td></tr>

        <tr><th style="padding:8px 0">Cidades</th><td>
            <?php
            $cidades_str = '';
            if ( $editing ) {
                $arr = json_decode($editing['cidades'],true) ?: [];
                $cidades_str = implode("\n", $arr);
            }
            ?>
            <textarea name="cidades" rows="8" style="width:100%;font-size:var(--sz-text-meta);font-family:var(--sz-font)"><?php echo esc_textarea($cidades_str); ?></textarea>
            <p class="description" style="font-size:var(--sz-text-sm)">Uma cidade por linha. Acentos são normalizados automaticamente.</p>
        </td></tr>

        <tr><th style="padding:8px 0">Status</th><td>
            <label><input type="checkbox" name="ativo" value="1" <?php checked((int)($editing['ativo']??1),1); ?>> Ativo</label>
        </td></tr>
        </table>

        <div style="margin-top:12px;display:flex;gap:8px">
            <button type="submit" class="button button-primary">💾 Salvar região</button>
            <?php if($editing): ?>
                <a href="<?php echo esc_url(admin_url('admin.php?page=sz-delivery-regions')); ?>" class="button">Cancelar</a>
            <?php endif; ?>
        </div>
    </form>
    </div>

    </div><!-- /grid -->
    </div>
    <?php
}
