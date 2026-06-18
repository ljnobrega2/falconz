<?php
/**
 * WalletTest — Testes unitários das operações atômicas da carteira.
 *
 * Cobre:
 *  - tpc_creditar / tpc_debitar
 *  - tpc_movimentar (idempotência, race protection, saldo insuficiente)
 *  - tpc_reservar / tpc_liberar_reserva / tpc_debitar_reserva
 *  - Ledger: integridade das transações registradas
 *  - Floating point: arredondamentos corretos
 */

declare(strict_types=1);

use PHPUnit\Framework\TestCase;

class WalletTest extends TestCase {

    private FakeWpdb $db;

    protected function setUp(): void {
        global $wpdb;
        $this->db = $wpdb;
        $this->db->reset();
        // Limpa transients entre testes
        $GLOBALS['_test_transients'] = [];
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_creditar
    // ──────────────────────────────────────────────────────────────────────────

    public function test_creditar_cria_carteira_e_saldo(): void {
        $tx_id = tpc_creditar( 1, 50.00, 'Recarga teste' );

        $this->assertIsInt( $tx_id );
        $this->assertGreaterThan( 0, $tx_id );

        $wallet = $this->db->get_wallet( 1 );
        $this->assertNotNull( $wallet );
        $this->assertEqualsWithDelta( 50.00, (float) $wallet['saldo'], 0.001 );
        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo_reservado'], 0.001 );
    }

    public function test_creditar_acumula_saldo_multiplas_vezes(): void {
        tpc_creditar( 2, 30.00, 'Recarga 1' );
        tpc_creditar( 2, 20.00, 'Recarga 2' );

        $wallet = $this->db->get_wallet( 2 );
        $this->assertEqualsWithDelta( 50.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_creditar_valor_zero_retorna_false(): void {
        $result = tpc_creditar( 1, 0.00, 'Zero' );
        $this->assertFalse( $result );
    }

    public function test_creditar_valor_negativo_retorna_false(): void {
        $result = tpc_creditar( 1, -10.00, 'Negativo' );
        $this->assertFalse( $result );
    }

    public function test_creditar_registra_transacao_no_ledger(): void {
        tpc_creditar( 3, 100.00, 'Crédito ledger' );

        $txs = $this->db->get_transactions( 3 );
        $this->assertCount( 1, $txs );
        $this->assertEquals( 'credito', $txs[0]['tipo'] );
        $this->assertEqualsWithDelta( 100.00, (float) $txs[0]['valor'], 0.001 );
        $this->assertEquals( 'confirmado', $txs[0]['status'] );
        $this->assertEqualsWithDelta( 100.00, (float) $txs[0]['saldo_apos'], 0.001 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_debitar
    // ──────────────────────────────────────────────────────────────────────────

    public function test_debitar_reduz_saldo(): void {
        $this->db->seed_wallet( 4, 100.00 );

        tpc_debitar( 4, 40.00, 'Frete' );

        $wallet = $this->db->get_wallet( 4 );
        $this->assertEqualsWithDelta( 60.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_debitar_saldo_insuficiente_retorna_false_e_nao_altera_saldo(): void {
        $this->db->seed_wallet( 5, 10.00 );

        $result = tpc_debitar( 5, 50.00, 'Frete maior que saldo' );

        $this->assertFalse( $result );
        $wallet = $this->db->get_wallet( 5 );
        $this->assertEqualsWithDelta( 10.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_debitar_saldo_exato_funciona(): void {
        $this->db->seed_wallet( 6, 25.00 );

        $tx_id = tpc_debitar( 6, 25.00, 'Frete exato' );
        $this->assertIsInt( $tx_id );

        $wallet = $this->db->get_wallet( 6 );
        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_debitar_nao_permite_saldo_negativo(): void {
        $this->db->seed_wallet( 7, 0.00 );

        $result = tpc_debitar( 7, 0.01, 'Saldo zero' );
        $this->assertFalse( $result );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Idempotência
    // ──────────────────────────────────────────────────────────────────────────

    public function test_creditar_idempotente_mesma_referencia_nao_duplica(): void {
        $meta = [ 'referencia' => 'pix-abc-123' ];

        $tx1 = tpc_creditar( 8, 50.00, 'Recarga PIX', $meta );
        $tx2 = tpc_creditar( 8, 50.00, 'Recarga PIX', $meta );

        // Deve retornar o mesmo ID
        $this->assertEquals( $tx1, $tx2 );

        // Saldo deve ser 50, não 100
        $wallet = $this->db->get_wallet( 8 );
        $this->assertEqualsWithDelta( 50.00, (float) $wallet['saldo'], 0.001 );

        // Apenas 1 transação no ledger
        $txs = $this->db->get_transactions( 8 );
        $this->assertCount( 1, $txs );
    }

    public function test_debitar_idempotente_mesma_referencia_nao_duplica(): void {
        $this->db->seed_wallet( 9, 100.00 );
        $meta = [ 'referencia' => 'frete-order-999' ];

        $tx1 = tpc_debitar( 9, 30.00, 'Frete', $meta );
        $tx2 = tpc_debitar( 9, 30.00, 'Frete', $meta );

        $this->assertEquals( $tx1, $tx2 );

        $wallet = $this->db->get_wallet( 9 );
        $this->assertEqualsWithDelta( 70.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_referencias_diferentes_sao_transacoes_independentes(): void {
        $this->db->seed_wallet( 10, 100.00 );

        tpc_debitar( 10, 10.00, 'Frete A', [ 'referencia' => 'ref-A' ] );
        tpc_debitar( 10, 10.00, 'Frete B', [ 'referencia' => 'ref-B' ] );

        $wallet = $this->db->get_wallet( 10 );
        $this->assertEqualsWithDelta( 80.00, (float) $wallet['saldo'], 0.001 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Floating point
    // ──────────────────────────────────────────────────────────────────────────

    public function test_arredondamento_centavos(): void {
        $this->db->seed_wallet( 11, 10.00 );

        tpc_debitar( 11, 3.33, 'Frete fracionado' );
        tpc_debitar( 11, 3.33, 'Frete fracionado 2', [ 'referencia' => 'r2' ] );
        tpc_debitar( 11, 3.33, 'Frete fracionado 3', [ 'referencia' => 'r3' ] );

        $wallet = $this->db->get_wallet( 11 );
        // 10 - 9.99 = 0.01
        $this->assertEqualsWithDelta( 0.01, round( (float) $wallet['saldo'], 2 ), 0.001 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_reservar
    // ──────────────────────────────────────────────────────────────────────────

    public function test_reservar_bloqueia_saldo_disponivel(): void {
        $this->db->seed_wallet( 12, 100.00 );

        $tx_id = tpc_reservar( 12, 40.00, 'Reserva frete', [ 'referencia' => 'res-1' ] );
        $this->assertIsInt( $tx_id );

        $wallet = $this->db->get_wallet( 12 );
        $this->assertEqualsWithDelta( 100.00, (float) $wallet['saldo'], 0.001 );
        $this->assertEqualsWithDelta( 40.00, (float) $wallet['saldo_reservado'], 0.001 );
    }

    public function test_reservar_impede_segundo_debito_acima_disponivel(): void {
        $this->db->seed_wallet( 13, 50.00 );

        tpc_reservar( 13, 30.00, 'Reserva 1', [ 'referencia' => 'res-13-1' ] );
        // Disponível = 50 - 30 = 20. Tentar reservar 25 deve falhar.
        $result = tpc_reservar( 13, 25.00, 'Reserva 2', [ 'referencia' => 'res-13-2' ] );

        $this->assertFalse( $result );

        $wallet = $this->db->get_wallet( 13 );
        $this->assertEqualsWithDelta( 30.00, (float) $wallet['saldo_reservado'], 0.001 );
    }

    public function test_reservar_idempotente(): void {
        $this->db->seed_wallet( 14, 100.00 );
        $meta = [ 'referencia' => 'res-idem-14' ];

        $tx1 = tpc_reservar( 14, 20.00, 'Reserva', $meta );
        $tx2 = tpc_reservar( 14, 20.00, 'Reserva', $meta );

        $this->assertEquals( $tx1, $tx2 );
        $wallet = $this->db->get_wallet( 14 );
        $this->assertEqualsWithDelta( 20.00, (float) $wallet['saldo_reservado'], 0.001 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_liberar_reserva
    // ──────────────────────────────────────────────────────────────────────────

    public function test_liberar_reserva_restaura_disponivel(): void {
        $this->db->seed_wallet( 15, 100.00 );
        $tx_id = tpc_reservar( 15, 50.00, 'Reserva', [ 'referencia' => 'res-15' ] );

        $ok = tpc_liberar_reserva( $tx_id, 'pedido cancelado' );
        $this->assertTrue( $ok );

        $wallet = $this->db->get_wallet( 15 );
        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo_reservado'], 0.001 );
        $this->assertEqualsWithDelta( 100.00, (float) $wallet['saldo'], 0.001 );
    }

    public function test_liberar_reserva_inexistente_retorna_false(): void {
        $result = tpc_liberar_reserva( 99999 );
        $this->assertFalse( $result );
    }

    public function test_liberar_reserva_ja_confirmada_retorna_false(): void {
        $this->db->seed_wallet( 16, 100.00 );
        $tx_id = tpc_reservar( 16, 10.00, 'Reserva', [ 'referencia' => 'res-16' ] );
        tpc_debitar_reserva( $tx_id );

        // Tentar liberar uma reserva já confirmada deve falhar
        $result = tpc_liberar_reserva( $tx_id );
        $this->assertFalse( $result );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_debitar_reserva
    // ──────────────────────────────────────────────────────────────────────────

    public function test_debitar_reserva_desconta_saldo_e_libera_reservado(): void {
        $this->db->seed_wallet( 17, 100.00 );
        $tx_id = tpc_reservar( 17, 40.00, 'Reserva frete', [ 'referencia' => 'res-17' ] );

        $ok = tpc_debitar_reserva( $tx_id );
        $this->assertTrue( $ok );

        $wallet = $this->db->get_wallet( 17 );
        $this->assertEqualsWithDelta( 60.00, (float) $wallet['saldo'], 0.001 );
        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo_reservado'], 0.001 );
    }

    public function test_debitar_reserva_idempotente_segundo_chamada_retorna_true(): void {
        $this->db->seed_wallet( 18, 100.00 );
        $tx_id = tpc_reservar( 18, 20.00, 'Reserva', [ 'referencia' => 'res-18' ] );

        $ok1 = tpc_debitar_reserva( $tx_id );
        $ok2 = tpc_debitar_reserva( $tx_id ); // já confirmada

        $this->assertTrue( $ok1 );
        $this->assertTrue( $ok2 ); // deve retornar true sem debitar novamente

        $wallet = $this->db->get_wallet( 18 );
        $this->assertEqualsWithDelta( 80.00, (float) $wallet['saldo'], 0.001 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // Fluxo completo: reserva → etiqueta emitida → débito confirmado
    // ──────────────────────────────────────────────────────────────────────────

    public function test_fluxo_completo_recarga_reserva_debito(): void {
        // 1. Cliente recarrega R$ 100
        tpc_creditar( 20, 100.00, 'Recarga PIX', [ 'referencia' => 'pix-20-1' ] );

        // 2. Pedido emitido: reserva R$ 35
        $tx_res = tpc_reservar( 20, 35.00, 'Frete pedido #500', [ 'referencia' => 'frete-500' ] );

        $wallet = $this->db->get_wallet( 20 );
        $this->assertEqualsWithDelta( 100.00, (float) $wallet['saldo'], 0.001 );
        $this->assertEqualsWithDelta( 35.00, (float) $wallet['saldo_reservado'], 0.001 );

        // 3. Etiqueta confirmada no ME: debita reserva
        tpc_debitar_reserva( $tx_res );

        $wallet = $this->db->get_wallet( 20 );
        $this->assertEqualsWithDelta( 65.00, (float) $wallet['saldo'], 0.001 );
        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo_reservado'], 0.001 );

        // 4. Ledger deve ter 2 transações: crédito + débito
        $txs = $this->db->get_transactions( 20 );
        $tipos = array_column( $txs, 'tipo' );
        $this->assertContains( 'credito', $tipos );
        $this->assertContains( 'debito', $tipos );
    }

    public function test_fluxo_cancelamento_reserva_restituida(): void {
        tpc_creditar( 21, 80.00, 'Recarga', [ 'referencia' => 'pix-21' ] );
        $tx_res = tpc_reservar( 21, 30.00, 'Frete pedido #501', [ 'referencia' => 'frete-501' ] );

        // Pedido cancelado: libera reserva
        tpc_liberar_reserva( $tx_res, 'cliente cancelou' );

        $wallet = $this->db->get_wallet( 21 );
        $this->assertEqualsWithDelta( 80.00, (float) $wallet['saldo'], 0.001 );
        $this->assertEqualsWithDelta( 0.00, (float) $wallet['saldo_reservado'], 0.001 );
    }

    // ──────────────────────────────────────────────────────────────────────────
    // tpc_tem_saldo
    // ──────────────────────────────────────────────────────────────────────────

    public function test_tem_saldo_considera_reservado(): void {
        $this->db->seed_wallet( 22, 100.00, 70.00 ); // 100 total, 70 reservado = 30 disponível

        $this->assertTrue( tpc_tem_saldo( 22, 30.00 ) );
        $this->assertFalse( tpc_tem_saldo( 22, 30.01 ) );
    }

    public function test_tem_saldo_usuario_sem_carteira_retorna_false(): void {
        $this->assertFalse( tpc_tem_saldo( 9999, 1.00 ) );
    }
}
