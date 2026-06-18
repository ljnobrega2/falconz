<?php
/**
 * Senderzz Dashboard V2 — Seção: Checkouts & Links (Fase 6 — v432)
 * -----------------------------------------------------------------------
 * Correções v432:
 *  - Afiliado usa affiliate_url (com rastreio), não url original
 *  - Agrupamento: 1 card por oferta principal; espelho Motoboy aparece
 *    dentro do card como link secundário (não como card separado editável)
 *  - Toggle/comissão somente no card principal
 *  - Excluir remove o card inteiro (principal + espelho DOM)
 *
 * Ações portadas:
 *   Copiar link:       client-side navigator.clipboard
 *   Excluir:           szaction=checkout_link_delete  (cascata espelho no backend)
 *   Toggle afiliado:   szaction=checkout_link_affiliate_toggle
 *   Comissão:          szaction=checkout_link_commission_update
 *
 * Ações fora do escopo:
 *   Criar novo link — requer FunnelKit/componentes (Checkout V2, fase futura)
 *   Editar nome/valor — sem handler backend de update
 *
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

// ── Perfil ─────────────────────────────────────────────────────────────────────
$sz6lk_is_aff = ( 'affiliate' === strtolower( trim( (string) ( $sz_v2_user->role ?? 'client' ) ) ) );
$sz6lk_is_sub = ! empty( $sz_v2_user->parent_user_id );

// ── Nonce / URL ───────────────────────────────────────────────────────────────
$sz6lk_nonce    = wp_create_nonce( 'senderzz_portal' );
$sz6lk_ajax_url = admin_url( 'admin-ajax.php' );

// ── Buscar links ──────────────────────────────────────────────────────────────
global $wpdb;
$sz6lk_raw = [];

if ( $sz6lk_is_aff ) {
    // Afiliado: função já filtra affiliate_visible=1 e tipo≠motoboy
    // Retorna array enriquecido com affiliate_url, affiliate_commission_pct, display_name
    if ( function_exists( 'sz_aff_get_visible_checkout_links_for_portal_user' ) ) {
        $sz6lk_raw = (array) sz_aff_get_visible_checkout_links_for_portal_user( $sz_v2_user );
    }
} else {
    // Produtor: todos os seus links ordenados por created_at
    if ( function_exists( 'senderzz_portal_ensure_checkout_links_table' ) ) {
        senderzz_portal_ensure_checkout_links_table();
    }
    $sz6lk_table = $wpdb->prefix . 'senderzz_checkout_links';
    $sz6lk_raw = (array) $wpdb->get_results( $wpdb->prepare(
        "SELECT * FROM {$sz6lk_table} WHERE user_id = %d ORDER BY created_at DESC, id ASC LIMIT 100",
        (int) $sz_v2_user->id
    ), ARRAY_A );
}

// ── Agrupar links do produtor: principal + espelho Motoboy ────────────────────
// Afiliado já recebe só principais (a função filtra tipo≠motoboy).
// Para produtor: indexar por id, depois associar espelhos.
$sz6lk_cards = []; // array de [ 'main' => row, 'motoboy' => row|null ]

if ( $sz6lk_is_aff ) {
    foreach ( $sz6lk_raw as $sz6lk_r ) {
        $sz6lk_cards[] = [ 'main' => $sz6lk_r, 'motoboy' => null ];
    }
} else {
    // Índice por id
    $sz6lk_by_id    = [];
    $sz6lk_mirrors  = []; // ids dos espelhos Motoboy (link_motoboy_id referenciado por um principal)
    foreach ( $sz6lk_raw as $sz6lk_r ) {
        $sz6lk_by_id[ (int) ( $sz6lk_r['id'] ?? 0 ) ] = $sz6lk_r;
    }
    // Primeira passagem: coletar quais ids são espelhos
    foreach ( $sz6lk_raw as $sz6lk_r ) {
        $sz6lk_mir_id = (int) ( $sz6lk_r['link_motoboy_id'] ?? 0 );
        if ( $sz6lk_mir_id > 0 ) {
            $sz6lk_mirrors[ $sz6lk_mir_id ] = true;
        }
    }
    // Segunda passagem: montar cards (somente linhas não-espelho como principal)
    foreach ( $sz6lk_raw as $sz6lk_r ) {
        $sz6lk_this_id = (int) ( $sz6lk_r['id'] ?? 0 );
        if ( isset( $sz6lk_mirrors[ $sz6lk_this_id ] ) ) {
            continue; // é espelho — será incluído dentro do card pai
        }
        $sz6lk_mir_id  = (int) ( $sz6lk_r['link_motoboy_id'] ?? 0 );
        $sz6lk_mb_row  = ( $sz6lk_mir_id > 0 && isset( $sz6lk_by_id[ $sz6lk_mir_id ] ) )
            ? $sz6lk_by_id[ $sz6lk_mir_id ] : null;
        $sz6lk_cards[] = [ 'main' => $sz6lk_r, 'motoboy' => $sz6lk_mb_row ];
    }
}

// ── Helpers ───────────────────────────────────────────────────────────────────
$sz6lk_money = static function( float $v ): string {
    return function_exists( 'senderzz_portal_money' )
        ? senderzz_portal_money( $v )
        : 'R$ ' . number_format( $v, 2, ',', '.' );
};
?>
<section id="sec-links" class="sz-sec" data-szv2-label="Checkouts & Links"
         data-szv2-ajax-url="<?php echo esc_attr( $sz6lk_ajax_url ); ?>">
    <?php if ( empty( $sz6lk_cards ) ) : ?>
        <?php echo sz_v2_empty_state( [ // phpcs:ignore
            'title' => 'Nenhum link encontrado',
            'text'  => $sz6lk_is_aff
                ? 'Nenhum checkout foi liberado para você ainda.'
                : 'Gere seu primeiro checkout no painel clássico.',
        ] ); ?>
    <?php else : ?>

    <div class="szv2-links-grid" id="szv2-links-list">
        <?php foreach ( $sz6lk_cards as $sz6lk_card ) :
            $sz6lk_main = (array) $sz6lk_card['main'];
            $sz6lk_mb   = $sz6lk_card['motoboy'] ? (array) $sz6lk_card['motoboy'] : null;

            $sz6lk_id      = (int) ( $sz6lk_main['id'] ?? 0 );
            $sz6lk_name    = (string) ( $sz6lk_main['display_name'] ?? $sz6lk_main['name'] ?? '—' );
            $sz6lk_comps   = (string) ( $sz6lk_main['components_text'] ?? '' );
            $sz6lk_created = substr( (string) ( $sz6lk_main['created_at'] ?? '' ), 0, 10 );

            // URL correta por perfil
            if ( $sz6lk_is_aff ) {
                $sz6lk_url_exp = trim( (string) ( $sz6lk_main['affiliate_url'] ?? '' ) );
            } else {
                $sz6lk_url_exp = trim( (string) ( $sz6lk_main['url'] ?? '' ) );
            }

            // URL Motoboy (somente produtor; afiliado não vê espelhos)
            $sz6lk_url_mb = '';
            if ( ! $sz6lk_is_aff && $sz6lk_mb !== null ) {
                $sz6lk_url_mb = trim( (string) ( $sz6lk_mb['url'] ?? '' ) );
            }

            // Valor
            $sz6lk_price = (string) ( $sz6lk_main['price_label'] ?? '' );
            if ( $sz6lk_price === '' && (float) ( $sz6lk_main['display_value'] ?? 0 ) > 0 ) {
                $sz6lk_price = $sz6lk_money( (float) $sz6lk_main['display_value'] );
            }

            // Comissão e visibilidade afiliado (somente produtor)
            $sz6lk_aff_visible = (bool) ( $sz6lk_main['affiliate_visible'] ?? false );
            $sz6lk_comm_pct    = (float) ( $sz6lk_main['affiliate_commission_pct'] ?? 0 );
            // Para afiliado: mostrar sua própria comissão
            if ( $sz6lk_is_aff ) {
                $sz6lk_comm_pct = (float) ( $sz6lk_main['affiliate_commission_pct'] ?? 0 );
            }

            // Permissões
            $sz6lk_can_manage = ! $sz6lk_is_aff && ! $sz6lk_is_sub;
            $sz6lk_can_delete = $sz6lk_can_manage; // backend cascata espelho
        ?>
        <div class="szv2-link-card" id="szv2-link-card-<?php echo esc_attr( (string) $sz6lk_id ); ?>"
             data-link-id="<?php echo esc_attr( (string) $sz6lk_id ); ?>">

            <!-- Cabeçalho do card -->
            <div class="szv2-link-card-head">
                <div class="szv2-link-card-meta">
                    <?php if ( $sz6lk_price !== '' ) : ?>
                    <span class="szv2-link-price szv2-num"><?php echo esc_html( $sz6lk_price ); ?></span>
                    <?php endif; ?>
                </div>
                <?php if ( $sz6lk_can_delete ) : ?>
                <button type="button"
                        class="szv2-link-btn szv2-link-del-btn"
                        data-action="delete_link"
                        data-link-id="<?php echo esc_attr( (string) $sz6lk_id ); ?>"
                        data-link-name="<?php echo esc_attr( $sz6lk_name ); ?>"
                        data-nonce="<?php echo esc_attr( $sz6lk_nonce ); ?>"
                        title="Excluir oferta">×</button>
                <?php endif; ?>
            </div>

            <h3 class="szv2-link-name"><?php echo esc_html( $sz6lk_name ); ?></h3>
            <?php if ( $sz6lk_comps !== '' ) : ?>
            <p class="szv2-link-components"><?php echo esc_html( $sz6lk_comps ); ?></p>
            <?php endif; ?>

            <!-- Links de checkout disponíveis -->
            <div class="szv2-link-urls">
                <!-- Expedição / principal -->
                <div class="szv2-link-url-row">
                    <span class="sz-badge szv2-badge-brand szv2-link-tipo-badge">Expedição</span>
                    <?php if ( $sz6lk_url_exp !== '' ) : ?>
                        <a class="szv2-link-url-text" href="<?php echo esc_url( $sz6lk_url_exp ); ?>" target="_blank" rel="noopener">
                            <?php echo esc_html( preg_replace( '#^https?://#', '', $sz6lk_url_exp ) ); ?>
                        </a>
                        <button type="button"
                                class="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-link-copy-btn"
                                data-url="<?php echo esc_attr( $sz6lk_url_exp ); ?>"
                                title="Copiar link Expedição">Copiar</button>
                    <?php else : ?>
                        <span class="szv2-text-faint-val">Link indisponível</span>
                    <?php endif; ?>
                </div>

                <!-- Motoboy (somente produtor; afiliado não vê espelhos motoboy) -->
                <?php if ( ! $sz6lk_is_aff && $sz6lk_url_mb !== '' ) : ?>
                <div class="szv2-link-url-row">
                    <span class="sz-badge szv2-badge-warning szv2-link-tipo-badge">Motoboy</span>
                    <a class="szv2-link-url-text" href="<?php echo esc_url( $sz6lk_url_mb ); ?>" target="_blank" rel="noopener">
                        <?php echo esc_html( preg_replace( '#^https?://#', '', $sz6lk_url_mb ) ); ?>
                    </a>
                    <button type="button"
                            class="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-link-copy-btn"
                            data-url="<?php echo esc_attr( $sz6lk_url_mb ); ?>"
                            title="Copiar link Motoboy">Copiar</button>
                </div>
                <?php elseif ( ! $sz6lk_is_aff && $sz6lk_mb !== null && $sz6lk_url_mb === '' ) : ?>
                <div class="szv2-link-url-row">
                    <span class="sz-badge szv2-badge-warning szv2-link-tipo-badge">Motoboy</span>
                    <span class="szv2-text-faint-val">Link indisponível</span>
                </div>
                <?php endif; ?>
            </div>

            <!-- Rodapé do card (somente produtor) -->
            <?php if ( ! $sz6lk_is_aff ) : ?>
            <div class="szv2-link-card-foot">
                <?php if ( $sz6lk_can_manage ) : ?>
                <!-- Toggle afiliado — opera no link principal -->
                <label class="szv2-link-toggle-label">
                    <input type="checkbox"
                           class="szv2-link-aff-toggle"
                           data-link-id="<?php echo esc_attr( (string) $sz6lk_id ); ?>"
                           data-nonce="<?php echo esc_attr( $sz6lk_nonce ); ?>"
                           <?php checked( $sz6lk_aff_visible ); ?>>
                    <span>Visível para afiliados</span>
                </label>
                <!-- Comissão — opera no link principal (backend propaga ao espelho) -->
                <div class="szv2-link-comm-row">
                    <label class="szv2-label" for="szv2-link-comm-<?php echo esc_attr( (string) $sz6lk_id ); ?>">Comissão %</label>
                    <input type="number"
                           id="szv2-link-comm-<?php echo esc_attr( (string) $sz6lk_id ); ?>"
                           class="szv2-input szv2-link-comm-input"
                           data-link-id="<?php echo esc_attr( (string) $sz6lk_id ); ?>"
                           data-nonce="<?php echo esc_attr( $sz6lk_nonce ); ?>"
                           value="<?php echo esc_attr( number_format( $sz6lk_comm_pct, 2, '.', '' ) ); ?>"
                           min="0" max="100" step="0.01">
                    <button type="button"
                            class="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-link-comm-save"
                            data-link-id="<?php echo esc_attr( (string) $sz6lk_id ); ?>"
                            data-nonce="<?php echo esc_attr( $sz6lk_nonce ); ?>">Salvar</button>
                </div>
                <?php else : ?>
                <!-- Sub-usuário: somente exibição -->
                <span class="szv2-link-info-label"><?php echo $sz6lk_aff_visible ? 'Visível afiliados' : 'Oculto afiliados'; ?></span>
                <?php if ( $sz6lk_comm_pct > 0 ) : ?>
                <span class="szv2-link-info-label"><?php echo esc_html( number_format( $sz6lk_comm_pct, 2, ',', '' ) ); ?>% comissão</span>
                <?php endif; ?>
                <?php endif; ?>
                <?php if ( $sz6lk_created !== '' ) : ?>
                <span class="szv2-link-date"><?php echo esc_html( $sz6lk_created ); ?></span>
                <?php endif; ?>
            </div>
            <?php elseif ( $sz6lk_comm_pct > 0 ) : ?>
            <!-- Afiliado: mostrar sua comissão no rodapé -->
            <div class="szv2-link-card-foot">
                <span class="szv2-link-info-label">Sua comissão: <?php echo esc_html( number_format( $sz6lk_comm_pct, 2, ',', '' ) ); ?>%</span>
            </div>
            <?php endif; ?>

        </div>
        <?php endforeach; ?>
    </div>
    <?php endif; ?>
</section>
