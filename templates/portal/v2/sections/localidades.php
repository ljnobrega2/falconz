<?php
/**
 * Senderzz V2 – Localidades (Áreas de operação)
 * Mostra as regiões (CDs) e as zonas/cidades atendidas.
 * @var object $sz_v2_user
 */
defined( 'ABSPATH' ) || exit;

global $wpdb;
$p = $wpdb->prefix;

$sz9lo_cd_table   = $p . 'sz_motoboy_cds';
$sz9lo_zona_table = $p . 'sz_motoboy_zonas';

// Verificar se tabelas existem
$sz9lo_has_cds  = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9lo_cd_table ) ) === $sz9lo_cd_table;
$sz9lo_has_zona = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sz9lo_zona_table ) ) === $sz9lo_zona_table;

// Buscar todos os CDs ativos
$sz9lo_cds = [];
if ( $sz9lo_has_cds ) {
    $sz9lo_cds = $wpdb->get_results(
        "SELECT * FROM {$sz9lo_cd_table} WHERE ativo = 1 ORDER BY nome ASC LIMIT 100",
        ARRAY_A
    ) ?: [];
}

// Para cada CD, buscar suas zonas (= cidades atendidas)
$sz9lo_zonas_por_cd = [];
if ( $sz9lo_has_zona && ! empty( $sz9lo_cds ) ) {
    foreach ( $sz9lo_cds as $sz9lo_cd ) {
        $cid = (int) $sz9lo_cd['id'];
        $sz9lo_zonas_por_cd[ $cid ] = $wpdb->get_results(
            $wpdb->prepare(
                "SELECT * FROM {$sz9lo_zona_table} WHERE cd_id = %d ORDER BY nome ASC",
                $cid
            ),
            ARRAY_A
        ) ?: [];
    }
}

$sz9lo_first = ! empty( $sz9lo_cds ) ? (int) $sz9lo_cds[0]['id'] : 0;
$sz9lo_total = count( $sz9lo_cds );
?>
<section id="sec-localidades" class="sz-sec" data-szv2-label="Localidades">

    <?php if ( empty( $sz9lo_cds ) ) : ?>
        <?php // phpcs:ignore WordPress.Security.EscapeOutput
        echo sz_v2_empty_state( [
            'title' => 'Nenhuma área configurada',
            'text'  => 'As regiões de entrega serão exibidas aqui quando configuradas pela Senderzz.',
        ] ); ?>
    <?php else : ?>



    <!-- Layout: lista lateral + detalhe -->
    <div style="display:grid;grid-template-columns:300px 1fr;gap:var(--szv2-space-4);align-items:flex-start">

        <!-- Lista de regiões (CDs) -->
        <div style="display:flex;flex-direction:column;gap:var(--szv2-space-2)" id="szv2-lo-list">
            <?php foreach ( $sz9lo_cds as $sz9lo_cd ) :
                $sz9lo_cid      = (int) $sz9lo_cd['id'];
                $sz9lo_n_zonas  = count( $sz9lo_zonas_por_cd[ $sz9lo_cid ] ?? [] );
                $sz9lo_is_first = $sz9lo_cid === $sz9lo_first;
            ?>
            <button type="button"
                    class="szv2-lo-item<?php echo $sz9lo_is_first ? ' szv2-lo-item--active' : ''; ?>"
                    data-lo-id="<?php echo esc_attr( (string) $sz9lo_cid ); ?>"
                    data-lo-search="<?php echo esc_attr( strtolower( $sz9lo_cd['nome'] . ' ' . $sz9lo_cd['cidade'] . ' ' . $sz9lo_cd['uf'] ) ); ?>"
                    onclick="szV2LoSelect(<?php echo esc_js( (string) $sz9lo_cid ); ?>)">
                <strong class="szv2-lo-item-name"><?php echo esc_html( $sz9lo_cd['nome'] ); ?></strong>
                <div style="display:flex;gap:12px;margin-top:3px">
                    <span class="szv2-lo-item-meta">
                        <svg viewBox="0 0 20 20" style="width:11px;height:11px;fill:currentColor;vertical-align:middle;margin-right:2px" aria-hidden="true"><path d="M3 3h6v6H3zM11 3h6v6h-6zM3 11h6v6H3zM11 11h6v6h-6z"/></svg>
                        1 unidade
                    </span>
                    <?php if ( $sz9lo_n_zonas > 0 ) : ?>
                    <span class="szv2-lo-item-meta">
                        <svg viewBox="0 0 20 20" style="width:11px;height:11px;fill:currentColor;vertical-align:middle;margin-right:2px" aria-hidden="true"><path d="M10 2a5 5 0 0 0-5 5c0 3.5 5 11 5 11s5-7.5 5-11a5 5 0 0 0-5-5z"/></svg>
                        <?php echo esc_html( (string) $sz9lo_n_zonas ); ?> cidade<?php echo $sz9lo_n_zonas !== 1 ? 's' : ''; ?>
                    </span>
                    <?php endif; ?>
                </div>
            </button>
            <?php endforeach; ?>
        </div>

        <!-- Painel de detalhe -->
        <div id="szv2-lo-detail">
            <?php foreach ( $sz9lo_cds as $sz9lo_cd ) :
                $sz9lo_cid   = (int) $sz9lo_cd['id'];
                $sz9lo_zonas = $sz9lo_zonas_por_cd[ $sz9lo_cid ] ?? [];
                $sz9lo_n     = count( $sz9lo_zonas );
            ?>
            <div class="szv2-card szv2-lo-panel"
                 id="szv2-lo-panel-<?php echo esc_attr( (string) $sz9lo_cid ); ?>"
                 style="<?php echo $sz9lo_cid !== $sz9lo_first ? 'display:none' : ''; ?>">

                <!-- Header da região -->
                <div style="margin-bottom:var(--szv2-space-5);padding-bottom:var(--szv2-space-4);border-bottom:1px solid var(--szv2-divider)">
                    <h2 style="font-size:var(--szv2-text-xl,18px);margin:0 0 2px;font-weight:700"><?php echo esc_html( $sz9lo_cd['nome'] ); ?></h2>
                    <?php if ( $sz9lo_cd['uf'] ) : ?>
                    <div style="font-size:13px;color:var(--szv2-text-muted);font-weight:500"><?php echo esc_html( $sz9lo_cd['uf'] ); ?></div>
                    <?php endif; ?>
                </div>

                <?php if ( $sz9lo_n > 0 ) : ?>
                <!-- Cobertura de entrega -->
                <h3 style="font-size:14px;font-weight:700;color:var(--szv2-text);margin:0 0 var(--szv2-space-3)">
                    Cobertura de entrega (<?php echo esc_html( (string) $sz9lo_n ); ?>)
                </h3>
                <div style="display:grid;grid-template-columns:1fr 1fr;gap:0;margin-bottom:var(--szv2-space-5)">
                    <?php
                    $sz9lo_shown = 4; // primeiros 4 visíveis, restante oculto
                    foreach ( $sz9lo_zonas as $sz9lo_idx => $sz9lo_zona ) :
                        $sz9lo_ativo = ! empty( $sz9lo_zona['ativo'] );
                    ?>
                    <div class="szv2-lo-city<?php echo $sz9lo_idx >= $sz9lo_shown ? ' szv2-lo-city--hidden' : ''; ?>"
                         style="align-items:center;gap:6px;font-size:13px;padding:6px 0;border-bottom:1px solid var(--szv2-divider)<?php echo ! $sz9lo_ativo ? ';opacity:.5' : ''; ?>">
                        <svg viewBox="0 0 20 20" style="width:12px;height:12px;fill:var(--szv2-text-muted);flex-shrink:0" aria-hidden="true"><path d="M10 2a5 5 0 0 0-5 5c0 3.5 5 11 5 11s5-7.5 5-11a5 5 0 0 0-5-5z"/></svg>
                        <?php echo esc_html( $sz9lo_zona['nome'] ); ?>
                        <?php if ( ! empty( $sz9lo_zona['cutoff_horarios'] ) ) : ?>
                        <svg viewBox="0 0 20 20" style="width:12px;height:12px;fill:var(--szv2-text-faint);margin-left:auto" title="Horário de corte configurado" aria-label="Horário de corte"><path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm0 3v5l3 2-1 1.7L9 11V5h1z"/></svg>
                        <?php endif; ?>
                    </div>
                    <?php endforeach; ?>
                </div>
                <?php if ( $sz9lo_n > $sz9lo_shown ) : ?>
                <button type="button"
                        class="szv2-btn szv2-btn-sm szv2-btn-secondary"
                        style="margin-bottom:var(--szv2-space-4)"
                        onclick="szV2LoToggleCities(this)">
                    <svg viewBox="0 0 20 20" style="width:14px;height:14px;fill:currentColor;margin-right:4px" aria-hidden="true"><path d="M10 12L4 6h12z"/></svg>
                    Ver todas (<?php echo esc_html( (string) $sz9lo_n ); ?> cidades)
                </button>
                <?php endif; ?>
                <?php endif; ?>

                <!-- Endereço da unidade removido (não exibir ao usuário) -->

            </div><!-- /szv2-lo-panel -->
            <?php endforeach; ?>
        </div>

    </div>
    <?php endif; ?>

</section>
<style>
.sz-dashboard-v2 .szv2-lo-item {
    display: block; width: 100%; text-align: left;
    padding: 12px 14px;
    border: 1px solid var(--szv2-border);
    border-radius: var(--szv2-radius-md);
    background: var(--szv2-surface);
    cursor: pointer;
    transition: border-color 0.1s, background 0.1s;
}
.sz-dashboard-v2 .szv2-lo-item:hover { background: var(--szv2-surface-alt); border-color: var(--szv2-brand); }
.sz-dashboard-v2 .szv2-lo-item--active {
    border: 2px solid var(--szv2-brand);
    background: var(--szv2-surface);
}
.sz-dashboard-v2 .szv2-lo-item-name { font-size: 13px; font-weight: 600; color: var(--szv2-text); }
.sz-dashboard-v2 .szv2-lo-item--active .szv2-lo-item-name { color: var(--szv2-brand); }
.sz-dashboard-v2 .szv2-lo-item-meta { font-size: 12px; color: var(--szv2-text-muted); }
.sz-dashboard-v2 .szv2-lo-city { display: flex; }
.sz-dashboard-v2 .szv2-lo-city--hidden { display: none; }
</style>
<script>
window.szV2LoSelect = function (id) {
    document.querySelectorAll('.szv2-lo-item').forEach(function (b) {
        b.classList.toggle('szv2-lo-item--active', b.dataset.loId === String(id));
    });
    document.querySelectorAll('.szv2-lo-panel').forEach(function (p) {
        p.style.display = p.id === 'szv2-lo-panel-' + id ? '' : 'none';
    });
};
// szV2LoSearch removido (campo de busca descontinuado)
window.szV2LoToggleCities = function (btn) {
    var grid = btn.previousElementSibling;
    var hidden = grid.querySelectorAll('.szv2-lo-city--hidden');
    if (hidden.length > 0) {
        hidden.forEach(function (c) { c.classList.remove('szv2-lo-city--hidden'); });
        btn.innerHTML = '<svg viewBox="0 0 20 20" style="width:14px;height:14px;fill:currentColor;margin-right:4px"><path d="M10 8l6 6H4z"/></svg> Mostrar menos';
    } else {
        var all = grid.querySelectorAll('.szv2-lo-city');
        all.forEach(function (c, i) { if (i >= 4) { c.classList.add('szv2-lo-city--hidden'); } });
        btn.innerHTML = '<svg viewBox="0 0 20 20" style="width:14px;height:14px;fill:currentColor;margin-right:4px"><path d="M10 12L4 6h12z"/></svg> Ver todas';
    }
};
</script>
