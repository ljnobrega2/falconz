<?php

namespace WC_MelhorEnvio\Admin\Bulk_Actions;

use WC_MelhorEnvio\Queue\Pipeline_Queue;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

/**
 * Bulk_Pipeline
 *
 * Registra bulk action "Gerar etiquetas ME" na lista de pedidos do WooCommerce.
 * Os pedidos selecionados são enfileirados no Pipeline_Queue para processamento
 * assíncrono em background — sem risco de timeout.
 */
class Bulk_Pipeline {
	use Helpers, Logger;

	protected $logger = 'wc-melhor-envio-bulk';

	public function __construct() {
		// Lista de pedidos clássica (posts)
		add_filter( 'bulk_actions-edit-shop_order', [ $this, 'register_bulk_action' ] );
		add_filter( 'handle_bulk_actions-edit-shop_order', [ $this, 'handle_bulk_action' ], 10, 3 );

		// Lista de pedidos HPOS
		add_filter( 'bulk_actions-woocommerce_page_wc-orders', [ $this, 'register_bulk_action' ] );
		add_filter( 'handle_bulk_actions-woocommerce_page_wc-orders', [ $this, 'handle_bulk_action' ], 10, 3 );

		add_action( 'admin_notices', [ $this, 'bulk_action_notice' ] );

		// AJAX para consultar progresso
		add_action( 'wp_ajax_me_bulk_pipeline_status', [ $this, 'ajax_bulk_status' ] );
	}

	/**
	 * Registra as bulk actions no select do admin
	 */
	public function register_bulk_action( $actions ) {
		$actions['me_generate_labels']       = __( 'ME: Gerar etiquetas', 'wc-melhor-envio' );
		$actions['me_generate_labels_nopdl'] = __( 'ME: Gerar etiquetas (sem baixar PDF)', 'wc-melhor-envio' );
		$actions['me_print_batch']           = __( 'ME: Imprimir etiquetas em lote (PDF único)', 'wc-melhor-envio' );

		return $actions;
	}

	/**
	 * Processa a bulk action — enfileira pedidos no Pipeline_Queue
	 */
	public function handle_bulk_action( $redirect_to, $action, $post_ids ) {
		if ( ! in_array( $action, [ 'me_generate_labels', 'me_generate_labels_nopdl', 'me_print_batch' ], true ) ) {
			return $redirect_to;
		}

		if ( ! current_user_can( 'manage_woocommerce' ) ) {
			return $redirect_to;
		}

		// Impressão em lote — redireciona para URL que gera o PDF e faz download
		if ( 'me_print_batch' === $action ) {
			$order_ids = array_map( 'absint', $post_ids );
			$redirect_to = add_query_arg( [
				'me_batch_print'  => 1,
				'me_order_ids'    => implode( ',', $order_ids ),
				'me_batch_nonce'  => wp_create_nonce( 'me_batch_print' ),
			], $redirect_to );
			return $redirect_to;
		}

		$download_pdf    = ( 'me_generate_labels' === $action );
		$settings        = $this->get_integration_settings();
		$status_after    = ( empty( $settings['status_after_print'] ) || 'none' === $settings['status_after_print'] ) ? false : $settings['status_after_print'];

		$queue   = new Pipeline_Queue();
		$queued  = 0;
		$skipped = 0;

		foreach ( $post_ids as $order_id ) {
			$order = wc_get_order( absint( $order_id ) );

			if ( ! $order ) {
				$skipped++;
				continue;
			}

			if ( ! $this->can_request_label( $order ) ) {
				$this->log( 'Pedido #' . $order_id . ' pulado na bulk action (sem permissão para etiqueta).' );
				$skipped++;
				continue;
			}

			$queue->push_to_queue( [
				'order_id'     => absint( $order_id ),
				'download_pdf' => $download_pdf,
				'status_after' => $status_after,
			] );

			$order->add_order_note( sprintf(
				__( 'Etiqueta enfileirada para geração em lote pelo usuário #%d.', 'wc-melhor-envio' ),
				get_current_user_id()
			) );

			$queued++;
		}

		if ( $queued > 0 ) {
			$queue->save()->dispatch();
		}

		$redirect_to = add_query_arg( [
			'me_bulk_queued'  => $queued,
			'me_bulk_skipped' => $skipped,
		], $redirect_to );

		return $redirect_to;
	}

	/**
	 * Exibe admin notice após bulk action
	 */
	public function bulk_action_notice() {
		// Processar impressão em lote se solicitado
		if ( isset( $_GET['me_batch_print'] ) && isset( $_GET['me_batch_nonce'] ) ) {
			if ( wp_verify_nonce( $_GET['me_batch_nonce'], 'me_batch_print' ) && current_user_can( 'manage_woocommerce' ) ) {
				$order_ids = array_slice( array_map( 'absint', array_filter( explode( ',', $_GET['me_order_ids'] ?? '' ) ) ), 0, 100 ); // cap em 100
				$this->render_batch_print_page( $order_ids );
				exit;
			}
		}

		if ( ! isset( $_GET['me_bulk_queued'] ) ) {
			return;
		}

		$queued  = absint( $_GET['me_bulk_queued'] );
		$skipped = absint( $_GET['me_bulk_skipped'] );

		if ( $queued > 0 ) {
			printf(
				'<div class="notice notice-success is-dismissible"><p>%s</p></div>',
				sprintf(
					_n(
						'Melhor Envio: %d etiqueta enfileirada para geração em background.',
						'Melhor Envio: %d etiquetas enfileiradas para geração em background.',
						$queued,
						'wc-melhor-envio'
					),
					$queued
				)
			);
		}

		if ( $skipped > 0 ) {
			printf(
				'<div class="notice notice-warning is-dismissible"><p>%s</p></div>',
				sprintf(
					_n(
						'Melhor Envio: %d pedido pulado (sem permissão ou inválido).',
						'Melhor Envio: %d pedidos pulados (sem permissão ou inválidos).',
						$skipped,
						'wc-melhor-envio'
					),
					$skipped
				)
			);
		}
	}

	/**
	 * render_batch_print_page
	 * Exibe página intermediária com todos os PDFs disponíveis para impressão em lote.
	 * Para cada pedido mostra status e link individual. Oferece botão de PDF unificado
	 * via Batch_Composer se disponível, ou links individuais como fallback.
	 */
	private function render_batch_print_page( array $order_ids ) {
		$orders_data = [];
		$pdf_paths   = [];
		$missing     = [];

		foreach ( $order_ids as $order_id ) {
			$order = wc_get_order( $order_id );
			if ( ! $order ) {
				continue;
			}

			$pdf_url   = $order->get_meta( '_melhor_envio_pdf_local_url' );
			$pdf_path  = $order->get_meta( '_melhor_envio_pdf_local_path' );
			$print_url = $order->get_meta( '_melhor_envio_print_url' );
			$status    = $order->get_meta( '_melhor_envio_label_status' );
			$item_id   = $order->get_meta( '_melhor_envio_item_id' );

			$orders_data[] = [
				'id'        => $order_id,
				'number'    => $order->get_order_number(),
				'customer'  => $order->get_formatted_billing_full_name(),
				'pdf_url'   => $pdf_url,
				'print_url' => $print_url,
				'status'    => $status,
				'has_label' => ! empty( $item_id ),
			];

			if ( $pdf_path && file_exists( $pdf_path ) ) {
				$pdf_paths[] = $pdf_path;
			} else {
				$missing[] = $order_id;
			}
		}

		// Tentar gerar PDF unificado
		$batch_url = null;
		if ( ! empty( $pdf_paths ) ) {
			try {
				$composer  = new \WC_MelhorEnvio\Pdf\Batch_Composer();
				$result    = $composer->compose( $order_ids );
				$batch_url = $result['url'] ?? null;
			} catch ( \Throwable $e ) {
				$this->log( 'Erro ao compor lote: ' . $e->getMessage() );
			}
		}

		// Renderizar página
		$site_name = get_bloginfo( 'name' );
		?>
		<!DOCTYPE html>
		<html>
		<head>
			<meta charset="UTF-8">
			<title>Impressão em Lote — <?php echo esc_html( $site_name ); ?></title>
			<style>
				* { box-sizing: border-box; margin: 0; padding: 0; }
				body { font-family: var(--sz-font); background: #f0f0f1; color: #1d2327; }
				.wrap { max-width: 900px; margin: 40px auto; padding: 0 20px; }
				h1 { font-size: var(--sz-text-xl); margin-bottom: 24px; color: #1d2327; }
				.batch-bar { background: #fff; border: 1px solid #c3c4c7; border-radius: 4px; padding: 20px 24px; margin-bottom: 24px; display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
				.batch-bar .count { font-size: var(--sz-text-md); color: #646970; flex: 1; }
				.btn { display: inline-flex; align-items: center; gap: 8px; padding: 8px 16px; border-radius: 3px; font-size: var(--sz-text-base); font-weight: 600; text-decoration: none; cursor: pointer; border: none; }
				.btn-primary { background: #2271b1; color: #fff; }
				.btn-primary:hover { background: #135e96; color: #fff; }
				.btn-secondary { background: #f6f7f7; color: #2271b1; border: 1px solid #2271b1; }
				.btn-secondary:hover { background: #f0f6fc; }
				.btn-success { background: #00a32a; color: #fff; }
				.btn-success:hover { background: #008a20; color: #fff; }
				.table-wrap { background: #fff; border: 1px solid #c3c4c7; border-radius: 4px; overflow: hidden; }
				table { width: 100%; border-collapse: collapse; font-size: var(--sz-text-base); }
				th { background: #f6f7f7; padding: 10px 16px; text-align: left; font-weight: 600; border-bottom: 1px solid #c3c4c7; color: #646970; text-transform: none; font-size: var(--sz-text-sm); letter-spacing:0; }
				td { padding: 12px 16px; border-bottom: 1px solid #f0f0f1; vertical-align: middle; }
				tr:last-child td { border-bottom: none; }
				tr:hover td { background: #f6f7f7; }
				.badge { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: var(--sz-text-sm); font-weight: 600; }
				.badge-ok { background: #edfaef; color: #00a32a; }
				.badge-warn { background: #fcf9e8; color: #996800; }
				.badge-error { background: #fce8e8; color: #d63638; }
				.badge-pending { background: #f0f0f1; color: #646970; }
				.actions { display: flex; gap: 8px; }
				.back { margin-top: 20px; }
				.alert { padding: 12px 16px; border-radius: 4px; margin-bottom: 16px; font-size: var(--sz-text-base); }
				.alert-info { background: #d5e8f5; border-left: 4px solid #2271b1; }
				.alert-warn { background: #fcf9e8; border-left: 4px solid #dba617; }
			</style>
		</head>
		<body>
		<div class="wrap">
			<h1>🖨️ Impressão em Lote — <?php echo count( $order_ids ); ?> pedido(s)</h1>

			<?php if ( $batch_url ) : ?>
				<div class="batch-bar">
					<span class="count"><?php echo count( $pdf_paths ); ?> PDF(s) disponíveis para impressão unificada</span>
					<a href="<?php echo esc_url( $batch_url ); ?>" target="_blank" class="btn btn-success">
						⬇ Baixar PDF Unificado (Todos em um arquivo)
					</a>
					<button onclick="window.open('<?php echo esc_url( $batch_url ); ?>', '_blank'); setTimeout(() => window.print(), 1000)" class="btn btn-primary">
						🖨 Abrir e Imprimir
					</button>
				</div>
			<?php elseif ( ! empty( $missing ) ) : ?>
				<div class="alert alert-warn">
					<?php echo count( $missing ); ?> pedido(s) ainda não têm PDF gerado. Gere as etiquetas primeiro usando "ME: Gerar etiquetas" e aguarde o processamento em background.
				</div>
			<?php endif; ?>

			<?php if ( empty( $pdf_paths ) && empty( array_filter( array_column( $orders_data, 'print_url' ) ) ) ) : ?>
				<div class="alert alert-info">
					Nenhum pedido selecionado tem etiqueta gerada ainda. Selecione os pedidos, use "ME: Gerar etiquetas" e aguarde até 5 minutos para o processamento automático.
				</div>
			<?php endif; ?>

			<div class="table-wrap">
				<table>
					<thead>
						<tr>
							<th>Pedido</th>
							<th>Cliente</th>
							<th>Status Etiqueta</th>
							<th>Ações</th>
						</tr>
					</thead>
					<tbody>
					<?php foreach ( $orders_data as $row ) : ?>
						<tr>
							<td><strong>#<?php echo esc_html( $row['number'] ); ?></strong></td>
							<td><?php echo esc_html( $row['customer'] ); ?></td>
							<td>
								<?php if ( $row['pdf_url'] ) : ?>
									<span class="badge badge-ok">PDF pronto</span>
								<?php elseif ( $row['print_url'] ) : ?>
									<span class="badge badge-warn">Link disponível</span>
								<?php elseif ( 'error' === $row['status'] ) : ?>
									<span class="badge badge-error">Erro</span>
								<?php elseif ( 'processing' === $row['status'] ) : ?>
									<span class="badge badge-warn">Processando...</span>
								<?php elseif ( $row['has_label'] ) : ?>
									<span class="badge badge-warn">Aguardando</span>
								<?php else : ?>
									<span class="badge badge-pending">Sem etiqueta</span>
								<?php endif; ?>
							</td>
							<td>
								<div class="actions">
									<?php if ( $row['pdf_url'] ) : ?>
										<a href="<?php echo esc_url( $row['pdf_url'] ); ?>" target="_blank" class="btn btn-success">⬇ PDF</a>
									<?php endif; ?>
									<?php if ( $row['print_url'] ) : ?>
										<a href="<?php echo esc_url( $row['print_url'] ); ?>" target="_blank" class="btn btn-secondary">🖨 Imprimir</a>
									<?php endif; ?>
									<?php if ( ! $row['has_label'] ) : ?>
										<a href="<?php echo esc_url( get_edit_post_link( $row['id'] ) ?: admin_url( 'post.php?post=' . $row['id'] . '&action=edit' ) ); ?>" class="btn btn-secondary">Ver pedido</a>
									<?php endif; ?>
								</div>
							</td>
						</tr>
					<?php endforeach; ?>
					</tbody>
				</table>
			</div>

			<div class="back">
				<a href="javascript:history.back()" class="btn btn-secondary">← Voltar para pedidos</a>
			</div>
		</div>
		</body>
		</html>
		<?php
	}

	/**
	 * AJAX: retorna status de um pedido para polling de progresso no painel
	 */
	public function ajax_bulk_status() {
		if ( ! current_user_can( 'manage_woocommerce' ) ) {
			wp_send_json_error( 'Sem permissão.' );
		}

		if ( ! isset( $_POST['order_ids'] ) ) {
			wp_send_json_error( 'Nenhum pedido informado.' );
		}

		$order_ids = array_map( 'absint', (array) $_POST['order_ids'] );
		$result    = [];

		foreach ( $order_ids as $order_id ) {
			$order = wc_get_order( $order_id );
			if ( ! $order ) {
				continue;
			}

			$result[] = [
				'order_id'     => $order_id,
				'label_status' => $order->get_meta( '_melhor_envio_label_status' ) ?: 'pending',
				'label_error'  => $order->get_meta( '_melhor_envio_label_error' ),
				'pdf_url'      => $order->get_meta( '_melhor_envio_pdf_local_url' ),
				'print_url'    => $order->get_meta( '_melhor_envio_print_url' ),
			];
		}

		wp_send_json_success( $result );
	}
}
