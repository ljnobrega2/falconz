<?php
/**
 * Senderzz Dashboard V2 — Layout (Fase 1)
 * Wrapper de compatibilidade: .sz-root .sz-dashboard-v2 (classe
 * legada preservada ao lado da nova, conforme prompt mestre).
 *
 * Único script inline permitido: anti-flicker de tema/sidebar
 * (1 instrução, antes do paint — mesmo papel do
 * critical_sidebar_script() legado, com chaves de storage próprias).
 */

defined( 'ABSPATH' ) || exit;
?><!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>Senderzz — Portal</title>
<?php wp_head(); ?>
</head>
<body <?php body_class( 'szv2-body' ); ?>>
<div class="sz-root sz-dashboard-v2" data-theme="light" data-sidebar="open" data-role="<?php echo esc_attr( $sz_v2_role ); ?>">
<script>(function(r){try{var t=localStorage.getItem("szV2Theme")||localStorage.getItem("szTheme")||"light";r.setAttribute("data-theme",t==="dark"?"dark":"light");var s=localStorage.getItem("szV2Sidebar")||"open";r.setAttribute("data-sidebar",s==="collapsed"?"collapsed":"open");}catch(e){}})(document.currentScript.parentNode);</script>

    <?php require __DIR__ . '/sidebar.php'; ?>

    <div class="szv2-main">
        <?php require __DIR__ . '/topbar.php'; ?>

        <main class="szv2-content" id="szv2-content">
            <?php
            foreach ( $sz_v2_sections as $sz_v2_key => $sz_v2_def ) {
                $sz_v2_section_file = __DIR__ . '/sections/' . $sz_v2_def[0] . '.php';
                if ( file_exists( $sz_v2_section_file ) ) {
                    /* Cada seção define seu próprio <section id="sec-{key}">. */
                    $sz_v2_section_key   = $sz_v2_key;
                    $sz_v2_section_label = $sz_v2_def[1];
                    $sz_v2_phase         = $sz_v2_section_phase[ $sz_v2_key ] ?? 0;
                    require $sz_v2_section_file;
                }
            }
            ?>
        </main>
    </div>

    <?php echo sz_v2_toast_holder(); // phpcs:ignore WordPress.Security.EscapeOutput ?>
</div>
<?php wp_footer(); ?>
</body>
</html>
