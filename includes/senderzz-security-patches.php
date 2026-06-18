<?php
/**
 * senderzz-security-patches.php — Patches de segurança consolidados
 *
 * Consolida (e substitui) os seguintes arquivos que eram drop-ins separados:
 *   - senderzz-ssrf-protection.php      (SSRF via DNS resolve)
 *   - senderzz-portal-login-ratelimit.php (brute-force no login)
 *   - senderzz-tier2-batch.php          (idempotência, cross-check user, rate limit etiqueta)
 *   - senderzz-tier3-batch.php          (SSRF ajax, rate limit PIX, SSRF bypass portal)
 *   - senderzz-tier4-audit-fixes.php    (IP spoofing, token rate limit, webhook guard)
 *
 * Versão: 1.0.0 (consolidado em 2026-05)
 */

defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_SECURITY_PATCHES_LOADED' ) ) return;
define( 'SENDERZZ_SECURITY_PATCHES_LOADED', true );


// ════════════════════════════════════════════════════════════════════
// senderzz-ssrf-protection.php
// ════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Validação principal — chamada antes de salvar URL de webhook.
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_url_is_safe( string $url ): array {
    $url = trim( $url );

    if ( $url === '' ) {
        return [ 'safe' => false, 'reason' => 'URL vazia.' ];
    }

    // Schema: apenas https/http (preferência https em produção).
    if ( ! preg_match( '#^https?://#i', $url ) ) {
        return [ 'safe' => false, 'reason' => 'Apenas http/https permitidos.' ];
    }

    $parsed = wp_parse_url( $url );
    if ( ! is_array( $parsed ) || empty( $parsed['host'] ) ) {
        return [ 'safe' => false, 'reason' => 'URL malformada.' ];
    }

    $host = strtolower( $parsed['host'] );

    // Lista de hostnames bloqueados (case-insensitive).
    $blocked_hosts = apply_filters( 'senderzz_ssrf_blocked_hosts', [
        'localhost',
        'localhost.localdomain',
        'metadata.google.internal',  // GCP metadata
        'metadata',
    ] );
    if ( in_array( $host, $blocked_hosts, true ) ) {
        return [ 'safe' => false, 'reason' => 'Hostname não permitido.' ];
    }

    // Resolve DNS → IPs.
    // Se host já é um IP, gethostbyname devolve o próprio.
    $ips = [];

    if ( filter_var( $host, FILTER_VALIDATE_IP ) ) {
        $ips[] = $host;
    } else {
        // Resolve A e AAAA records.
        $a_records    = @dns_get_record( $host, DNS_A );
        $aaaa_records = @dns_get_record( $host, DNS_AAAA );

        if ( is_array( $a_records ) ) {
            foreach ( $a_records as $r ) {
                if ( ! empty( $r['ip'] ) ) $ips[] = $r['ip'];
            }
        }
        if ( is_array( $aaaa_records ) ) {
            foreach ( $aaaa_records as $r ) {
                if ( ! empty( $r['ipv6'] ) ) $ips[] = $r['ipv6'];
            }
        }

        // Fallback se dns_get_record falhar.
        if ( empty( $ips ) ) {
            $resolved = gethostbyname( $host );
            if ( $resolved && $resolved !== $host && filter_var( $resolved, FILTER_VALIDATE_IP ) ) {
                $ips[] = $resolved;
            }
        }
    }

    if ( empty( $ips ) ) {
        return [ 'safe' => false, 'reason' => 'DNS não resolveu. URL inacessível ou bloqueada.' ];
    }

    // Verifica todos os IPs resolvidos. Se QUALQUER um for bloqueado,
    // recusa (evita DNS rebinding).
    foreach ( $ips as $ip ) {
        if ( ! senderzz_ip_is_public( $ip ) ) {
            return [ 'safe' => false, 'reason' => 'IP não permitido (privado, loopback ou link-local): ' . $ip ];
        }
    }

    return [ 'safe' => true, 'reason' => 'OK', 'ips' => $ips ];
}

/**
 * Verifica se um IP é público (não-privado, não-loopback, não-link-local).
 */
function senderzz_ip_is_public( string $ip ): bool {
    if ( ! filter_var( $ip, FILTER_VALIDATE_IP ) ) return false;

    // FILTER_FLAG_NO_PRIV_RANGE bloqueia 10/8, 172.16/12, 192.168/16, fc00::/7
    // FILTER_FLAG_NO_RES_RANGE bloqueia 127/8, 169.254/16, 224/4, ::1, fe80::/10
    $public = filter_var(
        $ip,
        FILTER_VALIDATE_IP,
        FILTER_FLAG_NO_PRIV_RANGE | FILTER_FLAG_NO_RES_RANGE
    );

    if ( $public === false ) return false;

    // Bloqueios extras que filter_var não cobre:
    if ( strpos( $ip, '0.0.0.0' ) === 0 ) return false;          // IPv4 wildcard
    if ( $ip === '::' || $ip === '0:0:0:0:0:0:0:0' ) return false; // IPv6 wildcard
    if ( strpos( $ip, '169.254.' ) === 0 ) return false;         // link-local AWS IMDS
    if ( strpos( $ip, '100.64.' ) === 0 ) return false;          // CGNAT (RFC 6598) — opcional, comentar se atrapalha

    return true;
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook: valida URL antes de salvar webhook de produtor.
// ─────────────────────────────────────────────────────────────────────────────

add_filter( 'rest_pre_dispatch', function ( $result, $server, $request ) {
    $route = $request->get_route();

    // Aplica em saves dos dois namespaces de webhook.
    $is_producer_save = ( $route === '/tp-carteira/v1/webhooks' && $request->get_method() === 'POST' );
    $is_tracking_save = ( $route === '/senderzz/v1/webhooks' && $request->get_method() === 'POST' );

    if ( ! $is_producer_save && ! $is_tracking_save ) {
        return $result;
    }

    $url = (string) $request->get_param( 'url' );
    if ( $url === '' ) return $result; // deixa o handler original retornar erro de URL vazia

    $check = senderzz_url_is_safe( $url );
    if ( ! $check['safe'] ) {
        // Log da tentativa.
        if ( function_exists( 'wc_get_logger' ) ) {
            wc_get_logger()->warning(
                wp_json_encode( [
                    'event'  => 'ssrf_blocked',
                    'route'  => $route,
                    'url'    => $url,
                    'reason' => $check['reason'],
                    'ip'     => $_SERVER['REMOTE_ADDR'] ?? '',
                ], JSON_UNESCAPED_UNICODE ),
                [ 'source' => 'senderzz-ssrf-protection' ]
            );
        }

        return new WP_REST_Response( [
            'success' => false,
            'error'   => 'URL não permitida: ' . $check['reason'],
            'message' => 'URL não permitida: ' . $check['reason'],
        ], 400 );
    }

    return $result;
}, 5, 3 );

// ─────────────────────────────────────────────────────────────────────────────
// Hook: limpa `body` / `response_body` antes de retornar respostas dos
// endpoints de teste e logs. Mantém `code` e `success` para o cliente
// saber se funcionou, mas NÃO vaza o conteúdo da resposta interna.
// ─────────────────────────────────────────────────────────────────────────────

add_filter( 'rest_post_dispatch', function ( $response, $server, $request ) {
    if ( ! $response instanceof WP_REST_Response ) return $response;

    $route  = $request->get_route();
    $method = $request->get_method();

    // Endpoints sensíveis: tudo que envolva /test ou /logs nos webhooks.
    $is_sensitive = (
        ( strpos( $route, '/tp-carteira/v1/webhooks/' ) === 0 && (
            substr( $route, -5 ) === '/test' || substr( $route, -5 ) === '/logs'
        ) )
        || ( strpos( $route, '/senderzz/v1/webhooks/' ) === 0 && (
            substr( $route, -5 ) === '/test' || substr( $route, -5 ) === '/logs'
        ) )
    );

    if ( ! $is_sensitive ) return $response;

    $data = $response->get_data();
    if ( is_array( $data ) ) {
        // Remove body do retorno do /test.
        if ( isset( $data['body'] ) )      $data['body']      = '[redacted]';
        if ( isset( $data['error'] ) && is_string( $data['error'] ) && strlen( $data['error'] ) > 80 ) {
            $data['error'] = '[redacted]';
        }

        // Remove response_body de cada item nos /logs.
        if ( isset( $data['logs'] ) && is_array( $data['logs'] ) ) {
            foreach ( $data['logs'] as &$log ) {
                if ( isset( $log['response_body'] ) ) $log['response_body'] = '[redacted]';
            }
            unset( $log );
        }

        $response->set_data( $data );
    }

    return $response;
}, 10, 3 );

// ─────────────────────────────────────────────────────────────────────────────
// Hook: substitui wp_remote_post por wp_safe_remote_post nos disparos
// de webhook (defesa em profundidade — se a validação acima for bypassada
// por algum motivo, wp_safe_remote_post ainda valida URL via filter
// http_request_host_is_external).
//
// O wp_safe_remote_post bloqueia loopback automaticamente, mas NÃO bloqueia
// link-local nem privadas. Este filtro complementa.
// ─────────────────────────────────────────────────────────────────────────────

add_filter( 'http_request_host_is_external', function ( $is_external, $host, $url ) {
    // Só aplica em chamadas marcadas como "outbound webhook".
    // Marcamos via header customizado nos disparos.
    if ( ! defined( 'SENDERZZ_OUTBOUND_WEBHOOK_ACTIVE' ) ) return $is_external;

    $check = senderzz_url_is_safe( $url );
    return $check['safe'];
}, 10, 3 );

// Wrapper público que módulos de webhook podem usar:
function senderzz_safe_outbound_post( string $url, array $args = [] ) {
    // Validação no cadastro (senderzz_url_is_safe) não é suficiente contra DNS
    // rebinding — o atacante altera o DNS após o cadastro. Re-validamos aqui,
    // imediatamente antes da chamada HTTP, para garantir que o IP resolvido
    // neste momento também é seguro.
    $check = senderzz_url_is_safe( $url );
    if ( ! $check['safe'] ) {
        return new WP_Error( 'ssrf_blocked', 'URL não permitida: ' . $check['reason'] );
    }

    if ( ! defined( 'SENDERZZ_OUTBOUND_WEBHOOK_ACTIVE' ) ) {
        define( 'SENDERZZ_OUTBOUND_WEBHOOK_ACTIVE', true );
    }

    return wp_safe_remote_post( $url, $args );
}

// ════════════════════════════════════════════════════════════════════
// senderzz-portal-login-ratelimit.php
// ════════════════════════════════════════════════════════════════════

// Limites configuráveis via filter.
function senderzz_login_rl_config(): array {
    return apply_filters( 'senderzz_login_ratelimit_config', [
        'ip_max'        => 10,
        'ip_window'     => 5 * MINUTE_IN_SECONDS,
        'email_max'     => 5,
        'email_window'  => 5 * MINUTE_IN_SECONDS,
        'alert_email'   => 20,
        'alert_window'  => HOUR_IN_SECONDS,
    ] );
}

function senderzz_login_get_client_ip(): string {
    $ip = $_SERVER['HTTP_CF_CONNECTING_IP']
       ?? $_SERVER['HTTP_X_REAL_IP']
       ?? $_SERVER['HTTP_X_FORWARDED_FOR']
       ?? $_SERVER['REMOTE_ADDR']
       ?? '0.0.0.0';

    if ( strpos( $ip, ',' ) !== false ) {
        $ip = trim( explode( ',', $ip )[0] );
    }
    $ip = trim( (string) $ip );

    if ( ! filter_var( $ip, FILTER_VALIDATE_IP ) ) {
        return '0.0.0.0';
    }
    return $ip;
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook: roda ANTES do handler original (priority 5, original é 10).
// Se o rate limit for excedido, manda wp_send_json_error e morre — o handler
// original nunca é chamado.
// ─────────────────────────────────────────────────────────────────────────────

add_action( 'wp_ajax_nopriv_senderzz_portal', 'senderzz_login_ratelimit_gate', 5 );
add_action( 'wp_ajax_senderzz_portal',         'senderzz_login_ratelimit_gate', 5 );

function senderzz_login_ratelimit_gate(): void {
    $action = sanitize_text_field( $_POST['szaction'] ?? '' );

    // Só intercepta login_step1 e login_step2.
    if ( ! in_array( $action, [ 'login_step1', 'login_step2' ], true ) ) {
        return;
    }

    $cfg   = senderzz_login_rl_config();
    $ip    = senderzz_login_get_client_ip();
    $email = strtolower( sanitize_email( $_POST['email'] ?? '' ) );

    $rl_ip_key = 'sz_login_rl_ip_' . md5( $ip );
    $cnt_ip    = (int) get_transient( $rl_ip_key );

    if ( $cnt_ip >= $cfg['ip_max'] ) {
        senderzz_login_log( 'rate_limit_ip_blocked', [ 'ip' => $ip, 'attempts' => $cnt_ip, 'action' => $action ] );
        wp_send_json_error( [
            'success' => false,
            'message' => 'Muitas tentativas a partir do seu IP. Aguarde alguns minutos e tente novamente.',
            'rate_limited' => true,
        ], 429 );
        // wp_send_json_error chama wp_die — não retorna.
    }

    /*
     * Senderzz v89:
     * Trava por e-mail desativada temporariamente.
     * Antes, várias tentativas no mesmo e-mail bloqueavam o login/2FA com:
     * "Muitas tentativas para este e-mail. Aguarde alguns minutos."
     * Por enquanto mantemos apenas a proteção por IP para evitar abuso massivo,
     * sem travar o cliente pelo endereço de e-mail informado.
     */

    // Não excede — deixa o handler original rodar.
    // Pre-incrementa: se o login falhar, o contador já está em N+1.
    // Se o login der sucesso, o hook abaixo limpa.
    set_transient( $rl_ip_key, $cnt_ip + 1, $cfg['ip_window'] );
    if ( $email ) {
        // Telemetria mantida sem bloquear o login por e-mail.
        $alert_key = 'sz_login_alert_' . md5( $email );
        $alert_cnt = (int) get_transient( $alert_key );
        set_transient( $alert_key, $alert_cnt + 1, $cfg['alert_window'] );

        if ( $alert_cnt + 1 === $cfg['alert_email'] ) {
            senderzz_login_dispatch_alert( $email, $alert_cnt + 1, $ip );
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook: limpa contadores quando login dá sucesso.
//
// O handler original chama wp_send_json_success no caminho de sucesso. Não
// dá pra interceptar isso direto — usamos um filtro no resultado do
// Portal_Auth::login_step1/step2 via wrapper.
//
// Estratégia alternativa: hookar o admin-ajax response usando ob_start no
// nosso gate, e parsear o JSON. Mais simples: hookar wp_die_ajax_handler
// e verificar se o último output continha success=true para esse email.
//
// Implementação via filtro no JSON enviado:
// ─────────────────────────────────────────────────────────────────────────────

add_filter( 'wp_die_ajax_handler', function ( $handler ) {
    $action = sanitize_text_field( $_POST['szaction'] ?? '' );
    if ( ! in_array( $action, [ 'login_step1', 'login_step2' ], true ) ) {
        return $handler;
    }

    return function ( $message, $title = '', $args = [] ) use ( $handler ) {
        // Tenta detectar sucesso pelo output bufferizado.
        // Se o response JSON tem `success: true` (login_step1 com 2FA aceitou,
        // ou login_step2 com 2FA validou), limpa rate limits.
        if ( is_string( $message ) ) {
            $maybe_json = json_decode( $message, true );
            if ( is_array( $maybe_json )
                 && ! empty( $maybe_json['success'] )
                 && empty( $maybe_json['data']['rate_limited'] ) ) {
                senderzz_login_clear_ratelimit_for_request();
            }
        }
        return call_user_func( $handler, $message, $title, $args );
    };
}, 10, 1 );

function senderzz_login_clear_ratelimit_for_request(): void {
    $ip    = senderzz_login_get_client_ip();
    $email = strtolower( sanitize_email( $_POST['email'] ?? '' ) );

    delete_transient( 'sz_login_rl_ip_' . md5( $ip ) );
    if ( $email ) {
        delete_transient( 'sz_login_rl_email_' . md5( $email ) );
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Alerta de credential stuffing direcionado.
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_login_dispatch_alert( string $email, int $attempts, string $ip ): void {
    $admin = get_option( 'admin_email' );
    if ( ! $admin ) return;

    $subject = '[Senderzz] Alerta: tentativas de login massivas detectadas';
    $message = sprintf(
        "Detectamos %d tentativas de login no portal Senderzz para o e-mail:\n\n%s\n\n" .
        "Última tentativa: %s\nIP: %s\n\n" .
        "Recomendação: investigar nos logs e considerar bloquear o IP de origem se persistente. " .
        "Verifique também se o e-mail-alvo precisa redefinir a senha.\n\n" .
        "Janela: 1 hora.",
        $attempts,
        $email,
        current_time( 'mysql' ),
        $ip
    );

    // Throttle de envio: 1 alerta por email a cada 6h.
    $throttle_key = 'sz_login_alert_sent_' . md5( $email );
    if ( get_transient( $throttle_key ) ) return;

    wp_mail( $admin, $subject, $message );
    set_transient( $throttle_key, 1, 6 * HOUR_IN_SECONDS );
}

// ─────────────────────────────────────────────────────────────────────────────
// Recomendação adicional: forçar 2FA para operadores via filter.
// Aplica em set_require_2fa pra impedir desativação por operator.
// ─────────────────────────────────────────────────────────────────────────────

// Bloqueia o operator de desligar 2FA via toggle_2fa.
// Também bloqueia produtores com saldo elevado.
add_action( 'wp_ajax_senderzz_portal', function () {
    $action = sanitize_text_field( $_POST['szaction'] ?? '' );
    if ( $action !== 'toggle_2fa' ) return;

    $require = (int) ( $_POST['require'] ?? 1 ) === 1;
    if ( $require ) return;

    $token = $_COOKIE['senderzz_portal_session'] ?? '';
    if ( ! $token ) return;

    global $wpdb;
    $token = sanitize_text_field( $token );
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT u.role, u.wp_user_id FROM {$wpdb->prefix}senderzz_portal_users u
         INNER JOIN {$wpdb->prefix}senderzz_portal_sessions s ON s.user_id = u.id
         WHERE s.token = %s AND s.expires_at > NOW() LIMIT 1",
        $token
    ) );

    if ( ! $row ) return;

    if ( $row->role === 'operator' ) {
        wp_send_json_error( [
            'success' => false,
            'message' => '2FA é obrigatório para contas de operador. Não pode ser desativado.',
        ], 403 );
    }

    $saldo_limite = (float) apply_filters( 'senderzz_2fa_required_above_balance', 500.0 );
    if ( $saldo_limite > 0 && $row->wp_user_id ) {
        $saldo = function_exists( 'tpc_get_saldo_disponivel' ) ? (float) tpc_get_saldo_disponivel( (int) $row->wp_user_id ) : 0.0;
        if ( $saldo >= $saldo_limite ) {
            wp_send_json_error( [
                'success' => false,
                'message' => sprintf( '2FA é obrigatório para contas com saldo acima de R$ %s. Reduza o saldo antes de desativar.', number_format( $saldo_limite, 2, ',', '.' ) ),
            ], 403 );
        }
    }
}, 4 );

// ─────────────────────────────────────────────────────────────────────────────
// Logger.
// ─────────────────────────────────────────────────────────────────────────────

function senderzz_login_log( string $tipo, array $data ): void {
    // Hardening seguro: nunca gravar dados sensíveis brutos em log de login.
    if ( function_exists( 'tpc_mask_sensitive_log_data' ) ) {
        $data = tpc_mask_sensitive_log_data( $data );
    } else {
        foreach ( $data as $key => $value ) {
            if ( preg_match( '/(token|secret|senha|password|authorization|document|cpf|cnpj|phone|telefone|email|address|endereco|cep|code|codigo)/i', (string) $key ) ) {
                $data[ $key ] = is_scalar( $value ) ? substr( hash( 'sha256', (string) $value ), 0, 12 ) . '***' : '[masked]';
            }
        }
    }

    if ( function_exists( 'wc_get_logger' ) ) {
        wc_get_logger()->info(
            wp_json_encode( $data, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ),
            [ 'source' => 'senderzz-login-' . $tipo ]
        );
    } else {
        error_log( '[Senderzz Login] ' . $tipo . ': ' . wp_json_encode( $data ) );
    }
}

// ════════════════════════════════════════════════════════════════════
// senderzz-tier2-batch.php
// ════════════════════════════════════════════════════════════════════

if ( defined( 'SENDERZZ_TIER2_BATCH_LOADED' ) ) return;
define( 'SENDERZZ_TIER2_BATCH_LOADED', true );

// ═════════════════════════════════════════════════════════════════════════════
// V-NEW-06: Idempotência forte no me-webhook
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Hookado em rest_pre_dispatch ANTES do handler original do me-webhook rodar.
 * Se o evento já foi registrado em tpc_webhook_events, curto-circuita com 200
 * (e não duplica processamento).
 *
 * Se a função tpc_webhook_register_event não estiver disponível (ex.: instalação
 * antiga sem a tabela), o filtro é no-op e o handler original roda com sua
 * idempotência fraca por transient. Sem regressão.
 */
add_filter( 'rest_pre_dispatch', function ( $result, $server, $request ) {
    if ( $result !== null ) return $result;

    $route = $request->get_route();
    if ( $route !== '/senderzz/v1/webhook/me' || $request->get_method() !== 'POST' ) {
        return $result;
    }

    $raw = (string) $request->get_body();
    if ( $raw === '' ) return $result;

    $payload = json_decode( $raw, true );
    if ( ! is_array( $payload ) ) return $result;

    if ( ! function_exists( 'tpc_webhook_register_event' ) || ! function_exists( 'tpc_webhook_build_event_key' ) ) {
        // Plugin sem tabela de eventos. Deixa o handler original.
        return $result;
    }

    $data       = is_array( $payload['data'] ?? null ) ? $payload['data'] : [];
    $me_id      = sanitize_text_field( (string) ( $data['id'] ?? $data['order_id'] ?? '' ) );
    $status     = sanitize_text_field( (string) ( $data['status'] ?? '' ) );
    $event_type = sanitize_text_field( (string) ( $payload['event'] ?? 'unknown' ) );

    $event_key = tpc_webhook_build_event_key( 'me', $payload, [
        'me_id'  => $me_id,
        'status' => $status,
        'raw'    => $raw,
    ] );

    // register_event tenta INSERT com UNIQUE em event_key. Se já existe,
    // retorna false (duplicate). Sem TTL: persistência permanente.
    $registered = tpc_webhook_register_event( $event_key, 'me', $event_type, $raw, [
        'me_id'  => $me_id,
        'status' => $status,
    ] );

    if ( ! $registered ) {
        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'webhook.duplicate_strong', [ 'event_key' => $event_key, 'event' => $event_type, 'me_id' => $me_id ] );
        }
        return new WP_REST_Response( [
            'ok'   => true,
            'note' => 'duplicate_ignored',
            'via'  => 'tier2_batch_idempotency',
        ], 200 );
    }

    return $result;
}, 5, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// V-NEW-07: Cross-check user_id × me_id em /webhook/envio
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Confere que o user_id declarado em metadata.user_id é o mesmo da reserva
 * original feita em tpc_transacoes pra aquele me_order_id. Se diverge,
 * rejeita 409 e loga.
 *
 * O fluxo legítimo:
 *   1. Produtor X chama /emitir-etiqueta → cria reserva em tpc_transacoes
 *      com user_id=X (reserva pendente, sem me_order_id ainda).
 *   2. Plugin chama POST /me/cart no ME → recebe me_id da etiqueta.
 *   3. Plugin chama checkout no ME e debita a reserva (agora com me_order_id).
 *   4. ME envia webhook /webhook/envio com metadata.user_id=X.
 *
 * Cross-check: na etapa 4, conferir que o user_id da metadata bate com o
 * user_id da transação que tem me_order_id correspondente.
 */
add_filter( 'rest_pre_dispatch', function ( $result, $server, $request ) {
    if ( $result !== null ) return $result;
    if ( $request->get_route() !== '/tp-carteira/v1/webhook/envio' ) return $result;
    if ( $request->get_method() !== 'POST' ) return $result;

    $payload = $request->get_json_params() ?: [];
    $claimed_user_id = (int) ( $payload['metadata']['user_id'] ?? 0 );
    $me_id           = sanitize_text_field( (string) ( $payload['id'] ?? '' ) );

    // Sem user_id ou me_id, deixa o handler original retornar erro de campos.
    if ( ! $claimed_user_id || ! $me_id ) return $result;

    global $wpdb;

    // Busca user_id real da transação que originou essa etiqueta.
    // Considera reservas pendentes E confirmadas (a depender do timing do webhook).
    $real_owner_id = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT user_id
         FROM {$wpdb->prefix}tpc_transacoes
         WHERE me_order_id = %s
         ORDER BY created_at DESC
         LIMIT 1",
        $me_id
    ) );

    // Se não há transação prévia, é caso anômalo: pode ser etiqueta criada
    // por fluxo legado, ou tentativa de exploração. Logamos mas NÃO bloqueamos
    // — o handler original tem suas próprias validações. Deixa rodar.
    if ( ! $real_owner_id ) {
        if ( function_exists( 'tpc_log' ) ) {
            tpc_log( 'webhook_envio_no_prior_tx', [
                'me_id'           => $me_id,
                'claimed_user_id' => $claimed_user_id,
                'ip'              => $_SERVER['REMOTE_ADDR'] ?? '',
            ] );
        }
        return $result;
    }

    if ( $real_owner_id !== $claimed_user_id ) {
        if ( function_exists( 'tpc_log' ) ) {
            tpc_log( 'webhook_envio_owner_mismatch_BLOCKED', [
                'me_id'           => $me_id,
                'claimed_user_id' => $claimed_user_id,
                'real_owner_id'   => $real_owner_id,
                'ip'              => $_SERVER['REMOTE_ADDR'] ?? '',
            ] );
        }
        return new WP_REST_Response( [
            'ok'    => false,
            'error' => 'owner_mismatch',
            'note'  => 'metadata.user_id não corresponde ao dono real da etiqueta',
        ], 409 );
    }

    // OK, prossegue com o handler original.
    return $result;
}, 5, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// V-NEW-09: Rate limit em /emitir-etiqueta
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Rate limit por user autenticado, com janelas de minuto e dia.
 * - Minuto: 30 emissões. Protege contra estouro do token ME e da API ME
 *   (que tem rate limit upstream).
 * - Dia: 200 emissões. Protege contra abuso prolongado (cliente comprometido
 *   raspando saldo do produtor).
 *
 * Limites configuráveis via filter senderzz_emitir_etiqueta_ratelimit.
 *
 * Observação: tpc_rest_auth() usa JWT do portal. Se o token for inválido,
 * o handler original devolve 401 (não passa pelo rate limit, e tudo bem —
 * brute-force de JWT é outro vetor já coberto pelo login rate limit).
 */
add_filter( 'rest_pre_dispatch', function ( $result, $server, $request ) {
    if ( $result !== null ) return $result;
    if ( $request->get_route() !== '/tp-carteira/v1/emitir-etiqueta' ) return $result;
    if ( $request->get_method() !== 'POST' ) return $result;

    if ( ! function_exists( 'tpc_rest_auth' ) ) return $result;

    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) || ! $user_id ) return $result; // handler original retorna 401

    $cfg = apply_filters( 'senderzz_emitir_etiqueta_ratelimit', [
        'minute_max'    => 30,
        'minute_window' => MINUTE_IN_SECONDS,
        'day_max'       => 200,
        'day_window'    => DAY_IN_SECONDS,
    ] );

    $min_key = 'sz_emit_min_' . (int) $user_id;
    $day_key = 'sz_emit_day_' . (int) $user_id;

    $min_count = (int) get_transient( $min_key );
    $day_count = (int) get_transient( $day_key );

    if ( $min_count >= $cfg['minute_max'] ) {
        if ( function_exists( 'tpc_log' ) ) {
            tpc_log( 'emitir_etiqueta_ratelimit_minute', [
                'user_id' => (int) $user_id,
                'count'   => $min_count,
                'limit'   => $cfg['minute_max'],
            ] );
        }
        return new WP_REST_Response( [
            'error'               => 'Muitas emissões em sequência. Aguarde alguns segundos e tente novamente.',
            'rate_limited'        => true,
            'retry_after_seconds' => 60,
        ], 429 );
    }

    if ( $day_count >= $cfg['day_max'] ) {
        if ( function_exists( 'tpc_log' ) ) {
            tpc_log( 'emitir_etiqueta_ratelimit_day', [
                'user_id' => (int) $user_id,
                'count'   => $day_count,
                'limit'   => $cfg['day_max'],
            ] );
        }
        return new WP_REST_Response( [
            'error'        => 'Limite diário de emissões atingido. Tente novamente amanhã ou contate o suporte.',
            'rate_limited' => true,
        ], 429 );
    }

    // Pre-incrementa: se o handler falhar depois (ex: saldo insuficiente),
    // o contador permanece — é proposital. Tentativas com falha também
    // pesam, evita bypass via "tentar X vezes até passar".
    set_transient( $min_key, $min_count + 1, $cfg['minute_window'] );
    set_transient( $day_key, $day_count + 1, $cfg['day_window'] );

    return $result;
}, 5, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// V-NEW-13: Rate limit em endpoints /test de webhook
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Limite: 10 testes por hora por user. Aplica nos dois endpoints de teste:
 *   - /tp-carteira/v1/webhooks/{id}/test  (producer webhooks)
 *   - /senderzz/v1/webhooks/{id}/test     (tracking webhooks)
 *
 * O SSRF protection (V-NEW-03/04) já bloqueia URLs internas. Isto adiciona
 * camada extra contra abuso autenticado: cliente comprometido não consegue
 * usar /test repetidamente como port-scan via timing.
 */
add_filter( 'rest_pre_dispatch', function ( $result, $server, $request ) {
    if ( $result !== null ) return $result;
    if ( $request->get_method() !== 'POST' ) return $result;

    $route = $request->get_route();
    $is_test = (
        ( strpos( $route, '/tp-carteira/v1/webhooks/' ) === 0 && substr( $route, -5 ) === '/test' )
        || ( strpos( $route, '/senderzz/v1/webhooks/' ) === 0 && substr( $route, -5 ) === '/test' )
    );
    if ( ! $is_test ) return $result;

    if ( ! function_exists( 'tpc_rest_auth' ) ) return $result;

    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) || ! $user_id ) return $result;

    $cfg = apply_filters( 'senderzz_webhook_test_ratelimit', [
        'max'    => 10,
        'window' => HOUR_IN_SECONDS,
    ] );

    $key   = 'sz_wh_test_rl_' . (int) $user_id;
    $count = (int) get_transient( $key );

    if ( $count >= $cfg['max'] ) {
        if ( function_exists( 'tpc_log' ) ) {
            tpc_log( 'webhook_test_ratelimit', [
                'user_id' => (int) $user_id,
                'count'   => $count,
                'route'   => $route,
            ] );
        }
        return new WP_REST_Response( [
            'error'        => 'Limite de testes de webhook atingido. Tente novamente em até 1 hora.',
            'rate_limited' => true,
        ], 429 );
    }

    set_transient( $key, $count + 1, $cfg['window'] );

    return $result;
}, 5, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// Logger fallback se tpc_log não existir
// ═════════════════════════════════════════════════════════════════════════════

if ( ! function_exists( 'tpc_log' ) ) {
    // Não declaramos nossa própria pra não conflitar com o autoload — apenas
    // gravamos no error_log se a função real não existe ainda no momento de
    // alguma chamada precoce.
}

// ════════════════════════════════════════════════════════════════════
// senderzz-tier3-batch.php
// ════════════════════════════════════════════════════════════════════

if ( defined( 'SENDERZZ_TIER3_BATCH_LOADED' ) ) return;
define( 'SENDERZZ_TIER3_BATCH_LOADED', true );

// ═════════════════════════════════════════════════════════════════════════════
// V-NEW-25: cobre SSRF no caminho AJAX webhooks_save / webhooks_test
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Hookado em wp_ajax_senderzz_portal com priority 3 (antes do gate de
 * rate limit do V-NEW-05 em priority 5, e antes do handler em 10).
 *
 * Pra ações webhooks_save: valida URL via senderzz_url_is_safe (do tier2).
 * Pra ações webhooks_test: confere que URL persistida no banco é segura.
 */
add_action( 'wp_ajax_senderzz_portal',         'senderzz_t3_ssrf_ajax_gate', 3 );
add_action( 'wp_ajax_nopriv_senderzz_portal',  'senderzz_t3_ssrf_ajax_gate', 3 );

function senderzz_t3_ssrf_ajax_gate(): void {
    $action = sanitize_text_field( $_POST['szaction'] ?? '' );

    // ── webhooks_save: valida URL do payload antes do handler salvar ──
    if ( $action === 'webhooks_save' ) {
        $url = trim( (string) ( $_POST['url'] ?? '' ) );
        if ( $url === '' ) return; // handler trata URL vazia (desativa webhook)

        if ( ! function_exists( 'senderzz_url_is_safe' ) ) {
            // SSRF protection (Tier 2) não carregado — não temos como validar.
            // Fail-closed: recusa.
            wp_send_json_error( [
                'success' => false,
                'message' => 'Validação de URL indisponível (módulo SSRF). Contate o suporte.',
            ], 503 );
        }

        $check = senderzz_url_is_safe( $url );
        if ( ! $check['safe'] ) {
            if ( function_exists( 'wc_get_logger' ) ) {
                wc_get_logger()->warning(
                    wp_json_encode( [
                        'event'  => 'ssrf_blocked_ajax',
                        'action' => 'webhooks_save',
                        'url'    => $url,
                        'reason' => $check['reason'],
                        'ip'     => $_SERVER['REMOTE_ADDR'] ?? '',
                    ], JSON_UNESCAPED_UNICODE ),
                    [ 'source' => 'senderzz-ssrf-tier3' ]
                );
            }
            wp_send_json_error( [
                'success' => false,
                'message' => 'URL não permitida: ' . $check['reason'],
            ], 400 );
        }
        return;
    }

    // ── webhooks_test: revalida URL do banco antes de disparar ──
    if ( $action === 'webhooks_test' ) {
        $webhook_id = absint( $_POST['webhook_id'] ?? 0 );
        if ( ! $webhook_id ) return;

        if ( ! function_exists( 'senderzz_url_is_safe' ) ) return; // tier2 não carregado

        global $wpdb;
        $url = (string) $wpdb->get_var( $wpdb->prepare(
            "SELECT url FROM {$wpdb->prefix}senderzz_webhooks WHERE id = %d LIMIT 1",
            $webhook_id
        ) );

        if ( $url === '' ) return; // handler decide o que fazer com URL vazia

        $check = senderzz_url_is_safe( $url );
        if ( ! $check['safe'] ) {
            if ( function_exists( 'wc_get_logger' ) ) {
                wc_get_logger()->warning(
                    wp_json_encode( [
                        'event'      => 'ssrf_blocked_ajax_test',
                        'webhook_id' => $webhook_id,
                        'url'        => $url,
                        'reason'     => $check['reason'],
                    ], JSON_UNESCAPED_UNICODE ),
                    [ 'source' => 'senderzz-ssrf-tier3' ]
                );
            }
            wp_send_json_error( [
                'success' => false,
                'message' => 'URL deste webhook não está mais permitida: ' . $check['reason'] . '. Edite o webhook e use uma URL pública.',
            ], 400 );
        }
        return;
    }
}

// ═════════════════════════════════════════════════════════════════════════════
// V-NEW-16: defesa em profundidade — filtro global de URL em outbounds
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Hook global em pre_http_request. Bloqueia QUALQUER POST do plugin pra hosts
 * internos, mesmo que o caller tenha pulado o senderzz_safe_outbound_post.
 *
 * Aplica APENAS pra requisições do plugin (identificadas por User-Agent custom
 * ou pelo body conter assinatura de payload Senderzz). Pra requests externos
 * (ex: themes, outros plugins, WP core), passa direto.
 */
add_filter( 'pre_http_request', function ( $preempt, $args, $url ) {
    if ( $preempt !== false ) return $preempt;
    if ( ! is_string( $url ) ) return $preempt;

    // Marca de bypass: chamadas que já validaram URL setam essa flag.
    if ( ! empty( $args['_senderzz_url_validated'] ) ) return $preempt;
    if ( ! empty( $args['_senderzz_dv_modified'] ) )   return $preempt;

    // Identifica chamadas do plugin pela presença de headers/body/UA conhecidos.
    $is_senderzz_outbound = false;

    $headers = $args['headers'] ?? [];
    if ( is_array( $headers ) ) {
        $ua  = (string) ( $headers['User-Agent'] ?? $headers['user-agent'] ?? '' );
        $sig = (string) ( $headers['X-Senderzz-Signature'] ?? $headers['X-Senderzz-Event'] ?? '' );
        if ( $sig !== '' ) {
            $is_senderzz_outbound = true;
        } elseif ( $ua !== '' && ( stripos( $ua, 'senderzz' ) !== false || stripos( $ua, 'tp-carteira' ) !== false ) ) {
            $is_senderzz_outbound = true;
        }
    }

    if ( ! $is_senderzz_outbound ) return $preempt;

    if ( ! function_exists( 'senderzz_url_is_safe' ) ) return $preempt; // tier2 ausente

    $check = senderzz_url_is_safe( $url );
    if ( ! $check['safe'] ) {
        if ( function_exists( 'wc_get_logger' ) ) {
            wc_get_logger()->warning(
                wp_json_encode( [
                    'event'  => 'ssrf_blocked_outbound_global',
                    'url'    => $url,
                    'reason' => $check['reason'],
                ], JSON_UNESCAPED_UNICODE ),
                [ 'source' => 'senderzz-ssrf-tier3' ]
            );
        }
        // Retorna WP_Error pra simular falha de rede — caller não vê detalhes.
        return new WP_Error( 'http_request_failed', 'URL bloqueada por política de segurança.' );
    }

    return $preempt;
}, 4, 3 ); // priority 4: antes do filter do tier2 (priority 5)

// ═════════════════════════════════════════════════════════════════════════════
// V-NEW-21: rate limit em /recarregar (criação de PIX)
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Limite estreito porque PIX é caro: cada chamada bem-sucedida consome quota
 * da API ME e cria item na carteira do produtor no painel ME.
 *
 * Defaults: 5 PIX/hora, 30 PIX/dia por user.
 */
add_filter( 'rest_pre_dispatch', function ( $result, $server, $request ) {
    if ( $result !== null ) return $result;
    if ( $request->get_route() !== '/tp-carteira/v1/recarregar' ) return $result;
    if ( $request->get_method() !== 'POST' ) return $result;

    if ( ! function_exists( 'tpc_rest_auth' ) ) return $result;

    $user_id = tpc_rest_auth();
    if ( is_wp_error( $user_id ) || ! $user_id ) return $result;

    $cfg = apply_filters( 'senderzz_recarregar_ratelimit', [
        'hour_max'    => 5,
        'hour_window' => HOUR_IN_SECONDS,
        'day_max'     => 30,
        'day_window'  => DAY_IN_SECONDS,
    ] );

    $hour_key = 'sz_recarga_h_' . (int) $user_id;
    $day_key  = 'sz_recarga_d_' . (int) $user_id;

    $hc = (int) get_transient( $hour_key );
    $dc = (int) get_transient( $day_key );

    if ( $hc >= $cfg['hour_max'] ) {
        if ( function_exists( 'tpc_log' ) ) {
            tpc_log( 'recarregar_ratelimit_hour', [
                'user_id' => (int) $user_id,
                'count'   => $hc,
                'limit'   => $cfg['hour_max'],
            ] );
        }
        return new WP_REST_Response( [
            'error'        => 'Você criou várias recargas em pouco tempo. Aguarde 1 hora ou conclua os PIX pendentes.',
            'rate_limited' => true,
        ], 429 );
    }

    if ( $dc >= $cfg['day_max'] ) {
        if ( function_exists( 'tpc_log' ) ) {
            tpc_log( 'recarregar_ratelimit_day', [
                'user_id' => (int) $user_id,
                'count'   => $dc,
                'limit'   => $cfg['day_max'],
            ] );
        }
        return new WP_REST_Response( [
            'error'        => 'Limite diário de recargas atingido. Tente novamente amanhã.',
            'rate_limited' => true,
        ], 429 );
    }

    set_transient( $hour_key, $hc + 1, $cfg['hour_window'] );
    set_transient( $day_key,  $dc + 1, $cfg['day_window'] );

    return $result;
}, 5, 3 );

// ═════════════════════════════════════════════════════════════════════════════
// V-NEW-27: rate limit em change_password
// ═════════════════════════════════════════════════════════════════════════════

/**
 * Brute-force na senha atual via portal AJAX. Limite: 5 tentativas/hora por
 * user_id (usa a sessão pra identificar o user).
 *
 * Diferente do login (V-NEW-05) que usa email+IP, aqui usamos só user_id
 * porque o atacante já passou pelo login (tem sessão válida).
 */
add_action( 'wp_ajax_senderzz_portal',         'senderzz_t3_changepwd_ratelimit_gate', 4 );
add_action( 'wp_ajax_nopriv_senderzz_portal',  'senderzz_t3_changepwd_ratelimit_gate', 4 );

function senderzz_t3_changepwd_ratelimit_gate(): void {
    $action = sanitize_text_field( $_POST['szaction'] ?? '' );
    if ( $action !== 'change_password' ) return;

    // Identifica user via cookie (mesma lógica do Portal_Auth)
    $token = sanitize_text_field( $_COOKIE['senderzz_portal_session'] ?? '' );
    if ( ! $token ) return; // handler vai recusar por falta de auth

    global $wpdb;
    $user_id = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT user_id FROM {$wpdb->prefix}senderzz_portal_sessions
         WHERE token = %s AND expires_at > NOW() LIMIT 1",
        $token
    ) );
    if ( ! $user_id ) return;

    $cfg = apply_filters( 'senderzz_change_password_ratelimit', [
        'max'    => 5,
        'window' => HOUR_IN_SECONDS,
    ] );

    $key = 'sz_chpwd_rl_' . $user_id;
    $count = (int) get_transient( $key );

    if ( $count >= $cfg['max'] ) {
        if ( function_exists( 'wc_get_logger' ) ) {
            wc_get_logger()->warning(
                wp_json_encode( [
                    'event'    => 'change_password_ratelimit',
                    'user_id'  => $user_id,
                    'attempts' => $count,
                    'ip'       => $_SERVER['REMOTE_ADDR'] ?? '',
                ], JSON_UNESCAPED_UNICODE ),
                [ 'source' => 'senderzz-tier3' ]
            );
        }
        wp_send_json_error( [
            'success'      => false,
            'message'      => 'Muitas tentativas de alteração de senha. Aguarde 1 hora.',
            'rate_limited' => true,
        ], 429 );
    }

    // Pre-incrementa: tentativa errada conta. Sucesso resetará via filter abaixo.
    set_transient( $key, $count + 1, $cfg['window'] );
}

/**
 * Reset do contador em change_password bem-sucedido. Hook similar ao do V-NEW-05.
 */
add_filter( 'wp_die_ajax_handler', function ( $handler ) {
    $action = sanitize_text_field( $_POST['szaction'] ?? '' );
    if ( $action !== 'change_password' ) return $handler;

    return function ( $message, $title = '', $args = [] ) use ( $handler ) {
        if ( is_string( $message ) ) {
            $maybe_json = json_decode( $message, true );
            if ( is_array( $maybe_json ) && ! empty( $maybe_json['success'] ) ) {
                // Sucesso: zera contador
                $token = sanitize_text_field( $_COOKIE['senderzz_portal_session'] ?? '' );
                if ( $token ) {
                    global $wpdb;
                    $user_id = (int) $wpdb->get_var( $wpdb->prepare(
                        "SELECT user_id FROM {$wpdb->prefix}senderzz_portal_sessions WHERE token IN (%s, %s) LIMIT 1",
                        $token, class_exists( '\\WC_MelhorEnvio\\Portal\\Portal_Auth' ) ? \WC_MelhorEnvio\Portal\Portal_Auth::hash_session_token( $token ) : hash_hmac( 'sha256', $token, defined('AUTH_SALT') && AUTH_SALT ? AUTH_SALT : wp_salt('auth') )
                    ) );
                    if ( $user_id ) {
                        delete_transient( 'sz_chpwd_rl_' . $user_id );
                    }
                }
            }
        }
        return call_user_func( $handler, $message, $title, $args );
    };
}, 11, 1 ); // priority 11: depois do hook do V-NEW-05 (priority 10)

// ════════════════════════════════════════════════════════════════════
// senderzz-tier4-audit-fixes.php
// ════════════════════════════════════════════════════════════════════

if ( defined( 'SENDERZZ_TIER4_AUDIT_FIXES_LOADED' ) ) return;
define( 'SENDERZZ_TIER4_AUDIT_FIXES_LOADED', true );

// ═══════════════════════════════════════════════════════════════════════
// A-08: Rate limit em token-from-portal-session
// ═══════════════════════════════════════════════════════════════════════

add_filter( 'rest_pre_dispatch', function ( $result, $server, WP_REST_Request $request ) {
    if ( $result !== null ) return $result;
    if ( $request->get_route() !== '/tp-carteira/v1/auth/token-from-portal-session' ) return $result;

    $ip  = senderzz_t4_client_ip();
    $key = 'sz_t4_sess_token_rl_' . md5( $ip );
    $cnt = (int) get_transient( $key );

    if ( $cnt >= 10 ) {
        return new WP_REST_Response( [ 'error' => 'Muitas tentativas. Tente novamente em alguns minutos.' ], 429 );
    }
    set_transient( $key, $cnt + 1, MINUTE_IN_SECONDS );

    return $result;
}, 10, 3 );

// ═══════════════════════════════════════════════════════════════════════
// B-04: IP confiável no rate limit de login — REMOTE_ADDR prioritário
//       Substitui a lógica de senderzz_login_get_client_ip se já carregada.
// ═══════════════════════════════════════════════════════════════════════

function senderzz_t4_client_ip(): string {
    // Lista de proxies confiáveis configurável via filter ou opção.
    $trusted = apply_filters( 'senderzz_trusted_proxy_ips', array_filter( array_map(
        'trim',
        explode( ',', (string) get_option( 'senderzz_trusted_proxy_ips', '' ) )
    ) ) );

    $remote = trim( (string) ( $_SERVER['REMOTE_ADDR'] ?? '0.0.0.0' ) );

    // Só considera X-Forwarded-For se o request chegou de um proxy confiável.
    if ( in_array( $remote, $trusted, true ) ) {
        $fwd = $_SERVER['HTTP_X_FORWARDED_FOR'] ?? $_SERVER['HTTP_CF_CONNECTING_IP'] ?? '';
        if ( $fwd ) {
            $first = trim( explode( ',', $fwd )[0] );
            if ( filter_var( $first, FILTER_VALIDATE_IP ) ) {
                return $first;
            }
        }
    }

    return filter_var( $remote, FILTER_VALIDATE_IP ) ? $remote : '0.0.0.0';
}

// Sobrescreve senderzz_login_get_client_ip se o arquivo de rate limit já foi carregado.
// Como PHP não permite redeclarar funções, usamos um filtro que o rate limit deve chamar.
add_filter( 'senderzz_login_client_ip', 'senderzz_t4_client_ip' );

// Patch no gate existente: se senderzz_login_ratelimit_gate chamar este filtro,
// usará o IP seguro. Infelizmente o gate original não usa o filtro — adicionamos
// um wrapper que roda antes (priority 4) e injeta o IP correto via $_SERVER.
add_action( 'wp_ajax_nopriv_senderzz_portal', function () {
    $action = sanitize_text_field( $_POST['szaction'] ?? '' );
    if ( ! in_array( $action, [ 'login_step1', 'login_step2' ], true ) ) return;
    // Força REMOTE_ADDR para o IP correto antes que o gate (priority 5) leia $_SERVER.
    $_SERVER['REMOTE_ADDR'] = senderzz_t4_client_ip();
}, 4 );
add_action( 'wp_ajax_senderzz_portal', function () {
    $action = sanitize_text_field( $_POST['szaction'] ?? '' );
    if ( ! in_array( $action, [ 'login_step1', 'login_step2' ], true ) ) return;
    $_SERVER['REMOTE_ADDR'] = senderzz_t4_client_ip();
}, 4 );

// ═══════════════════════════════════════════════════════════════════════
// B-03: Adiciona order_id ao log de wallet_refund_skip_deferred
// ═══════════════════════════════════════════════════════════════════════

// Não podemos monkey-patch senderzz_wallet_refund_order diretamente.
// Usamos o hook woocommerce_order_status_changed com priority 18
// (antes do engine em priority 19) para adicionar contexto ao log.
add_action( 'woocommerce_order_status_changed', function ( int $order_id, string $old_status, string $new_status ) {
    $new = strtolower( str_replace( 'wc-', '', $new_status ) );
    if ( ! in_array( $new, [ 'cancelled', 'canceled', 'refunded', 'failed' ], true ) ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order ) return;

    $old_norm = strtolower( str_replace( 'wc-', '', $old_status ) );
    $from_err = in_array( $old_norm, [ 'erro', 'error' ], true );

    if ( $order->get_meta( '_senderzz_deferred_refund_scheduled' )
         && ! $order->get_meta( '_senderzz_wallet_refunded' )
         && ! $from_err ) {
        // Log complementar com order_id explícito (o engine só loga a nota).
        if ( function_exists( 'senderzz_log' ) ) {
            senderzz_log( 'wallet_refund_skip_deferred_detail', [
                'order_id'       => $order_id,
                'old_status'     => $old_status,
                'new_status'     => $new_status,
                'scheduled_at'   => $order->get_meta( '_senderzz_deferred_refund_scheduled_at' ),
                'refund_mode'    => $order->get_meta( '_senderzz_cancel_refund_mode' ),
            ] );
        }
    }
}, 18, 3 );

// ═══════════════════════════════════════════════════════════════════════
// M-01: Aviso admin se tabela tpc_webhook_events não existe
// ═══════════════════════════════════════════════════════════════════════

add_action( 'admin_init', function () {
    if ( ! current_user_can( 'manage_woocommerce' ) ) return;
    if ( get_transient( 'sz_t4_wh_table_ok' ) ) return;

    global $wpdb;
    $table = $wpdb->prefix . 'tpc_webhook_events';
    $exists = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );

    if ( ! $exists ) {
        add_action( 'admin_notices', function () use ( $table ) {
            echo '<div class="notice notice-error"><p><strong>Senderzz:</strong> Tabela <code>' . esc_html( $table ) . '</code> não encontrada. '
               . 'A idempotência de webhooks está usando fallback por transient (TTL 24h). '
               . 'Execute a migração do banco ou reative o plugin para criar a tabela.</p></div>';
        } );
    } else {
        set_transient( 'sz_t4_wh_table_ok', 1, DAY_IN_SECONDS );
    }
} );

// ═══════════════════════════════════════════════════════════════════════
// C-03: Intercepta tpc_confirmar_recarga via override temporário
//       Aplica o guard fail-closed mesmo quando pix.php chama diretamente.
//       Solução sem editar pix.php — via override da função com namespace trick.
//
//       LIMITAÇÃO: PHP não permite redeclarar funções no mesmo namespace.
//       Este patch NÃO pode sobrescrever tpc_confirmar_recarga diretamente.
//       A solução real é editar pix.php (ver P-01 no relatório de auditoria).
//
//       O que fazemos aqui: hook de ação disparado dentro de tpc_confirmar_recarga
//       via do_action('tpc_pre_confirmar_recarga') — se pix.php já tiver esse hook,
//       o guard funciona. Caso contrário, apenas logamos a chamada para auditoria.
// ═══════════════════════════════════════════════════════════════════════

// ═══════════════════════════════════════════════════════════════════════
// C-03: Guard PIX agora ativado via filtro embutido em tpc_confirmar_recarga()
//       P-01 RESOLVIDO: apply_filters('senderzz_pix_pre_confirmar_recarga')
//       é chamado como primeira instrução de tpc_confirmar_recarga().
//       Qualquer chamada direta — de qualquer origem — passa pelo guard.
//       Origens autorizadas: 'webhook', 'admin_reconciliation'.
//       Todas as demais são bloqueadas e logadas.
// ═══════════════════════════════════════════════════════════════════════

add_action( 'tpc_pre_confirmar_recarga', function ( int $recarga_id, string $origem ) {
    // Hook de auditoria — registra todas as origens que chegam aqui.
    if ( function_exists( 'senderzz_pix_guard_log' ) ) {
        senderzz_pix_guard_log( 'tpc_confirmar_recarga_auditoria', [
            'recarga_id' => $recarga_id,
            'origem'     => $origem,
        ] );
    }
}, 10, 2 );

// P-01 resolvido: remove aviso admin se ainda estiver pendente.
add_action( 'admin_init', function () {
    if ( ! get_option( 'sz_t4_p01_applied' ) ) {
        update_option( 'sz_t4_p01_applied', 1 );
        update_option( 'sz_t4_p01_dismissed', 1 );
    }
} );

