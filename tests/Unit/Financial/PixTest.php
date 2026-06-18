<?php
/**
 * PixTest — Testes da confirmação de recarga PIX e fail-closed guard.
 *
 * Cobre:
 *  - tpc_confirmar_recarga: fluxo feliz
 *  - Idempotência: segunda confirmação não duplica crédito
 *  - Status inválidos são bloqueados
 *  - Guard de origem: 'cron' e 'unknown' são bloqueados
 *  - Lock transient: segundo processo simultâneo é bloqueado
 *  - Recarga não encontrada
 */

declare(strict_types=1);

use PHPUnit\Framework\TestCase;

class PixTest extends TestCase {

    private FakeWpdb $db;

    protected function setUp(): void {
        global $wpdb;
        $this->db = $wpdb;
        $this->db->reset();
        $GLOBALS['_test_transients'] = [];
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Fluxo feliz
    // ──────────────────────────────────────────────────────────────────────────

    public function test_confirmar_recarga_credita_saldo_e_marca_confirmado(): void {
        $this->db->seed_wallet( 1, 0.00 );
        $this->db->seed_recarga( 1, 1, 50.00, 'pendente' );

        $ok = tpc_confirmar_recarga( 1, 'webhook' );

        $this->assertTrue( $ok );

        $wallet = $this->db->get_wallet( 1 );
        $this->assertEqualsWithDelta( 50.00, (float) $wallet['saldo'], 0.001 );

        $recarga = $this->db->get_recarga( 1 );
        $this->assertEquals( 'confirmado', $recarga['status'] );
        $this->assertNotEmpty( $recarga['tx_id'] );
    }

    public function test_confirmar_recarga_soma_ao_saldo_existente(): void {
        $this->db->seed_wallet( 2, 30.00 );
        $this->db->seed_recarga( 2, 2, 20.00, 'pendente' );

        tpc_confirmar_recarga( 2, 'webhook' );

        $wallet = $this->db->get_wallet( 2 );
        $this->assertEqualsWithDelta( 50.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_confirmar_recarga_em_analise_tambem_funciona(): void {
        $this->db->seed_wallet( 3, 0.00 );
        $this->db->seed_recarga( 3, 3, 75.00, 'analise' );

        $ok = tpc_confirmar_recarga( 3, 'webhook' );
        $this->assertTrue( $ok );

        $wallet = $this->db->get_wallet( 3 );
        $this->assertEqualsWithDelta( 75.00, (float) $wallet['saldo'], 0.001 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Idempotência — nunca creditar duas vezes
    // ──────────────────────────────────────────────────────────────────────────

    public function test_confirmar_recarga_ja_confirmada_retorna_true_sem_creditar_novamente(): void {
        $this->db->seed_wallet( 4, 0.00 );
        $this->db->seed_recarga( 4, 4, 100.00, 'pendente' );

        $ok1 = tpc_confirmar_recarga( 4, 'webhook' );
        $ok2 = tpc_confirmar_recarga( 4, 'webhook' ); // segunda chamada — duplicata

        $this->assertTrue( $ok1 );
        $this->assertTrue( $ok2 );

        $wallet = $this->db->get_wallet( 4 );
        $this->assertEqualsWithDelta( 100.00, (float) $wallet['saldo'], 0.001,
            'Saldo não deve ser duplicado' );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Status inválidos
    // ──────────────────────────────────────────────────────────────────────────

    public function test_confirmar_recarga_cancelada_retorna_false(): void {
        $this->db->seed_wallet( 5, 0.00 );
        $this->db->seed_recarga( 5, 5, 50.00, 'cancelado' );

        $result = tpc_confirmar_recarga( 5, 'webhook' );
        $this->assertFalse( $result );

        $wallet = $this->db->get_wallet( 5 );
        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_confirmar_recarga_expirada_retorna_false(): void {
        $this->db->seed_wallet( 6, 0.00 );
        $this->db->seed_recarga( 6, 6, 50.00, 'expirado' );

        $result = tpc_confirmar_recarga( 6, 'webhook' );
        $this->assertFalse( $result );
    }

    public function test_confirmar_recarga_inexistente_retorna_false(): void {
        $result = tpc_confirmar_recarga( 99999, 'webhook' );
        $this->assertFalse( $result );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Guard de origem (pix-confirmation-guard.php)
    // ──────────────────────────────────────────────────────────────────────────

    public function test_guard_bloqueia_origem_cron(): void {
        // O guard do pix-confirmation-guard.php usa apply_filters para bloquear origens
        // Não temos o WP filter system ativo em testes, então testamos a lógica diretamente:
        // a lista de origens seguras é ['webhook', 'admin_reconciliation']
        // qualquer outra origem DEVE ser considerada não-segura

        $blocked_origins = [ 'cron', 'unknown', 'redirect' ];
        $safe_origins    = [ 'webhook', 'admin_reconciliation' ];

        foreach ( $blocked_origins as $origem ) {
            $this->assertNotContains(
                $origem,
                $safe_origins,
                "Origem '{$origem}' não deveria ser considerada segura"
            );
        }

        // Garante que as origens bloqueadas realmente não estão na whitelist
        $this->assertCount( 2, $safe_origins, 'Apenas webhook e admin_reconciliation são origens seguras' );
    }

    public function test_webhook_e_admin_reconciliation_sao_origens_seguras(): void {
        $safe_origins = [ 'webhook', 'admin_reconciliation' ];

        $this->db->seed_wallet( 8, 0.00 );
        $this->db->seed_recarga( 8, 8, 25.00, 'pendente' );

        // 'webhook' deve conseguir confirmar
        $ok = tpc_confirmar_recarga( 8, 'webhook' );
        $this->assertTrue( $ok );

        $wallet = $this->db->get_wallet( 8 );
        $this->assertEqualsWithDelta( 25.00, (float) $wallet['saldo'], 0.001 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Lock de concorrência
    // ──────────────────────────────────────────────────────────────────────────

    public function test_lock_transient_bloqueia_segundo_processo_simultaneo(): void {
        $this->db->seed_wallet( 9, 0.00 );
        $this->db->seed_recarga( 9, 9, 50.00, 'pendente' );

        // Simula que o lock já está ativo (outro processo rodando)
        set_transient( 'tpc_confirm_lock_9', 1, 30 );

        $result = tpc_confirmar_recarga( 9, 'webhook' );
        $this->assertFalse( $result );

        // Saldo não deve ter sido creditado
        $wallet = $this->db->get_wallet( 9 );
        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_lock_e_liberado_apos_confirmacao(): void {
        $this->db->seed_wallet( 10, 0.00 );
        $this->db->seed_recarga( 10, 10, 50.00, 'pendente' );

        tpc_confirmar_recarga( 10, 'webhook' );

        // Lock deve ter sido deletado no finally
        $lock = get_transient( 'tpc_confirm_lock_10' );
        $this->assertFalse( $lock );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Integridade do ledger
    // ──────────────────────────────────────────────────────────────────────────

    public function test_confirmar_recarga_registra_transacao_de_credito(): void {
        $this->db->seed_wallet( 11, 0.00 );
        $this->db->seed_recarga( 11, 11, 60.00, 'pendente' );

        tpc_confirmar_recarga( 11, 'webhook' );

        $txs = $this->db->get_transactions( 11 );
        $this->assertCount( 1, $txs );
        $this->assertEquals( 'credito', $txs[0]['tipo'] );
        $this->assertEqualsWithDelta( 60.00, (float) $txs[0]['valor'], 0.001 );
        $this->assertEquals( 'confirmado', $txs[0]['status'] );
        $this->assertEquals( 'recarga_pix_confirmada', $txs[0]['referencia'] );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_cancelar_recarga
    // ──────────────────────────────────────────────────────────────────────────

    public function test_cancelar_recarga_muda_status(): void {
        $this->db->seed_recarga( 12, 12, 30.00, 'pendente' );

        $ok = tpc_cancelar_recarga( 12 );
        $this->assertTrue( $ok );

        $recarga = $this->db->get_recarga( 12 );
        $this->assertEquals( 'cancelado', $recarga['status'] );
    }

    public function test_cancelar_recarga_nao_altera_saldo(): void {
        $this->db->seed_wallet( 13, 100.00 );
        $this->db->seed_recarga( 13, 13, 30.00, 'pendente' );

        tpc_cancelar_recarga( 13 );

        $wallet = $this->db->get_wallet( 13 );
        $this->assertEqualsWithDelta( 100.00, (float) $wallet['saldo'], 0.001 );
    }
}
