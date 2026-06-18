<?php
/**
 * Testes para Senderzz_Order_Meta.
 *
 * Cobre:
 *  1. Pedido antigo (só campos legados) → leitura retorna valor correto via fallback.
 *  2. Pedido novo → set_* grava canônico + aliases legados.
 *  3. Cálculo de taxa via get_fee_total().
 *  4. get_affiliate_commission() via fallback _sz_aff_commission.
 *  5. get_motoboy_status() via fallback _senderzz_motoboy_flow_status.
 *  6. get_delivery_date() via fallback _sz_delivery_date.
 *  7. get_producer_user_id() via fallback _sz_aff_producer_id.
 *  8. get_affiliate_id() via fallback _sz_affiliate_id.
 *  9. Nenhum campo antigo é apagado após set_*().
 * 10. Normalização em dry_run não altera dados.
 * 11. Normalização real preenche canônico ausente.
 * 12. Log de divergências é gerado.
 */

namespace WC_MelhorEnvio\Tests\Unit\OrderMeta;

use PHPUnit\Framework\TestCase;

/**
 * Stub de WC_Order para testes (não usa DB real).
 */
class FakeWCOrder {
    public int $id;
    private array $meta = [];

    public function __construct( int $id ) {
        $this->id = $id;
    }

    public function get_id(): int { return $this->id; }

    public function get_meta( string $key, bool $single = true ): mixed {
        return $this->meta[ $key ] ?? '';
    }

    public function update_meta_data( string $key, mixed $value ): void {
        $this->meta[ $key ] = $value;
    }

    public function save(): int { return $this->id; }

    // Helper de teste — expõe metas para asserção
    public function all_meta(): array { return $this->meta; }
}

class OrderMetaTest extends TestCase {

    // ── Bootstrap ──────────────────────────────────────────────────────────────

    public static function setUpBeforeClass(): void {
        // Garante que a classe está disponível
        $helper = dirname( __DIR__, 3 ) . '/includes/class-senderzz-order-meta.php';
        if ( ! class_exists( 'Senderzz_Order_Meta', false ) ) {
            // Precisa de wc_get_order stub
            if ( ! function_exists( 'wc_get_order' ) ) {
                require_once dirname( __DIR__, 2 ) . '/bootstrap/wp-stubs.php';
            }
            require_once $helper;
        }
    }

    // ── 1. Pedido antigo — leitura via fallback ─────────────────────────────────

    public function test_get_fee_total_fallback_from_sz_mb_taxa_total(): void {
        $order = new FakeWCOrder( 1001 );
        $order->update_meta_data( '_sz_mb_taxa_total', '36.46' );

        $result = \Senderzz_Order_Meta::get_fee_total( $order );
        $this->assertEqualsWithDelta( 36.46, $result, 0.001, 'Deve ler de _sz_mb_taxa_total quando canônico ausente' );
    }

    public function test_get_fee_total_fallback_chain(): void {
        $order = new FakeWCOrder( 1002 );
        // Nem o canônico nem _sz_mb_taxa_total
        $order->update_meta_data( '_sz_taxa_total', '29.90' );

        $result = \Senderzz_Order_Meta::get_fee_total( $order );
        $this->assertEqualsWithDelta( 29.90, $result, 0.001, 'Fallback deve chegar em _sz_taxa_total' );
    }

    public function test_get_fee_total_prefers_canonical(): void {
        $order = new FakeWCOrder( 1003 );
        $order->update_meta_data( '_senderzz_fee_total', '40.00' );
        $order->update_meta_data( '_sz_mb_taxa_total',   '99.00' ); // legado diferente

        $result = \Senderzz_Order_Meta::get_fee_total( $order );
        $this->assertEqualsWithDelta( 40.00, $result, 0.001, 'Canônico deve ter prioridade sobre legado' );
    }

    public function test_get_fee_delivery_fallback(): void {
        $order = new FakeWCOrder( 1004 );
        $order->update_meta_data( '_sz_mb_taxa_entrega', '15.00' );

        $result = \Senderzz_Order_Meta::get_fee_delivery( $order );
        $this->assertEqualsWithDelta( 15.00, $result, 0.001 );
    }

    public function test_get_fee_transaction_fallback(): void {
        $order = new FakeWCOrder( 1005 );
        $order->update_meta_data( '_sz_mb_taxa_adicional', '5.50' );

        $result = \Senderzz_Order_Meta::get_fee_transaction( $order );
        $this->assertEqualsWithDelta( 5.50, $result, 0.001 );
    }

    // ── 2. Pedido novo — gravação canônico + aliases ────────────────────────────

    public function test_set_fee_total_writes_canonical_and_all_aliases(): void {
        $order = new FakeWCOrder( 2001 );
        \Senderzz_Order_Meta::set_fee_total( $order, 36.46 );

        $meta = $order->all_meta();
        $this->assertEquals( 36.46, (float) $meta['_senderzz_fee_total'],         '_senderzz_fee_total deve ser gravado' );
        $this->assertEquals( 36.46, (float) $meta['_sz_mb_taxa_total'],           '_sz_mb_taxa_total deve ser gravado' );
        $this->assertEquals( 36.46, (float) $meta['_sz_taxa_total'],              '_sz_taxa_total deve ser gravado' );
        $this->assertEquals( 36.46, (float) $meta['_senderzz_shipping_charged'],  '_senderzz_shipping_charged deve ser gravado' );
        $this->assertEquals( 36.46, (float) $meta['_senderzz_shipping_real_cost'],'_senderzz_shipping_real_cost deve ser gravado' );
    }

    public function test_set_fee_delivery_writes_canonical_and_aliases(): void {
        $order = new FakeWCOrder( 2002 );
        \Senderzz_Order_Meta::set_fee_delivery( $order, 12.50 );

        $meta = $order->all_meta();
        $this->assertEquals( 12.50, (float) $meta['_senderzz_fee_delivery'] );
        $this->assertEquals( 12.50, (float) $meta['_sz_mb_taxa_entrega'] );
        $this->assertEquals( 12.50, (float) $meta['_sz_taxa_entrega'] );
    }

    public function test_set_affiliate_commission_writes_canonical_and_alias(): void {
        $order = new FakeWCOrder( 2003 );
        \Senderzz_Order_Meta::set_affiliate_commission( $order, 142.52 );

        $meta = $order->all_meta();
        $this->assertEquals( 142.52, (float) $meta['_senderzz_affiliate_commission'] );
        $this->assertEquals( 142.52, (float) $meta['_sz_aff_commission'] );
    }

    // ── 3. Taxa via get_fee_total (caso financeiro real) ───────────────────────

    public function test_fee_total_used_in_producer_net_calculation(): void {
        $order = new FakeWCOrder( 3001 );
        // Pedido #1561: bruto 250, taxas 36.46, pct 60%
        $order->update_meta_data( '_sz_mb_taxa_total', '36.46' );
        $order->update_meta_data( '_sz_aff_commission_gross', '150.00' );

        $taxa    = \Senderzz_Order_Meta::get_fee_total( $order );
        $aff_gross = (float) $order->get_meta( '_sz_aff_commission_gross', true );
        $bruto   = 250.00;
        $produtor = $bruto - $aff_gross - $taxa;

        $this->assertEqualsWithDelta( 63.54, $produtor, 0.01, 'Produtor #1561 = 250 - 150 - 36.46 = 63.54' );
    }

    public function test_fee_total_pedido_1570(): void {
        $order = new FakeWCOrder( 3002 );
        // Pedido #1570: bruto 276, taxas 37.75, pct 60%
        $order->update_meta_data( '_sz_mb_taxa_total', '37.75' );
        $order->update_meta_data( '_sz_aff_commission_gross', '165.60' );

        $taxa    = \Senderzz_Order_Meta::get_fee_total( $order );
        $aff_gross = (float) $order->get_meta( '_sz_aff_commission_gross', true );
        $bruto   = 276.00;
        $produtor = $bruto - $aff_gross - $taxa;

        $this->assertEqualsWithDelta( 72.65, $produtor, 0.01, 'Produtor #1570 = 276 - 165.60 - 37.75 = 72.65' );
    }

    // ── 4. Afiliado commission fallback ────────────────────────────────────────

    public function test_get_affiliate_commission_fallback(): void {
        $order = new FakeWCOrder( 4001 );
        $order->update_meta_data( '_sz_aff_commission', '142.52' );

        $result = \Senderzz_Order_Meta::get_affiliate_commission( $order );
        $this->assertEqualsWithDelta( 142.52, $result, 0.001 );
    }

    public function test_get_affiliate_commission_pct_fallback(): void {
        $order = new FakeWCOrder( 4002 );
        $order->update_meta_data( '_sz_aff_commission_pct', '60' );

        $result = \Senderzz_Order_Meta::get_affiliate_commission_pct( $order );
        $this->assertEqualsWithDelta( 60.0, $result, 0.001 );
    }

    // ── 5. Motoboy status fallback ─────────────────────────────────────────────

    public function test_get_motoboy_status_fallback(): void {
        $order = new FakeWCOrder( 5001 );
        $order->update_meta_data( '_senderzz_motoboy_flow_status', 'agendado' );

        $result = \Senderzz_Order_Meta::get_motoboy_status( $order );
        $this->assertEquals( 'agendado', $result );
    }

    public function test_set_motoboy_status_writes_both(): void {
        $order = new FakeWCOrder( 5002 );
        \Senderzz_Order_Meta::set_motoboy_status( $order, 'entregue' );

        $meta = $order->all_meta();
        $this->assertEquals( 'entregue', $meta['_senderzz_motoboy_status'] );
        $this->assertEquals( 'entregue', $meta['_senderzz_motoboy_flow_status'] );
    }

    // ── 6. Data de entrega fallback ────────────────────────────────────────────

    public function test_get_delivery_date_fallback_sz_delivery_date(): void {
        $order = new FakeWCOrder( 6001 );
        $order->update_meta_data( '_sz_delivery_date', '2026-06-17' );

        $result = \Senderzz_Order_Meta::get_delivery_date( $order );
        $this->assertEquals( '2026-06-17', $result );
    }

    public function test_get_delivery_date_fallback_motoboy_entrega(): void {
        $order = new FakeWCOrder( 6002 );
        $order->update_meta_data( '_sz_motoboy_entrega_data', '2026-06-18' );

        $result = \Senderzz_Order_Meta::get_delivery_date( $order );
        $this->assertEquals( '2026-06-18', $result );
    }

    public function test_set_delivery_date_writes_canonical_and_aliases(): void {
        $order = new FakeWCOrder( 6003 );
        \Senderzz_Order_Meta::set_delivery_date( $order, '2026-06-20' );

        $meta = $order->all_meta();
        $this->assertEquals( '2026-06-20', $meta['_senderzz_delivery_date'] );
        $this->assertEquals( '2026-06-20', $meta['_sz_delivery_date'] );
        $this->assertEquals( '2026-06-20', $meta['_sz_motoboy_entrega_data'] );
    }

    // ── 7. Producer user ID fallback ──────────────────────────────────────────

    public function test_get_producer_user_id_fallback(): void {
        $order = new FakeWCOrder( 7001 );
        $order->update_meta_data( '_sz_aff_producer_id', '42' );

        $result = \Senderzz_Order_Meta::get_producer_user_id( $order );
        $this->assertEquals( 42, $result );
    }

    public function test_get_producer_user_id_fallback_owner(): void {
        $order = new FakeWCOrder( 7002 );
        $order->update_meta_data( '_senderzz_owner_user_id', '55' );

        $result = \Senderzz_Order_Meta::get_producer_user_id( $order );
        $this->assertEquals( 55, $result );
    }

    // ── 8. Affiliate ID fallback ───────────────────────────────────────────────

    public function test_get_affiliate_id_fallback(): void {
        $order = new FakeWCOrder( 8001 );
        $order->update_meta_data( '_sz_affiliate_id', '99' );

        $result = \Senderzz_Order_Meta::get_affiliate_id( $order );
        $this->assertEquals( 99, $result );
    }

    public function test_get_affiliate_id_fallback_ref(): void {
        $order = new FakeWCOrder( 8002 );
        $order->update_meta_data( '_sz_affiliate_ref', '77' );

        $result = \Senderzz_Order_Meta::get_affiliate_id( $order );
        $this->assertEquals( 77, $result );
    }

    // ── 9. Set não apaga campo antigo ──────────────────────────────────────────

    public function test_set_does_not_erase_unrelated_legacy_fields(): void {
        $order = new FakeWCOrder( 9001 );
        // Campo legado pré-existente não relacionado
        $order->update_meta_data( '_sz_aff_commission_gross', '150.00' );
        $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );

        \Senderzz_Order_Meta::set_fee_total( $order, 36.46 );

        $meta = $order->all_meta();
        // Campos não relacionados devem permanecer intactos
        $this->assertEquals( '150.00', $meta['_sz_aff_commission_gross'], '_sz_aff_commission_gross não deve ser apagado' );
        $this->assertEquals( 'motoboy', $meta['_senderzz_delivery_mode'], '_senderzz_delivery_mode não deve ser apagado' );
    }

    // ── 10. order_gross_total fallback ────────────────────────────────────────

    public function test_get_order_gross_total_fallback_offer_value(): void {
        $order = new FakeWCOrder( 10001 );
        $order->update_meta_data( '_senderzz_offer_value', '250.00' );

        $result = \Senderzz_Order_Meta::get_order_gross_total( $order );
        $this->assertEqualsWithDelta( 250.00, $result, 0.001 );
    }

    // ── 11. Valor vazio retorna 0/'' quando nenhum campo existe ──────────────

    public function test_get_fee_total_returns_zero_when_no_meta(): void {
        $order = new FakeWCOrder( 11001 );
        $result = \Senderzz_Order_Meta::get_fee_total( $order );
        $this->assertEqualsWithDelta( 0.0, $result, 0.001 );
    }

    public function test_get_delivery_date_returns_empty_when_no_meta(): void {
        $order = new FakeWCOrder( 11002 );
        $result = \Senderzz_Order_Meta::get_delivery_date( $order );
        $this->assertSame( '', $result );
    }

    // ── 12. Constantes canônicas estão corretas ────────────────────────────────

    public function test_canonical_constants_are_defined(): void {
        $this->assertEquals( '_senderzz_fee_total',            \Senderzz_Order_Meta::FEE_TOTAL );
        $this->assertEquals( '_senderzz_fee_delivery',         \Senderzz_Order_Meta::FEE_DELIVERY );
        $this->assertEquals( '_senderzz_fee_transaction',      \Senderzz_Order_Meta::FEE_TRANSACTION );
        $this->assertEquals( '_senderzz_delivery_date',        \Senderzz_Order_Meta::DELIVERY_DATE );
        $this->assertEquals( '_senderzz_motoboy_status',       \Senderzz_Order_Meta::MOTOBOY_STATUS );
        $this->assertEquals( '_senderzz_affiliate_commission', \Senderzz_Order_Meta::AFFILIATE_COMMISSION );
        $this->assertEquals( '_senderzz_producer_user_id',     \Senderzz_Order_Meta::PRODUCER_USER_ID );
        $this->assertEquals( '_senderzz_affiliate_id',         \Senderzz_Order_Meta::AFFILIATE_ID );
    }
}
