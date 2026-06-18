<?php
/**
 * Senderzz — Remetente por Classe de Entrega
 *
 * Permite que cada classe de entrega tenha seu próprio Nome, CNPJ/CPF
 * e Telefone de remetente, sobrescrevendo apenas esses campos do
 * remetente global configurado nas Configurações do Melhor Envio.
 *
 * Endereço, e-mail e CNAE continuam vindo da configuração global.
 *
 * Opção salva: senderzz_sender_by_class
 * Formato: [
 *   class_id (int) => [
 *     'name'      => string,
 *     'document'  => string,
 *     'telephone' => string,
 *     'historico' => [
 *        [ 'campo' => 'telefone', 'de' => '...', 'para' => '...', 'por' => int, 'data' => 'YYYY-MM-DD HH:ii:ss' ],
 *        ...
 *     ],
 *   ]
 * ]
 *
 * Apenas administradores (manage_woocommerce) podem alterar.
 * O cliente, no painel, vê os dados em modo somente leitura + histórico.
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_SENDER_BY_CLASS_LOADED' ) ) return;
define( 'SENDERZZ_SENDER_BY_CLASS_LOADED', true );

define( 'SENDERZZ_SENDER_BY_CLASS_OPTION', 'senderzz_sender_by_class' );

/**
 * Helpers públicos para uso em qualquer parte do plugin (Portal etc).
 */
if ( ! function_exists( 'senderzz_sender_get_for_class' ) ) {
    function senderzz_sender_get_for_class( int $class_id ): array {
        $map = get_option( SENDERZZ_SENDER_BY_CLASS_OPTION, [] );
        $row = ( is_array( $map ) && isset( $map[ $class_id ] ) && is_array( $map[ $class_id ] ) ) ? $map[ $class_id ] : [];
        return [
            'name'      => $row['name']      ?? '',
            'document'  => $row['document']  ?? '',
            'telephone' => $row['telephone'] ?? '',
            'historico' => is_array( $row['historico'] ?? null ) ? $row['historico'] : [],
        ];
    }
}

if ( ! function_exists( 'senderzz_sender_format_phone' ) ) {
    function senderzz_sender_format_phone( string $raw ): string {
        $d = preg_replace( '/\D/', '', $raw );
        if ( $d === '' ) return '';
        if ( strlen( $d ) === 11 ) return '(' . substr( $d, 0, 2 ) . ') ' . substr( $d, 2, 5 ) . '-' . substr( $d, 7 );
        if ( strlen( $d ) === 10 ) return '(' . substr( $d, 0, 2 ) . ') ' . substr( $d, 2, 4 ) . '-' . substr( $d, 6 );
        return $d;
    }
}

if ( ! function_exists( 'senderzz_sender_format_doc' ) ) {
    function senderzz_sender_format_doc( string $raw ): string {
        $d = preg_replace( '/\D/', '', $raw );
        if ( strlen( $d ) === 11 ) {
            return substr( $d, 0, 3 ) . '.' . substr( $d, 3, 3 ) . '.' . substr( $d, 6, 3 ) . '-' . substr( $d, 9 );
        }
        if ( strlen( $d ) === 14 ) {
            return substr( $d, 0, 2 ) . '.' . substr( $d, 2, 3 ) . '.' . substr( $d, 5, 3 ) . '/' . substr( $d, 8, 4 ) . '-' . substr( $d, 12 );
        }
        return $d;
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. HOOK PRINCIPAL — sobrescreve name, document e telefone no payload do ME
// ─────────────────────────────────────────────────────────────────────────────

add_filter( 'melhor_envio_from_address', function ( array $address, $order ): array {
    if ( ! $order instanceof WC_Order ) return $address;

    $class_id = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
    if ( $class_id <= 0 ) {
        foreach ( $order->get_items() as $item ) {
            $product = $item instanceof WC_Order_Item_Product ? $item->get_product() : null;
            if ( $product ) {
                $class_id = (int) $product->get_shipping_class_id();
                if ( $class_id > 0 ) break;
            }
        }
    }

    if ( $class_id <= 0 ) return $address;

    $sender = senderzz_sender_get_for_class( $class_id );
    if ( empty( $sender['name'] ) && empty( $sender['document'] ) && empty( $sender['telephone'] ) ) {
        return $address;
    }

    if ( ! empty( $sender['name'] ) ) {
        $address['name'] = sanitize_text_field( $sender['name'] );
    }

    if ( ! empty( $sender['document'] ) ) {
        $doc = preg_replace( '/\D/', '', $sender['document'] );
        if ( strlen( $doc ) > 11 ) {
            unset( $address['document'] );
            $address['company_document'] = $doc;
        } else {
            $address['document'] = $doc;
            unset( $address['company_document'] );
        }
    }

    if ( ! empty( $sender['telephone'] ) ) {
        $tel = preg_replace( '/\D/', '', $sender['telephone'] );
        $address['phone'] = $tel;
    }

    return $address;

}, 20, 2 );

// ─────────────────────────────────────────────────────────────────────────────
// 2. SAVE HANDLER — diff por linha + histórico
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'admin_init', function () {
    if (
        empty( $_POST['senderzz_sender_class_nonce'] ) ||
        ! wp_verify_nonce( $_POST['senderzz_sender_class_nonce'], 'senderzz_sender_class_save' ) ||
        ! current_user_can( 'manage_woocommerce' )
    ) {
        return;
    }

    $raw       = isset( $_POST['sz_sender'] ) ? (array) $_POST['sz_sender'] : [];
    $previous  = get_option( SENDERZZ_SENDER_BY_CLASS_OPTION, [] );
    if ( ! is_array( $previous ) ) $previous = [];

    $clean     = [];
    $now       = current_time( 'mysql' );
    $by_user   = (int) get_current_user_id();

    foreach ( $raw as $class_id => $data ) {
        $cid  = absint( $class_id );
        if ( ! $cid && $cid !== 0 ) continue;

        $name = sanitize_text_field( $data['name'] ?? '' );
        $doc  = preg_replace( '/\D/', '', sanitize_text_field( $data['document']  ?? '' ) );
        $tel  = preg_replace( '/\D/', '', sanitize_text_field( $data['telephone'] ?? '' ) );

        $prev_row  = is_array( $previous[ $cid ] ?? null ) ? $previous[ $cid ] : [];
        $prev_name = (string) ( $prev_row['name']      ?? '' );
        $prev_doc  = (string) ( $prev_row['document']  ?? '' );
        $prev_tel  = (string) ( $prev_row['telephone'] ?? '' );
        $historico = is_array( $prev_row['historico'] ?? null ) ? $prev_row['historico'] : [];

        // linha sem dados e sem histórico anterior — ignora
        if ( $name === '' && $doc === '' && $tel === '' && empty( $historico ) ) continue;

        // diff campo a campo
        $diffs = [];
        if ( $name !== $prev_name ) {
            $diffs[] = [ 'campo' => 'nome',     'de' => $prev_name, 'para' => $name, 'por' => $by_user, 'data' => $now ];
        }
        if ( $doc !== $prev_doc ) {
            $diffs[] = [ 'campo' => 'cnpj',     'de' => $prev_doc,  'para' => $doc,  'por' => $by_user, 'data' => $now ];
        }
        if ( $tel !== $prev_tel ) {
            $diffs[] = [ 'campo' => 'telefone', 'de' => $prev_tel,  'para' => $tel,  'por' => $by_user, 'data' => $now ];
        }

        if ( ! empty( $diffs ) ) {
            $historico = array_merge( $diffs, $historico );
            $historico = array_slice( $historico, 0, 100 );
        }

        $clean[ $cid ] = [
            'name'      => $name,
            'document'  => $doc,
            'telephone' => $tel,
            'historico' => $historico,
        ];
    }

    update_option( SENDERZZ_SENDER_BY_CLASS_OPTION, $clean );

    add_action( 'admin_notices', function () {
        echo '<div class="notice notice-success is-dismissible"><p><strong>Senderzz:</strong> Remetentes por classe salvos.</p></div>';
    } );
} );

// ─────────────────────────────────────────────────────────────────────────────
// 3. ADMIN PAGE — Editar dados + Histórico de Alterações
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_sender_by_class_admin_page(): void {
    $saved   = get_option( SENDERZZ_SENDER_BY_CLASS_OPTION, [] );
    $classes = WC()->shipping ? WC()->shipping->get_shipping_classes() : [];

    $all_classes = array_merge(
        [ (object)[ 'term_id' => 0, 'name' => 'Produtos sem classe (fallback geral)', 'slug' => '' ] ],
        is_array( $classes ) ? $classes : []
    );

    $global = get_option( 'woocommerce_wc-melhor-envio_settings', [] );
    $global_name = $global['address']['name']     ?? '—';
    $global_doc  = $global['address']['document'] ?? '—';
    ?>
    <style>
        .sz-sender-wrap { max-width: 1100px; }
        .sz-sender-card { background:#fff; border:1px solid #dcdcde; border-radius:10px; padding:18px 20px; margin:12px 0; }
        .sz-sender-grid { display:grid; grid-template-columns:1fr 1fr 1fr; gap:12px 20px; margin-top:10px; }
        .sz-sender-grid label { display:flex; flex-direction:column; gap:4px; font-size:var(--sz-text-base); font-weight:600; color:#3c434a; }
        .sz-sender-grid input { padding:7px 10px; border:1px solid #c3c4c7; border-radius:6px; font-size:var(--sz-text-base); }
        .sz-sender-grid input:focus { border-color:#2271b1; outline:none; box-shadow:0 0 0 1px #2271b1; }
        .sz-sender-hint { font-size:var(--sz-text-sm); color:#787c82; margin-top:2px; font-weight:400; }
        .sz-sender-global { font-size:var(--sz-text-meta); background:#f6f7f7; border:1px solid #e0e0e0; border-radius:6px; padding:9px 14px; margin-bottom:16px; color:#50575e; }
        .sz-sender-global strong { color:#2271b1; }
        .sz-class-header { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:8px; }
        .sz-class-title { font-size:var(--sz-text-md); font-weight:700; color:#1d2327; }
        .sz-class-meta { font-size:var(--sz-text-sm); color:#787c82; }
        .sz-badge-override { font-size:var(--sz-text-sm); background:#e6f3fb; color:#0a6ea4; padding:2px 8px; border-radius:20px; border:1px solid #b8d9ef; }
        .sz-warn { background:#fff8e6; border:1px solid #f0d9a8; border-radius:6px; padding:9px 12px; font-size:var(--sz-text-meta); color:#8a6d12; margin:10px 0 0; }

        .sz-hist-block { margin-top:14px; border-top:1px solid #eee; padding-top:12px; }
        .sz-hist-title { font-size:var(--sz-text-meta); font-weight:700; text-transform:none; letter-spacing:.02em; color:#646970; margin:0 0 8px; }
        .sz-hist-table { width:100%; border-collapse:collapse; font-size:var(--sz-text-meta); }
        .sz-hist-table th { text-align:left; font-weight:700; color:#646970; text-transform:none; font-size:var(--sz-text-sm); letter-spacing:.02em; padding:6px 8px; border-bottom:1px solid #e6e6e6; background:#fafafa; }
        .sz-hist-table td { padding:7px 8px; border-bottom:1px solid #f0f0f0; vertical-align:top; }
        .sz-hist-table tr:last-child td { border-bottom:none; }
        .sz-hist-empty { color:#a0a4a8; font-style:italic; padding:8px 4px; font-size:var(--sz-text-meta); }
        .sz-hist-campo { display:inline-block; padding:2px 8px; border-radius:20px; background:#fff3e6; color:#c2530a; font-size:var(--sz-text-sm); font-weight:700; text-transform:none; letter-spacing:.02em; }
        .sz-hist-de  { color:#b32d2e; }
        .sz-hist-para{ color:#0a8a3a; font-weight:600; }
    </style>

    <div class="wrap sz-sender-wrap">
        <h1>👤 Remetente por Classe de Entrega</h1>
        <p style="color:#50575e; max-width:820px;">
            Defina <strong>Nome / Razão Social</strong>, <strong>CNPJ/CPF</strong> e <strong>Telefone</strong> do remetente para cada classe de entrega.
            O endereço e demais dados continuam vindo da configuração global do Melhor Envio.
            Somente o administrador pode alterar — o cliente vê os dados em modo somente leitura no painel.
            Toda alteração é registrada no <strong>Histórico de alterações</strong>.
        </p>

        <div class="sz-sender-global">
            Dados globais (endereço compartilhado): <strong><?php echo esc_html( $global_name ); ?></strong>
            · Doc: <strong><?php echo esc_html( $global_doc ); ?></strong>
            · <?php echo esc_html( $global['address']['address'] ?? '' ); ?>,
            <?php echo esc_html( $global['address']['city'] ?? '' ); ?>
            — <a href="<?php echo esc_url( admin_url( 'admin.php?page=wc-settings&tab=integration&section=wc-melhor-envio' ) ); ?>">Editar dados globais ↗</a>
        </div>

        <form method="post" action="">
            <?php wp_nonce_field( 'senderzz_sender_class_save', 'senderzz_sender_class_nonce' ); ?>

            <?php foreach ( $all_classes as $class ) :
                $cid     = absint( $class->term_id );
                $current = $saved[ $cid ] ?? [];
                $hist    = is_array( $current['historico'] ?? null ) ? $current['historico'] : [];
                $has_override = ! empty( $current['name'] ) || ! empty( $current['document'] ) || ! empty( $current['telephone'] );
            ?>
            <div class="sz-sender-card">
                <div class="sz-class-header">
                    <div>
                        <span class="sz-class-title"><?php echo esc_html( $class->name ); ?></span>
                        <?php if ( $cid > 0 ) : ?>
                            <span class="sz-class-meta"> &nbsp;·&nbsp; ID <?php echo esc_html( $cid ); ?> &nbsp;·&nbsp; slug: <?php echo esc_html( $class->slug ); ?></span>
                        <?php endif; ?>
                    </div>
                    <?php if ( $has_override ) : ?>
                        <span class="sz-badge-override">✓ remetente próprio configurado</span>
                    <?php endif; ?>
                </div>

                <div class="sz-sender-grid">
                    <label>
                        Nome / Razão Social
                        <input
                            type="text"
                            name="sz_sender[<?php echo esc_attr( $cid ); ?>][name]"
                            value="<?php echo esc_attr( $current['name'] ?? '' ); ?>"
                            placeholder="<?php echo esc_attr( $global_name ); ?> (global)"
                            maxlength="120"
                        >
                        <span class="sz-sender-hint">Nome completo ou razão social</span>
                    </label>
                    <label>
                        CNPJ ou CPF
                        <input
                            type="text"
                            name="sz_sender[<?php echo esc_attr( $cid ); ?>][document]"
                            value="<?php echo esc_attr( $current['document'] ?? '' ); ?>"
                            placeholder="<?php echo esc_attr( $global_doc ); ?> (global)"
                            maxlength="18"
                            inputmode="numeric"
                        >
                        <span class="sz-sender-hint">CNPJ (14 dígitos) ou CPF (11 dígitos)</span>
                    </label>
                    <label>
                        Telefone
                        <input
                            type="text"
                            name="sz_sender[<?php echo esc_attr( $cid ); ?>][telephone]"
                            value="<?php echo esc_attr( $current['telephone'] ?? '' ); ?>"
                            placeholder="(11) 99999-9999"
                            maxlength="20"
                            inputmode="numeric"
                        >
                        <span class="sz-sender-hint">DDD + número (somente dígitos serão salvos)</span>
                    </label>
                </div>

                <div class="sz-warn">
                    A alteração desses dados será registrada no histórico abaixo. O usuário será notificado por e-mail (se a notificação estiver habilitada).
                </div>

                <div class="sz-hist-block">
                    <p class="sz-hist-title">Histórico de alterações</p>
                    <?php if ( empty( $hist ) ) : ?>
                        <div class="sz-hist-empty">Sem alterações registradas.</div>
                    <?php else : ?>
                        <table class="sz-hist-table">
                            <thead>
                                <tr>
                                    <th>Data / Hora</th>
                                    <th>Campo alterado</th>
                                    <th>De</th>
                                    <th>Para</th>
                                    <th>Alterado por</th>
                                </tr>
                            </thead>
                            <tbody>
                                <?php foreach ( array_slice( $hist, 0, 30 ) as $h ) :
                                    $u = get_userdata( (int) ( $h['por'] ?? 0 ) );
                                    $by_label = $u ? $u->display_name : 'Admin';
                                    $de   = (string) ( $h['de']   ?? '' );
                                    $para = (string) ( $h['para'] ?? '' );
                                    $campo = (string) ( $h['campo'] ?? '' );
                                    if ( $campo === 'telefone' ) {
                                        $de   = senderzz_sender_format_phone( $de );
                                        $para = senderzz_sender_format_phone( $para );
                                        $campo_label = 'Telefone';
                                    } elseif ( $campo === 'cnpj' ) {
                                        $de   = senderzz_sender_format_doc( $de );
                                        $para = senderzz_sender_format_doc( $para );
                                        $campo_label = 'CNPJ / CPF';
                                    } elseif ( $campo === 'nome' ) {
                                        $campo_label = 'Nome / Razão Social';
                                    } else {
                                        $campo_label = ucfirst( $campo );
                                    }
                                ?>
                                <tr>
                                    <td><?php echo esc_html( mysql2date( 'd/m/Y H:i', $h['data'] ?? '' ) ); ?></td>
                                    <td><span class="sz-hist-campo"><?php echo esc_html( $campo_label ); ?></span></td>
                                    <td class="sz-hist-de"><?php echo $de === '' ? '—' : esc_html( $de ); ?></td>
                                    <td class="sz-hist-para"><?php echo $para === '' ? '—' : esc_html( $para ); ?></td>
                                    <td><?php echo esc_html( $by_label ); ?></td>
                                </tr>
                                <?php endforeach; ?>
                            </tbody>
                        </table>
                    <?php endif; ?>
                </div>
            </div>
            <?php endforeach; ?>

            <p style="margin-top:16px;">
                <input type="submit" class="button button-primary button-large" value="Salvar alterações">
            </p>
        </form>
    </div>
    <?php
}