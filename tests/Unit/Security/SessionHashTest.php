<?php
/**
 * SessionHashTest — Testa que os lookups de sessão usam IN(%s,%s) para suportar
 * tokens raw e tokens HMAC (migração de sessões antigas).
 *
 * V-SEC-01: Tracking_Webhook deve aceitar token raw OU hash HMAC.
 * V-SEC-02: tpc/rest-api deve aceitar token raw OU hash HMAC.
 */

declare(strict_types=1);

namespace WC_MelhorEnvio\Tests\Unit\Security;

use PHPUnit\Framework\TestCase;

class SessionHashTest extends TestCase
{
    public function test_tracking_webhook_uses_in_clause_for_session_lookup(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/src/Webhook/Tracking_Webhook.php' );
        $this->assertStringContainsString('IN (%s,%s)', $content,
            'V-SEC-01: Tracking_Webhook deve usar IN(%s,%s) para suportar tokens raw+HMAC');
        $this->assertStringNotContainsString('WHERE s.token = %s', $content,
            'V-SEC-01 regressão: comparação raw-only detectada em Tracking_Webhook');
    }

    public function test_tpc_rest_api_uses_in_clause_for_session_lookup(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/includes/tpc/rest-api.php' );
        $this->assertStringContainsString('IN (%s,%s)', $content,
            'V-SEC-02: tpc/rest-api deve usar IN(%s,%s) para auth/token-from-portal-session');
    }
}
