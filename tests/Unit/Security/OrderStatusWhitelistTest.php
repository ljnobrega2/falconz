<?php
/**
 * OrderStatusWhitelistTest — Testa CRIT-02: whitelist de transições de status para clientes.
 *
 * Garante que clientes não possam mover pedidos em estados avançados (aprovado/approved)
 * para cancelamento — somente admins têm essa permissão.
 *
 * Referência: CHANGELOG-SECURITY.md CRIT-02.
 */

declare(strict_types=1);

namespace WC_MelhorEnvio\Tests\Unit\Security;

use PHPUnit\Framework\TestCase;

class OrderStatusWhitelistTest extends TestCase
{
    public function test_crit02_does_not_allow_aprovado_to_emcancelamento_for_client(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/includes/senderzz-rest.php' );
        // CRIT-02: 'aprovado' não deve estar no mapa de transições cliente
        $this->assertStringNotContainsString("'aprovado'       => [ 'emcancelamento' ]", $content,
            'CRIT-02 regressão: cliente pode mover aprovado→emcancelamento');
        $this->assertStringNotContainsString("'approved'       => [ 'emcancelamento' ]", $content,
            'CRIT-02 regressão: cliente pode mover approved→emcancelamento');
    }

    public function test_crit02_allows_pending_to_emcancelamento(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/includes/senderzz-rest.php' );
        $this->assertStringContainsString("'pending'", $content, 'pending deve estar no mapa de transições');
        $this->assertStringContainsString("'on-hold'", $content, 'on-hold deve estar no mapa de transições');
    }
}
