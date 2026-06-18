<?php
/**
 * IdempotencyTest — Testa UNIQUE KEY em tpc_transacoes e idempotência no ME webhook.
 *
 * S8: Garante que a migration de tpc_transacoes inclui a UNIQUE KEY uq_user_ref_tipo
 *     para evitar débitos/créditos duplicados em nível de banco de dados.
 *
 * S10: Garante que o ME webhook usa tpc_webhook_register_event() para idempotência
 *      persistente (e não apenas transient volátil como fallback primário).
 */

declare(strict_types=1);

namespace WC_MelhorEnvio\Tests\Unit\Financial;

use PHPUnit\Framework\TestCase;

class IdempotencyTest extends TestCase
{
    public function test_tpc_transacoes_has_unique_key_migration(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/includes/tpc/database.php' );
        $this->assertStringContainsString('uq_user_ref_tipo', $content,
            'S8: UNIQUE KEY uq_user_ref_tipo deve existir na migration de tpc_transacoes');
    }

    public function test_me_webhook_uses_persistent_idempotency(): void
    {
        $content = file_get_contents( dirname(__DIR__, 3) . '/includes/senderzz-me-webhook.php' );
        $this->assertStringContainsString('tpc_webhook_register_event', $content,
            'S10: ME webhook deve usar tpc_webhook_register_event() para idempotência persistente');
        // Transient de 5min deve estar apenas como fallback
        $this->assertStringContainsString('Fallback', $content,
            'S10: Transient deve existir apenas como fallback');
    }
}
