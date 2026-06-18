<?php
/**
 * Senderzz Dashboard V2 — Controller de template (Fase 1)
 * ----------------------------------------------------------------
 * Incluído por Portal_Page::render_dashboard_v2() SOMENTE quando a
 * feature flag está ativa para o usuário logado (desktop, não-OL).
 *
 * Regras da Fase 1:
 *  - Nenhuma query nova, nenhum cálculo, nenhum fetch.
 *  - Seções nascem como wrappers preservando os IDs #sec-* e
 *    data-nav do contrato; o conteúdo real é portado nas Fases 2–7.
 *  - O que o perfil não pode ver NÃO vai ao DOM.
 *
 * Variáveis recebidas:
 *  @var object $sz_v2_user  Usuário do portal (Portal_Auth::get_current_user()).
 */

defined( 'ABSPATH' ) || exit;

if ( ! isset( $sz_v2_user ) || ! is_object( $sz_v2_user ) ) {
    return; // Sem usuário válido, Portal_Page volta ao fluxo legado.
}

$sz_v2_dir = __DIR__;

/* Componentes: funções sz_v2_* (idempotentes via function_exists). */
foreach ( [
    'kpi-card', 'status-badge', 'data-table', 'filter-bar', 'empty-state',
    'modal', 'toast', 'tabs', 'wallet-summary', 'order-row',
] as $sz_v2_component ) {
    $sz_v2_component_file = $sz_v2_dir . '/components/' . $sz_v2_component . '.php';
    if ( file_exists( $sz_v2_component_file ) ) {
        require_once $sz_v2_component_file;
    }
}

/* ── Perfil ──────────────────────────────────────────────────── */
$sz_v2_role = strtolower( trim( (string) ( $sz_v2_user->role ?? 'client' ) ) );
if ( '' === $sz_v2_role ) {
    $sz_v2_role = 'client';
}
$sz_v2_is_affiliate = ( 'affiliate' === $sz_v2_role );
$sz_v2_is_ol        = ( 'operator' === $sz_v2_role );
$sz_v2_is_wp_admin  = is_user_logged_in() && current_user_can( 'manage_options' );

$sz_v2_first_name = trim( (string) ( $sz_v2_user->name ?? $sz_v2_user->email ?? '' ) );
if ( false !== strpos( $sz_v2_first_name, ' ' ) ) {
    $sz_v2_first_name = strtok( $sz_v2_first_name, ' ' );
}
if ( '' === $sz_v2_first_name ) {
    $sz_v2_first_name = 'Bem-vindo';
}

/* ── Mapa de seções por perfil ───────────────────────────────────
 * key (= data-nav e sufixo de #sec-*) → [arquivo, rótulo].
 * Chaves idênticas às do contrato legado; nada novo, nada renomeado.
 * Afiliado: somente o permitido — o restante NÃO é renderizado.
 */
$sz_v2_sections_producer = [
    'dashboard'    => [ 'dashboard',    'Dashboard' ],
    'orders'       => [ 'orders',       'Pedidos' ],
    'motoboy'      => [ 'motoboy',      'Cash On Delivery' ],
    'expedicao'    => [ 'expedicao',    'Expedição' ],
    'products'     => [ 'products',     'Produtos' ],
    'vitrine'      => [ 'vitrine',      'Vitrine' ],
    'affiliates'   => [ 'affiliates',   'Afiliação' ],
    'wallet'       => [ 'wallet',       'Carteira' ],
    'webhooks'     => [ 'webhooks',     'Webhooks' ],
    'integrations' => [ 'integrations', 'Integrações' ],
    'freight'      => [ 'freight',      'Frete' ],
    'localidades'  => [ 'localidades',  'Localidades' ],
    'settings'     => [ 'settings',     'Configurações' ],
];

$sz_v2_sections_affiliate = [
    'dashboard'  => [ 'dashboard',  'Dashboard' ],
    'vitrine'    => [ 'vitrine',    'Vitrine' ],
    'affiliates' => [ 'affiliates', 'Afiliação' ],
    'links'      => [ 'links',      'Meus links' ],
    'orders'     => [ 'orders',     'Pedidos' ],
    'wallet'     => [ 'wallet',     'Carteira' ],
    'settings'   => [ 'settings',   'Configurações' ],
];

// OL tem acesso a "Motoboys — dia" (somente operator)
if ( $sz_v2_is_ol ) {
    $sz_v2_sections_producer['motoboys-dia'] = [ 'motoboys-dia', 'Motoboys — dia' ];
}

// Usuário sem classe de entrega não é produtor ativo → sem Expedição
// Estoque removido: informações já disponíveis na aba Produtos
$sz_v2_has_shipping_class = ! empty( $sz_v2_user->shipping_class_id );
if ( ! $sz_v2_has_shipping_class ) {
    unset( $sz_v2_sections_producer['expedicao'] );
}

$sz_v2_sections = $sz_v2_is_affiliate ? $sz_v2_sections_affiliate : $sz_v2_sections_producer;

/* Grupos da sidebar (apenas apresentação; itens vêm do mapa acima). */
if ( $sz_v2_is_affiliate ) {
    $sz_v2_nav_groups = [
        ''            => [ 'dashboard' ],
        'Vendas'      => [ 'vitrine', 'affiliates', 'links', 'orders' ],
        'Financeiro'  => [ 'wallet' ],
        'Plataforma'  => [ 'settings' ],
    ];
} else {
    $sz_v2_pedidos_group = $sz_v2_is_ol
        ? [ 'motoboy', 'motoboys-dia', 'expedicao' ]
        : ( $sz_v2_has_shipping_class ? [ 'motoboy', 'expedicao' ] : [ 'motoboy' ] );
    $sz_v2_nav_groups = [
        ''            => [ 'dashboard' ],
        'Pedidos'     => $sz_v2_pedidos_group,
        'Vendas'      => [ 'products', 'vitrine', 'affiliates' ],
        'Financeiro'  => [ 'wallet' ],
        'Plataforma'  => [ 'webhooks', 'integrations', 'freight', 'localidades', 'settings' ],
    ];
}

/* Fase em que cada seção recebe o conteúdo real (kit: 01-ORDEM). */
$sz_v2_section_phase = [
    'dashboard'  => 2,
    'orders'     => 3,
    'motoboy'    => 3,
    'expedicao'  => 3,
    'links'      => 4,
    'affiliates' => 5,
    'wallet'     => 6,
    'products'   => 5,
];

require $sz_v2_dir . '/layout.php';
