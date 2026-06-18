<?php
/**
 * admin.php
 *
 * Página WooCommerce → Carteira de Frete
 * - Configurações (token ME, webhook secret, JWT secret)
 * - Lista de clientes com saldo
 * - Ajuste manual de saldo
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TPC_ADMIN_LOADED' ) ) return;
define( 'SENDERZZ_TPC_ADMIN_LOADED', true );



if ( ! function_exists( 'tpc_admin_get_live_user_name' ) ) {
    /**
     * Nome vivo do cliente/produtor para telas de carteira.
     * Não usa nome salvo/congelado em tabelas financeiras.
     */
    function tpc_admin_get_live_user_name( int $user_id ): string {
        $user = get_userdata( $user_id );
        if ( ! $user ) {
            return 'ID ' . $user_id;
        }

        $candidates = [];

        $first = trim( (string) get_user_meta( $user_id, 'first_name', true ) );
        $last  = trim( (string) get_user_meta( $user_id, 'last_name', true ) );
        $candidates[] = trim( $first . ' ' . $last );

        $billing_first = trim( (string) get_user_meta( $user_id, 'billing_first_name', true ) );
        $billing_last  = trim( (string) get_user_meta( $user_id, 'billing_last_name', true ) );
        $candidates[] = trim( $billing_first . ' ' . $billing_last );

        $candidates[] = trim( (string) $user->display_name );
        $candidates[] = trim( (string) $user->user_login );

        foreach ( $candidates as $name ) {
            if ( $name !== '' ) {
                return $name;
            }
        }

        return $user->user_email ?: 'ID ' . $user_id;
    }
}

function tpc_status_label_admin( string $status ): string {
    return match ( $status ) {
        'pendente'   => 'Pendente',
        'analise'    => 'Em análise',
        'confirmado' => 'Confirmado',
        'cancelado'  => 'Cancelado',
        default      => ucfirst( $status ),
    };
}



/**
 * Hostinger/browser cache guard para a Carteira Senderzz.
 * Evita F5/back-forward cache mostrando saldo antigo após reset, PIX ou alteração de carteira.
 */
if ( ! function_exists( 'tpc_admin_is_wallet_screen' ) ) {
    function tpc_admin_is_wallet_screen(): bool {
        if ( ! is_admin() ) {
            return false;
        }

        $page = sanitize_key( $_GET['page'] ?? '' );
        $tab  = sanitize_key( $_GET['tab'] ?? '' );

        return $page === 'tpc-carteira' || ( $page === 'senderzz' && $tab === 'carteira' );
    }
}

if ( ! function_exists( 'tpc_admin_force_no_cache_headers' ) ) {
    function tpc_admin_force_no_cache_headers(): void {
        if ( ! tpc_admin_is_wallet_screen() ) {
            return;
        }

        if ( ! headers_sent() ) {
            nocache_headers();
            header( 'Cache-Control: no-store, no-cache, must-revalidate, max-age=0, private', true );
            header( 'Cache-Control: post-check=0, pre-check=0', false );
            header( 'Pragma: no-cache', true );
            header( 'Expires: Wed, 11 Jan 1984 05:00:00 GMT', true );
            header( 'Surrogate-Control: no-store', true );
            header( 'CDN-Cache-Control: no-store', true );
            header( 'X-LiteSpeed-Cache-Control: no-cache, private, no-store', true );
            header( 'X-Accel-Expires: 0', true );
            header( 'Last-Modified: ' . gmdate( 'D, d M Y H:i:s' ) . ' GMT', true );
        }
    }
}
add_action( 'admin_init', 'tpc_admin_force_no_cache_headers', 0 );
add_action( 'send_headers', 'tpc_admin_force_no_cache_headers', 0 );

add_action( 'admin_head', function () {
    if ( ! tpc_admin_is_wallet_screen() ) {
        return;
    }
    echo "\n" . '<meta http-equiv="Cache-Control" content="no-store, no-cache, must-revalidate, max-age=0">' . "\n";
    echo '<meta http-equiv="Pragma" content="no-cache">' . "\n";
    echo '<meta http-equiv="Expires" content="0">' . "\n";
}, 0 );

add_action( 'admin_footer', function () {
    if ( ! tpc_admin_is_wallet_screen() ) {
        return;
    }
    ?>
    <script>
    (function(){
        try {
            if (window.history && window.history.replaceState) {
                var u = new URL(window.location.href);
                u.searchParams.set('_sznc', String(Date.now()));
                window.history.replaceState(null, document.title, u.toString());
            }
            window.addEventListener('pageshow', function(e){
                var nav = performance && performance.getEntriesByType ? performance.getEntriesByType('navigation')[0] : null;
                if (e.persisted || (nav && nav.type === 'back_forward')) {
                    window.location.reload();
                }
            });
        } catch(e) {}
    })();
    </script>
    <?php
}, 999 );

// Senderzz v221: submenu legado removido da origem; aba Carteira vive em Admin > Senderzz.
// add_action( 'admin_menu', function () {
//     add_submenu_page(
//         'woocommerce',
//         'Carteira de Frete',
//         'Carteira de Frete',
//         'manage_woocommerce',
//         'tpc-carteira',
//         'tpc_admin_render'
//     );
// } );

add_action( 'admin_init', function () {
    // Salvar configurações
    if (
        isset( $_POST['tpc_config_nonce'] ) &&
        wp_verify_nonce( $_POST['tpc_config_nonce'], 'tpc_salvar_config' ) &&
        current_user_can( 'manage_woocommerce' )
    ) {
        update_option( 'tpc_me_token',        sanitize_text_field( $_POST['tpc_me_token']        ?? '' ) );
        update_option( 'tpc_webhook_secret',  sanitize_text_field( $_POST['tpc_webhook_secret']  ?? '' ) );
        update_option( 'tpc_saldo_minimo',    floatval( $_POST['tpc_saldo_minimo'] ?? 0 ) );
        update_option( 'tpc_pix_valid_minutes', max( 5, min( 1440, absint( $_POST['tpc_pix_valid_minutes'] ?? 30 ) ) ) );
        update_option( 'tpc_pix_auto_cancel_expired', isset( $_POST['tpc_pix_auto_cancel_expired'] ) ? 'yes' : 'no' );
        update_option( 'senderzz_enforce_wallet_on_label', isset( $_POST['senderzz_enforce_wallet_on_label'] ) ? 'yes' : 'no' );
        update_option( 'senderzz_block_duplicate_label', isset( $_POST['senderzz_block_duplicate_label'] ) ? 'yes' : 'no' );
        if ( isset( $_POST['senderzz_checkout_template_id'] ) ) {
            update_option( 'senderzz_checkout_template_id', absint( $_POST['senderzz_checkout_template_id'] ) );
        }
        $owners_map = [];
        if ( isset( $_POST['senderzz_shipping_class_wallet_owners'] ) && is_array( $_POST['senderzz_shipping_class_wallet_owners'] ) ) {
            foreach ( $_POST['senderzz_shipping_class_wallet_owners'] as $class_id => $owner_id ) {
                $owners_map[ (string) absint( $class_id ) ] = absint( $owner_id );
            }
        }
        update_option( 'senderzz_shipping_class_wallet_owners', $owners_map );

        add_action( 'admin_notices', fn() =>
            print '<div class="notice notice-success is-dismissible"><p>Configurações salvas!</p></div>'
        );
    }

    // Ajuste manual de saldo desativado por regra operacional Senderzz.
    // Qualquer crédito deve vir do saldo confirmado no Melhor Envio via PIX.
    if (
        isset( $_POST['tpc_ajuste_nonce'] ) &&
        wp_verify_nonce( $_POST['tpc_ajuste_nonce'], 'tpc_ajuste_saldo' ) &&
        current_user_can( 'manage_woocommerce' )
    ) {
        add_action( 'admin_notices', fn() =>
            print '<div class="notice notice-error is-dismissible"><p>Crédito/débito manual está desativado. Use recarga PIX confirmada pelo Melhor Envio.</p></div>'
        );
    }


    // Reset manual da carteira Senderzz — operação destrutiva protegida por nonce + confirmação digitada.
    if (
        isset( $_POST['tpc_reset_wallet_nonce'] ) &&
        wp_verify_nonce( $_POST['tpc_reset_wallet_nonce'], 'tpc_reset_wallet_all' ) &&
        current_user_can( 'manage_woocommerce' )
    ) {
        $confirm = strtoupper( trim( sanitize_text_field( $_POST['tpc_reset_wallet_confirm'] ?? '' ) ) );
        if ( $confirm !== 'RESETAR' ) {
            add_action( 'admin_notices', fn() =>
                print '<div class="notice notice-error is-dismissible"><p>Reset cancelado. Digite RESETAR para confirmar.</p></div>'
            );
        } else {
            $result = function_exists( 'tpc_admin_reset_wallet_all' ) ? tpc_admin_reset_wallet_all() : [ 'ok' => false, 'error' => 'Função de reset indisponível.' ];
            if ( ! empty( $result['ok'] ) ) {
                $msg = sprintf(
                    'Carteira resetada. Saldos zerados: %d | Transações removidas: %d | Recargas limpas: %d | Metas de pedidos limpas: %d',
                    (int) ( $result['wallets'] ?? 0 ),
                    (int) ( $result['transactions'] ?? 0 ),
                    (int) ( $result['recharges'] ?? 0 ),
                    (int) ( $result['order_meta'] ?? 0 )
                );
                add_action( 'admin_notices', fn() =>
                    print '<div class="notice notice-success is-dismissible"><p>' . esc_html( $msg ) . '</p></div>'
                );
            } else {
                $err = (string) ( $result['error'] ?? 'Falha ao resetar carteira.' );
                add_action( 'admin_notices', fn() =>
                    print '<div class="notice notice-error is-dismissible"><p>' . esc_html( $err ) . '</p></div>'
                );
            }
        }
    }
} );

function tpc_admin_render(): void {
    $tab = sanitize_key( $_GET['subtab'] ?? ( $_GET['tab'] ?? 'clientes' ) );
    ?>
    <div class="wrap">
        <h1>💳 Carteira de Frete</h1>

        <nav class="nav-tab-wrapper" style="margin-bottom:20px;">
            <a href="admin.php?page=senderzz&tab=carteira&subtab=clientes"      class="nav-tab <?php echo $tab === 'clientes'     ? 'nav-tab-active' : ''; ?>">Clientes</a>
            <a href="admin.php?page=senderzz&tab=carteira&subtab=transacoes"    class="nav-tab <?php echo $tab === 'transacoes'   ? 'nav-tab-active' : ''; ?>">Transações</a>
            <a href="admin.php?page=senderzz&tab=carteira&subtab=configuracoes" class="nav-tab <?php echo $tab === 'configuracoes' ? 'nav-tab-active' : ''; ?>">Configurações</a>
            <a href="admin.php?page=senderzz&tab=carteira&subtab=api"           class="nav-tab <?php echo $tab === 'api'          ? 'nav-tab-active' : ''; ?>">REST API</a>
            <a href="admin.php?page=senderzz&tab=carteira&subtab=pix"           class="nav-tab <?php echo $tab === 'pix'          ? 'nav-tab-active' : ''; ?>">💳 PIX / Reconciliação</a>
        </nav>

        <?php
        match ( $tab ) {
            'clientes'      => tpc_tab_clientes(),
            'transacoes'    => tpc_tab_transacoes(),
            'configuracoes' => tpc_tab_configuracoes(),
            'api'           => tpc_tab_api(),
            'pix'           => tpc_tab_pix_reconciliacao(),
            default         => tpc_tab_clientes(),
        };
        ?>
    </div>
    <?php
}

/* ── Aba: Clientes ─────────────────────────────────────────────────────── */
function tpc_tab_clientes(): void {
    global $wpdb;

    // Query movida para após processamento do POST — ver abaixo
    $clientes_placeholder = true;

    // ── Estado: PIX gerado para recarga admin ──
    $pix_pendente = get_transient( 'tpc_admin_pix_' . get_current_user_id() );

    // ── Processar solicitação de recarga via PIX ──
    $pix_data     = null;
    $credito_msg  = '';
    $credito_erro = '';

    if (
        isset( $_POST['tpc_pix_recarga_nonce'] ) &&
        wp_verify_nonce( $_POST['tpc_pix_recarga_nonce'], 'tpc_pix_recarga' ) &&
        current_user_can( 'manage_woocommerce' )
    ) {
        $email  = sanitize_email( $_POST['tpc_credito_email'] ?? '' );
        $valor  = abs( floatval( $_POST['tpc_credito_valor'] ?? 0 ) );
        $motivo = sanitize_text_field( $_POST['tpc_credito_motivo'] ?? 'Recarga via PIX pelo admin' );
        $user   = get_user_by( 'email', $email );

        if ( ! $user ) {
            $credito_erro = 'Nenhum usuário encontrado com o e-mail <strong>' . esc_html( $email ) . '</strong>.';
        } elseif ( $valor < 10 ) {
            $credito_erro = 'Valor mínimo: R$ 10,00.';
        } else {
            // Cria recarga interna pendente, sem criar pedido no WooCommerce
            $recarga_id = tpc_criar_recarga_pendente( $user->ID, $valor, $motivo );
            if ( ! $recarga_id ) {
                $credito_erro = 'Erro ao criar recarga interna. Tente novamente.';
            } else {
                // Gera PIX real no Melhor Envio vinculado à recarga interna
                $pix_result = tpc_gerar_pix_melhor_envio( $recarga_id );
                if ( is_wp_error( $pix_result ) ) {
                    $credito_erro = 'Erro ao gerar PIX: ' . $pix_result->get_error_message();
                    tpc_cancelar_recarga_pendente( $recarga_id );
                } else {
                    // Guarda estado para exibir o QR Code
                    $pix_pendente = [
                        'recarga_id' => $recarga_id,
                        'user_id'    => $user->ID,
                        'user_nome'  => $user->display_name,
                        'user_email' => $user->user_email,
                        'valor'      => $valor,
                        'motivo'     => $motivo,
                        'pix'        => $pix_result,
                    ];
                    set_transient( 'tpc_admin_pix_' . get_current_user_id(), $pix_pendente, ( tpc_pix_validade_minutos() + 5 ) * MINUTE_IN_SECONDS );
                    $pix_data = $pix_pendente;
                }
            }
        }
    }

    // ── Cancelar PIX pendente ──
    if ( isset( $_GET['tpc_cancelar_pix'] ) && wp_verify_nonce( $_GET['_wpnonce'] ?? '', 'tpc_cancelar_pix' ) ) {
        if ( is_array( $pix_pendente ) && ! empty( $pix_pendente['recarga_id'] ) ) {
            tpc_cancelar_recarga_pendente( (int) $pix_pendente['recarga_id'] );
        }
        delete_transient( 'tpc_admin_pix_' . get_current_user_id() );
        $pix_pendente = null;
    }

    // Usa pix pendente salvo se não gerou um novo agora
    if ( ! $pix_data && $pix_pendente ) {
        $pix_data = $pix_pendente;
    }
    ?>

    <!-- Recarga via PIX -->
    <div style="background:#fff;border:1px solid #ddd;border-radius:8px;padding:20px 24px;max-width:760px;margin-bottom:24px;">
        <h3 style="margin-top:0;margin-bottom:4px;">💳 Adicionar crédito via PIX</h3>
        <p style="color:#666;font-size:var(--sz-text-base);margin-bottom:16px;">
            Gera um PIX real no Melhor Envio. O crédito só é adicionado após a confirmação do pagamento.
        </p>

        <?php if ( $credito_msg ) : ?>
            <div style="background:#d4edda;border:1px solid #c3e6cb;border-radius:6px;padding:10px 14px;margin-bottom:14px;color:#1d6f42;">✅ <?php echo $credito_msg; ?></div>
        <?php endif; ?>
        <?php if ( $credito_erro ) : ?>
            <div style="background:#fde8e8;border:1px solid #f5c6cb;border-radius:6px;padding:10px 14px;margin-bottom:14px;color:#c0392b;">❌ <?php echo $credito_erro; ?></div>
        <?php endif; ?>

        <?php if ( $pix_data ) : ?>
            <!-- QR Code PIX gerado -->
            <div style="display:flex;gap:24px;align-items:flex-start;flex-wrap:wrap;">
                <div style="text-align:center;">
                    <?php
                    $qr_src = $pix_data['pix']['qr_src'] ?? '';
                    $qr_raw = $pix_data['pix']['qr_code'] ?? '';
                    $link   = $pix_data['pix']['link'] ?? '';
                    $copia  = $pix_data['pix']['copia_cola'] ?? '';

                    if ( ! $qr_src && function_exists( 'tpc_pix_normalize_img_src' ) ) {
                        $qr_src = tpc_pix_normalize_img_src( (string) $qr_raw );
                    }
                    if ( ! $qr_src && $link && preg_match( '#\\.(svg|png|jpe?g|webp)(\\?.*)?$#i', $link ) ) {
                        $qr_src = (string) $link;
                    }
                    if ( ! $qr_src && $copia && function_exists( 'tpc_pix_qr_from_copy_paste' ) ) {
                        $qr_src = tpc_pix_qr_from_copy_paste( (string) $copia );
                    }
                    ?>
                    <?php if ( $qr_src ) : ?>
                        <img src="<?php echo esc_url( $qr_src ); ?>" style="width:180px;height:180px;border-radius:8px;border:1px solid #eee;">
                    <?php else : ?>
                        <div style="width:180px;height:180px;display:flex;align-items:center;justify-content:center;background:#f5f5f5;border-radius:8px;font-size:var(--sz-text-meta);color:#999;border:1px solid #eee;">QR Code não disponível</div>
                    <?php endif; ?>
                    <?php if ( $link ) : ?>
                        <p style="margin:8px 0 0;"><a href="<?php echo esc_url( $link ); ?>" class="button button-small" target="_blank">Abrir link do PIX</a></p>
                    <?php endif; ?>
                    <p style="font-size:var(--sz-text-sm);color:#999;margin-top:6px;">Escaneie para pagar</p>
                </div>
                <div style="flex:1;min-width:240px;">
                    <p style="margin-bottom:6px;font-size:var(--sz-text-base);"><strong>Cliente:</strong> <?php echo esc_html( $pix_data['user_nome'] ); ?> (<?php echo esc_html( $pix_data['user_email'] ); ?>)</p>
                    <p style="margin-bottom:6px;font-size:var(--sz-text-base);"><strong>Valor:</strong> R$ <?php echo number_format( $pix_data['valor'], 2, ',', '.' ); ?></p>
                    <p style="margin-bottom:6px;font-size:var(--sz-text-base);"><strong>Recarga:</strong> #<?php echo $pix_data['recarga_id'] ?? ''; ?></p>
                    <?php if ( ! empty( $pix_data['pix']['expira_em'] ) ) : ?>
                        <p style="margin-bottom:10px;font-size:var(--sz-text-meta);color:#666;"><strong>Validade:</strong> <?php echo esc_html( date_i18n( 'd/m/Y H:i', tpc_pix_timestamp( (string) $pix_data['pix']['expira_em'] ) ?: time() ) ); ?></p>
                    <?php endif; ?>

                    <?php if ( ! empty( $pix_data['pix']['copia_cola'] ) ) : ?>
                        <label style="font-size:var(--sz-text-meta);font-weight:600;color:#555;">Pix Copia e Cola:</label>
                        <div style="display:flex;gap:6px;margin-top:4px;margin-bottom:12px;">
                            <input type="text" value="<?php echo esc_attr( $pix_data['pix']['copia_cola'] ); ?>"
                                   readonly id="tpc-admin-pix-cc"
                                   style="flex:1;font-size:var(--sz-text-sm);font-family:var(--sz-font);padding:6px 8px;border:1px solid #ddd;border-radius:6px;">
                            <button onclick="document.getElementById('tpc-admin-pix-cc').select();document.execCommand('copy');this.textContent='Copiado!';setTimeout(()=>this.textContent='Copiar',2000)"
                                    class="button button-small">Copiar</button>
                        </div>
                    <?php endif; ?>

                    <div style="background:#fff8e1;border:1px solid #ffe082;border-radius:6px;padding:8px 12px;font-size:var(--sz-text-meta);color:#856404;margin-bottom:12px;">
                        ⏳ Aguardando pagamento. O crédito será adicionado automaticamente após a confirmação do PIX.
                    </div>

                    <div style="display:flex;gap:8px;">
                        <a href="<?php echo esc_url( wp_nonce_url( add_query_arg( [ 'page' => 'senderzz', 'tab' => 'carteira', 'subtab' => 'clientes', 'tpc_cancelar_pix' => 1 ] ), 'tpc_cancelar_pix' ) ); ?>"
                           class="button" onclick="return confirm('Cancelar este PIX?')">Cancelar PIX</a>
                        <a href="<?php echo esc_url( add_query_arg( [ 'page' => 'senderzz', 'tab' => 'carteira', 'subtab' => 'transacoes' ], admin_url( 'admin.php' ) ) ); ?>"
                           class="button">Ver transações</a>
                    </div>

                    <?php
                    $recarga_id  = (int) ( $pix_data['recarga_id'] ?? 0 );
                    $debug_store = $recarga_id ? get_option( 'tpc_pix_recarga_' . $recarga_id ) : null;
                    $debug_raw   = is_array( $debug_store ) ? ( $debug_store['raw'] ?? null ) : null;
                    $debug_code  = is_array( $debug_store ) ? ( $debug_store['http_code'] ?? '' ) : '';
                    $last_req    = get_option( 'tpc_last_pix_request' );
                    $last_resp   = get_option( 'tpc_last_pix_response' );
                    $last_map    = get_option( 'tpc_last_pix_mapped' );
                    ?>
                    <details style="margin-top:12px;background:#f6f7f7;border:1px solid #dcdcde;border-radius:6px;padding:10px;">
                        <summary style="cursor:pointer;font-weight:600;">Debug PIX / logs</summary>
                        <p style="font-size:var(--sz-text-meta);margin:8px 0;"><strong>HTTP:</strong> <?php echo esc_html( $debug_code ?: '-' ); ?></p>
                        <p style="font-size:var(--sz-text-meta);margin:8px 0;">Logs também ficam em <strong>WooCommerce → Status → Logs</strong>, fontes: <code>tp-carteira-pix_request</code>, <code>tp-carteira-pix_response</code>, <code>tp-carteira-pix_mapped</code>.</p>
                        <textarea readonly style="width:100%;height:180px;font-family:var(--sz-font);font-size:var(--sz-text-sm);"><?php echo esc_textarea( wp_json_encode( [
                            'recarga_id' => $recarga_id,
                            'raw_recarga' => $debug_raw,
                            'last_request' => $last_req,
                            'last_response' => $last_resp,
                            'last_mapped' => $last_map,
                        ], JSON_PRETTY_PRINT | JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ) ); ?></textarea>
                    </details>
                </div>
            </div>

        <?php else : ?>
            <!-- Formulário para gerar novo PIX -->
            <form method="post">
                <?php wp_nonce_field( 'tpc_pix_recarga', 'tpc_pix_recarga_nonce' ); ?>
                <div style="display:grid;grid-template-columns:2fr 1fr 2fr auto;gap:10px;align-items:end;">
                    <div>
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;color:#555;">E-mail do cliente</label>
                        <input type="email" name="tpc_credito_email" placeholder="cliente@email.com" required
                               style="width:100%;padding:8px 10px;border:1px solid #ddd;border-radius:6px;font-size:var(--sz-text-md);"
                               value="<?php echo esc_attr( $_POST['tpc_credito_email'] ?? '' ); ?>">
                    </div>
                    <div>
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;color:#555;">Valor (R$)</label>
                        <input type="number" name="tpc_credito_valor" placeholder="0,00" min="10" step="0.01" required
                               style="width:100%;padding:8px 10px;border:1px solid #ddd;border-radius:6px;font-size:var(--sz-text-md);">
                    </div>
                    <div>
                        <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;color:#555;">Motivo</label>
                        <input type="text" name="tpc_credito_motivo" value="Recarga via PIX pelo admin"
                               style="width:100%;padding:8px 10px;border:1px solid #ddd;border-radius:6px;font-size:var(--sz-text-md);">
                    </div>
                    <div>
                        <input type="submit" class="button button-primary" value="Gerar PIX" style="padding:8px 16px;height:auto;">
                    </div>
                </div>
            </form>
        <?php endif; ?>
    </div>

    <div style="background:#fff;border:1px solid #d63638;border-left:4px solid #d63638;border-radius:8px;padding:18px 20px;max-width:760px;margin-bottom:24px;">
        <h3 style="margin-top:0;margin-bottom:6px;color:#b32d2e;">Resetar carteira Senderzz</h3>
        <p style="margin:0 0 12px;color:#50575e;">Zera todos os saldos, remove histórico de transações/recargas internas e limpa metas antigas de carteira nos pedidos. Não altera o saldo real da conta Melhor Envio.</p>
        <form method="post" onsubmit="return confirm('Confirmar reset total da carteira Senderzz? Esta ação não pode ser desfeita.');" style="display:flex;gap:10px;align-items:end;flex-wrap:wrap;">
            <?php wp_nonce_field( 'tpc_reset_wallet_all', 'tpc_reset_wallet_nonce' ); ?>
            <div>
                <label style="display:block;font-size:var(--sz-text-meta);font-weight:600;margin-bottom:4px;">Digite RESETAR</label>
                <input type="text" name="tpc_reset_wallet_confirm" autocomplete="off" style="width:160px;">
            </div>
            <button type="submit" class="button button-secondary" style="border-color:#d63638;color:#b32d2e;">Resetar carteira</button>
        </form>
    </div>

    <?php
    // Recarrega lista após qualquer POST para refletir saldo atual
    $clientes = $wpdb->get_results( "
        SELECT c.user_id, c.saldo, c.updated_at,
               u.display_name, u.user_email,
               COUNT(t.id) as total_transacoes
        FROM   {$wpdb->prefix}tpc_carteira c
        JOIN   {$wpdb->users} u ON u.ID = c.user_id
        LEFT JOIN {$wpdb->prefix}tpc_transacoes t ON t.user_id = c.user_id
        GROUP  BY c.user_id
        ORDER  BY c.saldo DESC
        LIMIT 100
    " );
    ?>
    <h2>Clientes com carteira ativa</h2>
    <table class="widefat striped">
        <thead>
            <tr><th style="width:50px">ID</th><th>Cliente</th><th>Email</th><th>Saldo carteira</th><th>Reservado</th><th>Disponível</th><th>Transações</th><th>Última atualização</th><th>Ações</th></tr>
        </thead>
        <tbody>
        <?php foreach ( $clientes as $c ) : ?>
            <tr>
                <td style="color:#888;font-size:var(--sz-text-meta)"><?php echo (int) $c->user_id; ?></td>
                <td><strong><?php echo esc_html( tpc_admin_get_live_user_name( (int) $c->user_id ) ); ?></strong></td>
                <td><?php echo esc_html( $c->user_email ); ?></td>
                <?php $sz_wallet_breakdown = function_exists( 'tpc_admin_get_user_wallet_breakdown' ) ? tpc_admin_get_user_wallet_breakdown( (int) $c->user_id ) : [ 'saldo' => (float) $c->saldo, 'reservado' => 0, 'disponivel' => (float) $c->saldo ]; ?>
                <td><strong style="color:<?php echo $sz_wallet_breakdown['saldo'] > 0 ? '#1d6f42' : '#c0392b'; ?>">
                    R$ <?php echo number_format( (float) $sz_wallet_breakdown['saldo'], 2, ',', '.' ); ?>
                </strong></td>
                <td>R$ <?php echo number_format( (float) $sz_wallet_breakdown['reservado'], 2, ',', '.' ); ?></td>
                <td>R$ <?php echo number_format( (float) $sz_wallet_breakdown['disponivel'], 2, ',', '.' ); ?></td>
                <td><?php echo intval( $c->total_transacoes ); ?></td>
                <td><?php echo esc_html( $c->updated_at ); ?></td>
                <td>
                    <a href="admin.php?page=senderzz&tab=carteira&subtab=transacoes&user_id=<?php echo $c->user_id; ?>" class="button button-small">Extrato</a>
                    <button class="button button-small" disabled title="Ajuste manual desativado. Use recarga PIX confirmada pelo Melhor Envio.">Ajuste manual bloqueado</button>
                </td>
            </tr>
        <?php endforeach; ?>
        <?php if ( empty( $clientes ) ) : ?>
            <tr><td colspan="9" style="text-align:center;color:#999">Nenhum cliente com carteira ainda.</td></tr>
        <?php endif; ?>
        </tbody>
    </table>

    <!-- Modal de ajuste de saldo -->
    <div id="tpc-modal-ajuste" style="display:none; position:fixed; top:0; left:0; right:0; bottom:0; background:rgba(0,0,0,.5); z-index:99999; align-items:center; justify-content:center;">
        <div style="background:#fff; border-radius:8px; padding:24px; max-width:440px; width:90%;">
            <h3 id="tpc-modal-title" style="margin-top:0">Ajustar saldo</h3>
            <form method="post">
                <?php wp_nonce_field( 'tpc_ajuste_saldo', 'tpc_ajuste_nonce' ); ?>
                <input type="hidden" name="tpc_ajuste_user" id="tpc_ajuste_user">
                <table class="form-table" style="margin:0">
                    <tr>
                        <th>Tipo</th>
                        <td>
                            <select name="tpc_ajuste_tipo" style="width:100%">
                                <option value="credito">Crédito (adicionar)</option>
                                <option value="debito">Débito (remover)</option>
                            </select>
                        </td>
                    </tr>
                    <tr>
                        <th>Valor (R$)</th>
                        <td><input type="number" name="tpc_ajuste_valor" min="0.01" step="0.01" style="width:100%" required></td>
                    </tr>
                    <tr>
                        <th>Motivo</th>
                        <td><input type="text" name="tpc_ajuste_motivo" value="Ajuste manual pelo admin" style="width:100%"></td>
                    </tr>
                </table>
                <div style="margin-top:16px; display:flex; gap:8px; justify-content:flex-end;">
                    <button type="button" class="button" onclick="document.getElementById('tpc-modal-ajuste').style.display='none'">Cancelar</button>
                    <input type="submit" class="button button-primary" value="Confirmar ajuste">
                </div>
            </form>
        </div>
    </div>
    <script>
    function tpcAjuste(userId, nome) {
        document.getElementById('tpc_ajuste_user').value = userId;
        document.getElementById('tpc-modal-title').textContent = 'Ajustar saldo: ' + nome;
        document.getElementById('tpc-modal-ajuste').style.display = 'flex';
    }
    </script>
    <?php
}

/* ── Aba: Transações ────────────────────────────────────────────────────── */
function tpc_tab_transacoes(): void {
    $user_id  = (int) ( $_GET['user_id'] ?? 0 );
    $tipo     = sanitize_key( $_GET['tipo'] ?? '' );
    $data_ini = sanitize_text_field( $_GET['data_ini'] ?? '' );
    $data_fim = sanitize_text_field( $_GET['data_fim'] ?? '' );
    $page     = max( 1, (int) ( $_GET['paged'] ?? 1 ) );
    $per_page = 20;

    $args = compact( 'tipo', 'data_ini', 'data_fim', 'page', 'per_page' );
    if ( $user_id ) $args['user_id_filter'] = $user_id;

    // Se filtrando por usuário, usa funções normais; senão faz query direta
    global $wpdb;
    $where = [ "(meta_json IS NULL OR meta_json NOT LIKE '%senderzz_admin_me_allocation%')" ];
    if ( $user_id )  $where[] = $wpdb->prepare( 'user_id = %d', $user_id );
    if ( $tipo )     $where[] = $wpdb->prepare( 'tipo = %s', $tipo );
    if ( $data_ini ) $where[] = $wpdb->prepare( 'created_at >= %s', $data_ini . ' 00:00:00' );
    if ( $data_fim ) $where[] = $wpdb->prepare( 'created_at <= %s', $data_fim . ' 23:59:59' );

    $sql_where = implode( ' AND ', $where );
    $offset    = ( $page - 1 ) * $per_page;
    $total     = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$wpdb->prefix}tpc_transacoes WHERE $sql_where" );
    $rows      = $wpdb->get_results( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}tpc_transacoes WHERE $sql_where ORDER BY created_at DESC LIMIT %d OFFSET %d", $per_page, $offset ), ARRAY_A );
    ?>
    <h2>Transações</h2>

    <?php if ( isset( $_GET['tpc_msg'] ) ) : ?>
        <div class="notice notice-success is-dismissible"><p><?php echo esc_html( match( sanitize_key( $_GET['tpc_msg'] ) ) { 'transacao_excluida' => 'Transação excluída e saldo recalculado.', 'pix_verificado' => 'Verificação de PIX executada. Veja detalhes nos logs.', default => 'Ação concluída.' } ); ?></p></div>
    <?php endif; ?>

    <p style="margin: 8px 0 16px;">
        <a class="button button-secondary" href="<?php echo esc_url( wp_nonce_url( add_query_arg( [ 'page' => 'senderzz', 'tab' => 'carteira', 'subtab' => 'transacoes', 'tpc_verificar_pix' => 1 ], admin_url( 'admin.php' ) ), 'tpc_verificar_pix' ) ); ?>">Verificar PIX pendentes agora</a>
    </p>

    <form method="get" style="display:flex; gap:8px; flex-wrap:wrap; align-items:flex-end; margin-bottom:16px;">
        <input type="hidden" name="page" value="tpc-carteira">
        <input type="hidden" name="tab" value="transacoes">
        <div>
            <label style="display:block; font-size:var(--sz-text-meta);">Tipo</label>
            <select name="tipo"><option value="">Todos</option><option value="credito" <?php selected($tipo,'credito'); ?>>Crédito</option><option value="debito" <?php selected($tipo,'debito'); ?>>Débito</option></select>
        </div>
        <div>
            <label style="display:block; font-size:var(--sz-text-meta);">Data início</label>
            <input type="date" name="data_ini" value="<?php echo esc_attr($data_ini); ?>">
        </div>
        <div>
            <label style="display:block; font-size:var(--sz-text-meta);">Data fim</label>
            <input type="date" name="data_fim" value="<?php echo esc_attr($data_fim); ?>">
        </div>
        <?php if ( $user_id ) : ?><input type="hidden" name="user_id" value="<?php echo $user_id; ?>"><?php endif; ?>
        <input type="submit" class="button" value="Filtrar">
    </form>

    <table class="widefat striped">
        <thead>
            <tr><th>ID</th><th>Usuário</th><th>Tipo</th><th>Status</th><th>Valor</th><th>Saldo após</th><th>Descrição</th><th>Data</th><th>Ações</th></tr>
        </thead>
        <tbody>
        <?php foreach ( $rows as $t ) :
            $u = get_user_by( 'id', $t['user_id'] );
        ?>
            <tr>
                <td>#<?php echo $t['id']; ?></td>
                <td><?php echo esc_html( $u ? tpc_admin_get_live_user_name( (int) $t['user_id'] ) : 'ID ' . $t['user_id'] ); ?></td>
                <td>
                    <span style="padding:2px 10px; border-radius:20px; font-size:var(--sz-text-sm); font-weight:600;
                        background:<?php echo $t['tipo'] === 'credito' ? '#d4edda' : '#f8d7da'; ?>;
                        color:<?php echo $t['tipo'] === 'credito' ? '#1d6f42' : '#721c24'; ?>">
                        <?php echo $t['tipo'] === 'credito' ? '+ Crédito' : '− Débito'; ?>
                    </span>
                </td>
                <td><?php echo esc_html( tpc_status_label_admin( $t['status'] ?? 'confirmado' ) ); ?></td>
                <td><strong>R$ <?php echo number_format( (float)$t['valor'], 2, ',', '.' ); ?></strong></td>
                <td>R$ <?php echo number_format( (float)$t['saldo_apos'], 2, ',', '.' ); ?></td>
                <td><?php echo esc_html( $t['descricao'] ); ?></td>
                <td><?php echo esc_html( $t['created_at'] ); ?></td>
                <td><a class="button button-small button-link-delete" href="<?php echo esc_url( wp_nonce_url( add_query_arg( [ 'page' => 'senderzz', 'tab' => 'carteira', 'subtab' => 'transacoes', 'tpc_delete_transacao' => (int) $t['id'] ], admin_url( 'admin.php' ) ), 'tpc_delete_transacao_' . (int) $t['id'] ) ); ?>" onclick="return confirm('Excluir esta transação e recalcular saldo?');">Excluir</a></td>
            </tr>
        <?php endforeach; ?>
        <?php if ( empty( $rows ) ) : ?>
            <tr><td colspan="9" style="text-align:center;color:#999">Nenhuma transação encontrada.</td></tr>
        <?php endif; ?>
        </tbody>
    </table>

    <?php
    // Paginação
    $pages = (int) ceil( $total / $per_page );
    if ( $pages > 1 ) {
        echo '<div style="margin-top:12px">';
        echo paginate_links( [
            'base'    => add_query_arg( 'paged', '%#%' ),
            'format'  => '',
            'current' => $page,
            'total'   => $pages,
        ] );
        echo '</div>';
    }
}

/* ── Aba: Configurações ─────────────────────────────────────────────────── */
function tpc_tab_configuracoes(): void {
    $token          = get_option( 'tpc_me_token', '' );
    $webhook_secret = get_option( 'tpc_webhook_secret', '' );
    $saldo_minimo   = get_option( 'tpc_saldo_minimo', 0 );
    $pix_valid_minutes = (int) get_option( 'tpc_pix_valid_minutes', 30 );
    $pix_auto_cancel   = get_option( 'tpc_pix_auto_cancel_expired', 'yes' );
    $enforce_wallet    = get_option( 'senderzz_enforce_wallet_on_label', 'yes' );
    $block_duplicate   = get_option( 'senderzz_block_duplicate_label', 'yes' );
    $class_owner_map   = get_option( 'senderzz_shipping_class_wallet_owners', [] );
    if ( ! is_array( $class_owner_map ) ) { $class_owner_map = []; }
    $shipping_classes  = function_exists( 'WC' ) && WC()->shipping ? WC()->shipping->get_shipping_classes() : [];
    $all_classes       = array_merge( [ (object) [ 'term_id' => 0, 'name' => 'Produtos sem classe', 'slug' => '' ] ], is_array( $shipping_classes ) ? $shipping_classes : [] );
    $wallet_users      = get_users( [ 'fields' => [ 'ID', 'display_name', 'user_email', 'user_login' ], 'number' => 500, 'orderby' => 'display_name', 'order' => 'ASC' ] );


    // Testa conexão ME
    $saldo_me = null;
    if ( $token ) {
        $result   = tpc_consultar_saldo_me();
        $saldo_me = is_wp_error( $result ) ? 'Erro: ' . $result->get_error_message() : 'R$ ' . number_format( $result, 2, ',', '.' );
    }
    ?>
    <form method="post" style="max-width:680px;">
        <?php wp_nonce_field( 'tpc_salvar_config', 'tpc_config_nonce' ); ?>

        <h2>Melhor Envio</h2>
        <table class="form-table">
            <tr>
                <th>Token de acesso</th>
                <td>
                    <input type="password" name="tpc_me_token" value="<?php echo esc_attr( $token ); ?>" style="width:460px">
                    <p class="description">Token OAuth do Melhor Envio. <a href="https://melhorenvio.com.br/painel/gerenciar/tokens" target="_blank">Gerar token</a></p>
                    <?php if ( $saldo_me ) : ?>
                        <p><strong>Saldo atual na conta ME:</strong> <?php echo esc_html( $saldo_me ); ?></p>
                    <?php endif; ?>
                </td>
            </tr>
            <tr>
                <th>Webhook secret</th>
                <td>
                    <input type="text" name="tpc_webhook_secret" value="<?php echo esc_attr( $webhook_secret ); ?>" style="width:460px">
                    <p class="description">Chave para validar assinatura dos webhooks. Deixe vazio em desenvolvimento.</p>
                </td>
            </tr>
            <tr>
                <th>URL do webhook PIX</th>
                <td>
                    <code><?php echo esc_html( rest_url( 'tp-carteira/v1/webhook/pix' ) ); ?></code>
                    <p class="description">Configure esta URL no painel do Melhor Envio para receber notificações de PIX pago.</p>
                </td>
            </tr>
        </table>

        <h2>Regras da carteira</h2>
        <table class="form-table">
            <tr>
                <th>Saldo mínimo para alerta (R$)</th>
                <td><input type="number" name="tpc_saldo_minimo" value="<?php echo esc_attr( $saldo_minimo ); ?>" min="0" step="0.01" style="width:120px">
                <p class="description">O cliente verá um alerta de saldo baixo abaixo deste valor.</p></td>
            </tr>
        </table>

        <h2>Motor Senderzz</h2>
        <table class="form-table">
            <tr>
                <th>Validade visual do PIX</th>
                <td>
                    <input type="number" name="tpc_pix_valid_minutes" value="<?php echo esc_attr( $pix_valid_minutes ); ?>" min="5" max="1440" step="1" style="width:90px"> minutos
                    <p class="description">Usado quando a API não retorna expiração clara. Se a API retornar prazo próprio, o plugin usa o prazo da API.</p>
                </td>
            </tr>
            <tr>
                <th>Cancelar PIX expirado</th>
                <td><label><input type="checkbox" name="tpc_pix_auto_cancel_expired" value="1" <?php checked( $pix_auto_cancel, 'yes' ); ?>> Cancelar automaticamente recargas PIX vencidas localmente se ainda estiverem pendentes.</label></td>
            </tr>
            <tr>
                <th>Validar saldo na emissão</th>
                <td><label><input type="checkbox" name="senderzz_enforce_wallet_on_label" value="1" <?php checked( $enforce_wallet, 'yes' ); ?>> Bloquear emissão de etiqueta se o cliente não tiver saldo disponível.</label><p class="description">Não bloqueia checkout. Validação acontece somente no fluxo de emissão/confirmar envio.</p></td>
            </tr>
            <tr>
                <th>Uma etiqueta por pedido</th>
                <td><label><input type="checkbox" name="senderzz_block_duplicate_label" value="1" <?php checked( $block_duplicate, 'yes' ); ?>> Bloquear tentativa de gerar nova etiqueta se o pedido já tiver etiqueta/protocolo/rastreio.</label></td>
            </tr>
            <tr>
                <th>Template de Checkout FunnelKit</th>
                <td>
                    <input type="number" name="senderzz_checkout_template_id" value="<?php echo esc_attr( (int) get_option( 'senderzz_checkout_template_id', 140 ) ); ?>" min="1" style="width:100px;">
                    <p class="description">ID do post <code>wfacp_checkout</code> usado como template para duplicação ao criar links de checkout no painel do produtor. Padrão: <strong>140</strong>.</p>
                </td>
            </tr>
        </table>

        <h2>Dono financeiro por classe de entrega</h2>
        <p class="description">Use a própria <strong>classe de entrega</strong> do WooCommerce para definir qual carteira paga o frete. Assim o checkout pode ser público e o débito sai da carteira do lojista/dono da classe.</p>
        <table class="widefat striped" style="max-width:900px;margin-top:10px;margin-bottom:20px;">
            <thead><tr><th>Classe de entrega</th><th>Slug</th><th>Carteira / usuário dono</th></tr></thead>
            <tbody>
                <?php foreach ( $all_classes as $class ) :
                    $class_id = (int) $class->term_id;
                    $selected_owner = isset( $class_owner_map[ (string) $class_id ] ) ? absint( $class_owner_map[ (string) $class_id ] ) : 0;
                ?>
                    <tr>
                        <td><strong><?php echo esc_html( $class->name ); ?></strong></td>
                        <td><code><?php echo esc_html( $class->slug ?? '' ); ?></code></td>
                        <td>
                            <select name="senderzz_shipping_class_wallet_owners[<?php echo esc_attr( $class_id ); ?>]" style="min-width:360px;">
                                <option value="0">— Não vincular / usar cliente do pedido —</option>
                                <?php foreach ( $wallet_users as $u ) : ?>
                                    <option value="<?php echo esc_attr( $u->ID ); ?>" <?php selected( $selected_owner, (int) $u->ID ); ?>>
                                        <?php echo esc_html( tpc_admin_get_live_user_name( (int) $u->ID ) . ' (' . $u->user_email . ')' ); ?>
                                    </option>
                                <?php endforeach; ?>
                            </select>
                        </td>
                    </tr>
                <?php endforeach; ?>
            </tbody>
        </table>

        <p><input type="submit" class="button-primary" value="Salvar configurações"></p>
    </form>
    <?php
}

/* ── Aba: REST API ─────────────────────────────────────────────────────── */
function tpc_tab_api(): void {
    $base = rest_url( 'tp-carteira/v1' );
    ?>
    <h2>Endpoints disponíveis para o painel externo</h2>
    <p>Base URL: <code><?php echo esc_html( $base ); ?></code></p>
    <p>Autenticação: <code>Authorization: Bearer {token}</code> — obter token via <code>POST /auth/token</code></p>

    <table class="widefat">
        <thead><tr><th>Método</th><th>Endpoint</th><th>Descrição</th></tr></thead>
        <tbody>
            <?php
            $endpoints = [
                ['POST', '/auth/token',                    'Login — retorna JWT (username + password)'],
                ['GET',  '/me',                            'Dados do usuário + saldo'],
                ['GET',  '/saldo',                         'Saldo atual'],
                ['GET',  '/extrato',                       'Extrato paginado com filtros (tipo, data_ini, data_fim, per_page, page)'],
                ['POST', '/recarregar',                    'Cria pedido de recarga e retorna QR Code PIX (body: valor)'],
                ['GET',  '/recarga/{recarga_id}/pix',      'Status e dados do PIX de uma recarga interna'],
                ['POST', '/webhook/pix',                   'Webhook ME — PIX confirmado (interno)'],
                ['POST', '/webhook/envio',                 'Webhook ME — envio criado, débito automático (interno)'],
            ];
            foreach ( $endpoints as [$method, $path, $desc] ) : ?>
                <tr>
                    <td><span style="font-family:var(--sz-font); font-weight:600; color:<?php echo match($method){'POST'=>'#1d6f42','GET'=>'#1a5276',default=>'#666'}; ?>"><?php echo $method; ?></span></td>
                    <td><code><?php echo esc_html( $base . $path ); ?></code></td>
                    <td><?php echo esc_html( $desc ); ?></td>
                </tr>
            <?php endforeach; ?>
        </tbody>
    </table>

    <h3 style="margin-top:24px">Webhook Melhor Envio</h3>
    <p>Use esta URL no painel do Melhor Envio em <strong>Integrações &gt; Área Dev &gt; Webhooks</strong>:</p>
    <p><code><?php echo esc_html( rest_url( 'senderzz/v1/webhook/me' ) ); ?></code></p>
    <p>Evento: <strong>Atualização das etiquetas criadas e editadas</strong>. A rota aceita teste vazio e sempre responde HTTP 200.</p>

    <h3 style="margin-top:24px">Exemplo: buscar extrato</h3>
    <pre style="background:#f5f5f0; padding:16px; border-radius:6px; overflow-x:auto; font-size:var(--sz-text-meta);"><?php echo esc_html(
'// 1. Login
const { token } = await fetch("' . $base . '/auth/token", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ username: "cliente@email.com", password: "senha" })
}).then(r => r.json());

// 2. Extrato
const extrato = await fetch("' . $base . '/extrato?per_page=10&page=1&tipo=debito", {
  headers: { "Authorization": "Bearer " + token }
}).then(r => r.json());

// 3. Recarregar
const recarga = await fetch("' . $base . '/recarregar", {
  method: "POST",
  headers: { "Authorization": "Bearer " + token, "Content-Type": "application/json" },
  body: JSON.stringify({ valor: 100.00 })
}).then(r => r.json());

// recarga.pix.qr_code  → imagem base64 do QR Code
// recarga.pix.copia_cola → string para copiar e colar'
    ); ?></pre>
    <?php
}

/* ── Ações administrativas extras: excluir transação e verificar PIX ───── */
add_action( 'admin_init', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;

    if ( isset( $_GET['tpc_delete_transacao'] ) ) {
        $id = (int) $_GET['tpc_delete_transacao'];
        if ( $id && wp_verify_nonce( $_GET['_wpnonce'] ?? '', 'tpc_delete_transacao_' . $id ) ) {
            $ok = tpc_admin_excluir_transacao( $id );
            wp_safe_redirect( add_query_arg( [
                'page'   => 'senderzz',
                'tab'    => 'carteira',
                'subtab' => 'transacoes',
                'tpc_msg' => $ok ? 'transacao_excluida' : 'erro_excluir',
            ], admin_url( 'admin.php' ) ) );
            exit;
        }
    }

    if ( isset( $_GET['tpc_verificar_pix'] ) && wp_verify_nonce( $_GET['_wpnonce'] ?? '', 'tpc_verificar_pix' ) ) {
        $resultado = function_exists( 'tpc_processar_recargas_pendentes' ) ? tpc_processar_recargas_pendentes( 20, 'admin' ) : [ 'erro' => 'função indisponível' ];
        set_transient( 'tpc_admin_last_verificacao_' . get_current_user_id(), $resultado, 5 * MINUTE_IN_SECONDS );
        wp_safe_redirect( add_query_arg( [
            'page'   => 'senderzz',
            'tab'    => 'carteira',
            'subtab' => 'transacoes',
            'tpc_msg' => 'pix_verificado',
        ], admin_url( 'admin.php' ) ) );
        exit;
    }
} );

function tpc_admin_recalcular_saldo_usuario( int $user_id ): float {
    global $wpdb;
    $ignore_admin_allocation = " AND (meta_json IS NULL OR meta_json NOT LIKE '%senderzz_admin_me_allocation%')";
    $credito = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(valor),0) FROM {$wpdb->prefix}tpc_transacoes WHERE user_id = %d AND tipo = 'credito' AND status = 'confirmado' {$ignore_admin_allocation}",
        $user_id
    ) );
    $debito = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(valor),0) FROM {$wpdb->prefix}tpc_transacoes WHERE user_id = %d AND tipo = 'debito' AND status = 'confirmado' {$ignore_admin_allocation}",
        $user_id
    ) );
    $reservado = (float) $wpdb->get_var( $wpdb->prepare(
        "SELECT COALESCE(SUM(valor),0) FROM {$wpdb->prefix}tpc_transacoes WHERE user_id = %d AND tipo = 'debito' AND status = 'pendente' AND referencia LIKE 'senderzz_wallet_reserve_order_%' {$ignore_admin_allocation}",
        $user_id
    ) );
    $saldo = round( $credito - $debito, 2 );

    $wpdb->query( $wpdb->prepare(
        "INSERT INTO {$wpdb->prefix}tpc_carteira (user_id, saldo, saldo_reservado) VALUES (%d, %f, %f)
         ON DUPLICATE KEY UPDATE saldo = VALUES(saldo), saldo_reservado = VALUES(saldo_reservado)",
        $user_id,
        $saldo,
        $reservado
    ) );

    if ( function_exists( 'tpc_pix_log' ) ) {
        tpc_pix_log( 'saldo_recalculado', [ 'user_id' => $user_id, 'saldo' => $saldo, 'saldo_reservado' => $reservado, 'source' => 'me_api_only' ] );
    }

    return $saldo;
}

function tpc_admin_eliminar_alocacoes_manuais(): void {
    global $wpdb;
    if ( get_option( 'senderzz_manual_allocations_removed_v1' ) === 'yes' ) return;

    $user_ids = $wpdb->get_col( "SELECT DISTINCT user_id FROM {$wpdb->prefix}tpc_transacoes WHERE meta_json LIKE '%senderzz_admin_me_allocation%'" );
    if ( $user_ids ) {
        foreach ( $user_ids as $uid ) {
            tpc_admin_recalcular_saldo_usuario( (int) $uid );
        }
    }
    update_option( 'senderzz_manual_allocations_removed_v1', 'yes', false );
}
add_action( 'admin_init', 'tpc_admin_eliminar_alocacoes_manuais', 3 );

function tpc_admin_excluir_transacao( int $id ): bool {
    global $wpdb;
    $row = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$wpdb->prefix}tpc_transacoes WHERE id = %d", $id ), ARRAY_A );
    if ( ! $row ) return false;

    $user_id = (int) $row['user_id'];
    $ok = $wpdb->delete( $wpdb->prefix . 'tpc_transacoes', [ 'id' => $id ], [ '%d' ] );

    if ( $ok ) {
        delete_option( 'tpc_pix_recarga_' . $id );
        tpc_admin_recalcular_saldo_usuario( $user_id );
        if ( function_exists( 'tpc_pix_log' ) ) {
            tpc_pix_log( 'transacao_excluida', [ 'id' => $id, 'user_id' => $user_id, 'row' => $row ] );
        }
        return true;
    }
    return false;
}


function tpc_admin_table_exists( string $table ): bool {
    global $wpdb;
    return (bool) $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
}

function tpc_admin_reset_wallet_all(): array {
    global $wpdb;

    if ( ! current_user_can( 'manage_woocommerce' ) ) {
        return [ 'ok' => false, 'error' => 'Permissão insuficiente.' ];
    }

    $wallet_table   = $wpdb->prefix . 'tpc_carteira';
    $tx_table       = $wpdb->prefix . 'tpc_transacoes';
    $recarga_table  = $wpdb->prefix . 'tpc_recargas';
    $events_table   = $wpdb->prefix . 'tpc_webhook_events';
    $orders_meta    = $wpdb->prefix . 'wc_orders_meta';

    $result = [
        'ok'           => false,
        'wallets'      => 0,
        'transactions' => 0,
        'recharges'    => 0,
        'events'       => 0,
        'order_meta'   => 0,
        'options'      => 0,
    ];

    $wallet_keys = [
        '_senderzz_wallet_debited',
        '_senderzz_wallet_debit_tx',
        '_senderzz_wallet_debit_value',
        '_senderzz_wallet_debit_context',
        '_senderzz_wallet_reserved',
        '_senderzz_wallet_reserved_value',
        '_senderzz_wallet_reserve_tx',
        '_senderzz_wallet_reserve_released',
    ];

    $wpdb->query( 'START TRANSACTION' );
    try {
        if ( tpc_admin_table_exists( $wallet_table ) ) {
            $result['wallets'] = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$wallet_table}" );
            $wpdb->query( "UPDATE {$wallet_table} SET saldo = 0.00, saldo_reservado = 0.00" );
        }

        if ( tpc_admin_table_exists( $tx_table ) ) {
            $result['transactions'] = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$tx_table}" );
            $wpdb->query( "DELETE FROM {$tx_table}" );
        }

        if ( tpc_admin_table_exists( $recarga_table ) ) {
            $result['recharges'] = (int) $wpdb->get_var( "SELECT COUNT(*) FROM {$recarga_table}" );
            $wpdb->query( "DELETE FROM {$recarga_table}" );
        }

        if ( tpc_admin_table_exists( $events_table ) ) {
            $result['events'] = (int) $wpdb->query( "DELETE FROM {$events_table} WHERE source IN ('melhor_envio_pix','melhor_envio','senderzz','tpc') OR event_type LIKE '%pix%' OR event_type LIKE '%wallet%'" );
        }

        $placeholders = implode( ',', array_fill( 0, count( $wallet_keys ), '%s' ) );
        $result['order_meta'] += (int) $wpdb->query( $wpdb->prepare(
            "DELETE FROM {$wpdb->postmeta} WHERE meta_key IN ({$placeholders})",
            ...$wallet_keys
        ) );

        if ( tpc_admin_table_exists( $orders_meta ) ) {
            $result['order_meta'] += (int) $wpdb->query( $wpdb->prepare(
                "DELETE FROM {$orders_meta} WHERE meta_key IN ({$placeholders})",
                ...$wallet_keys
            ) );
        }

        $result['options'] += (int) $wpdb->query( "DELETE FROM {$wpdb->options} WHERE option_name LIKE 'tpc_pix_recarga_%' OR option_name LIKE '_transient_tpc_admin_pix_%' OR option_name LIKE '_transient_timeout_tpc_admin_pix_%' OR option_name IN ('tpc_last_pix_request','tpc_last_pix_response','tpc_last_pix_mapped')" );

        update_option( 'senderzz_wallet_reset_last', [
            'at'       => current_time( 'mysql' ),
            'actor_id' => get_current_user_id(),
            'result'   => $result,
        ], false );

        $wpdb->query( 'COMMIT' );
        $result['ok'] = true;

        if ( function_exists( 'tpc_pix_log' ) ) {
            tpc_pix_log( 'wallet_reset_manual', $result );
        }

        return $result;
    } catch ( Throwable $e ) {
        $wpdb->query( 'ROLLBACK' );
        $result['error'] = $e->getMessage();
        return $result;
    }
}

/* ── Senderzz: saldo Melhor Envio via API; alocação manual removida ─────── */
function tpc_admin_tx_is_admin_allocation( array $row ): bool {
    $meta = json_decode( (string) ( $row['meta_json'] ?? '' ), true );
    return is_array( $meta ) && ( $meta['source'] ?? '' ) === 'senderzz_admin_me_allocation';
}

function tpc_admin_get_admin_allocated_balance( int $user_id = 0 ): float {
    return 0.0;
}

function tpc_admin_get_user_wallet_breakdown( int $user_id ): array {
    $saldo = function_exists( 'tpc_get_saldo' ) ? tpc_get_saldo( $user_id ) : 0.0;
    $reservado = function_exists( 'tpc_get_saldo_reservado' ) ? tpc_get_saldo_reservado( $user_id ) : 0.0;
    $disponivel = function_exists( 'tpc_get_saldo_disponivel' ) ? tpc_get_saldo_disponivel( $user_id ) : max( 0, $saldo - $reservado );
    return [
        'saldo' => round( (float) $saldo, 2 ),
        'reservado' => round( (float) $reservado, 2 ),
        'disponivel' => round( (float) $disponivel, 2 ),
        'admin_alocado' => 0.0,
        'usuario_proprio' => round( (float) $saldo, 2 ),
    ];
}

add_action( 'admin_init', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    if ( empty( $_POST['tpc_alloc_nonce'] ) ) return;
    $redirect = add_query_arg( [ 'page' => 'senderzz', 'tab' => 'carteira', 'subtab' => 'clientes', 'tpc_alloc_msg' => 'desativado' ], admin_url( 'admin.php' ) );
    wp_safe_redirect( $redirect );
    exit;
} );

function tpc_tab_alocacoes_me(): void {
    $token = get_option( 'tpc_me_token', '' );
    $saldo_me = null;
    $erro = '';
    if ( $token && function_exists( 'tpc_consultar_saldo_me' ) ) {
        $saldo_me_result = tpc_consultar_saldo_me();
        if ( is_wp_error( $saldo_me_result ) ) {
            $erro = $saldo_me_result->get_error_message();
        } else {
            $saldo_me = (float) $saldo_me_result;
        }
    } else {
        $erro = 'Token ME não configurado.';
    }
    ?>
    <h2>Saldo Melhor Envio</h2>
    <p class="description">A alocação manual foi removida. Entrada de crédito: PIX criado/confirmado pela API do Melhor Envio. Devolução: somente por cancelamento/estorno confirmado pelo Melhor Envio.</p>
    <div style="display:grid;grid-template-columns:repeat(2,minmax(220px,1fr));gap:12px;max-width:700px;margin:16px 0;">
        <div style="background:#fff;border:1px solid #ccd0d4;border-radius:8px;padding:14px;"><strong>Saldo ME real</strong><br><span style="font-size:var(--sz-text-xl);font-weight:700;"><?php echo $saldo_me === null ? 'Indisponível' : 'R$ ' . number_format( $saldo_me, 2, ',', '.' ); ?></span></div>
        <div style="background:#fff;border:1px solid #ccd0d4;border-radius:8px;padding:14px;"><strong>Fonte da carteira Senderzz</strong><br><span style="font-size:var(--sz-text-lg);font-weight:700;color:#15803d;">API ME / PIX confirmado</span></div>
    </div>
    <?php if ( $erro ) : ?><div class="notice notice-error"><p><?php echo esc_html( $erro ); ?></p></div><?php endif; ?>
    <p><a class="button" href="<?php echo esc_url( add_query_arg( [ 'page' => 'senderzz', 'tab' => 'carteira', 'subtab' => 'clientes' ], admin_url( 'admin.php' ) ) ); ?>">Voltar para clientes</a></p>
    <?php
}

/* ── Aba: PIX / Reconciliação ──────────────────────────────────────────── */
function tpc_tab_pix_reconciliacao(): void {
    global $wpdb;
    $msg = '';
    $tz  = wp_timezone();

    // Helper: converte datetime UTC do banco para horário BR
    $br_date = function( string $dt ) use ( $tz ): string {
        if ( ! $dt ) return '—';
        try {
            $d = new DateTime( $dt, new DateTimeZone( 'UTC' ) );
            $d->setTimezone( $tz );
            return $d->format( 'd/m/Y H:i' );
        } catch ( \Throwable $e ) {
            return $dt;
        }
    };

    // Forçar reconciliação
    if ( isset( $_POST['sz_force_reconcile'], $_POST['_wpnonce_pix'] ) &&
         wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['_wpnonce_pix'] ) ), 'sz_pix_tab' ) ) {
        delete_transient( 'sz_pix_reconcile_lock' );
        delete_transient( 'sz_pix_me_transactions_cache' );
        if ( function_exists( 'sz_pix_auto_reconcile_run' ) ) {
            sz_pix_auto_reconcile_run();
            $msg = '<div class="notice notice-success is-dismissible"><p>✅ Reconciliação executada.</p></div>';
        } else {
            $msg = '<div class="notice notice-error"><p>❌ Módulo de reconciliação não carregado.</p></div>';
        }
    }

    // Confirmar manualmente
    if ( isset( $_POST['sz_manual_confirm'], $_POST['recarga_id'], $_POST['_wpnonce_pix'] ) &&
         wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['_wpnonce_pix'] ) ), 'sz_pix_tab' ) &&
         function_exists( 'tpc_confirmar_recarga' ) ) {
        $rid = absint( $_POST['recarga_id'] );
        $ok  = tpc_confirmar_recarga( $rid, 'admin_reconciliation' );
        $msg = $ok
            ? "<div class='notice notice-success is-dismissible'><p>✅ Recarga #{$rid} confirmada.</p></div>"
            : "<div class='notice notice-error is-dismissible'><p>❌ Falha ao confirmar #{$rid}.</p></div>";
    }

    // Cancelar recarga sem me_pix_id
    if ( isset( $_POST['sz_cancelar_recarga'], $_POST['recarga_id'], $_POST['_wpnonce_pix'] ) &&
         wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['_wpnonce_pix'] ) ), 'sz_pix_tab' ) ) {
        $rid = absint( $_POST['recarga_id'] );
        $upd = $wpdb->update(
            $wpdb->prefix . 'tpc_recargas',
            [ 'status' => 'cancelado' ],
            [ 'id' => $rid, 'me_pix_id' => null ],
            [ '%s' ], [ '%d', 'NULL' ]
        );
        // Tenta também com me_pix_id vazio
        if ( $upd === false || $upd === 0 ) {
            $upd = $wpdb->query( $wpdb->prepare(
                "UPDATE {$wpdb->prefix}tpc_recargas SET status = 'cancelado' WHERE id = %d AND (me_pix_id IS NULL OR me_pix_id = '') AND status IN ('pendente','analise')",
                $rid
            ) );
        }
        $msg = $upd
            ? "<div class='notice notice-success is-dismissible'><p>🗑 Recarga #{$rid} cancelada.</p></div>"
            : "<div class='notice notice-error is-dismissible'><p>❌ Não foi possível cancelar #{$rid}.</p></div>";
    }

    // Cancelar todas sem me_pix_id de uma vez
    if ( isset( $_POST['sz_cancelar_todas_sem_pix'], $_POST['_wpnonce_pix'] ) &&
         wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['_wpnonce_pix'] ) ), 'sz_pix_tab' ) ) {
        $n = $wpdb->query(
            "UPDATE {$wpdb->prefix}tpc_recargas SET status = 'cancelado'
             WHERE (me_pix_id IS NULL OR me_pix_id = '') AND status IN ('pendente','analise')"
        );
        $msg = "<div class='notice notice-success is-dismissible'><p>🗑 {$n} recargas sem PIX canceladas.</p></div>";
    }

    // Débito / estorno manual
    if ( isset( $_POST['sz_manual_debit'], $_POST['debit_user_id'], $_POST['debit_valor'], $_POST['_wpnonce_pix'] ) &&
         wp_verify_nonce( sanitize_text_field( wp_unslash( $_POST['_wpnonce_pix'] ) ), 'sz_pix_tab' ) &&
         function_exists( 'tpc_debitar' ) ) {
        $uid = absint( $_POST['debit_user_id'] );
        $val = round( (float) sanitize_text_field( wp_unslash( $_POST['debit_valor'] ) ), 2 );
        $dsc = sanitize_text_field( wp_unslash( $_POST['debit_desc'] ?? '' ) ) ?: 'Estorno/ajuste manual admin';
        if ( $uid > 0 && $val > 0 ) {
            $tx  = tpc_debitar( $uid, $val, $dsc, [ 'referencia' => 'admin_debit' ] );
            $msg = $tx
                ? "<div class='notice notice-success is-dismissible'><p>✅ Debitado R$ " . number_format( $val, 2, ',', '.' ) . " do usuário #{$uid}.</p></div>"
                : "<div class='notice notice-error is-dismissible'><p>❌ Falha no débito.</p></div>";
        }
    }

    $last    = get_option( 'sz_pix_reconcile_last_run', null );
    $history = array_reverse( (array) get_option( 'sz_pix_reconcile_history', [] ) );
    $next    = wp_next_scheduled( 'sz_pix_auto_reconcile_cron' );

    // Recargas pendentes (com e sem me_pix_id)
    $pendentes = $wpdb->get_results(
        "SELECT r.id, r.user_id, r.valor, r.status, r.created_at, r.me_pix_id, u.user_email
         FROM {$wpdb->prefix}tpc_recargas r
         LEFT JOIN {$wpdb->users} u ON u.ID = r.user_id
         WHERE r.status IN ('pendente','analise')
         ORDER BY r.created_at DESC LIMIT 30",
        ARRAY_A
    ) ?: [];

    // Recargas confirmadas recentes (histórico)
    $confirmadas = $wpdb->get_results(
        "SELECT r.id, r.user_id, r.valor, r.status, r.created_at, r.me_pix_id, u.user_email
         FROM {$wpdb->prefix}tpc_recargas r
         LEFT JOIN {$wpdb->users} u ON u.ID = r.user_id
         WHERE r.status = 'confirmado'
         ORDER BY r.created_at DESC LIMIT 20",
        ARRAY_A
    ) ?: [];

    $sem_pix_count = count( array_filter( $pendentes, fn($r) => empty( $r['me_pix_id'] ) ) );

    echo wp_kses_post( $msg );
    ?>
    <style>
        .sz-pix-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin:16px 0}
        .sz-pix-card{background:#fff;border:1px solid #dcdcde;border-radius:8px;padding:16px}
        .sz-pix-card .label{font-size:var(--sz-text-sm);color:#888;text-transform:none;letter-spacing:.02em;margin-bottom:6px}
        .sz-pix-card .value{font-size:var(--sz-text-lg);font-weight:700;color:#1a1a1a}
        .sz-pix-card .sub{font-size:var(--sz-text-sm);color:#666;margin-top:4px}
        .sz-pix-card.ok{background:#f0fdf4;border-color:#a3cfbb}
        .sz-badge-pendente{background:#fff3cd;color:#856404;padding:2px 8px;border-radius:12px;font-size:var(--sz-text-sm);font-weight:600}
        .sz-badge-confirmado{background:#d1e7dd;color:#0f5132;padding:2px 8px;border-radius:12px;font-size:var(--sz-text-sm);font-weight:600}
        .sz-badge-sem-pix{background:#f8d7da;color:#842029;padding:2px 8px;border-radius:12px;font-size:var(--sz-text-sm);font-weight:600}
        @media(max-width:900px){.sz-pix-grid{grid-template-columns:1fr}}
    </style>

    <div class="sz-pix-grid">
        <div class="sz-pix-card">
            <div class="label">Última execução (horário Brasil)</div>
            <div class="value"><?php
                if ( $last ) {
                    echo esc_html( $br_date( $last['at'] ) );
                } else {
                    echo 'Nunca';
                }
            ?></div>
            <?php if ( $last ): ?>
            <div class="sub">
                ✅ <?= (int)($last['result']['confirmadas']??0) ?> confirmadas &nbsp;|&nbsp;
                👁 <?= (int)($last['result']['verificadas']??0) ?> verificadas &nbsp;|&nbsp;
                ⏭ <?= (int)($last['result']['sem_match']??0) ?> sem match
            </div>
            <?php endif; ?>
        </div>
        <div class="sz-pix-card">
            <div class="label">Próxima execução automática</div>
            <div class="value"><?php
                if ( $next ) {
                    $next_br = new DateTime( '@' . $next );
                    $next_br->setTimezone( $tz );
                    echo esc_html( human_time_diff( time(), $next ) . ' (~' . $next_br->format( 'H:i' ) . ')' );
                } else {
                    echo 'Não agendado';
                }
            ?></div>
            <div class="sub" style="margin-top:8px">
                <form method="post" style="display:inline">
                    <?php wp_nonce_field( 'sz_pix_tab', '_wpnonce_pix' ); ?>
                    <button type="submit" name="sz_force_reconcile" value="1" class="button button-small">▶ Executar agora</button>
                </form>
            </div>
        </div>
        <div class="sz-pix-card ok">
            <div class="label">Método</div>
            <div class="value" style="font-size:var(--sz-text-base);color:#0f5132">✅ API /me/transactions</div>
            <div class="sub" style="color:#0f5132">Cruzamento por me_pix_id — individual por usuário</div>
        </div>
    </div>

    <?php if ( ! empty( $pendentes ) ): ?>
    <div style="display:flex;align-items:center;gap:12px;margin:16px 0 8px">
        <h3 style="margin:0">Recargas pendentes (<?= count( $pendentes ) ?>)</h3>
        <?php if ( $sem_pix_count > 0 ): ?>
        <form method="post" style="display:inline" onsubmit="return confirm('Cancelar <?= $sem_pix_count ?> recargas sem PIX gerado?')">
            <?php wp_nonce_field( 'sz_pix_tab', '_wpnonce_pix' ); ?>
            <button type="submit" name="sz_cancelar_todas_sem_pix" value="1" class="button button-small" style="border-color:#b91c1c;color:#b91c1c">
                🗑 Cancelar <?= $sem_pix_count ?> sem PIX
            </button>
        </form>
        <?php endif; ?>
    </div>
    <table class="wp-list-table widefat fixed striped" style="max-width:1100px">
        <thead><tr>
            <th style="width:45px">ID</th>
            <th>Usuário</th>
            <th style="width:90px">Valor</th>
            <th style="width:90px">Status</th>
            <th style="width:150px">Criada em (BR)</th>
            <th>ME PIX ID</th>
            <th style="width:150px">Ação</th>
        </tr></thead>
        <tbody>
        <?php foreach ( $pendentes as $r ):
            $mins    = round( ( time() - strtotime( $r['created_at'] ) ) / 60 );
            $bg      = $mins > 10 ? '#fef2f2' : ( $mins > 3 ? '#fffbeb' : '' );
            $sem_pix = empty( $r['me_pix_id'] );
        ?>
        <tr style="<?= $bg ? "background:{$bg}" : '' ?>">
            <td><?= esc_html( $r['id'] ) ?></td>
            <td><?= esc_html( $r['user_email'] ?: 'ID ' . $r['user_id'] ) ?></td>
            <td><strong>R$ <?= number_format( (float)$r['valor'], 2, ',', '.' ) ?></strong></td>
            <td>
                <?php if ( $sem_pix ): ?>
                    <span class="sz-badge-sem-pix">sem PIX</span>
                <?php else: ?>
                    <span class="sz-badge-pendente">pendente</span>
                <?php endif; ?>
            </td>
            <td><?= esc_html( $br_date( $r['created_at'] ) ) ?></td>
            <td style="font-family:var(--sz-font);font-size:var(--sz-text-sm);word-break:break-all"><?= esc_html( $r['me_pix_id'] ?: '—' ) ?></td>
            <td style="display:flex;gap:6px;flex-wrap:wrap">
                <?php if ( ! $sem_pix ): ?>
                <form method="post" onsubmit="return confirm('Confirmar manualmente?')">
                    <?php wp_nonce_field( 'sz_pix_tab', '_wpnonce_pix' ); ?>
                    <input type="hidden" name="sz_manual_confirm" value="1">
                    <input type="hidden" name="recarga_id" value="<?= absint($r['id']) ?>">
                    <button type="submit" class="button button-primary button-small">✅</button>
                </form>
                <?php endif; ?>
                <?php if ( $sem_pix ): ?>
                <form method="post" onsubmit="return confirm('Cancelar esta recarga?')">
                    <?php wp_nonce_field( 'sz_pix_tab', '_wpnonce_pix' ); ?>
                    <input type="hidden" name="sz_cancelar_recarga" value="1">
                    <input type="hidden" name="recarga_id" value="<?= absint($r['id']) ?>">
                    <button type="submit" class="button button-small" style="border-color:#b91c1c;color:#b91c1c">🗑</button>
                </form>
                <?php endif; ?>
            </td>
        </tr>
        <?php endforeach; ?>
        </tbody>
    </table>
    <?php else: ?>
    <p style="color:#0f5132;background:#f0fdf4;border:1px solid #a3cfbb;padding:12px 16px;border-radius:6px;max-width:600px;margin:16px 0">
        ✅ Nenhuma recarga pendente.
    </p>
    <?php endif; ?>

    <?php if ( ! empty( $confirmadas ) ): ?>
    <h3 style="margin-top:28px">Recargas confirmadas recentes</h3>
    <table class="wp-list-table widefat fixed striped" style="max-width:1100px">
        <thead><tr>
            <th style="width:45px">ID</th>
            <th>Usuário</th>
            <th style="width:90px">Valor</th>
            <th style="width:100px">Status</th>
            <th style="width:150px">Confirmada em (BR)</th>
            <th>ME PIX ID</th>
        </tr></thead>
        <tbody>
        <?php foreach ( $confirmadas as $r ): ?>
        <tr>
            <td><?= esc_html( $r['id'] ) ?></td>
            <td><?= esc_html( $r['user_email'] ?: 'ID ' . $r['user_id'] ) ?></td>
            <td><strong>R$ <?= number_format( (float)$r['valor'], 2, ',', '.' ) ?></strong></td>
            <td><span class="sz-badge-confirmado">confirmado</span></td>
            <td><?= esc_html( $br_date( $r['created_at'] ) ) ?></td>
            <td style="font-family:var(--sz-font);font-size:var(--sz-text-sm);word-break:break-all"><?= esc_html( $r['me_pix_id'] ?: '—' ) ?></td>
        </tr>
        <?php endforeach; ?>
        </tbody>
    </table>
    <?php endif; ?>

    <h3 style="margin-top:28px">🔻 Débito / Estorno Manual</h3>
    <div style="background:#fff;border:1px solid #dcdcde;border-radius:8px;padding:20px;max-width:480px">
        <p style="color:#666;font-size:var(--sz-text-base);margin-top:0">Use para estornar créditos confirmados indevidamente.</p>
        <form method="post">
            <?php wp_nonce_field( 'sz_pix_tab', '_wpnonce_pix' ); ?>
            <input type="hidden" name="sz_manual_debit" value="1">
            <table class="form-table" style="margin:0">
                <tr><th style="padding:8px 0;width:120px">User ID</th><td><input type="number" name="debit_user_id" class="small-text" min="1" required placeholder="Ex: 5"></td></tr>
                <tr><th style="padding:8px 0">Valor (R$)</th><td><input type="number" name="debit_valor" class="small-text" step="0.01" min="0.01" required placeholder="10.00"></td></tr>
                <tr><th style="padding:8px 0">Descrição</th><td><input type="text" name="debit_desc" class="regular-text" placeholder="Estorno recarga indevida"></td></tr>
            </table>
            <button type="submit" class="button button-secondary" style="margin-top:12px;border-color:#b91c1c;color:#b91c1c"
                onclick="return confirm('Confirmar débito? Não pode ser desfeito.')">🔻 Debitar saldo</button>
        </form>
    </div>

    <?php if ( ! empty( $history ) ): ?>
    <h3 style="margin-top:28px">Histórico de reconciliação</h3>
    <table class="wp-list-table widefat fixed striped" style="max-width:1100px;font-size:var(--sz-text-meta)">
        <thead><tr>
            <th style="width:140px">Data (BR)</th>
            <th style="width:180px">Evento</th>
            <th>Detalhes</th>
        </tr></thead>
        <tbody>
        <?php foreach ( array_slice( $history, 0, 50 ) as $h ):
            $cor = str_contains( $h['tipo'], 'confirmed' ) ? '#f0fdf4' :
                   ( str_contains( $h['tipo'], 'error' ) || str_contains( $h['tipo'], 'fail' ) ? '#fef2f2' : '' );
            $det = $h; unset( $det['tipo'], $det['at'] );
        ?>
        <tr style="<?= $cor ? "background:{$cor}" : '' ?>">
            <td style="white-space:nowrap"><?= esc_html( $br_date( $h['at'] ) ) ?></td>
            <td><code style="font-size:var(--sz-text-sm)"><?= esc_html( $h['tipo'] ) ?></code></td>
            <td style="font-family:var(--sz-font);font-size:var(--sz-text-sm);word-break:break-all"><?= esc_html( wp_json_encode( $det, JSON_UNESCAPED_UNICODE ) ) ?></td>
        </tr>
        <?php endforeach; ?>
        </tbody>
    </table>
    <?php endif; ?>
    <?php
}
