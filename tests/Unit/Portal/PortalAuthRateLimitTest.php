<?php
/**
 * PortalAuthRateLimitTest — Verifica que os rate limits do portal estão em valores de produção.
 *
 * Garante que valores temporários de desenvolvimento (ex.: 1000 tentativas)
 * não sejam acidentalmente enviados para produção.
 */

declare(strict_types=1);

namespace WC_MelhorEnvio\Tests\Unit\Portal;

use PHPUnit\Framework\TestCase;

class PortalAuthRateLimitTest extends TestCase
{
    public function test_login_step1_rate_limit_is_production_value(): void
    {
        // Ler o valor de attempts >= X em Portal_Auth.php step1
        $content = file_get_contents( dirname(__DIR__, 3) . '/src/Portal/Portal_Auth.php' );
        $this->assertStringContainsString('$attempts >= 5', $content, 'Login step1 rate limit deve ser >= 5 (produção), não 1000 (dev)');
        $this->assertStringNotContainsString('$attempts >= 1000', $content, 'Rate limit dev (1000) detectado — restaurar para produção');
    }

    public function test_2fa_rate_limit_is_production_value(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/src/Portal/Portal_Auth.php' );
        $this->assertStringContainsString("'attempts'] >= 3", $content, '2FA rate limit deve ser >= 3 (produção)');
        $this->assertStringNotContainsString("'attempts'] >= 1000", $content, '2FA rate limit dev (1000) detectado');
    }

    public function test_login_ratelimit_config_is_production(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/includes/senderzz-security-patches.php' );
        $this->assertStringContainsString("'ip_max'        => 10,", $content, 'ip_max deve ser 10 (produção)');
        $this->assertStringNotContainsString("'ip_max'        => 10000", $content, 'ip_max dev (10000) detectado');
        $this->assertStringContainsString("'email_max'     => 5,", $content, 'email_max deve ser 5 (produção)');
    }
}
