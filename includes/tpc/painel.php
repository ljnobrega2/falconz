<?php
/**
 * painel.php — Painel unificado admin + cliente
 * Shortcode: [tpc_painel]
 *
 * Níveis de acesso detectados via JWT:
 *   role=admin  → usuário WP com manage_woocommerce
 *   role=client → qualquer outro usuário WooCommerce
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TPC_PAINEL_LOADED' ) ) return;
define( 'SENDERZZ_TPC_PAINEL_LOADED', true );

add_shortcode( 'tpc_painel', 'tpc_painel_shortcode' );

function tpc_painel_shortcode(): string {
    if ( is_admin() ) return '<p>Disponível no frontend.</p>';

    if ( ! headers_sent() ) {
        nocache_headers();
        header( 'Cache-Control: no-store, no-cache, must-revalidate, max-age=0, private', true );
        header( 'Pragma: no-cache', true );
        header( 'Expires: 0', true );
        header( 'CDN-Cache-Control: no-store', true );
        header( 'X-LiteSpeed-Cache-Control: no-cache, private, no-store', true );
        header( 'X-Accel-Expires: 0', true );
    }

    $tpc_css_ver    = file_exists( TPC_PATH . 'assets/css/painel.css' ) ? (string) filemtime( TPC_PATH . 'assets/css/painel.css' ) : TPC_VERSION;
    $tpc_js_ver     = file_exists( TPC_PATH . 'assets/js/painel.js' )  ? (string) filemtime( TPC_PATH . 'assets/js/painel.js' )  : TPC_VERSION;
    $tpc_tokens_ver = file_exists( TPC_PATH . 'assets/css/senderzz-tokens.css' ) ? (string) filemtime( TPC_PATH . 'assets/css/senderzz-tokens.css' ) : TPC_VERSION;
    wp_enqueue_style( 'sz-tokens', TPC_URL . 'assets/css/senderzz-tokens.css', [], $tpc_tokens_ver );
    wp_enqueue_style( 'tpc-painel', TPC_URL . 'assets/css/painel.css', [ 'sz-tokens' ], $tpc_css_ver );
    wp_enqueue_style( 'sz-typography', TPC_URL . 'assets/css/senderzz-typography.css', [ 'tpc-painel' ], defined( 'TPC_VERSION' ) ? TPC_VERSION : $tpc_css_ver );
    wp_enqueue_script( 'tpc-painel', TPC_URL . 'assets/js/painel.js',  [], $tpc_js_ver, true );
    wp_localize_script( 'tpc-painel', 'tpcPainel', [
        'apiBase'  => rest_url( 'tp-carteira/v1' ),
        'nonce'    => wp_create_nonce( 'wp_rest' ),
        'nomeLoja' => get_option( 'tpc_painel_nome', get_bloginfo( 'name' ) ),
        'logoUrl'  => get_option( 'tpc_painel_logo', '' ),
    ] );

    ob_start(); ?>
    <div id="tpc-root">

        <!-- LOGIN -->
        <div id="tpc-login" class="tpc-screen">
            <div class="login-card">
                <div class="login-logo">📦</div>
                <h1 id="login-nome"><?php echo esc_html( get_option( 'tpc_painel_nome', get_bloginfo('name') ) ); ?></h1>
                <p class="login-sub">Acesse sua conta</p>
                <div id="login-err" class="alert alert-err" style="display:none"></div>
                <div class="field"><label>E-mail</label><input type="email" id="l-email" autocomplete="email" placeholder="seu@email.com"></div>
                <div class="field"><label>Senha</label><input type="password" id="l-pass" autocomplete="current-password" placeholder="••••••••"></div>
                <button id="btn-login" class="btn btn-primary btn-full">Entrar</button>
            </div>
        </div>

        <!-- APP -->
        <div id="tpc-app" class="tpc-screen" style="display:none">
            <aside class="sidebar">
                <div class="sb-brand">
                    <?php
                    $logo_url = get_option( 'tpc_painel_logo', '' );
                    if ( $logo_url ) :
                    ?>
                    <img src="<?php echo esc_url( $logo_url ); ?>" alt="<?php echo esc_attr( get_option( 'tpc_painel_nome', get_bloginfo('name') ) ); ?>" id="sb-logo-img">
                    <?php else : ?>
                    <span id="sb-logo-fallback" style="font-size:var(--sz-text-hero);line-height:1">📦</span>
                    <?php endif; ?>
                    <span class="sb-brand-text" id="sb-nome"></span>
                </div>

                <!-- Nav cliente -->
                <nav id="nav-client" class="nav">
                    <button class="nav-btn active" data-s="dashboard">◉ Dashboard</button>
                    <button class="nav-btn" data-s="recarregar">＋ Recarregar</button>
                    <button class="nav-btn" data-s="extrato">≡ Extrato</button>
                    <button class="nav-btn" data-s="fretes">🚚 Fretes</button>
                    <button class="nav-btn" data-s="etiqueta">🏷 Etiqueta</button>
                    <button class="nav-btn" data-s="webhooks">⇄ Webhooks</button>
                    <button class="nav-btn" data-s="rastreio">📍 Rastreio</button>
                    <button class="nav-btn" data-s="suporte">💬 Suporte</button>
                </nav>

                <!-- Nav admin -->
                <nav id="nav-admin" class="nav" style="display:none">
                    <button class="nav-btn active" data-s="adm-dashboard">◉ Visão geral</button>
                    <button class="nav-btn" data-s="adm-clientes">👥 Clientes</button>
                    <button class="nav-btn" data-s="adm-transacoes">≡ Transações</button>
                    <button class="nav-btn" data-s="adm-graficos">📊 Gráficos</button>
                    <button class="nav-btn" data-s="adm-alertas">🔔 Alertas</button>
                </nav>

                <div class="sb-footer">
                    <div class="user-row">
                        <div class="avatar" id="sb-av"></div>
                        <div><div class="u-name" id="sb-name"></div><div class="u-email" id="sb-email"></div></div>
                    </div>
                    <button id="btn-logout" class="btn-logout">Sair</button>
                </div>
            </aside>

            <main class="main">

                <!-- ═══ CLIENTE: Dashboard ═══ -->
                <section id="s-dashboard" class="sec active">
                    <h2 class="sec-title">Dashboard</h2>
                    <div class="cards">
                        <div class="card card-orange">
                            <div class="card-lbl">Saldo disponível</div>
                            <div class="card-val" id="c-saldo">—</div>
                            <button class="btn btn-white btn-sm" onclick="tpcNav('recarregar')">＋ Recarregar</button>
                        </div>
                        <div class="card"><div class="card-lbl">Fretes este mês</div><div class="card-val" id="c-fretes-mes">—</div></div>
                        <div class="card"><div class="card-lbl">Gasto este mês</div><div class="card-val" id="c-gasto">—</div></div>
                        <div class="card"><div class="card-lbl">Total recarregado</div><div class="card-val" id="c-recarga-total">—</div></div>
                    </div>
                    <div class="two-col">
                        <div class="box"><div class="box-title">Últimas transações</div><div id="dash-tx"></div></div>
                        <div class="box"><div class="box-title">Últimos fretes</div><div id="dash-fr"></div></div>
                    </div>
                </section>

                <!-- ═══ CLIENTE: Recarregar ═══ -->
                <section id="s-recarregar" class="sec" style="display:none">
                    <h2 class="sec-title">Recarregar carteira</h2>
                    <div id="pix-form-wrap" class="box" style="max-width:500px">
                        <div class="box-title">Escolha o valor</div>
                        <div class="chips">
                            <?php foreach([50,100,200,500,1000] as $v): ?>
                            <button class="chip" data-v="<?= $v ?>">R$ <?= number_format($v,0,',','.') ?></button>
                            <?php endforeach; ?>
                        </div>
                        <div class="field" style="margin-top:12px">
                            <label>Outro valor (mín. R$ 10,00)</label>
                            <input type="number" id="rec-val" min="10" step="0.01" placeholder="0,00">
                        </div>
                        <div id="rec-err" class="alert alert-err" style="display:none"></div>
                        <button id="btn-pix" class="btn btn-primary btn-full" style="margin-top:14px">Gerar PIX</button>
                    </div>
                    <div id="pix-wrap" class="box" style="max-width:500px;display:none;text-align:center">
                        <div class="box-title">Pague via PIX</div>
                        <img id="pix-img" src="" alt="QR Code" style="width:200px;height:200px;margin:16px auto;display:block;border-radius:8px;border:1px solid #eee">
                        <p style="font-size:var(--sz-text-meta);color:#888;margin-bottom:6px">Ou copie o código:</p>
                        <div style="display:flex;gap:8px;margin-bottom:12px">
                            <input id="pix-cc" type="text" readonly style="flex:1;font-size:var(--sz-text-sm);font-family:var(--sz-font);padding:7px 10px;border:1px solid #ddd;border-radius:6px">
                            <button id="btn-copy" class="btn btn-secondary">Copiar</button>
                        </div>
                        <p class="pix-exp">Expira em <strong id="pix-timer">30:00</strong></p>
                        <p id="pix-wait" style="font-size:var(--sz-text-base);color:#E8650A">⏳ Aguardando pagamento...</p>
                        <div id="pix-ok" class="alert alert-ok" style="display:none">✅ PIX confirmado! Novo saldo: <strong id="pix-novo-saldo"></strong></div>
                        <button class="btn btn-ghost" style="margin-top:10px" onclick="tpcCancelPix()">← Novo PIX</button>
                    </div>
                </section>

                <!-- ═══ CLIENTE: Extrato ═══ -->
                <section id="s-extrato" class="sec" style="display:none">
                    <h2 class="sec-title">Extrato</h2>
                    <div class="filter-bar">
                        <select id="ext-tipo"><option value="">Todos</option><option value="credito">Créditos</option><option value="debito">Débitos</option></select>
                        <input type="date" id="ext-ini"><input type="date" id="ext-fim">
                        <button class="btn btn-secondary" onclick="loadExt(1)">Filtrar</button>
                        <button class="btn btn-ghost" onclick="clearExt()">Limpar</button>
                        <button class="btn btn-ghost" onclick="exportCSV('extrato')">⬇ CSV</button>
                    </div>
                    <div class="box"><div id="ext-body" class="loading">Carregando...</div><div id="ext-pag" class="pag"></div></div>
                </section>

                <!-- ═══ CLIENTE: Fretes ═══ -->
                <section id="s-fretes" class="sec" style="display:none">
                    <h2 class="sec-title">Histórico de fretes</h2>
                    <div class="filter-bar">
                        <input type="date" id="frt-ini"><input type="date" id="frt-fim">
                        <button class="btn btn-secondary" onclick="loadFrt(1)">Filtrar</button>
                        <button class="btn btn-ghost" onclick="clearFrt()">Limpar</button>
                        <button class="btn btn-ghost" onclick="exportCSV('fretes')">⬇ CSV</button>
                    </div>
                    <div class="box"><div id="frt-body" class="loading">Carregando...</div><div id="frt-pag" class="pag"></div></div>
                </section>

                <!-- ═══ CLIENTE: Etiqueta ═══ -->
                <section id="s-etiqueta" class="sec" style="display:none">
                    <h2 class="sec-title">Emitir etiqueta</h2>
                    <div class="two-col">
                        <div class="box">
                            <div class="box-title">Dados do destinatário</div>
                            <div class="field"><label>Nome completo</label><input type="text" id="et-nome" placeholder="Nome do destinatário"></div>
                            <div class="field"><label>CPF / CNPJ</label><input type="text" id="et-doc" placeholder="000.000.000-00"></div>
                            <div class="field"><label>E-mail</label><input type="email" id="et-email" placeholder="destinatario@email.com"></div>
                            <div class="field"><label>Telefone</label><input type="text" id="et-tel" placeholder="(11) 99999-9999"></div>
                            <div class="field"><label>CEP</label><input type="text" id="et-cep" placeholder="00000-000" oninput="buscarCEP(this.value)"></div>
                            <div class="field"><label>Endereço</label><input type="text" id="et-end" placeholder="Rua, número"></div>
                            <div class="field"><label>Bairro</label><input type="text" id="et-bairro"></div>
                            <div class="field" style="display:grid;grid-template-columns:1fr 1fr;gap:8px">
                                <div><label>Cidade</label><input type="text" id="et-cidade"></div>
                                <div><label>Estado</label><input type="text" id="et-estado" maxlength="2" placeholder="SP"></div>
                            </div>
                        </div>
                        <div class="box">
                            <div class="box-title">Dados do pacote</div>
                            <div class="field"><label>Peso (kg)</label><input type="number" id="et-peso" min="0.1" step="0.1" value="0.5"></div>
                            <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:8px">
                                <div class="field"><label>Altura (cm)</label><input type="number" id="et-altura" value="10"></div>
                                <div class="field"><label>Largura (cm)</label><input type="number" id="et-largura" value="15"></div>
                                <div class="field"><label>Comprimento</label><input type="number" id="et-comp" value="20"></div>
                            </div>
                            <div class="field"><label>Transportadora</label>
                                <select id="et-service">
                                    <option value="">Calculando...</option>
                                </select>
                            </div>
                            <div id="et-cotacoes" style="margin-bottom:12px"></div>
                            <div id="et-err" class="alert alert-err" style="display:none"></div>
                            <div id="et-ok" class="alert alert-ok" style="display:none"></div>
                            <button class="btn btn-secondary btn-full" onclick="calcularFrete()" style="margin-bottom:8px">Calcular fretes</button>
                            <button id="btn-emitir" class="btn btn-primary btn-full">Emitir etiqueta</button>
                        </div>
                    </div>
                    <div id="et-resultado" style="display:none" class="box">
                        <div class="box-title">Etiqueta gerada</div>
                        <div id="et-resultado-body"></div>
                    </div>
                </section>

                <!-- ═══ CLIENTE: Webhooks ═══ -->
                <section id="s-webhooks" class="sec" style="display:none">
                    <h2 class="sec-title">Webhooks</h2>
                    <div class="two-col">
                    <div class="box">
                            <div class="box-title">Cadastrar webhook por classe de entrega</div>
                            <p style="font-size:var(--sz-text-base);color:#666;margin-bottom:16px">Informe a URL externa que receberá as atualizações dos pedidos. Regra: somente 1 webhook por classe de entrega.</p>
                            <div class="alert alert-warn" style="margin-bottom:16px;font-size:var(--sz-text-meta);">⚠ Dispara nas mudanças operacionais do pedido, como aprovado, emitindo etiqueta, enviado, em retirada, a caminho, coletado e extravio. Cancelado, falhou e reembolsado não disparam.</div>
                            <div class="field"><label>Classe de entrega</label><select id="wh-class"></select></div>
                            <div class="field"><label>URL de destino</label><input type="url" id="wh-url" placeholder="https://seusistema.com.br/webhook/senderzz"></div>
                            <div class="field"><label>Chave secreta</label><input type="text" id="wh-secret" placeholder="Gerada automaticamente"></div>
                            <label style="display:flex;gap:8px;align-items:center;margin:12px 0 16px"><input type="checkbox" id="wh-active" checked> <span style="font-size:var(--sz-text-base);font-weight:500">Webhook ativo</span></label>
                            <div id="wh-msg" class="alert" style="display:none"></div>
                            <button id="btn-wh-save" class="btn btn-primary btn-full">Salvar webhook</button>
                        </div>
                        <div class="box">
                            <div class="box-title">Payload exemplo</div>
                            <pre id="wh-payload" style="white-space:pre-wrap;font-size:var(--sz-text-sm);background:#111;color:#f5f5f5;padding:12px;border-radius:10px;max-height:380px;overflow:auto"></pre>
                        </div>
                    </div>
                    <div class="box" style="margin-top:16px">
                        <div class="box-title">Webhooks cadastrados</div>
                        <div id="wh-list" class="loading">Carregando...</div>
                    </div>
                </section>

                <!-- ═══ CLIENTE: Rastreio ═══ -->
                <section id="s-rastreio" class="sec" style="display:none">
                    <h2 class="sec-title">Página de Rastreio</h2>
                    <div class="box" style="max-width:540px">
                        <div class="box-title">Identidade visual da sua página</div>
                        <p style="font-size:var(--sz-text-base);color:#666;margin-bottom:18px">
                            Configure como sua página de rastreamento aparece para os seus clientes.<br>
                            URL da página: <code id="rast-url-example" style="font-size:var(--sz-text-meta)"></code>
                        </p>
                        <div class="field">
                            <label>Logo (URL da imagem)</label>
                            <input type="url" id="rast-logo" placeholder="https://seusite.com.br/logo.png" style="width:100%">
                            <small style="color:#888">PNG ou SVG transparente. Recomendado: 300×80px.</small>
                        </div>
                        <div class="field" style="display:flex;gap:16px;align-items:flex-end">
                            <div style="flex:1">
                                <label>Cor primária</label>
                                <div style="display:flex;gap:8px;align-items:center">
                                    <input type="color" id="rast-cor-picker" value="#E8650A" style="width:48px;height:38px;padding:2px;border:1px solid #ddd;border-radius:6px;cursor:pointer">
                                    <input type="text" id="rast-cor" value="#E8650A" placeholder="#E8650A" style="width:100px">
                                </div>
                            </div>
                            <div style="flex:1">
                                <label>Cor do texto</label>
                                <div style="display:flex;gap:8px;align-items:center">
                                    <input type="color" id="rast-cor-texto-picker" value="#ffffff" style="width:48px;height:38px;padding:2px;border:1px solid #ddd;border-radius:6px;cursor:pointer">
                                    <input type="text" id="rast-cor-texto" value="#ffffff" placeholder="#ffffff" style="width:100px">
                                </div>
                            </div>
                        </div>
                        <div class="field">
                            <label>Nome da marca</label>
                            <input type="text" id="rast-nome" placeholder="Ex: Avenobis" style="width:100%">
                            <small style="color:#888">Aparece se não houver logo configurado.</small>
                        </div>
                        <div class="field">
                            <label>Texto do rodapé</label>
                            <input type="text" id="rast-rodape" placeholder="Ex: Dúvidas? fale@avenobis.com.br" style="width:100%">
                        </div>
                        <div style="margin-top:8px;padding:14px;background:#f9f9f9;border-radius:10px;border:1px solid #eee" id="rast-preview">
                            <div style="font-size:var(--sz-text-sm);font-weight:700;text-transform:none;letter-spacing:0;color:#999;margin-bottom:8px">Preview da barra superior</div>
                            <div id="rast-preview-bar" style="border-radius:8px;padding:12px 16px;display:flex;align-items:center;gap:10px;background:#E8650A">
                                <span id="rast-preview-nome" style="color:#fff;font-weight:700;font-size:var(--sz-text-lg)">Senderzz</span>
                            </div>
                        </div>
                        <button id="btn-rast-save" class="btn btn-primary btn-full" style="margin-top:18px">Salvar configurações</button>
                        <div id="rast-msg" style="margin-top:10px;font-size:var(--sz-text-base);display:none"></div>
                    </div>
                </section>

                <!-- ═══ CLIENTE: Suporte ═══ -->
                <section id="s-suporte" class="sec" style="display:none">
                    <h2 class="sec-title">Suporte</h2>
                    <div class="two-col">
                        <div class="box">
                            <div class="box-title">Abrir chamado</div>
                            <div class="field"><label>Assunto</label>
                                <select id="sup-ass">
                                    <option value="saldo">Problema com saldo</option>
                                    <option value="frete">Problema com frete</option>
                                    <option value="pix">PIX não confirmado</option>
                                    <option value="etiqueta">Etiqueta com erro</option>
                                    <option value="outro">Outro</option>
                                </select>
                            </div>
                            <div class="field"><label>Mensagem</label><textarea id="sup-msg" rows="4" placeholder="Descreva sua dúvida..."></textarea></div>
                            <div id="sup-err" class="alert alert-err" style="display:none"></div>
                            <div id="sup-ok" class="alert alert-ok" style="display:none"></div>
                            <button id="btn-ticket" class="btn btn-primary">Enviar chamado</button>
                        </div>
                        <div class="box"><div class="box-title">Meus chamados</div><div id="tickets-list" class="loading">Carregando...</div></div>
                    </div>
                </section>

                <!-- ═══ ADMIN: Visão geral ═══ -->
                <section id="s-adm-dashboard" class="sec" style="display:none">
                    <h2 class="sec-title">Visão geral</h2>
                    <div class="cards">
                        <div class="card card-orange"><div class="card-lbl">Saldo total em carteiras</div><div class="card-val" id="adm-saldo-total">—</div></div>
                        <div class="card"><div class="card-lbl">Clientes ativos</div><div class="card-val" id="adm-clientes">—</div></div>
                        <div class="card"><div class="card-lbl">Fretes este mês</div><div class="card-val" id="adm-fretes-mes">—</div></div>
                        <div class="card"><div class="card-lbl">Receita este mês</div><div class="card-val" id="adm-receita">—</div></div>
                    </div>
                    <div class="box">
                        <div class="box-title">Clientes com saldo baixo</div>
                        <div id="adm-baixo" class="loading">Carregando...</div>
                    </div>
                    <div class="box" style="margin-top:14px">
                        <div class="box-title">Últimas transações (todos os clientes)</div>
                        <div id="adm-ultimas" class="loading">Carregando...</div>
                    </div>
                </section>

                <!-- ═══ ADMIN: Clientes ═══ -->
                <section id="s-adm-clientes" class="sec" style="display:none">
                    <h2 class="sec-title">Clientes</h2>
                    <div class="filter-bar">
                        <input type="text" id="adm-busca" placeholder="Buscar por nome ou email..." style="flex:1;max-width:300px">
                        <button class="btn btn-secondary" onclick="loadAdmClientes()">Buscar</button>
                        <button class="btn btn-ghost" onclick="exportCSV('admin-clientes')">⬇ CSV</button>
                    </div>
                    <!-- Crédito rápido inline -->
                    <div class="box" style="margin-bottom:14px">
                        <div class="box-title">➕ Adicionar crédito manual</div>
                        <div style="display:grid;grid-template-columns:2fr 1fr 2fr auto;gap:10px;align-items:end">
                            <div class="field" style="margin:0"><label>E-mail do cliente</label><input type="email" id="adm-cr-email" placeholder="cliente@email.com"></div>
                            <div class="field" style="margin:0"><label>Valor R$</label><input type="number" id="adm-cr-valor" min="0.01" step="0.01" placeholder="0,00"></div>
                            <div class="field" style="margin:0"><label>Motivo</label><input type="text" id="adm-cr-motivo" value="Crédito manual"></div>
                            <button class="btn btn-primary" onclick="admCreditar()">Adicionar</button>
                        </div>
                        <div id="adm-cr-msg" style="margin-top:10px;font-size:var(--sz-text-base)"></div>
                    </div>
                    <div class="box"><div id="adm-clientes-body" class="loading">Carregando...</div><div id="adm-cli-pag" class="pag"></div></div>
                </section>

                <!-- ═══ ADMIN: Transações ═══ -->
                <section id="s-adm-transacoes" class="sec" style="display:none">
                    <h2 class="sec-title">Transações</h2>
                    <div class="filter-bar">
                        <select id="adm-tx-tipo"><option value="">Todos</option><option value="credito">Créditos</option><option value="debito">Débitos</option></select>
                        <input type="date" id="adm-tx-ini"><input type="date" id="adm-tx-fim">
                        <input type="text" id="adm-tx-user" placeholder="User ID">
                        <button class="btn btn-secondary" onclick="loadAdmTx(1)">Filtrar</button>
                        <button class="btn btn-ghost" onclick="exportCSV('admin-transacoes')">⬇ CSV</button>
                    </div>
                    <div class="box"><div id="adm-tx-body" class="loading">Carregando...</div><div id="adm-tx-pag" class="pag"></div></div>
                </section>

                <!-- ═══ ADMIN: Gráficos ═══ -->
                <section id="s-adm-graficos" class="sec" style="display:none">
                    <h2 class="sec-title">Gráficos</h2>
                    <div class="two-col">
                        <div class="box"><div class="box-title">Recargas por mês</div><canvas id="chart-recargas" height="220"></canvas></div>
                        <div class="box"><div class="box-title">Fretes por mês</div><canvas id="chart-fretes" height="220"></canvas></div>
                    </div>
                    <div class="box" style="margin-top:14px">
                        <div class="box-title">Top 10 clientes por volume (R$)</div>
                        <canvas id="chart-top" height="160"></canvas>
                    </div>
                </section>

                <!-- ═══ ADMIN: Alertas ═══ -->
                <section id="s-adm-alertas" class="sec" style="display:none">
                    <h2 class="sec-title">Alertas de saldo baixo</h2>
                    <div class="filter-bar">
                        <label style="font-size:var(--sz-text-base)">Limite de alerta: R$</label>
                        <input type="number" id="adm-limite" value="50" min="0" step="10" style="width:90px">
                        <button class="btn btn-secondary" onclick="loadAlertas()">Atualizar</button>
                    </div>
                    <div class="box"><div id="adm-alertas-body" class="loading">Carregando...</div></div>
                </section>

            </main>
        </div>
    </div>
    <?php return ob_get_clean();
}

/* ── Endpoints REST adicionais (admin) ──────────────────────────────────── */
add_action( 'rest_api_init', function () {

    // Admin: lista de clientes com saldo
    register_rest_route( 'tp-carteira/v1', '/admin/clientes', [
        'methods'             => 'GET',
        'callback'            => 'tpc_ep_adm_clientes',
        'permission_callback' => fn() => tpc_is_admin_jwt(),
    ] );

    // Admin: transações globais
    register_rest_route( 'tp-carteira/v1', '/admin/transacoes', [
        'methods'             => 'GET',
        'callback'            => 'tpc_ep_adm_transacoes',
        'permission_callback' => fn() => tpc_is_admin_jwt(),
    ] );

    // Admin: resumo do dashboard
    register_rest_route( 'tp-carteira/v1', '/admin/resumo', [
        'methods'             => 'GET',
        'callback'            => 'tpc_ep_adm_resumo',
        'permission_callback' => fn() => tpc_is_admin_jwt(),
    ] );

    // Admin: creditar manualmente
    register_rest_route( 'tp-carteira/v1', '/admin/creditar', [
        'methods'             => 'POST',
        'callback'            => 'tpc_ep_adm_creditar',
        'permission_callback' => fn() => tpc_is_admin_jwt(),
        'args'                => [
            'email'  => [ 'type' => 'string', 'required' => true ],
            'valor'  => [ 'type' => 'number', 'required' => true ],
            'motivo' => [ 'type' => 'string', 'default'  => 'Crédito manual admin' ],
        ],
    ] );

    // Admin: gráficos por mês
    register_rest_route( 'tp-carteira/v1', '/admin/graficos', [
        'methods'             => 'GET',
        'callback'            => 'tpc_ep_adm_graficos',
        'permission_callback' => fn() => tpc_is_admin_jwt(),
    ] );

    // Verifica role do usuário autenticado
    register_rest_route( 'tp-carteira/v1', '/me/role', [
        'methods'             => 'GET',
        'callback'            => fn() => new WP_REST_Response( [
            'role' => tpc_is_admin_jwt() ? 'admin' : 'client',
        ] ),
        'permission_callback' => 'tpc_rest_auth',
    ] );

    // Melhor Envio: calcular frete
    register_rest_route( 'tp-carteira/v1', '/calcular-frete', [
        'methods'             => 'POST',
        'callback'            => 'tpc_ep_calcular_frete',
        'permission_callback' => 'tpc_rest_auth',
    ] );

    // Melhor Envio: emitir etiqueta
    register_rest_route( 'tp-carteira/v1', '/emitir-etiqueta', [
        'methods'             => 'POST',
        'callback'            => 'tpc_ep_emitir_etiqueta',
        'permission_callback' => 'tpc_rest_auth',
    ] );

    // Tickets (suporte)
    register_rest_route( 'tp-carteira/v1', '/tickets', [
        [ 'methods' => 'GET',  'callback' => 'tpc_ep_get_tickets',   'permission_callback' => 'tpc_rest_auth' ],
        [ 'methods' => 'POST', 'callback' => 'tpc_ep_criar_ticket',  'permission_callback' => 'tpc_rest_auth',
          'args' => [ 'assunto' => ['required'=>true], 'mensagem' => ['required'=>true] ] ],
    ] );
} );

function tpc_is_admin_jwt(): bool {
    $uid = tpc_rest_auth();
    if ( is_wp_error( $uid ) ) return false;
    return user_can( $uid, 'manage_woocommerce' );
}

/* ── Handlers admin ─────────────────────────────────────────────────────── */

function tpc_ep_adm_clientes( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $busca    = sanitize_text_field( $req->get_param('busca') ?? '' );
    $page     = max(1, (int)($req->get_param('page') ?? 1));
    $per_page = 20;
    $offset   = ($page-1)*$per_page;

    $where = '1=1';
    $params = [];
    if ( $busca ) {
        $like = '%' . $wpdb->esc_like($busca) . '%';
        $where = '(u.display_name LIKE %s OR u.user_email LIKE %s)';
        $params = [$like, $like];
    }

    $sql = "SELECT c.user_id, c.saldo, c.updated_at, u.display_name, u.user_email,
                   COUNT(t.id) as total_tx,
                   SUM(CASE WHEN t.tipo='debito' THEN t.valor ELSE 0 END) as total_gasto,
                   SUM(CASE WHEN t.tipo='credito' THEN t.valor ELSE 0 END) as total_recarga
            FROM {$wpdb->prefix}tpc_carteira c
            JOIN {$wpdb->users} u ON u.ID = c.user_id
            LEFT JOIN {$wpdb->prefix}tpc_transacoes t ON t.user_id = c.user_id
            WHERE $where
            GROUP BY c.user_id ORDER BY c.saldo DESC LIMIT %d OFFSET %d";

    $rows = $wpdb->get_results(
        $params ? $wpdb->prepare($sql, ...[...$params, $per_page, $offset]) : $wpdb->prepare($sql, $per_page, $offset),
        ARRAY_A
    );

    $total = (int)$wpdb->get_var("SELECT COUNT(*) FROM {$wpdb->prefix}tpc_carteira c JOIN {$wpdb->users} u ON u.ID=c.user_id WHERE $where" . ($params ? $wpdb->prepare(' AND ('.$where.')', ...$params) : ''));

    return new WP_REST_Response(['data'=>$rows,'total'=>$total,'pages'=>ceil($total/$per_page)]);
}

function tpc_ep_adm_transacoes( WP_REST_Request $req ): WP_REST_Response {
    global $wpdb;
    $tipo    = sanitize_key($req->get_param('tipo') ?? '');
    $ini     = sanitize_text_field($req->get_param('data_ini') ?? '');
    $fim     = sanitize_text_field($req->get_param('data_fim') ?? '');
    $user_id = (int)($req->get_param('user_id') ?? 0);
    $page    = max(1,(int)($req->get_param('page')??1));
    $per     = 20;

    $where = ['1=1'];
    if ($tipo)    $where[] = $wpdb->prepare('tipo=%s',$tipo);
    if ($ini)     $where[] = $wpdb->prepare('created_at>=%s',$ini.' 00:00:00');
    if ($fim)     $where[] = $wpdb->prepare('created_at<=%s',$fim.' 23:59:59');
    if ($user_id) $where[] = $wpdb->prepare('user_id=%d',$user_id);

    $w = implode(' AND ',$where);
    $total = (int)$wpdb->get_var("SELECT COUNT(*) FROM {$wpdb->prefix}tpc_transacoes WHERE $w");
    $rows  = $wpdb->get_results($wpdb->prepare(
        "SELECT t.*, u.display_name, u.user_email FROM {$wpdb->prefix}tpc_transacoes t
         JOIN {$wpdb->users} u ON u.ID=t.user_id WHERE $w ORDER BY created_at DESC LIMIT %d OFFSET %d",
        $per, ($page-1)*$per
    ), ARRAY_A);

    foreach($rows as &$r) {
        $r['valor_fmt']     = 'R$ '.number_format((float)$r['valor'],2,',','.');
        $r['saldo_apos_fmt']= 'R$ '.number_format((float)$r['saldo_apos'],2,',','.');
    }

    return new WP_REST_Response(['data'=>$rows,'total'=>$total,'pages'=>ceil($total/$per)]);
}

function tpc_ep_adm_resumo(): WP_REST_Response {
    global $wpdb;
    $mes_ini = date('Y-m-01 00:00:00');
    $mes_fim = date('Y-m-t 23:59:59');

    $saldo_total  = (float)$wpdb->get_var("SELECT SUM(saldo) FROM {$wpdb->prefix}tpc_carteira");
    $n_clientes   = (int)$wpdb->get_var("SELECT COUNT(*) FROM {$wpdb->prefix}tpc_carteira WHERE saldo>0");
    $fretes_mes   = (int)$wpdb->get_var($wpdb->prepare("SELECT COUNT(*) FROM {$wpdb->prefix}tpc_transacoes WHERE tipo='debito' AND created_at BETWEEN %s AND %s",$mes_ini,$mes_fim));
    $receita_mes  = (float)$wpdb->get_var($wpdb->prepare("SELECT SUM(valor) FROM {$wpdb->prefix}tpc_transacoes WHERE tipo='credito' AND created_at BETWEEN %s AND %s",$mes_ini,$mes_fim));

    $saldo_baixo  = $wpdb->get_results($wpdb->prepare(
        "SELECT c.user_id,c.saldo,u.display_name,u.user_email FROM {$wpdb->prefix}tpc_carteira c
         JOIN {$wpdb->users} u ON u.ID=c.user_id WHERE c.saldo < %f AND c.saldo >= 0 ORDER BY c.saldo ASC LIMIT 10",
        (float)get_option('tpc_saldo_minimo',50)
    ), ARRAY_A);

    $ultimas = $wpdb->get_results(
        "SELECT t.*,u.display_name FROM {$wpdb->prefix}tpc_transacoes t JOIN {$wpdb->users} u ON u.ID=t.user_id ORDER BY t.created_at DESC LIMIT 10",
        ARRAY_A
    );

    return new WP_REST_Response(compact('saldo_total','n_clientes','fretes_mes','receita_mes','saldo_baixo','ultimas'));
}

function tpc_ep_adm_creditar( WP_REST_Request $req ): WP_REST_Response {
    $email  = sanitize_email($req->get_param('email'));
    $valor  = abs((float)$req->get_param('valor'));
    $motivo = sanitize_text_field($req->get_param('motivo'));
    $user   = get_user_by('email',$email);

    if (!$user) return new WP_REST_Response(['error'=>'Usuário não encontrado.'],404);
    if ($valor<=0) return new WP_REST_Response(['error'=>'Valor inválido.'],400);

    $tid = tpc_creditar($user->ID,$valor,$motivo,['referencia'=>'credito_admin_painel']);
    if (!$tid) return new WP_REST_Response(['error'=>'Erro ao creditar.'],500);

    return new WP_REST_Response([
        'ok'         => true,
        'nome'       => $user->display_name,
        'novo_saldo' => tpc_get_saldo($user->ID),
        'novo_saldo_fmt' => 'R$ '.number_format(tpc_get_saldo($user->ID),2,',','.'),
    ]);
}

function tpc_ep_adm_graficos(): WP_REST_Response {
    global $wpdb;

    $meses = [];
    for($i=11;$i>=0;$i--) {
        $meses[] = date('Y-m', strtotime("-$i months"));
    }

    $recargas = []; $fretes_g = [];
    foreach($meses as $m) {
        $ini = $m.'-01 00:00:00';
        $fim = date('Y-m-t 23:59:59', strtotime($m.'-01'));
        $recargas[] = (float)$wpdb->get_var($wpdb->prepare("SELECT SUM(valor) FROM {$wpdb->prefix}tpc_transacoes WHERE tipo='credito' AND created_at BETWEEN %s AND %s",$ini,$fim)) ?: 0;
        $fretes_g[] = (float)$wpdb->get_var($wpdb->prepare("SELECT SUM(valor) FROM {$wpdb->prefix}tpc_transacoes WHERE tipo='debito' AND created_at BETWEEN %s AND %s",$ini,$fim)) ?: 0;
    }

    $top = $wpdb->get_results(
        "SELECT u.display_name as nome, SUM(t.valor) as total FROM {$wpdb->prefix}tpc_transacoes t
         JOIN {$wpdb->users} u ON u.ID=t.user_id WHERE t.tipo='debito' GROUP BY t.user_id ORDER BY total DESC LIMIT 10",
        ARRAY_A
    );

    return new WP_REST_Response([
        'labels'   => array_map(fn($m)=>date('M/y',strtotime($m.'-01')),$meses),
        'recargas' => $recargas,
        'fretes'   => $fretes_g,
        'top'      => $top,
    ]);
}

/* ── Etiqueta / Frete ME ─────────────────────────────────────────────────── */

function tpc_ep_calcular_frete( WP_REST_Request $req ): WP_REST_Response {
    $token = get_option('tpc_me_token','');
    if(!$token) return new WP_REST_Response(['error'=>'Token ME não configurado.'],500);

    $body = [
        'from' => [ 'postal_code' => get_option('tpc_cep_origem','01001000') ],
        'to'   => [ 'postal_code' => preg_replace('/\D/','',$req->get_param('cep_destino')??'') ],
        'package' => [
            'height' => (float)($req->get_param('altura')??10),
            'width'  => (float)($req->get_param('largura')??15),
            'length' => (float)($req->get_param('comprimento')??20),
            'weight' => (float)($req->get_param('peso')??0.5),
        ],
        'options' => [ 'receipt'=>false, 'own_hand'=>false ],
        'services' => '1,2,3,4,7,8,9,10,11,12,13,14,15,16,17',
    ];

    $r = wp_remote_post( TPC_ME_API.'/me/shipment/calculate', [
        'timeout'=>20,
        'headers'=> ['Authorization'=>'Bearer '.$token,'Content-Type'=>'application/json','Accept'=>'application/json','User-Agent'=>'TP-Carteira/1.0'],
        'body'   => wp_json_encode($body),
    ]);

    if(is_wp_error($r)) return new WP_REST_Response(['error'=>$r->get_error_message()],500);
    $data = json_decode(wp_remote_retrieve_body($r),true);
    return new WP_REST_Response($data);
}

function tpc_ep_emitir_etiqueta( WP_REST_Request $req ): WP_REST_Response {
    // S6: usar senderzz_get_me_token() — evita ler literal 'env-managed' se hooks não rodaram
    $token = function_exists( 'senderzz_get_me_token' ) ? senderzz_get_me_token() : (string) get_option( 'tpc_me_token', '' );
    if(!$token) return new WP_REST_Response(['error'=>'Token ME não configurado.'],500);

    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) ) return new WP_REST_Response( [ 'error' => 'Não autenticado.' ], 401 );

    // CRIT-01: NUNCA aceitar `preco` do cliente. Recalcular no servidor.
    $service = (int) $req->get_param('service_id');
    if ( $service <= 0 ) return new WP_REST_Response( [ 'error' => 'service_id obrigatório.' ], 400 );

    // Validações de pacote — limites razoáveis pra evitar lixo de input.
    $altura      = (float) $req->get_param('altura');
    $largura     = (float) $req->get_param('largura');
    $comprimento = (float) $req->get_param('comprimento');
    $peso        = (float) $req->get_param('peso');
    if ( $altura <= 0 || $largura <= 0 || $comprimento <= 0 || $peso <= 0 ) {
        return new WP_REST_Response( [ 'error' => 'Dimensões e peso devem ser > 0.' ], 400 );
    }
    if ( $altura > 200 || $largura > 200 || $comprimento > 200 || $peso > 100 ) {
        return new WP_REST_Response( [ 'error' => 'Dimensões/peso fora do limite permitido.' ], 400 );
    }

    $cep_destino = preg_replace( '/\D/', '', (string) $req->get_param('cep') );
    if ( strlen( $cep_destino ) !== 8 ) {
        return new WP_REST_Response( [ 'error' => 'CEP de destino inválido.' ], 400 );
    }

    // Cota o frete diretamente no Melhor Envio com os MESMOS dados que serão usados na etiqueta.
    $calc_body = [
        'from'    => [ 'postal_code' => preg_replace( '/\D/', '', (string) get_option( 'tpc_cep_origem', '01001000' ) ) ],
        'to'      => [ 'postal_code' => $cep_destino ],
        'package' => [
            'height' => $altura,
            'width'  => $largura,
            'length' => $comprimento,
            'weight' => $peso,
        ],
        'options'  => [ 'receipt' => false, 'own_hand' => false ],
        'services' => (string) $service,
    ];
    $calc_resp = wp_remote_post( TPC_ME_API . '/me/shipment/calculate', [
        'timeout' => 20,
        'headers' => [ 'Authorization' => 'Bearer ' . $token, 'Content-Type' => 'application/json', 'Accept' => 'application/json', 'User-Agent' => 'TP-Carteira/1.0' ],
        'body'    => wp_json_encode( $calc_body ),
    ] );
    if ( is_wp_error( $calc_resp ) ) {
        return new WP_REST_Response( [ 'error' => 'Falha ao cotar frete: ' . $calc_resp->get_error_message() ], 502 );
    }
    $calc_data = json_decode( wp_remote_retrieve_body( $calc_resp ), true );
    if ( ! is_array( $calc_data ) ) {
        return new WP_REST_Response( [ 'error' => 'Resposta inválida do Melhor Envio.' ], 502 );
    }

    // Extrai a cotação para o serviço pedido. ME retorna array indexado.
    $servico_cotado = null;
    foreach ( $calc_data as $s ) {
        if ( ! is_array( $s ) ) continue;
        if ( (int) ( $s['id'] ?? 0 ) === $service && empty( $s['error'] ) && ! empty( $s['price'] ) ) {
            $servico_cotado = $s;
            break;
        }
    }
    if ( ! $servico_cotado ) {
        return new WP_REST_Response( [ 'error' => 'Serviço não disponível para essa cotação.', 'detail' => $calc_data ], 422 );
    }

    $custo_real = (float) $servico_cotado['price'];
    if ( $custo_real <= 0 ) {
        return new WP_REST_Response( [ 'error' => 'Cotação retornou preço zerado.' ], 422 );
    }

    // Aplica markup configurado pelo operador, se o módulo expôs a função.
    $preco_servidor = function_exists( 'senderzz_apply_markup' )
        ? (float) senderzz_apply_markup( $custo_real, 0 )
        : $custo_real;
    $preco_servidor = round( max( $preco_servidor, $custo_real ), 2 );

    // Verifica saldo contra o preço calculado pelo SERVIDOR — não pelo cliente.
    if ( ! tpc_tem_saldo( $user_id, $preco_servidor ) ) {
        return new WP_REST_Response( [
            'error'          => 'Saldo insuficiente. Recarregue sua carteira.',
            'preco_real'     => $preco_servidor,
            'preco_real_fmt' => 'R$ ' . number_format( $preco_servidor, 2, ',', '.' ),
        ], 402 );
    }

    // Reserva o saldo ANTES de chamar o ME — se o ME falhar, liberamos.
    $reserva_ref = 'etiqueta_' . wp_generate_uuid4();
    $reserva_id  = function_exists( 'tpc_reservar' )
        ? tpc_reservar( $user_id, $preco_servidor, 'Reserva para emissão de etiqueta', [ 'referencia' => $reserva_ref ] )
        : false;

    if ( ! $reserva_id ) {
        return new WP_REST_Response( [ 'error' => 'Não foi possível reservar saldo.' ], 500 );
    }

    $body = [
        'service'  => $service,
        'agency'   => null,
        'from'     => [
            'name'             => get_option('tpc_remetente_nome', get_bloginfo('name')),
            'phone'            => get_option('tpc_remetente_tel',''),
            'email'            => get_option('admin_email'),
            'document'         => get_option('tpc_remetente_doc',''),
            'company_document' => '',
            'state_register'   => '',
            'postal_code'      => preg_replace( '/\D/', '', (string) get_option( 'tpc_cep_origem', '01001000' ) ),
            'address'          => get_option('tpc_remetente_end',''),
            'complement'       => '',
            'number'           => '',
            'district'         => get_option('tpc_remetente_bairro',''),
            'city'             => get_option('tpc_remetente_cidade',''),
            'country_id'       => 'BR',
            'state_abbr'       => get_option('tpc_remetente_uf','SP'),
        ],
        'to' => [
            'name'        => sanitize_text_field($req->get_param('nome')),
            'phone'       => sanitize_text_field($req->get_param('telefone')),
            'email'       => sanitize_email($req->get_param('email')),
            'document'    => preg_replace('/\D/','',$req->get_param('documento')??''),
            'postal_code' => $cep_destino,
            'address'     => sanitize_text_field($req->get_param('endereco')),
            'complement'  => sanitize_text_field($req->get_param('complemento')??''),
            'number'      => sanitize_text_field($req->get_param('numero')??''),
            'district'    => sanitize_text_field($req->get_param('bairro')),
            'city'        => sanitize_text_field($req->get_param('cidade')),
            'country_id'  => 'BR',
            'state_abbr'  => strtoupper(sanitize_text_field($req->get_param('estado'))),
        ],
        'products' => [ [ 'name' => 'Produto', 'quantity' => 1, 'unitary_value' => (float) get_option( 'senderzz_pix_unitary_value', 50 ) ] ],
        'volumes'  => [ [ 'height' => $altura, 'width' => $largura, 'length' => $comprimento, 'weight' => $peso ] ],
        'options'  => [ 'receipt' => false, 'own_hand' => false, 'reverse' => false, 'non_commercial' => false ],
        'platform' => 'TP Carteira',
    ];

    // 1. Adiciona ao carrinho ME
    $r = wp_remote_post( TPC_ME_API . '/me/cart', [
        'timeout' => 20,
        'headers' => [ 'Authorization' => 'Bearer ' . $token, 'Content-Type' => 'application/json', 'Accept' => 'application/json', 'User-Agent' => 'TP-Carteira/1.0' ],
        'body'    => wp_json_encode( $body ),
    ] );
    if ( is_wp_error( $r ) ) {
        if ( function_exists( 'tpc_liberar_reserva' ) ) tpc_liberar_reserva( $reserva_id, 'erro_cart' );
        return new WP_REST_Response( [ 'error' => $r->get_error_message() ], 502 );
    }
    $cart = json_decode( wp_remote_retrieve_body( $r ), true );
    if ( empty( $cart['id'] ) ) {
        if ( function_exists( 'tpc_liberar_reserva' ) ) tpc_liberar_reserva( $reserva_id, 'cart_sem_id' );
        return new WP_REST_Response( [ 'error' => 'Erro ao criar envio no ME.', 'detail' => $cart ], 502 );
    }
    $order_id_me = $cart['id'];

    // Sanity check: confirma que o ME cobrou um preço próximo do que cotamos.
    // Se divergir mais de 5% pra cima, aborta e libera a reserva (cliente refaz a cotação).
    $cart_price = isset( $cart['price'] ) ? (float) $cart['price'] : $custo_real;
    if ( $cart_price > $custo_real * 1.05 ) {
        if ( function_exists( 'tpc_liberar_reserva' ) ) tpc_liberar_reserva( $reserva_id, 'preco_divergente' );
        return new WP_REST_Response( [
            'error'      => 'Preço cotado divergiu na hora da emissão. Refaça a cotação.',
            'cotado'     => $custo_real,
            'recebido'   => $cart_price,
        ], 409 );
    }

    // 2. Checkout ME
    $r2 = wp_remote_post( TPC_ME_API . '/me/shipment/checkout', [
        'timeout' => 20,
        'headers' => [ 'Authorization' => 'Bearer ' . $token, 'Content-Type' => 'application/json', 'Accept' => 'application/json', 'User-Agent' => 'TP-Carteira/1.0' ],
        'body'    => wp_json_encode( [ 'orders' => [ $order_id_me ] ] ),
    ] );
    if ( is_wp_error( $r2 ) ) {
        if ( function_exists( 'tpc_liberar_reserva' ) ) tpc_liberar_reserva( $reserva_id, 'erro_checkout' );
        return new WP_REST_Response( [ 'error' => $r2->get_error_message() ], 502 );
    }
    $checkout_code = (int) wp_remote_retrieve_response_code( $r2 );
    if ( $checkout_code < 200 || $checkout_code >= 300 ) {
        if ( function_exists( 'tpc_liberar_reserva' ) ) tpc_liberar_reserva( $reserva_id, 'checkout_http_' . $checkout_code );
        return new WP_REST_Response( [ 'error' => 'Falha no checkout do Melhor Envio.', 'http' => $checkout_code ], 502 );
    }

    // 3. Confirma débito da reserva — saldo cliente vai embora SOMENTE depois do checkout no ME.
    if ( function_exists( 'tpc_debitar_reserva' ) ) {
        tpc_debitar_reserva( $reserva_id, [ 'me_order_id' => $order_id_me ] );
    } else {
        // Fallback se o módulo de reserva não estiver carregado por alguma razão.
        tpc_debitar( $user_id, $preco_servidor, 'Etiqueta gerada - ' . ( $cart['service']['name'] ?? 'Frete' ),
            [ 'me_order_id' => $order_id_me, 'referencia' => $reserva_ref ] );
    }

    // 4. Gera etiqueta
    $r3 = wp_remote_post( TPC_ME_API . '/me/shipment/print', [
        'timeout' => 20,
        'headers' => [ 'Authorization' => 'Bearer ' . $token, 'Content-Type' => 'application/json', 'Accept' => 'application/json', 'User-Agent' => 'TP-Carteira/1.0' ],
        'body'    => wp_json_encode( [ 'mode' => 'public', 'orders' => [ $order_id_me ] ] ),
    ] );
    $print = is_wp_error( $r3 ) ? [] : ( json_decode( wp_remote_retrieve_body( $r3 ), true ) ?: [] );

    return new WP_REST_Response( [
        'ok'           => true,
        'order_id'     => $order_id_me,
        'etiqueta_url' => $print['url'] ?? ( $print[0]['url'] ?? '' ),
        'tracking'     => $cart['tracking'] ?? '',
        'servico'      => $cart['service']['name'] ?? '',
        'preco'        => $preco_servidor,
        'preco_fmt'    => 'R$ ' . number_format( $preco_servidor, 2, ',', '.' ),
    ] );
}

/* ── Tickets ─────────────────────────────────────────────────────────────── */

add_action('init',function(){
    register_post_type('tpc_ticket',[
        'label'=>'Chamados','public'=>false,'show_ui'=>true,
        'show_in_menu'=>'tpc-carteira','supports'=>['title','editor','author','custom-fields'],
    ]);
});

function tpc_ep_get_tickets(WP_REST_Request $req): WP_REST_Response {
    $uid = tpc_rest_auth();
    $is_admin = tpc_is_admin_jwt();
    $args = ['post_type'=>'tpc_ticket','posts_per_page'=>20,'post_status'=>['publish','pending','private'],'orderby'=>'date','order'=>'DESC'];
    if(!$is_admin) $args['author'] = $uid;
    $posts = get_posts($args);
    return new WP_REST_Response(array_map(fn($p)=>[
        'id'=>$p->ID,'assunto'=>$p->post_title,'mensagem'=>$p->post_content,
        'status'=>get_post_meta($p->ID,'_tpc_ticket_status',true)?:'aberto',
        'resposta'=>get_post_meta($p->ID,'_tpc_ticket_resposta',true)?:'',
        'criado_em'=>$p->post_date,'autor'=>get_user_by('id',$p->post_author)->display_name??'',
    ],$posts));
}

function tpc_ep_criar_ticket(WP_REST_Request $req): WP_REST_Response {
    $uid = tpc_rest_auth();
    $id  = wp_insert_post(['post_type'=>'tpc_ticket','post_title'=>sanitize_text_field($req->get_param('assunto')),'post_content'=>sanitize_textarea_field($req->get_param('mensagem')),'post_status'=>'publish','post_author'=>$uid]);
    if(is_wp_error($id)) return new WP_REST_Response(['error'=>'Erro ao criar chamado.'],500);
    update_post_meta($id,'_tpc_ticket_status','aberto');
    $u = get_user_by('id',$uid);
    wp_mail(get_option('admin_email'),'[Suporte] '.$req->get_param('assunto'),"De: {$u->display_name}\n\n".$req->get_param('mensagem')."\n\nResponder: ".admin_url('post.php?post='.$id.'&action=edit'));
    return new WP_REST_Response(['ok'=>true,'id'=>$id],201);
}
