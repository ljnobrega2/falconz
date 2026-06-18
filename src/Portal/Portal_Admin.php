<?php

namespace WC_MelhorEnvio\Portal;

/**
 * Portal_Admin v2.5
 *
 * Página unificada de gestão de usuários do portal.
 * Substitui "Painel de Clientes" + "Operador Logístico" em uma única tela.
 *
 * Colunas: Tipo | Nome | Email | Classe de entrega | Saldo | Última transação | Status | Ações
 */
class Portal_Admin {

    public function __construct() {
        // Senderzz v221: menus legados removidos da origem; render pelo menu unificado.
        // add_action( 'admin_menu',                                    [ $this, 'register_menu' ] );
        add_action( 'admin_post_senderzz_portal_create_user',        [ $this, 'handle_create_user' ] );
        add_action( 'admin_post_senderzz_portal_update_classes',      [ $this, 'handle_update_classes' ] );
        add_action( 'admin_post_senderzz_portal_delete_user',        [ $this, 'handle_delete_user' ] );
        add_action( 'admin_post_senderzz_portal_reset_password',     [ $this, 'handle_reset_password' ] );
        add_action( 'admin_post_senderzz_portal_toggle_status',      [ $this, 'handle_toggle_status' ] );
        add_action( 'admin_post_senderzz_portal_change_email',        [ $this, 'handle_change_email' ] );
        add_action( 'admin_post_senderzz_portal_change_name',         [ $this, 'handle_change_name' ] );
    }

    public function register_menu(): void {
        // Senderzz v221: registros legados cortados; Usuários, Onboarding e Push ficam no menu unificado.
        return;
    }

    /* ── Helpers ──────────────────────────────────────────────────────────── */

    /**
     * Senderzz v282: normaliza ID recebido pelo admin.
     * O cadastro visual trabalha com senderzz_portal_users.id, mas alguns fluxos/diagnósticos
     * podem enviar wp_users.ID. Aqui resolvemos para o ID real do portal antes de salvar classe.
     */
    private function resolve_portal_user_id( int $maybe_id ): int {
        if ( $maybe_id <= 0 ) return 0;
        global $wpdb;
        $table = $wpdb->prefix . 'senderzz_portal_users';

        $portal_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$table} WHERE id = %d LIMIT 1",
            $maybe_id
        ) );
        if ( $portal_id > 0 ) return $portal_id;

        $portal_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$table} WHERE wp_user_id = %d LIMIT 1",
            $maybe_id
        ) );
        return $portal_id > 0 ? $portal_id : 0;
    }

    private function get_user_saldo( int $portal_user_id ): ?float {
        global $wpdb;
        // Usa wp_user_id direto (coluna adicionada na v2.4.7)
        $wp_user_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COALESCE(wp_user_id, 0) FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d",
            $portal_user_id
        ) );
        if ( ! $wp_user_id ) {
            // Fallback: busca por email
            $email = $wpdb->get_var( $wpdb->prepare(
                "SELECT email FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d", $portal_user_id
            ) );
            $u = $email ? get_user_by( 'email', $email ) : null;
            $wp_user_id = $u ? $u->ID : 0;
        }
        if ( ! $wp_user_id ) return null;
        return function_exists( 'tpc_get_saldo_disponivel' ) ? tpc_get_saldo_disponivel( $wp_user_id ) : null;
    }

    private function get_last_transaction( int $portal_user_id ): ?array {
        global $wpdb;
        $wp_user_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT COALESCE(wp_user_id, 0) FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d",
            $portal_user_id
        ) );
        if ( ! $wp_user_id ) {
            $email = $wpdb->get_var( $wpdb->prepare(
                "SELECT email FROM {$wpdb->prefix}senderzz_portal_users WHERE id = %d", $portal_user_id
            ) );
            $u = $email ? get_user_by( 'email', $email ) : null;
            $wp_user_id = $u ? $u->ID : 0;
        }
        if ( ! $wp_user_id ) return null;
        return $wpdb->get_row( $wpdb->prepare(
            "SELECT tipo, valor, descricao, created_at FROM {$wpdb->prefix}tpc_transacoes
             WHERE user_id = %d AND status = 'confirmado'
             ORDER BY created_at DESC LIMIT 1",
            $wp_user_id
        ), ARRAY_A ) ?: null;
    }

    /* ── Render ───────────────────────────────────────────────────────────── */

    public function render_page(): void {
        global $wpdb;
        $table   = $wpdb->prefix . Portal_Auth::TABLE;

        // Auto-fix: corrige shipping_class_id=0 para usuários que têm classes na tabela multi-class
        $mc_table = $wpdb->prefix . 'senderzz_portal_user_classes';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mc_table ) ) === $mc_table ) {
            $stale = $wpdb->get_results(
                "SELECT pu.id, MIN(mc.shipping_class_id) AS primary_class
                   FROM {$wpdb->prefix}senderzz_portal_users pu
                   INNER JOIN {$mc_table} mc ON mc.portal_user_id = pu.id
                  WHERE pu.shipping_class_id = 0 AND mc.shipping_class_id > 0
                  GROUP BY pu.id"
            ) ?: [];
            foreach ( $stale as $row ) {
                $wpdb->update( $wpdb->prefix . 'senderzz_portal_users',
                    [ 'shipping_class_id' => (int) $row->primary_class ],
                    [ 'id' => (int) $row->id ], [ '%d' ], [ '%d' ] );
            }
        }

        // Portal users (have login)
        $portal_users = $wpdb->get_results( "SELECT *, 'portal' AS source FROM {$table} ORDER BY created_at DESC" ) ?: [];

        // WP users with wallet but NO portal account
        $wallet_only = $wpdb->get_results(
            "SELECT wu.ID as wp_id, wu.user_email as email, wu.display_name as name,
                    c.saldo, c.updated_at as last_wallet_update
             FROM {$wpdb->prefix}tpc_carteira c
             INNER JOIN {$wpdb->users} wu ON wu.ID = c.user_id
             WHERE c.user_id NOT IN (
                 SELECT COALESCE(wp_user_id, 0) FROM {$wpdb->prefix}senderzz_portal_users WHERE wp_user_id IS NOT NULL
             )
             ORDER BY c.updated_at DESC"
        ) ?: [];
        $notice  = get_transient( 'senderzz_portal_admin_notice' );
        $portal_url = get_permalink( get_option( 'senderzz_portal_page_id' ) );
        $shipping_classes = WC()->shipping() ? WC()->shipping()->get_shipping_classes() : [];
        $class_map = [];
        foreach ( $shipping_classes as $sc ) {
            $class_map[ $sc->term_id ] = $sc->name;
        }

        if ( $notice ) delete_transient( 'senderzz_portal_admin_notice' );
        ?>
        <div class="wrap">

            <h1 style="display:flex;align-items:center;gap:10px;">
                👥 Usuários do Portal
                <?php if ( $portal_url ) : ?>
                    <a href="<?php echo esc_url( $portal_url ); ?>" target="_blank"
                       style="font-size:var(--sz-text-meta);font-weight:400;color:#2271b1;text-decoration:none;border:1px solid #2271b1;border-radius:4px;padding:2px 10px;">
                        ↗ Abrir portal
                    </a>
                <?php endif; ?>
            </h1>

            <?php if ( $notice ) : ?>
                <div class="notice notice-<?php echo esc_attr( $notice['type'] ); ?> is-dismissible">
                    <p><?php echo esc_html( $notice['message'] ); ?></p>
                </div>
            <?php endif; ?>

            <!-- ── Criar usuário ── -->
            <div style="background:#fff;border:1px solid #ddd;border-radius:8px;padding:20px 24px;max-width:620px;margin:20px 0 28px;">
                <h2 style="margin:0 0 16px;font-size:var(--sz-text-lg);">Criar usuário</h2>
                <form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>">
                    <?php wp_nonce_field( 'senderzz_portal_create_user' ); ?>
                    <input type="hidden" name="action" value="senderzz_portal_create_user">

                    <div style="display:grid;grid-template-columns:1fr 1fr;gap:14px;margin-bottom:14px;">
                        <div>
                            <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;">TIPO *</label>
                            <div style="display:flex;flex-direction:column;gap:8px;padding:10px;border:1px solid #ddd;border-radius:6px;background:#fafafa;">
                                <label style="display:flex;align-items:flex-start;gap:8px;cursor:pointer;">
                                    <input type="radio" name="user_role" value="producer" checked style="margin-top:2px;">
                                    <span>
                                        <strong style="display:block;font-size:var(--sz-text-base);">Produtor</strong>
                                        <span style="font-size:var(--sz-text-sm);color:#888;">Dono de classe/produto, pedidos, saldo, frete e relatórios</span>
                                    </span>
                                </label>
                                <label style="display:flex;align-items:flex-start;gap:8px;cursor:pointer;">
                                    <input type="radio" name="user_role" value="client" style="margin-top:2px;">
                                    <span>
                                        <strong style="display:block;font-size:var(--sz-text-base);">Cliente</strong>
                                        <span style="font-size:var(--sz-text-sm);color:#888;">Conta sem liberação comercial até receber classe/afiliação</span>
                                    </span>
                                </label>
                                <label style="display:flex;align-items:flex-start;gap:8px;cursor:pointer;">
                                    <input type="radio" name="user_role" value="operator" style="margin-top:2px;">
                                    <span>
                                        <strong style="display:block;font-size:var(--sz-text-base);">Operador Logístico</strong>
                                        <span style="font-size:var(--sz-text-sm);color:#888;">Fila de etiquetas. Sem dados financeiros.</span>
                                    </span>
                                </label>
                            </div>
                        </div>
                        <div style="display:flex;flex-direction:column;gap:10px;">
                            <div>
                                <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;">NOME</label>
                                <input type="text" name="user_name" class="regular-text" placeholder="ex: João Silva" style="width:100%;">
                            </div>
                            <div>
                                <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;">EMAIL *</label>
                                <input type="email" name="email" class="regular-text" required style="width:100%;">
                            </div>
                            <div>
                                <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;">SENHA *</label>
                                <input type="password" name="password" class="regular-text" required placeholder="Mínimo 8 caracteres" autocomplete="new-password" style="width:100%;">
                            </div>
                        </div>
                    </div>

                    <div id="sz-class-field" style="margin-bottom:14px;">
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;">CLASSES DE ENTREGA</label>
                        <div style="border:1.5px solid #e5e7eb;border-radius:10px;background:#fff;padding:8px 10px;max-height:160px;overflow-y:auto;display:flex;flex-direction:column;gap:4px;">
                            <?php foreach ( $shipping_classes as $sc ) : ?>
                                <label style="display:flex;align-items:center;gap:10px;cursor:pointer;padding:6px 8px;border-radius:8px;transition:background .12s;" onmouseover="this.style.background='#fff5ee'" onmouseout="this.style.background='transparent'">
                                    <span style="position:relative;width:18px;height:18px;flex-shrink:0;">
                                        <input type="checkbox" name="shipping_class_ids[]" value="<?php echo esc_attr( $sc->term_id ); ?>"
                                               style="position:absolute;opacity:0;width:18px;height:18px;cursor:pointer;margin:0;"
                                               onchange="this.parentElement.querySelector('.sz-chk-box').style.background=this.checked?'#F7941D':'#fff';this.parentElement.querySelector('.sz-chk-box').style.borderColor=this.checked?'#F7941D':'#d1d5db';this.parentElement.querySelector('.sz-chk-mark').style.display=this.checked?'block':'none';">
                                        <span class="sz-chk-box" style="display:block;width:18px;height:18px;border:2px solid #d1d5db;border-radius:5px;background:#fff;transition:all .12s;"></span>
                                        <span class="sz-chk-mark" style="display:none;position:absolute;top:2px;left:5px;width:6px;height:10px;border:2px solid #fff;border-top:0;border-left:0;transform:rotate(45deg);"></span>
                                    </span>
                                    <span style="font-size:var(--sz-text-base);font-weight:600;color:#111827;"><?php echo esc_html( $sc->name ); ?></span>
                                    <span style="margin-left:auto;font-size:var(--sz-text-xs);font-weight:700;color:#9ca3af;background:#f3f4f6;padding:2px 7px;border-radius:99px;">#<?php echo esc_attr( $sc->term_id ); ?></span>
                                </label>
                            <?php endforeach; ?>
                        </div>
                        <p style="font-size:var(--sz-text-meta);color:#888;margin:4px 0 0;">Pedidos dessas classes serão visíveis para o cliente no portal. Selecione uma ou mais.</p>
                    </div>
                    <script>
                    (function(){
                        function szToggleClassField(){
                            var role = document.querySelector('input[name=user_role]:checked');
                            var field = document.getElementById('sz-class-field');
                            if(!field) return;
                            if(role && role.value === 'operator'){
                                field.style.display = 'none';
                                field.querySelectorAll('input[type=checkbox]').forEach(function(c){ c.checked=false; });
                            } else {
                                field.style.display = 'block';
                            }
                        }
                        document.querySelectorAll('input[name=user_role]').forEach(function(r){
                            r.addEventListener('change', szToggleClassField);
                        });
                        szToggleClassField();
                    })();
                    </script>

                    <button type="submit" class="button button-primary">Criar usuário</button>
                </form>
            </div>

            <!-- ── Tabela de usuários ── -->
            <h2 style="margin-bottom:10px;">
                Usuários cadastrados
                <span style="font-size:var(--sz-text-base);font-weight:400;color:#888;">(<?php echo count( $portal_users ) + count( $wallet_only ); ?>)</span>
            </h2>

            <?php if ( empty( $portal_users ) && empty( $wallet_only ) ) : ?>
                <p style="color:#888;">Nenhum usuário cadastrado ainda.</p>
            <?php else : ?>
            <table class="wp-list-table widefat fixed" style="border-radius:8px;overflow:hidden;">
                <thead>
                    <tr>
                        <th style="width:90px;">Tipo</th>
                        <th>Nome / Email</th>
                        <th style="width:160px;">Classe de entrega</th>
                        <th style="width:110px;">Saldo</th>
                        <th style="width:200px;">Última transação</th>
                        <th style="width:60px;">Status</th>
                        <th style="width:280px;">Ações</th>
                    </tr>
                </thead>
                <tbody>
                <?php
                // ── Portal users (have login) ──────────────────────────────
                foreach ( $portal_users as $u ) :
                    $role       = $u->role ?? 'client';
                    $class_name = $class_map[ $u->shipping_class_id ] ?? ( $u->shipping_class_id > 0 ? '#' . $u->shipping_class_id : '—' );
                    $sz_admin_is_producer  = function_exists( 'sz_aff_portal_user_has_producer_access' ) ? sz_aff_portal_user_has_producer_access( $u ) : ( (int) ( $u->shipping_class_id ?? 0 ) > 0 );
                    $sz_admin_is_affiliate = ! $sz_admin_is_producer && function_exists( 'sz_aff_portal_user_has_active_affiliate_link' ) && sz_aff_portal_user_has_active_affiliate_link( $u );
                    $saldo      = $this->get_user_saldo( (int) $u->id );
                    $last_tx    = $this->get_last_transaction( (int) $u->id );
                    $is_active  = ( $u->status ?? 'active' ) === 'active';
                ?>
                <tr>
                    <td>
                        <?php if ( $role === 'operator' ) : ?>
                            <span style="background:#eff6ff;color:#1d4ed8;padding:2px 8px;border-radius:99px;font-size:var(--sz-text-sm);font-weight:700;white-space:nowrap;">Operador</span>
                        <?php elseif ( $sz_admin_is_producer ) : ?>
                            <span style="background:#fff7ed;color:#c2410c;padding:2px 8px;border-radius:99px;font-size:var(--sz-text-sm);font-weight:700;white-space:nowrap;">Produtor</span>
                        <?php elseif ( $sz_admin_is_affiliate ) : ?>
                            <span style="background:#eef2ff;color:#4338ca;padding:2px 8px;border-radius:99px;font-size:var(--sz-text-sm);font-weight:700;white-space:nowrap;">Afiliado</span>
                        <?php else : ?>
                            <span style="background:#f0fdf4;color:#15803d;padding:2px 8px;border-radius:99px;font-size:var(--sz-text-sm);font-weight:700;white-space:nowrap;">Cliente</span>
                        <?php endif; ?>
                    </td>
                    <td>
                        <strong style="display:block;"><?php echo esc_html( $u->name ?: '—' ); ?></strong>
                        <span style="font-size:var(--sz-text-meta);color:#6b7280;"><?php echo esc_html( $u->email ); ?></span>
                        <?php if ( $u->last_login_at ) : ?>
                            <span style="display:block;font-size:var(--sz-text-sm);color:#9ca3af;">Último acesso: <?php echo esc_html( date_i18n( 'd/m/Y H:i', strtotime( $u->last_login_at ) ) ); ?></span>
                        <?php endif; ?>
                    </td>
                    <td style="font-size:var(--sz-text-base);"><?php
                        // Multi-class: lista todas as classes do usuário.
                        if ( function_exists( 'sz_get_user_class_ids' ) ) {
                            $u_class_ids = sz_get_user_class_ids( $u );
                            if ( empty( $u_class_ids ) ) {
                                echo '—';
                            } else {
                                $u_class_names = [];
                                foreach ( $u_class_ids as $ucid ) {
                                    $u_class_names[] = esc_html( $class_map[ $ucid ] ?? '#' . $ucid );
                                }
                                echo implode( ', ', $u_class_names );
                            }
                        } else {
                            echo esc_html( $class_name );
                        }
                    ?></td>
                    <td>
                        <?php if ( $role === 'operator' ) : ?>
                            <span style="color:#9ca3af;font-size:var(--sz-text-meta);">N/A</span>
                        <?php elseif ( $saldo !== null ) : ?>
                            <strong style="color:<?php echo $saldo >= 0 ? '#16a34a' : '#dc2626'; ?>;font-size:var(--sz-text-md);">
                                R$ <?php echo number_format( $saldo, 2, ',', '.' ); ?>
                            </strong>
                        <?php else : ?>
                            <span style="color:#d1d5db;font-size:var(--sz-text-meta);">Sem conta</span>
                        <?php endif; ?>
                    </td>
                    <td style="font-size:var(--sz-text-meta);">
                        <?php if ( $last_tx ) :
                            $tx_color = $last_tx['tipo'] === 'credito' ? '#16a34a' : '#dc2626';
                            $tx_prefix = $last_tx['tipo'] === 'credito' ? '+' : '−';
                        ?>
                            <span style="color:<?php echo $tx_color; ?>;font-weight:600;"><?php echo $tx_prefix; ?> R$ <?php echo number_format( (float)$last_tx['valor'], 2, ',', '.' ); ?></span>
                            <span style="display:block;color:#9ca3af;font-size:var(--sz-text-sm);"><?php echo esc_html( date_i18n( 'd/m/Y H:i', strtotime( $last_tx['created_at'] ) ) ); ?></span>
                            <span style="display:block;color:#6b7280;font-size:var(--sz-text-sm);max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="<?php echo esc_attr( $last_tx['descricao'] ); ?>">
                                <?php echo esc_html( $last_tx['descricao'] ); ?>
                            </span>
                        <?php else : ?>
                            <span style="color:#d1d5db;">—</span>
                        <?php endif; ?>
                    </td>
                    <td>
                        <span style="color:<?php echo $is_active ? '#16a34a' : '#dc2626'; ?>;font-size:var(--sz-text-meta);font-weight:600;">
                            <?php echo $is_active ? 'Ativo' : 'Inativo'; ?>
                        </span>
                    </td>
                    <td>
                        <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;">
                            <!-- Editar classes de entrega -->
                            <?php if ( $role !== 'operator' ) : ?>
                            <details style="display:inline-block;position:relative;">
                                <summary class="button button-small" style="cursor:pointer;list-style:none;padding:3px 8px;font-size:var(--sz-text-sm);">✏️ Classes</summary>
                                <div style="position:absolute;z-index:100;background:#fff;border:1px solid #ddd;border-radius:8px;padding:12px;min-width:240px;box-shadow:0 4px 12px rgba(0,0,0,.12);top:28px;left:0;">
                                    <form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>">
                                        <?php wp_nonce_field( 'senderzz_portal_update_classes' ); ?>
                                        <input type="hidden" name="action" value="senderzz_portal_update_classes">
                                        <input type="hidden" name="user_id" value="<?php echo esc_attr( $u->id ); ?>">
                                        <p style="margin:0 0 8px;font-size:var(--sz-text-meta);font-weight:600;">Classes de entrega:</p>
                                        <div style="max-height:130px;overflow-y:auto;display:flex;flex-direction:column;gap:2px;margin-bottom:10px;">
                                            <?php
                                            $u_current_class_ids = function_exists('sz_get_user_class_ids') ? sz_get_user_class_ids($u) : [(int)$u->shipping_class_id];
                                            foreach ( $shipping_classes as $sc_edit ) :
                                                $is_checked = in_array( (int)$sc_edit->term_id, $u_current_class_ids, true );
                                            ?>
                                                <label style="display:flex;align-items:center;gap:8px;cursor:pointer;padding:5px 6px;border-radius:7px;transition:background .12s;" onmouseover="this.style.background='#fff5ee'" onmouseout="this.style.background='transparent'">
                                                    <span style="position:relative;width:16px;height:16px;flex-shrink:0;">
                                                        <input type="checkbox" name="shipping_class_ids[]"
                                                               value="<?php echo esc_attr( $sc_edit->term_id ); ?>"
                                                               <?php checked( $is_checked ); ?>
                                                               style="position:absolute;opacity:0;width:16px;height:16px;cursor:pointer;margin:0;"
                                                               onchange="var b=this.parentElement.querySelector('.sz-chk-b');var m=this.parentElement.querySelector('.sz-chk-m');b.style.background=this.checked?'#F7941D':'#fff';b.style.borderColor=this.checked?'#F7941D':'#d1d5db';m.style.display=this.checked?'block':'none';">
                                                        <span class="sz-chk-b" style="display:block;width:16px;height:16px;border:2px solid <?php echo $is_checked ? '#F7941D' : '#d1d5db'; ?>;border-radius:4px;background:<?php echo $is_checked ? '#F7941D' : '#fff'; ?>;transition:all .12s;"></span>
                                                        <span class="sz-chk-m" style="display:<?php echo $is_checked ? 'block' : 'none'; ?>;position:absolute;top:2px;left:4px;width:5px;height:9px;border:2px solid #fff;border-top:0;border-left:0;transform:rotate(45deg);"></span>
                                                    </span>
                                                    <span style="font-size:var(--sz-text-meta);font-weight:600;color:#111827;"><?php echo esc_html( $sc_edit->name ); ?></span>
                                                    <span style="margin-left:auto;font-size:var(--sz-text-xs);font-weight:700;color:#9ca3af;background:#f3f4f6;padding:1px 6px;border-radius:99px;">#<?php echo esc_attr( $sc_edit->term_id ); ?></span>
                                                </label>
                                            <?php endforeach; ?>
                                        </div>
                                        <button type="submit" class="button button-primary button-small" style="width:100%;">Salvar classes</button>
                                    </form>
                                </div>
                            </details>
                            <?php endif; ?>
                            <!-- Reset senha -->
                            <form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>" style="display:flex;gap:4px;">
                                <?php wp_nonce_field( 'senderzz_portal_reset_password' ); ?>
                                <input type="hidden" name="action" value="senderzz_portal_reset_password">
                                <input type="hidden" name="user_id" value="<?php echo esc_attr( $u->id ); ?>">
                                <input type="password" name="new_password" placeholder="Nova senha" style="width:100px;padding:3px 6px;font-size:var(--sz-text-sm);" required minlength="8" autocomplete="new-password">
                                <button type="submit" class="button button-small">Redefinir senha</button>
                            </form>
                            <!-- Alterar e-mail -->
                            <form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>" style="display:flex;gap:4px;"
                                  onsubmit="return confirm('Alterar e-mail de <?php echo esc_js( $u->email ); ?> para ' + this.querySelector('[name=new_email]').value + '?')">
                                <?php wp_nonce_field( 'senderzz_portal_change_email' ); ?>
                                <input type="hidden" name="action" value="senderzz_portal_change_email">
                                <input type="hidden" name="user_id" value="<?php echo esc_attr( $u->id ); ?>">
                                <input type="email" name="new_email" placeholder="Novo e-mail" style="width:150px;padding:3px 6px;font-size:var(--sz-text-sm);" required>
                                <button type="submit" class="button button-small">Alterar e-mail</button>
                            </form>
                            <!-- Alterar nome -->
                            <form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>" style="display:flex;gap:4px;">
                                <?php wp_nonce_field( 'senderzz_portal_change_name' ); ?>
                                <input type="hidden" name="action" value="senderzz_portal_change_name">
                                <input type="hidden" name="user_id" value="<?php echo esc_attr( $u->id ); ?>">
                                <input type="text" name="new_name" placeholder="Novo nome" value="<?php echo esc_attr( $u->name ); ?>" style="width:140px;padding:3px 6px;font-size:var(--sz-text-sm);" required>
                                <button type="submit" class="button button-small">Alterar nome</button>
                            </form>
                            <!-- Ativar/Suspender -->
                            <form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>" style="display:inline;">
                                <?php wp_nonce_field( 'senderzz_portal_toggle_status' ); ?>
                                <input type="hidden" name="action" value="senderzz_portal_toggle_status">
                                <input type="hidden" name="user_id" value="<?php echo esc_attr( $u->id ); ?>">
                                <button type="submit" class="button button-small" style="color:<?php echo $is_active ? '#b91c1c' : '#15803d'; ?>;">
                                    <?php echo $is_active ? 'Suspender' : 'Ativar'; ?>
                                </button>
                            </form>
                            <!-- Excluir -->
                            <form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>" style="display:inline;"
                                  onsubmit="return confirm('Excluir <?php echo esc_js( $u->email ); ?>? Esta ação não pode ser desfeita.')">
                                <?php wp_nonce_field( 'senderzz_portal_delete_user' ); ?>
                                <input type="hidden" name="action" value="senderzz_portal_delete_user">
                                <input type="hidden" name="user_id" value="<?php echo esc_attr( $u->id ); ?>">
                                <button type="submit" class="button button-small button-link-delete">Excluir</button>
                            </form>
                        </div>
                    </td>
                </tr>
                <?php endforeach; ?>

                <?php
                // ── Wallet-only users (WP users with wallet, no portal login) ──
                foreach ( $wallet_only as $wu ) :
                    global $wpdb;
                    // Get last transaction for this WP user
                    $wp_user_obj = get_user_by( 'email', $wu->email );
                    $wp_uid  = $wp_user_obj ? $wp_user_obj->ID : 0;
                    $last_tx = $wp_uid ? $wpdb->get_row( $wpdb->prepare(
                        "SELECT tipo, valor, descricao, created_at FROM {$wpdb->prefix}tpc_transacoes
                         WHERE user_id = %d AND status = 'confirmado' ORDER BY created_at DESC LIMIT 1",
                        $wp_uid
                    ), ARRAY_A ) : null;
                    $saldo = (float) ( $wu->saldo ?? 0 );
                ?>
                <tr style="background:#fffbf5;">
                    <td>
                        <span style="background:#fef3c7;color:#92400e;padding:2px 8px;border-radius:99px;font-size:var(--sz-text-sm);font-weight:600;white-space:nowrap;">WP User</span>
                    </td>
                    <td>
                        <strong style="display:block;"><?php echo esc_html( $wu->name ?: '—' ); ?></strong>
                        <span style="font-size:var(--sz-text-meta);color:#6b7280;"><?php echo esc_html( $wu->email ); ?></span>
                        <span style="display:block;font-size:var(--sz-text-sm);color:#f59e0b;font-weight:600;">Sem login no portal</span>
                    </td>
                    <td style="font-size:var(--sz-text-base);color:#9ca3af;">—</td>
                    <td>
                        <strong style="color:<?php echo $saldo > 0 ? '#16a34a' : '#9ca3af'; ?>;font-size:var(--sz-text-md);">
                            R$ <?php echo number_format( $saldo, 2, ',', '.' ); ?>
                        </strong>
                    </td>
                    <td style="font-size:var(--sz-text-meta);">
                        <?php if ( $last_tx ) :
                            $tx_color  = $last_tx['tipo'] === 'credito' ? '#16a34a' : '#dc2626';
                            $tx_prefix = $last_tx['tipo'] === 'credito' ? '+' : '−';
                        ?>
                            <span style="color:<?php echo $tx_color; ?>;font-weight:600;"><?php echo $tx_prefix; ?> R$ <?php echo number_format( (float)$last_tx['valor'], 2, ',', '.' ); ?></span>
                            <span style="display:block;color:#9ca3af;font-size:var(--sz-text-sm);"><?php echo esc_html( date_i18n( 'd/m/Y H:i', strtotime( $last_tx['created_at'] ) ) ); ?></span>
                        <?php else : ?>
                            <span style="color:#d1d5db;">—</span>
                        <?php endif; ?>
                    </td>
                    <td><span style="color:#f59e0b;font-size:var(--sz-text-meta);font-weight:600;">Sem portal</span></td>
                    <td>
                        <!-- Create portal login for this WP user -->
                        <form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>" style="display:flex;gap:6px;align-items:center;">
                            <?php wp_nonce_field( 'senderzz_portal_create_user' ); ?>
                            <input type="hidden" name="action" value="senderzz_portal_create_user">
                            <input type="hidden" name="email" value="<?php echo esc_attr( $wu->email ); ?>">
                            <input type="hidden" name="user_name" value="<?php echo esc_attr( $wu->name ); ?>">
                            <input type="hidden" name="user_role" value="client">
                            <input type="hidden" name="shipping_class_id" value="0">
                            <input type="password" name="password" placeholder="Definir senha" required minlength="8" autocomplete="new-password"
                                   style="width:110px;padding:3px 6px;font-size:var(--sz-text-sm);">
                            <button type="submit" class="button button-small button-primary">Criar login</button>
                        </form>
                    </td>
                </tr>
                <?php endforeach; ?>
                </tbody>
            </table>
            <?php endif; ?>
        </div>
        <?php
    }

    /* ── Handlers ─────────────────────────────────────────────────────────── */

    public function handle_create_user(): void {
        check_admin_referer( 'senderzz_portal_create_user' );
        if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );

        $email    = sanitize_email( $_POST['email'] ?? '' );
        $password = sanitize_text_field( $_POST['password'] ?? '' );
        $name     = sanitize_text_field( $_POST['user_name'] ?? '' );
        $role     = in_array( $_POST['user_role'] ?? '', [ 'producer', 'client', 'operator' ], true )
                    ? sanitize_key( $_POST['user_role'] )
                    : 'producer';
        // Multi-class: lê array de IDs ou fallback para campo legado.
        if ( ! empty( $_POST['shipping_class_ids'] ) && is_array( $_POST['shipping_class_ids'] ) ) {
            $class_ids = array_map( 'absint', $_POST['shipping_class_ids'] );
        } else {
            $legacy_id = absint( $_POST['shipping_class_id'] ?? 0 );
            $class_ids = $legacy_id > 0 ? [ $legacy_id ] : [];
        }
        $class_id = ! empty( $class_ids ) ? $class_ids[0] : 0; // legado (primária)

        if ( strlen( $password ) < 8 ) {
            set_transient( 'senderzz_portal_admin_notice', [ 'type' => 'error', 'message' => 'Senha deve ter no mínimo 8 caracteres.' ], 30 );
        } else {
            if ( $role === 'operator' ) {
                $result = function_exists( 'senderzz_create_operator' )
                    ? senderzz_create_operator( $email, $password, $name )
                    : Portal_Auth::create_user( $email, $password, $class_id );
            } else {
                $result = Portal_Auth::create_user( $email, $password, $class_ids );
                // Update name/role and guarantee WP user + wallet for producer/client.
                if ( ! empty( $result['success'] ) ) {
                    global $wpdb;
                    $wpdb->update( $wpdb->prefix . Portal_Auth::TABLE, [ 'name' => $name, 'role' => $role ], [ 'id' => $result['user_id'] ] );
                    $portal_row = $wpdb->get_row( $wpdb->prepare(
                        "SELECT id,email,name,role,wp_user_id FROM {$wpdb->prefix}" . Portal_Auth::TABLE . " WHERE id = %d",
                        (int) $result['user_id']
                    ) );
                    if ( $portal_row && function_exists( 'senderzz_get_or_create_wp_user_for_portal_client' ) ) {
                        $created_wp_uid = senderzz_get_or_create_wp_user_for_portal_client( $portal_row );
                        if ( $role === 'producer' && $created_wp_uid ) {
                            update_user_meta( (int) $created_wp_uid, '_sz_has_motoboy', '1' );
                            update_user_meta( (int) $created_wp_uid, '_sz_has_expedicao', '1' );
                        }
                    }
                    // Garante sincronização multi-class após create (cobre race condition).
                    if ( function_exists( 'sz_set_user_class_ids' ) ) {
                        sz_set_user_class_ids( (int) $result['user_id'], $class_ids );
                    }
                }
            }
            set_transient( 'senderzz_portal_admin_notice', [
                'type'    => $result['success'] ? 'success' : 'error',
                'message' => $result['success'] ? 'Usuário criado com sucesso.' : $result['message'],
            ], 30 );
        }

        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
        exit;
    }

    /**
     * Atualiza as classes de entrega de um usuário do portal.
     */
    public function handle_update_classes(): void {
        check_admin_referer( 'senderzz_portal_update_classes' );
        if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );

        global $wpdb;
        $posted_user_id = absint( $_POST['user_id'] ?? 0 );
        $user_id = $this->resolve_portal_user_id( $posted_user_id );
        if ( ! $user_id ) wp_die( 'Usuário inválido.' );

        $class_ids = [];
        if ( ! empty( $_POST['shipping_class_ids'] ) && is_array( $_POST['shipping_class_ids'] ) ) {
            $class_ids = array_values( array_filter( array_map( 'absint', $_POST['shipping_class_ids'] ), fn($v) => $v > 0 ) );
        }

        // 1. Sincroniza tabela multi-class
        if ( function_exists( 'sz_set_user_class_ids' ) ) {
            sz_set_user_class_ids( $user_id, $class_ids );
        }

        // 2. Força update direto do campo legado (garante consistência independente de cache/função)
        $primary = ! empty( $class_ids ) ? $class_ids[0] : 0;
        $wpdb->update(
            $wpdb->prefix . 'senderzz_portal_users',
            [ 'shipping_class_id' => $primary ],
            [ 'id'                => $user_id ],
            [ '%d' ],
            [ '%d' ]
        );

        // 3. Popula tabela multi-class diretamente (sem depender de sz_set_user_class_ids)
        $mc_table = $wpdb->prefix . 'senderzz_portal_user_classes';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mc_table ) ) === $mc_table ) {
            $wpdb->delete( $mc_table, [ 'portal_user_id' => $user_id ], [ '%d' ] );
            foreach ( $class_ids as $cid ) {
                $wpdb->replace( $mc_table, [
                    'portal_user_id'    => $user_id,
                    'shipping_class_id' => $cid,
                    'created_at'        => current_time( 'mysql' ),
                ], [ '%d', '%d', '%s' ] );
            }
        }

        set_transient( 'senderzz_portal_admin_notice', [
            'type'    => 'success',
            'message' => 'Classes atualizadas. shipping_class_id primário: ' . $primary . '.',
        ], 30 );

        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
        exit;
    }

        public function handle_change_name(): void {
        check_admin_referer( 'senderzz_portal_change_name' );
        if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );

        global $wpdb;
        $user_id  = absint( $_POST['user_id'] ?? 0 );
        $new_name = sanitize_text_field( $_POST['new_name'] ?? '' );

        if ( ! $user_id || $new_name === '' ) {
            set_transient( 'senderzz_portal_admin_notice', [ 'type' => 'error', 'message' => 'Nome inválido.' ], 30 );
            wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
            exit;
        }

        $wpdb->update(
            $wpdb->prefix . 'senderzz_portal_users',
            [ 'name' => $new_name ],
            [ 'id'   => $user_id ],
            [ '%s' ],
            [ '%d' ]
        );

        set_transient( 'senderzz_portal_admin_notice', [
            'type'    => 'success',
            'message' => 'Nome atualizado para "' . $new_name . '".',
        ], 30 );

        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
        exit;
    }

    public function handle_delete_user(): void {
        check_admin_referer( 'senderzz_portal_delete_user' );
        if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );

        global $wpdb;
        $user_id = absint( $_POST['user_id'] ?? 0 );
        $wpdb->delete( $wpdb->prefix . Portal_Auth::TABLE,       [ 'id'      => $user_id ] );
        $wpdb->delete( $wpdb->prefix . Portal_Auth::TABLE_SESS,  [ 'user_id' => $user_id ] );
        $wpdb->delete( $wpdb->prefix . Portal_Auth::TABLE_2FA,   [ 'user_id' => $user_id ] );

        set_transient( 'senderzz_portal_admin_notice', [ 'type' => 'success', 'message' => 'Usuário excluído.' ], 30 );
        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
        exit;
    }

    public function handle_reset_password(): void {
        check_admin_referer( 'senderzz_portal_reset_password' );
        if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );

        $user_id  = absint( $_POST['user_id'] ?? 0 );
        $password = sanitize_text_field( $_POST['new_password'] ?? '' );

        if ( strlen( $password ) < 8 ) {
            set_transient( 'senderzz_portal_admin_notice', [ 'type' => 'error', 'message' => 'Senha deve ter no mínimo 8 caracteres.' ], 30 );
        } else {
            Portal_Auth::change_password( $user_id, $password );
            set_transient( 'senderzz_portal_admin_notice', [ 'type' => 'success', 'message' => 'Senha redefinida.' ], 30 );
        }

        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
        exit;
    }

    public function handle_toggle_status(): void {
        check_admin_referer( 'senderzz_portal_toggle_status' );
        if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );

        global $wpdb;
        $user_id = absint( $_POST['user_id'] ?? 0 );
        $current = $wpdb->get_var( $wpdb->prepare(
            "SELECT status FROM {$wpdb->prefix}" . Portal_Auth::TABLE . " WHERE id = %d",
            $user_id
        ) );
        $new_status = $current === 'active' ? 'suspended' : 'active';
        $wpdb->update( $wpdb->prefix . Portal_Auth::TABLE, [ 'status' => $new_status ], [ 'id' => $user_id ] );
        if ( $new_status === 'suspended' ) {
            $wpdb->delete( $wpdb->prefix . Portal_Auth::TABLE_SESS, [ 'user_id' => $user_id ] );
        }

        $msg = $new_status === 'active' ? 'Usuário ativado.' : 'Usuário suspenso e sessões encerradas.';
        set_transient( 'senderzz_portal_admin_notice', [ 'type' => 'success', 'message' => $msg ], 30 );
        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
        exit;
    }

    public function handle_change_email(): void {
        check_admin_referer( 'senderzz_portal_change_email' );
        if ( ! current_user_can( 'manage_woocommerce' ) ) wp_die( 'Sem permissão.' );

        global $wpdb;
        $user_id   = absint( $_POST['user_id'] ?? 0 );
        $new_email = sanitize_email( $_POST['new_email'] ?? '' );

        if ( ! $user_id || ! is_email( $new_email ) ) {
            set_transient( 'senderzz_portal_admin_notice', [ 'type' => 'error', 'message' => 'E-mail inválido.' ], 30 );
            wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
            exit;
        }

        // Check if new email is already taken in portal
        $taken = $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}" . Portal_Auth::TABLE . " WHERE email = %s AND id != %d",
            $new_email, $user_id
        ) );
        if ( $taken ) {
            set_transient( 'senderzz_portal_admin_notice', [ 'type' => 'error', 'message' => 'Este e-mail já está em uso por outro usuário do portal.' ], 30 );
            wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
            exit;
        }

        // Busca usuário atual ANTES de trocar o e-mail para preservar o wp_user_id.
        $portal_row = $wpdb->get_row( $wpdb->prepare(
            "SELECT id,email,name,role,wp_user_id FROM {$wpdb->prefix}" . Portal_Auth::TABLE . " WHERE id = %d",
            $user_id
        ) );

        $wp_user_id = $portal_row && ! empty( $portal_row->wp_user_id ) ? (int) $portal_row->wp_user_id : 0;
        if ( ! $wp_user_id && $portal_row && function_exists( 'senderzz_is_operator_portal_role' ) && function_exists( 'senderzz_get_or_create_wp_user_for_portal_client' ) && ! senderzz_is_operator_portal_role( $portal_row->role ?? '' ) ) {
            $wp_user_id = senderzz_get_or_create_wp_user_for_portal_client( $portal_row );
        }

        // Atualiza o login do portal. O saldo continua preso ao wp_user_id, nunca ao e-mail.
        $wpdb->update(
            $wpdb->prefix . Portal_Auth::TABLE,
            [ 'email' => $new_email, 'wp_user_id' => $wp_user_id ?: null ],
            [ 'id'    => $user_id ],
            [ '%s', '%d' ],
            [ '%d' ]
        );

        // Se houver WP user vinculado, tenta atualizar o e-mail dele também.
        // Se o e-mail novo já estiver em outro WP user, mantém o login do portal no novo e-mail
        // e preserva o saldo pelo wp_user_id antigo, sem reverter para o e-mail anterior.
        if ( $wp_user_id ) {
            $old_email = $portal_row && ! empty( $portal_row->email ) ? sanitize_email( $portal_row->email ) : '';
            update_user_meta( $wp_user_id, '_senderzz_previous_emails', array_values( array_unique( array_filter( [ strtolower( $old_email ), strtolower( $new_email ) ] ) ) ) );
            $taken_wp = get_user_by( 'email', $new_email );
            if ( ! $taken_wp || (int) $taken_wp->ID === $wp_user_id ) {
                $updated = wp_update_user( [ 'ID' => $wp_user_id, 'user_email' => $new_email ] );
                if ( ! is_wp_error( $updated ) ) clean_user_cache( $wp_user_id );
            } else {
                update_user_meta( $wp_user_id, '_senderzz_wp_email_sync_error', 'Novo e-mail já existe em outro usuário WP. Login do portal mantido no novo e-mail; carteira mantida por ID.' );
            }
            if ( function_exists( 'senderzz_ensure_tpc_wallet' ) && $portal_row && function_exists( 'senderzz_is_operator_portal_role' ) && ! senderzz_is_operator_portal_role( $portal_row->role ?? '' ) ) {
                senderzz_ensure_tpc_wallet( $wp_user_id );
                if ( function_exists( 'senderzz_wallet_rebuild_from_transactions' ) ) senderzz_wallet_rebuild_from_transactions( $wp_user_id );
            }
        }

        // Revoke all sessions (security — force re-login with new email)
        $wpdb->delete( $wpdb->prefix . Portal_Auth::TABLE_SESS, [ 'user_id' => $user_id ] );

        set_transient( 'senderzz_portal_admin_notice', [
            'type'    => 'success',
            'message' => 'E-mail alterado para ' . $new_email . '. Sessões encerradas por segurança — usuário precisará fazer login novamente.',
        ], 30 );
        wp_safe_redirect( admin_url( 'admin.php?page=senderzz&tab=usuarios' ) );
        exit;
    }
}
