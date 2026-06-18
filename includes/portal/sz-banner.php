<?php
/**
 * Senderzz — Banner único
 * sz_banner( string $kicker, string $title, string $subtitle, string $cta_label, string $cta_onclick )
 *
 * Um único componente, um único CSS.
 * Trocar o visual: editar só este arquivo.
 */
if ( ! defined( 'ABSPATH' ) ) exit;

function sz_banner(
    string $kicker,
    string $title,
    string $subtitle  = '',
    string $cta_label  = '',
    string $cta_onclick = ''
): void {
    $kicker   = esc_html( $kicker );
    $title    = esc_html( $title );
    $subtitle = esc_html( $subtitle );
    $cta_label  = esc_html( $cta_label );
    $cta_onclick = esc_attr( $cta_onclick );
    ?>
    <div class="sz-banner">
        <div class="sz-banner-copy">
            <?php if ( $kicker ): ?>
            <span class="sz-banner-kicker"><?php echo $kicker; ?></span>
            <?php endif; ?>
            <h1 class="sz-banner-title"><?php echo $title; ?></h1>
            <?php if ( $subtitle ): ?>
            <p class="sz-banner-sub"><?php echo $subtitle; ?></p>
            <?php endif; ?>
        </div>
        <?php if ( $cta_label ): ?>
        <button type="button" class="sz-banner-cta sz-quick"
                onclick="<?php echo $cta_onclick; ?>">
            <?php echo $cta_label; ?>
        </button>
        <?php endif; ?>
    </div>
    <?php
}
