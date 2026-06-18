<?php

namespace WC_MelhorEnvio\Queue;

use WC_Background_Process;
use WC_MelhorEnvio\Api\Print_Label;
use WC_MelhorEnvio\Api\Tracking;
use WC_MelhorEnvio\Traits\Logger;

if ( ! class_exists( 'WC_Background_Process', false ) ) {
	include_once WC_ABSPATH . 'includes/abstracts/class-wc-background-process.php';
}

class Queue extends WC_Background_Process {
	use Logger;

	public $cron_interval = 2;

	protected $logger = 'wc-melhor-envio-queue';


	/**
	 * Initiate new background process.
	 */
	public function __construct() {
		// Uses unique prefix per blog so each blog has separate queue.
		$this->prefix = 'wp_' . get_current_blog_id();
		$this->action = 'wc_melhor_envio_tracking_queue';

		parent::__construct();
	}

	/**
	 * Code to execute for each item in the queue
	 *
	 * @param  string  $item  Queue item to iterate over.
	 *
	 * @return bool
	 */
	protected function task( $item ) {
		if ( ! is_array( $item ) ) {
			return false;
		}

		$after_posted       = $item['after_posted'] ?? false;
		$after_delivered    = $item['after_delivered'] ?? false;
		$undelivered_status = $item['undelivered_status'] ?? false;

		if ( 'orders_chunk' === $item['type'] ) {
			return $this->process_chunk( $item['order_ids'], $after_posted, $after_delivered, $undelivered_status );
		}

		$this->log( 'Nenhum evento registrado: ' . print_r( $item, true ) );

		return false;
	}

	private function process_chunk( $order_ids, $after_posted, $after_delivered = false, $undelivered_status = false ) {
		$statuses = [
			'posted'      => $after_posted,
			'delivered'   => $after_delivered,
			'undelivered' => $undelivered_status,
		];

		$shipping_label = [
			'posted'      => 'Pedido enviado',
			'delivered'   => 'Pedido entregue',
			'undelivered' => 'Pedido não entregue',
		];

		$data_check = $orders_reference = [];

		foreach ( $order_ids as $order_id ) {

			$order = wc_get_order( $order_id );

			if ( ! $order ) {
				$this->log( 'Pedido $' . $order_id . ' não encontrado.' );
				continue;
			}

			$item_id = $order->get_meta( '_melhor_envio_item_id' );

			if ( ! $item_id ) {
				$this->log( 'ID do envio ainda não disponível no pedido #' . $order_id );
				continue;
			}

			$print_url = $order->get_meta( '_melhor_envio_print_url' );

			// se ainda não tem url, não foi finalizado (provavelmente só adicionado ao carrinho)!
			// então vamos ver se há novidades
			if ( ! $print_url ) {
				$this->log( 'URL de impressão não disponível no pedido #' . $order_id );

				try {
					$print = new Print_Label();
					$print->print( $order );

				} catch ( \Throwable $th ) {
					$this->log( 'URL de impressão permanece não disponível no pedido #' . $order_id );

					continue;
				}
			}

			$data_check[ $item_id ]       = $order->get_id();
			$orders_reference[ $item_id ] = $order;
		}

		if ( ! $data_check ) {
			$this->log( 'Nenhum atendeu os critérios de checagem!' );

			return false;
		}

		try {
			$this->log( 'Verificando pedidos no Melhor Envio: ' . print_r( $data_check, true ) );

			$tracking = new Tracking();

			$tracking->add_additional_param( 'bulk_order_ids', array_values( $data_check ) );
			$result = $tracking->tracking( array_keys( $data_check ) );

			if ( ! isset( $result['success'] ) || ! $result['success'] ) {
				$this->log( 'Resposta inválida: ' . print_r( $result, true ) );

				return false;
			}

			$data = (array) $result['data'];

			foreach ( $data as $item_id => $item ) {
				if ( ! isset( $orders_reference[ $item_id ] ) ) {
					$this->log( 'Item não encontrado: ' . $item_id );

					continue;
				}

				if ( ! isset( $item->status ) ) {
					$this->log( 'Item em formato inválido: ' . $item_id );

					continue;
				}

				$status = $item->status;

				$order = $orders_reference[ $item_id ];

				if ( in_array( $status, [ 'posted', 'delivered' ] ) ) {
					$this->log( 'Pedido #' . $orders_reference[ $item_id ]->get_id() . ' enviado! ' . print_r( $item,
							true ) );

					wc_melhor_envio_add_tracking_code( $order, $item->tracking, false );

					if ( ! empty( $statuses[ $status ] ) ) {
						$order->set_status( $statuses[ $status ],
							$shipping_label[ $status ] . '. Código de rastreio: ' . $item->tracking );
					} else {
						$order->add_order_note( $shipping_label[ $status ] . '. Código de rastreio: ' . $item->tracking );
					}

					$order->save();

				} elseif ( 'undelivered' === $status ) {
					$this->log( 'Pedido #' . $orders_reference[ $item_id ]->get_id() . ' não entregue! ' . print_r( $item,
							true ) );

					if ( $undelivered_status ) {
						$order->update_status( $undelivered_status,
							$shipping_label[ $status ] . '. Entre em contato com a transportadora para obter mais informações sobre o rastreio ' . $item->tracking );
					} else {
						$order->add_order_note( $shipping_label[ $status ] . '. Entre em contato com a transportadora para obter mais informações sobre o rastreio ' . $item->tracking );
					}

				} else {
					$this->log( 'Pedido ainda não enviado: ' . print_r( $item, true ) );
				}
			}
		} catch ( \Throwable $e ) {
			$this->log( 'Erro ao rastrear o envio: ' . $e->getMessage() );
		}

		return false;
	}
}
