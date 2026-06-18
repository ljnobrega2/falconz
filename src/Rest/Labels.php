<?php

namespace WC_MelhorEnvio\Rest;

use Exception;
use WC_MelhorEnvio\Api\Download_Label;
use WC_MelhorEnvio\Pipeline\Label_Pipeline;
use WC_MelhorEnvio\Traits\Helpers;
use WP_REST_Request;
use WP_REST_Response;

class Labels {
	use Helpers;

	public function __construct() {
		add_action( 'rest_api_init', [ $this, 'register_routes' ] );
	}

	public function register_routes() {
		register_rest_route( 'wc-melhor-envio/v1', '/labels', [
			'methods'             => 'GET',
			'callback'            => [ $this, 'list_labels' ],
			'permission_callback' => [ $this, 'permissions' ],
		] );

		register_rest_route( 'wc-melhor-envio/v1', '/labels/(?P<order_id>\d+)', [
			'methods'             => 'GET',
			'callback'            => [ $this, 'get_label' ],
			'permission_callback' => [ $this, 'permissions' ],
		] );

		register_rest_route( 'wc-melhor-envio/v1', '/labels/(?P<order_id>\d+)/process', [
			'methods'             => 'POST',
			'callback'            => [ $this, 'process_label' ],
			'permission_callback' => [ $this, 'permissions' ],
		] );

		register_rest_route( 'wc-melhor-envio/v1', '/labels/(?P<order_id>\d+)/download', [
			'methods'             => 'POST',
			'callback'            => [ $this, 'download_label' ],
			'permission_callback' => [ $this, 'permissions' ],
		] );

		// Composição de lote A4
		register_rest_route( 'wc-melhor-envio/v1', '/labels/batch-print', [
			'methods'             => 'POST',
			'callback'            => [ $this, 'batch_print' ],
			'permission_callback' => [ $this, 'permissions' ],
		] );

		// Migração de meta para tabela
		register_rest_route( 'wc-melhor-envio/v1', '/tools/migrate-labels', [
			'methods'             => 'POST',
			'callback'            => [ $this, 'migrate_labels' ],
			'permission_callback' => [ $this, 'permissions' ],
		] );
	}

	public function permissions() {
		return current_user_can( 'manage_woocommerce' );
	}

	public function list_labels( WP_REST_Request $request ) {
		$limit = min( 100, max( 1, absint( $request->get_param( 'limit' ) ?: 20 ) ) );

		$args = [
			'limit'    => $limit,
			'orderby'  => 'date',
			'order'    => 'DESC',
			'return'   => 'objects',
			'meta_key' => '_melhor_envio_item_id',
		];

		// Filtro por status do pedido WooCommerce
		if ( $status = $request->get_param( 'status' ) ) {
			$args['status'] = sanitize_text_field( $status );
		}

		// Filtro por status do pipeline ME (label_status)
		$meta_query = [];
		if ( $label_status = $request->get_param( 'label_status' ) ) {
			$meta_query[] = [
				'key'     => '_melhor_envio_label_status',
				'value'   => sanitize_text_field( $label_status ),
				'compare' => '=',
			];
		}

		// Filtro por transportadora (melhorenvio_method_id nos itens de envio)
		// Implementado via filtro pós-query pois shipping items ficam em tabela separada

		// Filtro por data (from / to)
		if ( $from = $request->get_param( 'from' ) ) {
			$args['date_created'] = '>=' . sanitize_text_field( $from );
		}
		if ( $to = $request->get_param( 'to' ) ) {
			// Se já tem from, usa intervalo; senão, só to
			if ( isset( $args['date_created'] ) ) {
				$from_val = str_replace( '>=', '', $args['date_created'] );
				$args['date_created'] = $from_val . '...' . sanitize_text_field( $to );
			} else {
				$args['date_created'] = '<=' . sanitize_text_field( $to );
			}
		}

		if ( ! empty( $meta_query ) ) {
			$args['meta_query'] = $meta_query;
		}

		$orders = wc_get_orders( $args );

		$data           = [];
		$carrier_filter = $request->get_param( 'carrier' ) ? sanitize_text_field( $request->get_param( 'carrier' ) ) : null;

		foreach ( $orders as $order ) {
			if ( ! $order->get_meta( '_melhor_envio_item_id' ) ) {
				continue;
			}

			$formatted = $this->format_order( $order );

			// Filtro pós-query por transportadora
			if ( $carrier_filter ) {
				$carrier_normalized = strtolower( $carrier_filter );
				$order_carrier      = strtolower( $formatted['carrier'] ?? '' );
				if ( false === strpos( $order_carrier, $carrier_normalized ) ) {
					continue;
				}
			}

			$data[] = $formatted;
		}

		return new WP_REST_Response( [
			'total'  => count( $data ),
			'limit'  => $limit,
			'orders' => $data,
		] );
	}

	public function get_label( WP_REST_Request $request ) {
		$order = wc_get_order( absint( $request['order_id'] ) );
		if ( ! $order ) {
			return new WP_REST_Response( [ 'message' => 'Pedido não encontrado.' ], 404 );
		}
		return new WP_REST_Response( $this->format_order( $order ) );
	}

	public function process_label( WP_REST_Request $request ) {
		$order = wc_get_order( absint( $request['order_id'] ) );
		if ( ! $order ) {
			return new WP_REST_Response( [ 'message' => 'Pedido não encontrado.' ], 404 );
		}

		try {
			$pipeline = new Label_Pipeline();
			$result = $pipeline->process( $order, true );
			return new WP_REST_Response( [
				'success' => true,
				'result'   => $result,
				'order'    => $this->format_order( $order ),
			] );
		} catch ( Exception $e ) {
			return new WP_REST_Response( [ 'success' => false, 'message' => $e->getMessage() ], 500 );
		}
	}

	public function download_label( WP_REST_Request $request ) {
		$order = wc_get_order( absint( $request['order_id'] ) );
		if ( ! $order ) {
			return new WP_REST_Response( [ 'message' => 'Pedido não encontrado.' ], 404 );
		}

		try {
			$download = new Download_Label();
			$result = $download->download( $order );
			return new WP_REST_Response( [ 'success' => true, 'result' => $result, 'order' => $this->format_order( $order ) ] );
		} catch ( Exception $e ) {
			return new WP_REST_Response( [ 'success' => false, 'message' => $e->getMessage() ], 500 );
		}
	}

	/**
	 * batch_print
	 * Compõe um PDF em lote com etiquetas de múltiplos pedidos.
	 *
	 * Body params:
	 *   order_ids[]           int[]  IDs dos pedidos
	 *   include_packing_slip  bool   Incluir romaneio por pedido
	 *   include_cover         bool   Incluir capa de branding por pedido
	 */
	public function batch_print( WP_REST_Request $request ) {
		$order_ids = array_map( 'absint', (array) $request->get_param( 'order_ids' ) );

		if ( empty( $order_ids ) ) {
			return new WP_REST_Response( [ 'success' => false, 'message' => 'Informe pelo menos um order_id.' ], 400 );
		}

		$options = [
			'include_packing_slip' => (bool) $request->get_param( 'include_packing_slip' ),
			'include_cover'        => (bool) $request->get_param( 'include_cover' ),
		];

		try {
			$composer = new \WC_MelhorEnvio\Pdf\Batch_Composer();
			$result   = $composer->compose( $order_ids, $options );

			return new WP_REST_Response( [
				'success' => true,
				'result'  => $result,
			] );
		} catch ( Exception $e ) {
			return new WP_REST_Response( [ 'success' => false, 'message' => $e->getMessage() ], 500 );
		}
	}

	/**
	 * migrate_labels
	 * Migra dados de post_meta para a tabela me_labels.
	 * Seguro re-executar (idempotente — pula registros já migrados).
	 */
	public function migrate_labels( WP_REST_Request $request ) {
		$batch    = absint( $request->get_param( 'batch' ) ?: 100 );
		$migrated = \WC_MelhorEnvio\Database\Label_Table::migrate_from_meta( $batch );

		return new WP_REST_Response( [
			'success'  => true,
			'migrated' => $migrated,
			'message'  => sprintf( '%d registros migrados neste lote.', $migrated ),
		] );
	}

	private function format_order( $order ) {
		$carrier = '';
		foreach ( $order->get_items( 'shipping' ) as $item ) {
			if ( $method_id = $item->get_meta( 'melhorenvio_method_id' ) ) {
				$carrier = $item->get_name();
				break;
			}
		}

		return [
			'order_id'              => $order->get_id(),
			'number'                => $order->get_order_number(),
			'status'                => $order->get_status(),
			'carrier'               => $carrier,
			'melhor_envio_item_id'  => $order->get_meta( '_melhor_envio_item_id' ),
			'melhor_envio_protocol' => $order->get_meta( '_melhor_envio_order_id' ),
			'tracking'              => $order->get_meta( '_melhor_envio_tracking_codes' ),
			'print_url'             => $order->get_meta( '_melhor_envio_print_url' ),
			'pdf_local_url'         => $order->get_meta( '_melhor_envio_pdf_local_url' ),
			'label_status'          => $order->get_meta( '_melhor_envio_label_status' ),
			'label_error'           => $order->get_meta( '_melhor_envio_label_error' ),
			'generated_at'          => $order->get_meta( '_melhor_envio_pdf_downloaded_at' ),
			'operator'              => $order->get_meta( '_melhor_envio_requested_by' ),
		];
	}
}
