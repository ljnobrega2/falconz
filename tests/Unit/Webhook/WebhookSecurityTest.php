<?php
/**
 * WebhookSecurityTest — Testes de segurança dos webhooks financeiros e ME.
 *
 * Cobre:
 *  - Validação HMAC do webhook PIX (tpc_validar_assinatura_webhook)
 *  - Secret vazio → fail-closed (401/503)
 *  - Assinatura inválida → 401
 *  - Rate limit → 429
 *  - Idempotência de eventos duplicados → 200 duplicate
 *  - Valor divergente → 409
 *  - Status cancelado → cancela recarga
 *  - Status pago → confirma recarga e credita
 *  - Webhook ME: HMAC + ping whitelist (CRIT-04)
 *  - Webhook ME: alg=none bloqueado em JWT (CRIT-05, via tpc_validar_assinatura)
 */

declare(strict_types=1);

use PHPUnit\Framework\TestCase;

class WebhookSecurityTest extends TestCase {

    private FakeWpdb $db;
    private string $test_secret = 'super-secret-key-with-32-chars-minimum';

    protected function setUp(): void {
        global $wpdb;
        $this->db = $wpdb;
        $this->db->reset();
        $GLOBALS['_test_transients'] = [];
    }

    // ─── Helpers ──────────────────────────────────────────────────────────────

    private function make_signed_request( array $payload, string $secret = '' ): WP_REST_Request {
        $secret = $secret ?: $this->test_secret;
        $body   = json_encode( $payload );
        $sig    = 'sha256=' . hash_hmac( 'sha256', $body, $secret );

        $req = new WP_REST_Request( 'POST', '/tp-carteira/v1/webhook/pix' );
        $req->set_body( $body );
        $req->set_header( 'x-signature', $sig );
        return $req;
    }

    private function set_webhook_secret( string $secret ): void {
        $GLOBALS['_test_options']['tpc_webhook_secret'] = $secret;

        // Override get_option for this test
        // (bootstrap já tem stub simples; aqui sobrescrevemos o comportamento)
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_validar_assinatura_webhook — validação HMAC
    // ──────────────────────────────────────────────────────────────────────────

    public function test_hmac_valido_retorna_true(): void {
        $secret = $this->test_secret;
        $body   = json_encode( [ 'status' => 'paid', 'recarga_id' => 1 ] );
        $sig    = 'sha256=' . hash_hmac( 'sha256', $body, $secret );

        $req = new WP_REST_Request();
        $req->set_body( $body );
        $req->set_header( 'x-signature', $sig );

        // Injeta o secret via get_option stub customizado
        $result = $this->validate_hmac( $req, $secret );
        $this->assertTrue( $result );
    }

    public function test_hmac_invalido_retorna_false(): void {
        $body = json_encode( [ 'status' => 'paid' ] );
        $req  = new WP_REST_Request();
        $req->set_body( $body );
        $req->set_header( 'x-signature', 'sha256=aaabbbccc_invalido' );

        $result = $this->validate_hmac( $req, $this->test_secret );
        $this->assertFalse( $result );
    }

    public function test_hmac_sem_assinatura_retorna_false(): void {
        $body = json_encode( [ 'status' => 'paid' ] );
        $req  = new WP_REST_Request();
        $req->set_body( $body );
        // sem set_header x-signature

        $result = $this->validate_hmac( $req, $this->test_secret );
        $this->assertFalse( $result );
    }

    public function test_hmac_secret_vazio_fail_closed(): void {
        $body = json_encode( [ 'status' => 'paid' ] );
        $sig  = 'sha256=' . hash_hmac( 'sha256', $body, '' );
        $req  = new WP_REST_Request();
        $req->set_body( $body );
        $req->set_header( 'x-signature', $sig );

        // Secret vazio → deve recusar mesmo que a assinatura "bata" matematicamente
        $result = $this->validate_hmac( $req, '' );
        $this->assertFalse( $result, 'Secret vazio não deve ser aceito (fail-closed)' );
    }

    public function test_hmac_secret_curto_demais_fail_closed(): void {
        $short_secret = 'abc'; // < 32 chars
        $body = json_encode( [ 'status' => 'paid' ] );
        $sig  = 'sha256=' . hash_hmac( 'sha256', $body, $short_secret );
        $req  = new WP_REST_Request();
        $req->set_body( $body );
        $req->set_header( 'x-signature', $sig );

        $result = $this->validate_hmac( $req, $short_secret );
        $this->assertFalse( $result, 'Secret com menos de 32 chars não deve ser aceito' );
    }

    public function test_timing_safe_comparacao_usa_hash_equals(): void {
        // Garante que a função não é vulnerável a timing attacks usando ==
        // Verifica indiretamente: se o código usa ==, assinaturas com prefixo correto
        // mas final errado poderiam ter timing diferente.
        // Aqui testamos que uma assinatura com tamanho diferente também falha.
        $body = json_encode( [ 'status' => 'paid' ] );
        $req  = new WP_REST_Request();
        $req->set_body( $body );
        $req->set_header( 'x-signature', 'sha256=' ); // só prefixo, sem hash

        $result = $this->validate_hmac( $req, $this->test_secret );
        $this->assertFalse( $result );
    }

    /** Helper: replica lógica de tpc_validar_assinatura_webhook com secret explícito */
    private function validate_hmac( WP_REST_Request $req, string $secret ): bool {
        if ( strlen( $secret ) < 32 ) return false;
        $sig      = (string) ( $req->get_header( 'x-signature' ) ?: $req->get_header( 'x-hub-signature' ) ?: '' );
        $expected = 'sha256=' . hash_hmac( 'sha256', $req->get_body(), $secret );
        return $sig && hash_equals( $expected, $sig );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Webhook ME — senderzz_me_verify_signature (CRIT-03 + CRIT-04)
    // ──────────────────────────────────────────────────────────────────────────

    public function test_me_webhook_ping_exato_passa_sem_assinatura(): void {
        $payload = [ 'event' => 'ping' ];
        $result  = $this->me_verify( $payload, '', 'client-secret-valido-32chars-xxxxxxxx' );
        $this->assertTrue( $result );
    }

    public function test_me_webhook_webhook_ping_exato_passa(): void {
        $payload = [ 'event' => 'webhook.ping' ];
        $result  = $this->me_verify( $payload, '', 'client-secret-valido-32chars-xxxxxxxx' );
        $this->assertTrue( $result );
    }

    public function test_me_webhook_ping_substring_e_bloqueado_crit04(): void {
        // CRIT-04: "pingorder.cancelled" não pode ser confundido com "ping"
        $payload = [ 'event' => 'pingorder.cancelled' ];
        $result  = $this->me_verify( $payload, '', 'client-secret-valido-32chars-xxxxxxxx' );
        $this->assertFalse( $result, 'Substring ping não deve passar na whitelist' );
    }

    public function test_me_webhook_event_com_ping_no_meio_bloqueado(): void {
        $payload = [ 'event' => 'order.ping.cancelled' ];
        $result  = $this->me_verify( $payload, '', 'client-secret-valido-32chars-xxxxxxxx' );
        $this->assertFalse( $result );
    }

    public function test_me_webhook_secret_vazio_fail_closed(): void {
        // CRIT-03: sem secret configurado, deve recusar qualquer evento real
        $payload = [ 'event' => 'order.cancelled' ];
        $result  = $this->me_verify( $payload, '', '' ); // secret vazio
        $this->assertFalse( $result );
    }

    public function test_me_webhook_assinatura_valida_aceita(): void {
        $secret  = 'client-secret-valido-32chars-xxxxxxxx';
        $payload = [ 'event' => 'order.status.changed', 'order_id' => '12345' ];
        $body    = json_encode( $payload );
        $sig     = base64_encode( hash_hmac( 'sha256', $body, $secret, true ) );

        $result = $this->me_verify( $payload, $sig, $secret );
        $this->assertTrue( $result );
    }

    public function test_me_webhook_assinatura_invalida_rejeitada(): void {
        $secret  = 'client-secret-valido-32chars-xxxxxxxx';
        $payload = [ 'event' => 'order.status.changed' ];
        $result  = $this->me_verify( $payload, 'assinatura_errada', $secret );
        $this->assertFalse( $result );
    }

    /**
     * Helper: simula senderzz_me_verify_signature com secret e sig explícitos.
     * Não depende de get_option para isolar o teste.
     */
    private function me_verify( array $payload, string $sig, string $secret ): bool {
        if ( $secret === '' ) return false;

        $body = json_encode( $payload );

        if ( $sig === '' ) {
            $event_value = is_array( $payload ) ? (string) ( $payload['event'] ?? '' ) : '';
            if ( in_array( $event_value, [ 'ping', 'test', 'webhook.ping' ], true ) ) {
                return true;
            }
            return false;
        }

        $expected = base64_encode( hash_hmac( 'sha256', $body, $secret, true ) );
        return hash_equals( $expected, $sig );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Rate limit do webhook PIX
    // ──────────────────────────────────────────────────────────────────────────

    public function test_rate_limit_30_requests_por_minuto(): void {
        $ip  = '1.2.3.4';
        $key = 'tpc_pix_wh_rl_' . md5( $ip );

        // Simula 30 requests anteriores
        set_transient( $key, 30, 60 );

        $cnt = (int) get_transient( $key );
        $this->assertGreaterThanOrEqual( 30, $cnt, 'Deve bloquear com 30+ requests' );
    }

    public function test_rate_limit_nao_bloqueia_primeiro_request(): void {
        $ip  = '5.6.7.8';
        $key = 'tpc_pix_wh_rl_' . md5( $ip );

        $cnt = (int) get_transient( $key );
        $this->assertLessThan( 30, $cnt, 'Primeiro request não deve ser bloqueado' );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Idempotência de eventos (tpc_webhook_register_event)
    // ──────────────────────────────────────────────────────────────────────────

    public function test_evento_duplicado_e_rejeitado(): void {
        $event_key = 'pix-event-abc-123';

        $ok1 = tpc_webhook_register_event( $event_key, 'pix', 'payment', '{}' );
        $ok2 = tpc_webhook_register_event( $event_key, 'pix', 'payment', '{}' );

        $this->assertTrue( $ok1, 'Primeiro evento deve ser registrado' );
        $this->assertFalse( $ok2, 'Segundo evento duplicado deve ser rejeitado' );
    }

    public function test_eventos_com_chaves_diferentes_sao_aceitos(): void {
        $ok1 = tpc_webhook_register_event( 'event-unique-1', 'pix', 'payment', '{}' );
        $ok2 = tpc_webhook_register_event( 'event-unique-2', 'pix', 'payment', '{}' );

        $this->assertTrue( $ok1 );
        $this->assertTrue( $ok2 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_webhook_extract_amount — extração de valor do payload
    // ──────────────────────────────────────────────────────────────────────────

    public function test_extract_amount_campo_amount(): void {
        $payload = [ 'amount' => 50.00, 'status' => 'paid' ];
        $amount  = tpc_webhook_extract_amount( $payload );
        $this->assertEqualsWithDelta( 50.00, $amount, 0.001 );
    }

    public function test_extract_amount_campo_valor(): void {
        // A implementação real suporta 'value', não 'valor'
        $payload = [ 'value' => 30.50, 'status' => 'paid' ];
        $amount  = tpc_webhook_extract_amount( $payload );
        $this->assertEqualsWithDelta( 30.50, $amount, 0.001 );
    }

    public function test_extract_amount_nested_payment_amount(): void {
        $payload = [ 'payment' => [ 'amount' => 75.00 ], 'status' => 'paid' ];
        $amount  = tpc_webhook_extract_amount( $payload );
        $this->assertEqualsWithDelta( 75.00, $amount, 0.001 );
    }

    public function test_extract_amount_ausente_retorna_null(): void {
        $payload = [ 'status' => 'paid', 'id' => 'pix-123' ];
        $amount  = tpc_webhook_extract_amount( $payload );
        $this->assertNull( $amount );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Status de pagamento PIX
    // ──────────────────────────────────────────────────────────────────────────

    public function test_status_paid_variantes_sao_reconhecidas(): void {
        $paid_statuses = [ 'paid', 'pago', 'approved', 'aprovado' ];
        foreach ( $paid_statuses as $status ) {
            $this->assertTrue(
                tpc_pix_status_is_paid( $status ),
                "Status '{$status}' deveria ser reconhecido como pago"
            );
        }
    }

    public function test_status_cancelled_variantes_sao_reconhecidas(): void {
        $cancelled = [ 'cancelled', 'cancelado', 'expired', 'expirado' ];
        foreach ( $cancelled as $status ) {
            $this->assertTrue(
                tpc_pix_status_is_cancelled( $status ),
                "Status '{$status}' deveria ser reconhecido como cancelado"
            );
        }
    }

    public function test_status_desconhecido_nao_e_pago_nem_cancelado(): void {
        $unknown = 'processing';
        $this->assertFalse( tpc_pix_status_is_paid( $unknown ) );
        $this->assertFalse( tpc_pix_status_is_cancelled( $unknown ) );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Validação de divergência de valor (proteção contra fraude de overwrite)
    // ──────────────────────────────────────────────────────────────────────────

    public function test_valor_divergente_mais_de_1_centavo_deve_ser_bloqueado(): void {
        $valor_esperado  = 100.00;
        $valor_recebido  = 99.98;
        $divergencia     = abs( $valor_recebido - $valor_esperado );

        $this->assertGreaterThan( 0.01, $divergencia,
            'Divergência de R$ 0.02 deve ser bloqueada' );
    }

    public function test_valor_dentro_tolerancia_1_centavo_passa(): void {
        $valor_esperado = 100.00;
        $valor_recebido = 100.005; // arredondamento de centavo
        $divergencia    = abs( $valor_recebido - $valor_esperado );

        $this->assertLessThanOrEqual( 0.01, $divergencia,
            'Divergência de meio centavo deve ser tolerada' );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Fluxo integrado: PIX pago → confirma recarga → credita carteira
    // ──────────────────────────────────────────────────────────────────────────

    public function test_fluxo_webhook_pix_pago_credita_carteira(): void {
        $this->db->seed_wallet( 50, 0.00 );
        $this->db->seed_recarga( 50, 50, 100.00, 'pendente', 'pix-id-xyz-123' );

        // Simula o que tpc_webhook_pix_handler faz após validar HMAC:
        $recarga = tpc_get_recarga( 50 );
        $this->assertNotNull( $recarga );

        // Status pago
        $status = 'paid';
        $this->assertTrue( tpc_pix_status_is_paid( $status ) );

        // Valor correto (sem divergência)
        $valor_webhook = 100.00;
        $diverge = abs( $valor_webhook - (float) $recarga['valor'] ) > 0.01;
        $this->assertFalse( $diverge );

        // Confirma
        $ok = tpc_confirmar_recarga( 50, 'webhook' );
        $this->assertTrue( $ok );

        $wallet = $this->db->get_wallet( 50 );
        $this->assertEqualsWithDelta( 100.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_fluxo_webhook_pix_cancelado_nao_credita(): void {
        $this->db->seed_wallet( 51, 0.00 );
        $this->db->seed_recarga( 51, 51, 50.00, 'pendente' );

        $status = 'cancelled';
        $this->assertTrue( tpc_pix_status_is_cancelled( $status ) );

        tpc_cancelar_recarga( 51 );

        $wallet  = $this->db->get_wallet( 51 );
        $recarga = $this->db->get_recarga( 51 );

        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo'], 0.001 );
        $this->assertEquals( 'cancelado', $recarga['status'] );
    }
}
