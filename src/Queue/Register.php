<?php

namespace WC_MelhorEnvio\Queue;

use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

class Register {
	use Logger, Helpers;

	public $queue;

	public $integration = array();

	protected $logger = 'wc-melhor-envio-queue';

	function __construct() {
		add_action( 'admin_init', [ $this, 'cron_exists' ] );
		add_action( 'wc_melhor_envio_check_posted', [ $this, 'add_orders_to_queue' ] );

		add_action( 'admin_init', [ $this, 'manual_trigger' ] );

		//  always up and running
		$this->queue = new Queue;
	}


	public function manual_trigger() {
		if ( isset( $_GET['melhor-envio-force-cron'] ) && current_user_can( 'manage_woocommerce' ) ) {
			do_action( 'wc_melhor_envio_check_posted' );
			wp_die(
				__( 'As consultas serão verificadas em segundo plano. Você pode fechar esta página.',
					'wc-melhor-envio' ),
				__( 'Consulta manual.', 'wc-melhor-envio' ),
				[
					'response' => 200,
				]
			);
		}
	}


	/**
	 * cron_exists
	 *
	 * @return void
	 */
	public function cron_exists() {
		if ( ! wp_next_scheduled( 'wc_melhor_envio_check_posted' ) ) {
			$this->log( 'Recriando cron de rastreio.' );

			$this->integration = $this->get_integration_settings();

			$interval = isset( $this->integration['check_interval'] ) && in_array( $this->integration['check_interval'],
				[ 'daily', 'twicedaily', 'hourly' ] ) ? $this->integration['check_interval'] : 'daily';

			$time = current_time( 'timestamp' );
			wp_schedule_event( $time, $interval, 'wc_melhor_envio_check_posted' );
		}
	}


	/**
	 * add_orders_to_queue
	 *
	 * @return void
	 */
	public function add_orders_to_queue() {
		$this->integration = $this->get_integration_settings();

		$this->log( 'Colocando pedidos em fila para processamento...' );
		$this->log( print_r( $this->integration, true ) );

		// nenhum status definido para atualização automática
		if ( empty( $this->integration['status_to_tracking'] ) ) {
			$this->log( 'Nenhum status configurado: ' . print_r( $this->integration, true ) );

			return false;
		}

		$all_orders = $this->get_order_ids( $this->integration['status_to_tracking'] );

		$this->log( 'Pedidos: ' . print_r( $all_orders, true ) );

		// let's break it down
		$all_orders = array_chunk( $all_orders, 30 );

		$after_posted       = ( empty( $this->integration['status_after_posted'] ) || 'none' === $this->integration['status_after_posted'] ) ? false : $this->integration['status_after_posted'];
		$after_delivered    = ( empty( $this->integration['status_after_delivered'] ) || 'none' === $this->integration['status_after_delivered'] ) ? false : $this->integration['status_after_delivered'];
		$undelivered_status = ( empty( $this->integration['undelivered_status'] ) || 'none' === $this->integration['undelivered_status'] ) ? false : $this->integration['undelivered_status'];

		foreach ( $all_orders as $orders ) {
			$event = [
				'type'               => 'orders_chunk',
				'order_ids'          => $orders,
				'after_posted'       => $after_posted,
				'after_delivered'    => $after_delivered,
				'undelivered_status' => $undelivered_status,
			];
			$this->queue->push_to_queue( $event );
		}

		$this->queue->save()->dispatch();
	}


	private function get_order_ids( $statuses ) {
		// wc_get_orders() com HPOS espera status SEM prefixo 'wc-'
		// Com posts legados, ambos os formatos funcionam — normalizar para sem prefixo
		foreach ( $statuses as $k => $status ) {
			$statuses[ $k ] = str_replace( 'wc-', '', $status );
		}

		$this->log( 'Status dos pedidos: ' . print_r( $statuses, true ) );

		$args = array(
			'status' => $statuses,
			'return' => 'ids',
			'limit'  => -1,
		);

		return wc_get_orders( $args );
	}
}
