<?php

namespace WC_MelhorEnvio\Queue;

use WC_Background_Process;
use WC_MelhorEnvio\Pipeline\Label_Pipeline;
use WC_MelhorEnvio\Traits\Logger;

if ( ! class_exists( 'WC_Background_Process', false ) ) {
	include_once WC_ABSPATH . 'includes/abstracts/class-wc-background-process.php';
}

class Pipeline_Queue extends WC_Background_Process {
	use Logger;

	public $cron_interval = 1;
	protected $logger = 'wc-melhor-envio-pipeline-queue';

	public function __construct() {
		$this->prefix = 'wp_' . get_current_blog_id();
		$this->action = 'wc_melhor_envio_pipeline_queue';
		parent::__construct();
	}

	protected function task( $item ) {
		if ( ! is_array( $item ) || empty( $item['order_id'] ) ) {
			return false;
		}

		$order_id     = absint( $item['order_id'] );
		$download     = ! empty( $item['download_pdf'] );
		$status_after = isset( $item['status_after'] ) ? $item['status_after'] : false;
		$order        = wc_get_order( $order_id );

		if ( ! $order ) {
			$this->log( 'Pedido #' . $order_id . ' não encontrado.' );
			return false;
		}

		try {
			$this->log( 'Iniciando pipeline para pedido #' . $order_id );

			$pipeline = new Label_Pipeline();
			$pipeline->process( $order, $download );

			if ( $status_after && ! $order->has_status( $status_after ) ) {
				$order->update_status( $status_after, 'Fluxo automático do Melhor Envio concluído.' );
			} else {
				$order->add_order_note( 'Fluxo automático do Melhor Envio concluído.' );
			}

			$this->log( 'Pipeline concluído para pedido #' . $order_id );
		} catch ( \Exception $e ) {
			$this->log( 'Erro no pipeline pedido #' . $order_id . ': ' . $e->getMessage() );
			$order->update_meta_data( '_melhor_envio_label_status', 'error' );
			$order->update_meta_data( '_melhor_envio_label_error', $e->getMessage() );
			$order->save();
			$order->add_order_note( 'Erro no fluxo automático do Melhor Envio: ' . $e->getMessage() );
		}

		return false;
	}
}
