<?php
/**
 * MotoboySecurityTest — Testa controles de segurança do módulo Motoboy.
 *
 * V-SEC-03: Endpoint de reagendamento deve validar posse do pedido via order key
 *           (enviada no e-mail de confirmação WC), impedindo que qualquer usuário
 *           reagende pedidos de terceiros apenas com o ID numérico.
 */

declare(strict_types=1);

namespace WC_MelhorEnvio\Tests\Unit\Security;

use PHPUnit\Framework\TestCase;

class MotoboySecurityTest extends TestCase
{
    public function test_reagendar_requires_order_key(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/includes/motoboy/rest-api.php' );
        $this->assertStringContainsString('V-SEC-03', $content,
            'V-SEC-03: reagendar deve validar posse via order key');
        $this->assertStringContainsString('hash_equals( $order->get_order_key()', $content,
            'V-SEC-03: hash_equals com order key deve estar presente');
    }

    public function test_tracking_url_helper_includes_key(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/includes/motoboy/router.php' );
        $this->assertStringContainsString('sz_motoboy_tracking_url', $content,
            'V-SEC-03: helper sz_motoboy_tracking_url() deve existir');
        $this->assertStringContainsString('get_order_key', $content,
            'V-SEC-03: helper deve incluir order key na URL');
    }
}
