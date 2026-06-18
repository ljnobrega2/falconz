<?php

namespace WC_MelhorEnvio\Portal;

use WC_MelhorEnvio\Api\Cancel_Label;

/**
 * Portal_Orders
 *
 * Busca e gerencia pedidos para o painel do cliente.
 * Vinculação: shipping_class_id do usuário do portal → _senderzz_product_shipping_class_id dos pedidos.
 */
class Portal_Orders {

	// Status que permitem cancelamento — apenas enquanto o pedido está aguardando pagamento.
	// Após aprovação/etiqueta gerada, o cancelamento não é mais disponível ao produtor.
	const CANCELLABLE_STATUSES = [ 'on-hold' ];

	// Status que permitem reprocessamento de etiqueta
	const RETRYABLE_STATUSES = [ 'saldoinsuficiente', 'erro' ];

	// Status que bloqueiam cancelamento e permitem apenas suspensão
	const SUSPENSION_STATUSES = [ 'enviado', 'emretirada', 'acaminho', 'coletado' ];

	// Status que permitem relato de perda/extravio
	const LOSS_STATUSES = [ 'enviado', 'emretirada', 'acaminho', 'coletado', 'asuspender' ];

	/**
	 * Retorna todos os pedidos do cliente.
	 */
	public static function get_orders( $shipping_class_id, int $page = 1, int $per_page = 20 ): array {
		// Aceita int (legado) ou array de IDs (multi-class).
		$class_ids = is_array( $shipping_class_id ) ? array_map( 'intval', $shipping_class_id ) : [ (int) $shipping_class_id ];
		$class_ids = array_values( array_unique( array_filter( $class_ids, fn($id) => $id >= 0 ) ) );
		if ( empty( $class_ids ) ) return [];
		$cache_key = 'sz_orders_' . implode( '_', $class_ids ) . '_p' . $page . '_' . $per_page;
		$cached    = get_transient( $cache_key );
		if ( is_array( $cached ) ) return $cached;

		$args = [
			'limit'      => $per_page,
			'offset'     => ( $page - 1 ) * $per_page,
			'orderby'    => 'date',
			'order'      => 'DESC',
			'return'     => 'ids',
			'meta_query' => [
				[
					'key'     => '_senderzz_product_shipping_class_id',
					'value'   => $class_ids,
					'compare' => count( $class_ids ) === 1 ? '=' : 'IN',
				],
			],
		];

		$order_ids = wc_get_orders( $args );
		$orders    = [];

		foreach ( $order_ids as $order_id ) {
			$order = wc_get_order( $order_id );
			if ( $order && function_exists( 'senderzz_force_delete_if_draft_order' ) && senderzz_force_delete_if_draft_order( $order ) ) continue;
			if ( ! $order ) continue;
			$orders[] = self::format_order( $order );
		}

		set_transient( $cache_key, $orders, 60 ); // 60s — suficiente para amortecer, curto para não exibir stale
		return $orders;
	}

	/**
	 * Invalida o cache de orders de uma classe. Chamar após approve/cancel/status change.
	 */
	public static function invalidate_cache( $shipping_class_id ): void {
		// Aceita int (legado) ou array de IDs (multi-class).
		$class_ids = is_array( $shipping_class_id ) ? array_map( 'intval', $shipping_class_id ) : [ (int) $shipping_class_id ];
		// Invalida cache para cada classe individualmente E para a combinação multi-class.
		$all_keys = [];
		foreach ( $class_ids as $cid ) {
			$all_keys[] = (string) $cid;
		}
		if ( count( $class_ids ) > 1 ) {
			$all_keys[] = implode( '_', $class_ids );
		}
		foreach ( $all_keys as $key ) {
			foreach ( [ 20, 50, 100 ] as $per_page ) {
				for ( $p = 1; $p <= 5; $p++ ) {
					delete_transient( 'sz_orders_' . $key . '_p' . $p . '_' . $per_page );
				}
			}
		}
	}

	/**
	 * Retorna um pedido específico se pertencer ao cliente.
	 */
	public static function get_order( int $order_id, $shipping_class_id ): ?array {
		$order = wc_get_order( $order_id );
		if ( ! $order ) return null;

		$class     = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
		$class_ids = is_array( $shipping_class_id ) ? array_map( 'intval', $shipping_class_id ) : [ (int) $shipping_class_id ];
		if ( ! in_array( $class, $class_ids, true ) ) return null;

		return self::format_order( $order );
	}

	/**
	 * Aprova pedido (on-hold → aprovado), disparando o pipeline automaticamente.
	 */
	public static function approve_order( int $order_id, $shipping_class_id ): array {
		$order = wc_get_order( $order_id );
		if ( ! $order ) {
			return [ 'success' => false, 'message' => 'Pedido não encontrado.' ];
		}

				$class     = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
		$class_ids = is_array( $shipping_class_id ) ? array_map( 'intval', $shipping_class_id ) : [ (int) $shipping_class_id ];
		if ( ! in_array( $class, $class_ids, true ) ) {
			return [ 'success' => false, 'message' => 'Pedido não pertence a esta conta.' ];
		}

		if ( ! $order->has_status( 'on-hold' ) ) {
			return [ 'success' => false, 'message' => 'Pedido não está em aguardando aprovação.' ];
		}

		$order->update_status( 'aprovado', 'Cliente autorizou emissão de etiqueta via painel.' );

		// Relê o pedido do banco para verificar o status real após o hook de débito.
		$order = wc_get_order( $order_id );
		if ( ! $order ) {
			return [ 'success' => false, 'message' => 'Erro ao verificar resultado da aprovação.' ];
		}

		$final_status = $order->get_status();

		// Se o débito falhou, o hook reverteu o status. Informa o motivo correto.
		if ( $final_status !== 'aprovado' ) {
			$error_note = '';
			foreach ( array_reverse( $order->get_customer_order_notes() ?: [] ) as $note ) {
				if ( strpos( $note->comment_content ?? '', 'Senderzz' ) !== false ) {
					$error_note = wp_strip_all_tags( $note->comment_content );
					break;
				}
			}
			// Busca nas notas internas também
			if ( ! $error_note ) {
				$notes = wc_get_order_notes( [ 'order_id' => $order_id, 'type' => 'internal', 'limit' => 5 ] );
				foreach ( array_reverse( $notes ?: [] ) as $note ) {
					if ( strpos( $note->content ?? '', 'Senderzz' ) !== false ) {
						$error_note = wp_strip_all_tags( $note->content );
						break;
					}
				}
			}
			$msg = $error_note ?: 'Saldo insuficiente na carteira para emissão da etiqueta.';
			return [ 'success' => false, 'message' => $msg ];
		}

		self::invalidate_cache( $shipping_class_id ); // v-scale
		return [ 'success' => true, 'message' => 'Pedido aprovado. Etiqueta sendo gerada.' ];
	}

	/**
	 * Cancela pedido — lógica diferente por status.
	 */
	public static function cancel_order( int $order_id, $shipping_class_id ): array {
		$order = wc_get_order( $order_id );
		if ( ! $order ) {
			return [ 'success' => false, 'message' => 'Pedido não encontrado.' ];
		}

				$class     = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
		$class_ids = is_array( $shipping_class_id ) ? array_map( 'intval', $shipping_class_id ) : [ (int) $shipping_class_id ];
		if ( ! in_array( $class, $class_ids, true ) ) {
			return [ 'success' => false, 'message' => 'Pedido não pertence a esta conta.' ];
		}

		$status = $order->get_status();

		// REQ6: pedidos COD Motoboy seguem regra própria — cancelamento só quando
		// EMBALADO e sem custo para ninguém (produtor, Senderzz e afiliado).
		if ( function_exists( 'senderzz_order_is_motoboy_cod' ) && senderzz_order_is_motoboy_cod( $order ) ) {
			if ( ! function_exists( 'senderzz_motoboy_can_cancel' ) || ! senderzz_motoboy_can_cancel( $order ) ) {
				return [ 'success' => false, 'message' => 'O cancelamento só é permitido enquanto o pedido está EMBALADO.' ];
			}
			$res = senderzz_motoboy_cancel_order( $order_id );
			self::invalidate_cache( $shipping_class_id );
			return $res;
		}

		// Em cancelamento já é um pedido de cancelamento em andamento.
		// Não deixa aparecer/rodar a ação novamente.
		if ( $status === 'emcancelamento' ) {
			return [ 'success' => false, 'message' => 'Pedido já está em cancelamento.' ];
		}

		if ( ! in_array( $status, self::CANCELLABLE_STATUSES, true ) ) {
			return [ 'success' => false, 'message' => 'Este pedido não pode ser cancelado no status atual.' ];
		}

		// Se está em aprovado e tem etiqueta, cancela no ME também
		$has_label = (bool) $order->get_meta( '_melhor_envio_item_id' );

		// Com etiqueta ativa → redireciona para emcancelamento (aguarda confirmação ME)
		if ( $has_label && $status !== 'emcancelamento' ) {
			$order->update_status( 'emcancelamento', 'Cliente solicitou cancelamento via painel. Aguardando confirmação do Melhor Envio.' );
			return [ 'success' => true, 'message' => 'Solicitação enviada ao Melhor Envio. O pedido será cancelado assim que confirmado.' ];
		}

		// Sem etiqueta ou já em emcancelamento → cancela direto
		$order->update_status( 'cancelled', 'Cliente solicitou cancelamento via painel.' );
		self::invalidate_cache( $shipping_class_id ); // v-scale
		return [ 'success' => true, 'message' => 'Pedido cancelado com sucesso.' ];
	}

	/**
	 * Reprocessa etiqueta de pedido com saldo insuficiente.
	 * Volta para "aprovado" e deixa o pipeline tentar emitir novamente.
	 */
	public static function retry_label( int $order_id, $shipping_class_id ): array {
		$order = wc_get_order( $order_id );
		if ( ! $order ) {
			return [ 'success' => false, 'message' => 'Pedido não encontrado.' ];
		}

				$class     = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
		$class_ids = is_array( $shipping_class_id ) ? array_map( 'intval', $shipping_class_id ) : [ (int) $shipping_class_id ];
		if ( ! in_array( $class, $class_ids, true ) ) {
			return [ 'success' => false, 'message' => 'Pedido não pertence a esta conta.' ];
		}

		$status = $order->get_status();
		if ( ! in_array( $status, self::RETRYABLE_STATUSES, true ) ) {
			return [ 'success' => false, 'message' => 'Reprocessamento disponível apenas para pedidos com saldo insuficiente ou erro de etiqueta.' ];
		}

		if ( $status === 'erro' && $order->get_meta( '_senderzz_wallet_debited' ) === 'yes' ) {
			try {
				$order->add_order_note( 'Senderzz: reprocessamento de etiqueta solicitado pelo produtor. Carteira já debitada, sem novo débito.' );

				if ( function_exists( 'senderzz_operator_ensure_label_pdf' ) ) {
					senderzz_operator_ensure_label_pdf( $order );
				} elseif ( class_exists( '\WC_MelhorEnvio\Pipeline\Label_Pipeline' ) ) {
					$pipeline = new \WC_MelhorEnvio\Pipeline\Label_Pipeline();
					$pipeline->process( $order, true );
				} else {
					throw new \Exception( 'Pipeline de etiqueta indisponível.' );
				}

				$order = wc_get_order( $order_id );
				if ( ! $order ) {
					return [ 'success' => false, 'message' => 'Etiqueta reprocessada, mas não foi possível recarregar o pedido.' ];
				}

				$order->delete_meta_data( '_melhor_envio_label_error' );
				$order->update_meta_data( '_melhor_envio_label_status', 'downloaded' );
				$order->save();
				// Senderzz: não usamos mais o status "separado".
				// Reprocessa etiqueta e mantém/retorna o pedido para APROVADO.
				if ( ! in_array( $order->get_status(), [ 'aprovado', 'approved' ], true ) ) {
					$order->update_status( 'aprovado', 'Senderzz: etiqueta reprocessada com sucesso sem novo débito de carteira.' );
				} else {
					$order->add_order_note( 'Senderzz: etiqueta reprocessada com sucesso. Pedido mantido como aprovado.' );
					$order->save();
				}

				return [ 'success' => true, 'message' => 'Etiqueta reprocessada com sucesso. Pedido mantido como aprovado.' ];
			} catch ( \Throwable $e ) {
				$order = wc_get_order( $order_id );
				if ( $order ) {
					$order->update_meta_data( '_melhor_envio_label_status', 'error' );
					$order->update_meta_data( '_melhor_envio_label_error', $e->getMessage() );
					$order->save();
					$order->update_status( 'erro', 'Senderzz: reprocessamento de etiqueta falhou. Carteira mantida debitada.' );
					$order->add_order_note( 'Senderzz: reprocessamento de etiqueta falhou. Carteira mantida debitada para nova tentativa ou cancelamento. Erro: ' . $e->getMessage() );
				}
				return [ 'success' => false, 'message' => 'Não foi possível reprocessar a etiqueta agora. Tente novamente ou cancele o pedido para estornar a carteira. Erro: ' . $e->getMessage() ];
			}
		}

		$existing_tx = (int) $order->get_meta( '_senderzz_wallet_reserve_tx' );
		if ( $existing_tx && function_exists( 'tpc_liberar_reserva' ) ) {
			tpc_liberar_reserva( $existing_tx, 'reprocessamento solicitado pelo produtor' );
		}

		$order->delete_meta_data( '_senderzz_wallet_debited' );
		$order->delete_meta_data( '_senderzz_wallet_debit_tx' );
		$order->delete_meta_data( '_senderzz_wallet_debit_value' );
		$order->delete_meta_data( '_senderzz_wallet_debit_context' );
		$order->delete_meta_data( '_senderzz_wallet_reserve_released' );
		$order->delete_meta_data( '_senderzz_wallet_reserve_tx' );
		$order->delete_meta_data( '_senderzz_wallet_reserved' );
		$order->delete_meta_data( '_senderzz_wallet_reserved_value' );
		$order->save();

		delete_transient( 'senderzz_debit_lock_' . $order_id );

		$order = wc_get_order( $order_id );
		if ( ! $order ) {
			return [ 'success' => false, 'message' => 'Erro ao recarregar pedido.' ];
		}

		$order->update_status( 'aprovado', 'Produtor solicitou reprocessamento de etiqueta via painel.' );
		$order->add_order_note( 'Reprocessamento solicitado pelo produtor. Aguardando nova tentativa de emissão.' );

		$order = wc_get_order( $order_id );
		if ( $order && ! $order->get_meta( '_senderzz_wallet_debited' ) && $order->get_status() !== 'aprovado' ) {
			return [ 'success' => false, 'message' => 'Saldo insuficiente na carteira para reprocessamento.' ];
		}

		return [ 'success' => true, 'message' => 'Pedido reenviado para emissão de etiqueta.' ];
	}
	/**
	 * Processa ações em massa sobre uma lista de pedidos.
	 * Ações suportadas: approve, cancel, retry_label.
	 */
	public static function bulk_action( string $action, array $order_ids, $shipping_class_id ): array {
		$results = [ 'success' => [], 'failed' => [] ];


		// Cap de segurança: máximo 10 pedidos por requisição síncrona.
		// Lotes maiores são enfileirados via Action Scheduler.
		$bulk_sync_limit = 10;
		$sync_ids  = array_slice( $order_ids, 0, $bulk_sync_limit );
		$async_ids = array_slice( $order_ids, $bulk_sync_limit );
		if ( ! empty( $async_ids ) && function_exists( 'as_enqueue_async_action' ) ) {
			foreach ( array_chunk( $async_ids, $bulk_sync_limit ) as $chunk ) {
				as_enqueue_async_action( 'senderzz_bulk_action_async', [
					'action'            => $action,
					'order_ids'         => $chunk,
					'shipping_class_id' => $shipping_class_id,
				], 'senderzz' );
			}
			$results['queued'] = count( $async_ids );
		}
		$order_ids = $sync_ids;

		foreach ( $order_ids as $order_id ) {
			$order_id = (int) $order_id;
			if ( $order_id <= 0 ) continue;

			$result = match ( $action ) {
				'approve'      => self::approve_order( $order_id, $shipping_class_id ),
				'cancel'       => self::cancel_order( $order_id, $shipping_class_id ),
				'retry_label'  => self::retry_label( $order_id, $shipping_class_id ),
				default        => [ 'success' => false, 'message' => 'Ação inválida.' ],
			};

			if ( $result['success'] ) {
				$results['success'][] = $order_id;
			} else {
				$results['failed'][] = [ 'id' => $order_id, 'reason' => $result['message'] ];
			}
		}

		$total_ok   = count( $results['success'] );
		$total_fail = count( $results['failed'] );
		$message    = "{$total_ok} pedido(s) processado(s) com sucesso.";
		if ( $total_fail ) {
			$message .= " {$total_fail} pedido(s) com erro.";
		}

		return [
			'success' => $total_ok > 0,
			'message' => $message,
			'detail'  => $results,
		];
	}

	/**
	 * Relata perda/extravio — altera o pedido para o status extravio.
	 */
	public static function report_loss( int $order_id, $shipping_class_id ): array {
		$order = wc_get_order( $order_id );
		if ( ! $order ) {
			return [ 'success' => false, 'message' => 'Pedido não encontrado.' ];
		}

				$class     = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
		$class_ids = is_array( $shipping_class_id ) ? array_map( 'intval', $shipping_class_id ) : [ (int) $shipping_class_id ];
		if ( ! in_array( $class, $class_ids, true ) ) {
			return [ 'success' => false, 'message' => 'Pedido não pertence a esta conta.' ];
		}

		$status = $order->get_status();
		if ( ! in_array( $status, self::LOSS_STATUSES, true ) ) {
			return [ 'success' => false, 'message' => 'Relato de perda não disponível para o status atual.' ];
		}

		$order->update_status( 'extravio', 'Produtor relatou perda/extravio via painel Senderzz.' );
		$order->add_order_note( 'Relato de perda/extravio registrado pelo produtor no painel Senderzz.' );

		return [ 'success' => true, 'message' => 'Perda/extravio registrado. O pedido foi movido para Extravio.' ];
	}

	/** Compatibilidade com versões anteriores do painel. */
	public static function request_suspension( int $order_id, $shipping_class_id ): array {
		return self::report_loss( $order_id, $shipping_class_id );
	}
/**
	 * Abre um chamado de suporte para suspensão/problema no envio.
	 * Envia e-mail ao admin do WooCommerce com todos os dados do pedido.
	 *
	 * @param int    $order_id          ID do pedido WooCommerce.
	 * @param int    $shipping_class_id Shipping class do produtor (validação de ownership).
	 * @param string $reason            Motivo descrito pelo produtor (livre).
	 * @return array { success: bool, message: string }
	 */
	public static function request_support( int $order_id, $shipping_class_id, string $reason = '' ): array {
		$order = wc_get_order( $order_id );
		if ( ! $order ) {
			return [ 'success' => false, 'message' => 'Pedido não encontrado.' ];
		}

				$class     = (int) $order->get_meta( '_senderzz_product_shipping_class_id' );
		$class_ids = is_array( $shipping_class_id ) ? array_map( 'intval', $shipping_class_id ) : [ (int) $shipping_class_id ];
		if ( ! in_array( $class, $class_ids, true ) ) {
			return [ 'success' => false, 'message' => 'Pedido não pertence a esta conta.' ];
		}

		$item_id      = (string) $order->get_meta( '_melhor_envio_item_id' );
		$tracking     = (string) $order->get_meta( '_melhor_envio_tracking_code' );
		$label_status = (string) $order->get_meta( '_melhor_envio_label_status' );
		$shipping_to  = $order->get_address( 'shipping' );
		$billing      = $order->get_address( 'billing' );

		$items_text = '';
		foreach ( $order->get_items() as $item ) {
			$items_text .= sprintf( "  - %s (x%d) — %s\n", $item->get_name(), $item->get_quantity(), wc_price( $item->get_total() ) );
		}

		$reason_clean = sanitize_textarea_field( $reason );
		$admin_email  = get_option( 'woocommerce_email_recipient_new_order', get_option( 'admin_email' ) );
		$subject      = sprintf( '[Senderzz] Chamado de Suporte — Pedido #%d', $order_id );

		$body  = "=== CHAMADO DE SUPORTE SENDERZZ ===\n\n";
		$body .= sprintf( "Pedido:         #%d\n", $order_id );
		$body .= sprintf( "Data:           %s\n", $order->get_date_created() ? $order->get_date_created()->date( 'd/m/Y H:i' ) : 'N/D' );
		$body .= sprintf( "Status atual:   %s\n", $order->get_status() );
		$body .= sprintf( "Total:          %s\n\n", wp_strip_all_tags( wc_price( $order->get_total() ) ) );
		$body .= "--- ETIQUETA ---\n";
		$body .= sprintf( "Item ID ME:     %s\n", $item_id ?: 'Sem etiqueta' );
		$body .= sprintf( "Rastreio:       %s\n", $tracking ?: 'N/D' );
		$body .= sprintf( "Status ME:      %s\n\n", $label_status ?: 'N/D' );
		$body .= "--- DESTINATÁRIO ---\n";
		$body .= sprintf( "Nome:           %s %s\n", $shipping_to['first_name'] ?? '', $shipping_to['last_name'] ?? '' );
		$body .= sprintf( "Endereço:       %s, %s — %s/%s  CEP: %s\n\n",
			$shipping_to['address_1'] ?? '', $shipping_to['address_2'] ?? '',
			$shipping_to['city'] ?? '', $shipping_to['state'] ?? '', $shipping_to['postcode'] ?? ''
		);
		$body .= "--- PRODUTOR ---\n";
		$body .= sprintf( "Nome:           %s %s\n", $billing['first_name'] ?? '', $billing['last_name'] ?? '' );
		$body .= sprintf( "E-mail:         %s\n", $billing['email'] ?? '' );
		$body .= sprintf( "Telefone:       %s\n\n", $billing['phone'] ?? '' );
		$body .= "--- ITENS ---\n" . $items_text . "\n";
		$body .= "--- MOTIVO INFORMADO PELO PRODUTOR ---\n";
		$body .= ( $reason_clean ?: '(Não informado)' ) . "\n\n";
		$body .= sprintf( "Chamado aberto em: %s\n", current_time( 'mysql' ) );
		$body .= sprintf( "Ver pedido: %s\n", admin_url( 'post.php?post=' . $order_id . '&action=edit' ) );

		$sent = wp_mail( $admin_email, $subject, $body );

		$note = sprintf(
			'Senderzz: chamado de suporte aberto pelo produtor. Motivo: %s. E-mail %s para %s.',
			$reason_clean ?: '(não informado)',
			$sent ? 'enviado' : 'NÃO enviado (verifique SMTP)',
			$admin_email
		);
		$order->add_order_note( $note );
		$order->update_meta_data( '_senderzz_support_request_at', current_time( 'mysql' ) );
		$order->update_meta_data( '_senderzz_support_request_reason', $reason_clean );
		$order->save();

		if ( ! $sent ) {
			return [ 'success' => false, 'message' => 'Não foi possível enviar o e-mail. Verifique as configurações de SMTP.' ];
		}

		return [ 'success' => true, 'message' => 'Chamado enviado com sucesso. Nossa equipe entrará em contato em breve.' ];
	}

	/**
	 * Formata pedido para exibição no painel — sem custo real de frete/margem.
	 */
	public static function format_order( \WC_Order $order ): array {
		$status = $order->get_status();

		// Determina ações disponíveis
		$can_approve   = $status === 'on-hold';
		$can_cancel    = in_array( $status, self::CANCELLABLE_STATUSES, true );
		// REQ6: COD Motoboy pode cancelar somente quando EMBALADO.
		if ( ! $can_cancel && function_exists( 'senderzz_motoboy_can_cancel' ) && senderzz_motoboy_can_cancel( $order ) ) {
			$can_cancel = true;
		}
		$can_retry     = in_array( $status, self::RETRYABLE_STATUSES, true );
		$can_suspend   = in_array( $status, self::LOSS_STATUSES, true );
		$has_label     = (bool) $order->get_meta( '_melhor_envio_item_id' );
		$label_status  = (string) $order->get_meta( '_melhor_envio_label_status' );

		// Itens do pedido
		$items = [];
		foreach ( $order->get_items() as $item ) {
			$product = $item->get_product();
			$items[] = [
				'name'     => $item->get_name(),
				'qty'      => $item->get_quantity(),
				'subtotal' => wc_price( $item->get_subtotal() ),
				'total'    => wc_price( $item->get_total() ),
				'image'    => $product ? wp_get_attachment_image_url( $product->get_image_id(), 'thumbnail' ) : null,
				'_senderzz_offer_name'  => (string) $item->get_meta( '_senderzz_offer_name', true ),
				'_senderzz_offer_value' => (float) $item->get_meta( '_senderzz_offer_value', true ),
				'_senderzz_offer_token' => (string) $item->get_meta( '_senderzz_offer_token', true ),
			];
		}

		// Rastreio
		$tracking_codes = wc_melhor_envio_get_tracking_codes( $order );

		// Transportadora e prazo
		$carrier       = '';
		$delivery_time = '';
		foreach ( $order->get_items( 'shipping' ) as $s ) {
			$carrier       = $carrier ?: $s->get_name();
			$delivery_time = $delivery_time ?: (string) $s->get_meta( 'melhorenvio_delivery_time' );
		}

		$shipping_total = (float) $order->get_shipping_total() + (float) $order->get_shipping_tax();
		$is_motoboy_order = (string) $order->get_meta( '_senderzz_delivery_mode', true ) === 'motoboy';
		if ( ! $is_motoboy_order ) {
			foreach ( $order->get_items( 'shipping' ) as $_mb_item ) {
				if ( stripos( (string) $_mb_item->get_method_id(), 'sz_motoboy' ) !== false || stripos( (string) $_mb_item->get_name(), 'motoboy' ) !== false ) { $is_motoboy_order = true; break; }
			}
		}
		if ( $is_motoboy_order ) {
			$mb_fee = (float) $order->get_meta( '_sz_mb_taxa_total', true );
			if ( $mb_fee <= 0 ) {
				$mb_fee = (float) $order->get_meta( '_sz_mb_taxa_entrega', true ) + (float) $order->get_meta( '_sz_mb_taxa_manuseio', true ) + (float) $order->get_meta( '_sz_mb_taxa_adicional', true );
			}
			if ( $mb_fee > 0 ) { $shipping_total = round( $mb_fee, 2 ); }
		}
		$total_without_shipping = max( 0, (float) $order->get_total() - ( $is_motoboy_order ? 0 : $shipping_total ) );
		$order_total_raw = (float) $order->get_total();

		// Links personalizados Senderzz: no portal o produto deve ser o nome comercial do kit,
		// e o valor deve ser o valor fixo cadastrado no link, sem multiplicar por componentes/quantidade.
		$senderzz_offer_name  = sanitize_text_field( (string) $order->get_meta( '_senderzz_offer_name', true ) );
		$senderzz_offer_value = (float) $order->get_meta( '_senderzz_offer_value', true );
		$senderzz_offer_token = sanitize_text_field( (string) $order->get_meta( '_senderzz_offer_token', true ) );

		foreach ( $items as $senderzz_item ) {
			if ( $senderzz_offer_name === '' && ! empty( $senderzz_item['_senderzz_offer_name'] ) ) {
				$senderzz_offer_name = sanitize_text_field( (string) $senderzz_item['_senderzz_offer_name'] );
			}
			if ( $senderzz_offer_value <= 0 && ! empty( $senderzz_item['_senderzz_offer_value'] ) ) {
				$senderzz_offer_value = (float) $senderzz_item['_senderzz_offer_value'];
			}
			if ( $senderzz_offer_token === '' && ! empty( $senderzz_item['_senderzz_offer_token'] ) ) {
				$senderzz_offer_token = sanitize_text_field( (string) $senderzz_item['_senderzz_offer_token'] );
			}
		}

		$products_label = $senderzz_offer_name !== '' ? $senderzz_offer_name : implode( ', ', array_map( function( $i ) {
			$qty = isset( $i['qty'] ) ? (int) $i['qty'] : 1;
			return $i['name'] . ( $qty > 1 ? ' ×' . $qty : '' );
		}, $items ) );

		if ( $senderzz_offer_value > 0 ) {
			$total_without_shipping = $senderzz_offer_value;
		}

		$sz_affiliate_id = (int) ( $order->get_meta( '_sz_affiliate_id', true ) ?: $order->get_meta( '_sz_affiliate_ref', true ) );
		$sz_affiliate_name = '';
		$sz_affiliate_commission_pct = (float) $order->get_meta( '_sz_aff_commission_pct', true );
		$sz_affiliate_commission = (float) $order->get_meta( '_sz_aff_commission', true ); // valor líquido (afiliado recebe)
		$sz_affiliate_commission_gross = (float) $order->get_meta( '_sz_aff_commission_gross', true ); // bruto (desconto do produtor)
		if ( $sz_affiliate_id && function_exists( 'sz_aff_get_affiliate_row' ) ) {
			$sz_aff_row = sz_aff_get_affiliate_row( $sz_affiliate_id );
			if ( is_array( $sz_aff_row ) ) {
				if ( $sz_affiliate_commission_pct <= 0 ) $sz_affiliate_commission_pct = (float) ( $sz_aff_row['commission_pct'] ?? 0 );
				$sz_aff_user = ! empty( $sz_aff_row['user_id'] ) ? get_user_by( 'id', (int) $sz_aff_row['user_id'] ) : false;
				if ( $sz_aff_user ) $sz_affiliate_name = $sz_aff_user->display_name ?: $sz_aff_user->user_email;
			}
		}

		// Se a comissão ainda não foi persistida no pedido, calcula para exibição imediata.
		// Isso evita que pedido agendado/novo apareça no produtor sem desconto e no afiliado sem valor líquido.
		$sz_aff_base = $senderzz_offer_value > 0 ? $senderzz_offer_value : (float) $order->get_total();
		if ( $sz_affiliate_id > 0 && $sz_affiliate_commission <= 0 && $sz_affiliate_commission_pct > 0 ) {
			$sz_affiliate_commission = round( max( 0, $sz_aff_base ) * $sz_affiliate_commission_pct / 100, 2 );
		}
		// Garantir gross disponível para cálculo correto do produtor
		if ( $sz_affiliate_id > 0 && $sz_affiliate_commission_gross <= 0 && $sz_affiliate_commission_pct > 0 ) {
			$sz_affiliate_commission_gross = round( max( 0, $sz_aff_base ) * $sz_affiliate_commission_pct / 100, 2 );
		}
		if ( $sz_affiliate_commission_gross <= 0 ) {
			$sz_affiliate_commission_gross = $sz_affiliate_commission; // último fallback
		}

		$data = [
			'id'             => $order->get_id(),
			'number'         => $order->get_order_number(),
			'status'         => $status,
			'status_label'   => self::get_status_label( $status ),
			'date'           => $order->get_date_created() ? $order->get_date_created()->date_i18n( 'd/m/Y H:i' ) : '',
			'date_machine'   => $order->get_date_created() ? $order->get_date_created()->date( 'Y-m-d H:i:s' ) : '',
			'product_name'   => $products_label,
			'senderzz_offer_name'  => $senderzz_offer_name,
			'senderzz_offer_value' => $senderzz_offer_value,
			'senderzz_offer_token' => $senderzz_offer_token,
			'affiliate_id' => $sz_affiliate_id,
			'affiliate_name' => $sz_affiliate_name,
			'affiliate_commission_pct' => $sz_affiliate_commission_pct,
			'affiliate_commission' => $sz_affiliate_commission,
			'affiliate_commission_gross' => $sz_affiliate_commission_gross,
			'total_raw'      => $order_total_raw,
			'total'          => $order->get_formatted_order_total(),
			'total_no_ship_raw' => $total_without_shipping,
			'total_no_ship'  => wc_price( $total_without_shipping ),
			'shipping_total_raw' => $shipping_total,
			'shipping_total' => wc_price( $shipping_total ),
			'items'          => $items,
			'items_count'    => count( $items ),
			'delivery_mode'  => $order->get_meta( '_senderzz_delivery_mode', true ) ?: '',
			// Taxas motoboy individuais — lidas direto do pedido (HPOS-compatible via get_meta)
			'mb_taxa_entrega'    => round( (float) $order->get_meta( '_sz_mb_taxa_entrega',    true ), 2 ),
			'mb_taxa_manuseio'   => round( (float) $order->get_meta( '_sz_mb_taxa_manuseio',   true ), 2 ),
			'mb_taxa_adicional'  => round( (float) $order->get_meta( '_sz_mb_taxa_adicional',  true ), 2 ),
			'mb_taxa_percentual' => round( (float) $order->get_meta( '_sz_mb_taxa_percentual', true ), 2 ),
			'mb_taxa_total'      => round( (float) $order->get_meta( '_sz_mb_taxa_total',      true ), 2 ),
			'shipping_name'  => $carrier,
			'delivery_time'  => $delivery_time ? $delivery_time . ' dias úteis' : '',
			'tracking_codes' => $tracking_codes,
			'pdf_url'        => $order->get_meta( '_melhor_envio_pdf_local_url' ) ?: $order->get_meta( '_melhor_envio_print_url' ),
			// Campos extras incluídos aqui para evitar N+1 em order_row (wc_get_order por linha)
			'billing_cpf'       => $order->get_meta( '_billing_cpf' ) ?: $order->get_meta( 'billing_cpf' ) ?: $order->get_meta( '_billing_cnpj' ) ?: '',
			'shipping_number'   => $order->get_meta( '_shipping_number' ) ?: $order->get_meta( '_billing_number' ) ?: '',
			'shipping_neighborhood' => $order->get_meta( '_shipping_neighborhood' ) ?: $order->get_meta( '_billing_neighborhood' ) ?: '',
			'label_status'   => $label_status,
			'has_label'      => $has_label,
			'billing'        => [
				'name'    => $order->get_formatted_billing_full_name(),
				'address' => self::format_address_without_name( $order, 'billing' ),
				'email'   => $order->get_billing_email(),
				'phone'   => $order->get_billing_phone(),
			],
			'shipping'       => [
				'address' => self::format_address_without_name( $order, 'shipping' ),
			],
			'actions'        => [
				'can_approve' => $can_approve,
				'can_cancel'  => $can_cancel,
				'can_retry'   => $can_retry,
				'can_suspend' => $can_suspend,
			],
		];

		if ( $is_motoboy_order || (string) $order->get_meta( '_senderzz_motoboy_flow_status', true ) !== '' ) {
			global $wpdb;
			$mb_table = $wpdb->prefix . 'sz_motoboy_pedidos';
			if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $mb_table ) ) ) {
				$mb = $wpdb->get_row( $wpdb->prepare(
					"SELECT status, ts_embalado, ts_em_rota, ts_a_caminho, ts_entregue, ts_frustrado, created_at, updated_at FROM {$mb_table} WHERE wc_order_id = %d ORDER BY id DESC LIMIT 1",
					$order->get_id()
				), ARRAY_A );
				if ( $mb ) {
					$mb_status = sanitize_key( (string) ( $mb['status'] ?? '' ) );
					if ( $mb_status === 'aprovado' ) { $mb_status = 'agendado'; }
					if ( $mb_status !== '' ) {
						$data['status'] = $mb_status;
						$data['status_label'] = self::get_status_label( $mb_status );
						$data['motoboy_status'] = $mb_status;
						$data['delivery_mode'] = 'motoboy';
					}
					$dates = [
						'agendado'  => $mb['created_at'] ?? '',
						'embalado'  => $mb['ts_embalado'] ?? '',
						'em_rota'   => $mb['ts_em_rota'] ?? '',
						'a_caminho' => $mb['ts_a_caminho'] ?? '',
						'entregue'  => $mb['ts_entregue'] ?? '',
						'frustrado' => $mb['ts_frustrado'] ?? '',
					];
					foreach ( $dates as $key => $value ) {
						$data[ 'motoboy_' . $key . '_at' ] = $value ? ( function_exists( 'sz_motoboy_format_br_datetime' ) ? sz_motoboy_format_br_datetime( $value, 'd/m/Y H:i' ) : sz_br_format( $value, 'd/m/Y H:i' ) ) : '';
					}
					foreach ( [ 'frustrado','entregue','a_caminho','em_rota','embalado','agendado' ] as $key ) {
						if ( ! empty( $data[ 'motoboy_' . $key . '_at' ] ) ) { $data['date_updated'] = $data[ 'motoboy_' . $key . '_at' ]; break; }
					}
				}
			}
		}

		return $data;
	}

	/**
	 * Labels amigáveis para os status.
	 */

	/**
	 * Monta endereço formatado SEM incluir nome do cliente.
	 * get_formatted_*_address() do WC inclui nome — aqui montamos manualmente.
	 */
	public static function format_address_without_name( \WC_Order $order, string $type = 'shipping' ): string {
		$get = fn( string $field ) => $type === 'shipping'
			? $order->{'get_' . $type . '_' . $field}()
			: $order->{'get_' . $type . '_' . $field}();

		$addr1  = $order->{'get_' . $type . '_address_1'}();
		$addr2  = $order->{'get_' . $type . '_address_2'}();
		$city   = $order->{'get_' . $type . '_city'}();
		$state  = $order->{'get_' . $type . '_state'}();
		$zip    = $order->{'get_' . $type . '_postcode'}();
		$num    = $order->get_meta( '_' . $type . '_number' )   ?: $order->get_meta( '_billing_number' );
		$neigh  = $order->get_meta( '_' . $type . '_neighborhood' ) ?: $order->get_meta( '_billing_neighborhood' );

		// Fallback shipping→billing para endereços não preenchidos
		if ( $type === 'shipping' && empty( $addr1 ) ) {
			$addr1 = $order->get_billing_address_1();
			$addr2 = $order->get_billing_address_2();
			$city  = $order->get_billing_city();
			$state = $order->get_billing_state();
			$zip   = $order->get_billing_postcode();
			$num   = $num   ?: $order->get_meta( '_billing_number' );
			$neigh = $neigh ?: $order->get_meta( '_billing_neighborhood' );
		}

		$street = trim( $addr1 . ( $num ? ', ' . $num : '' ) );
		$parts  = array_filter( [ $street, trim( $addr2 ), trim( $neigh ), trim( $city . ( $state ? '/' . $state : '' ) ), $zip ] );
		$full   = implode( ', ', $parts );

		// Normalizar ALL CAPS
		if ( $full && $full === strtoupper( $full ) ) {
			$full = ucwords( strtolower( $full ) );
		}

		return $full;
	}

	public static function get_status_label( string $status ): string {
		$labels = [
			'on-hold'          => 'Aguardando aprovação',
			'agendado'         => 'Agendado',
			'aprovado'         => 'Aprovado',
			'embalado'         => 'Embalado',
			'em_rota'          => 'Em rota',
			'a_caminho'        => 'A caminho',
			'entregue'         => 'Entregue',
			'frustrado'        => 'Frustrado',
			'avariado'         => 'Avariado',
			'processing'       => 'Processando',
			'completed'        => 'Concluído',
			'cancelled'        => 'Cancelado',
			'refunded'         => 'Reembolsado',
			'failed'           => 'Falhou',
			'enviado'          => 'Enviado',
			'emretirada'       => 'Em Retirada',
			'acaminho'         => 'A Caminho',
			'coletado'         => 'Coletado',
			'asuspender'       => 'A Suspender',
			'extravio'         => 'Extravio',
			'saldoinsuficiente'=> 'Saldo Insuficiente',
		];

		return $labels[ $status ] ?? ucfirst( $status );
	}
}

// ── Handler assíncrono para bulk actions via Action Scheduler ─────────────────
add_action( 'senderzz_bulk_action_async', function( string $action, array $order_ids, int $shipping_class_id ): void {
    foreach ( $order_ids as $order_id ) {
        $order_id = (int) $order_id;
        if ( $order_id <= 0 ) continue;
        match ( $action ) {
            'approve'     => \WC_MelhorEnvio\Portal\Portal_Orders::approve_order( $order_id, $shipping_class_id ),
            'cancel'      => \WC_MelhorEnvio\Portal\Portal_Orders::cancel_order( $order_id, $shipping_class_id ),
            'retry_label' => \WC_MelhorEnvio\Portal\Portal_Orders::retry_label( $order_id, $shipping_class_id ),
            default       => null,
        };
    }
}, 10, 3 );
