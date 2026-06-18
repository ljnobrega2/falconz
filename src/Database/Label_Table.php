<?php

namespace WC_MelhorEnvio\Database;

/**
 * Label_Table
 *
 * Gerencia a tabela {prefix}_me_labels para armazenar e consultar
 * dados de etiquetas com performance, independente de post meta.
 *
 * Estrutura da tabela:
 *   id            INT AUTO_INCREMENT PRIMARY KEY
 *   order_id      BIGINT NOT NULL (FK para pedido WC)
 *   shipment_id   VARCHAR(100) — item_id do carrinho ME
 *   protocol      VARCHAR(100) — protocol/order_id do ME
 *   carrier       VARCHAR(100) — nome da transportadora
 *   status        VARCHAR(50)  — status do pipeline (processing, printed, downloaded, error)
 *   tracking      VARCHAR(255) — código de rastreio
 *   pdf_path      TEXT         — caminho local do PDF
 *   pdf_url       TEXT         — URL pública do PDF
 *   print_url     TEXT         — URL de impressão do ME
 *   error_message TEXT         — mensagem de erro se houver
 *   operator_id   BIGINT       — ID do usuário que gerou
 *   generated_at  DATETIME     — data de geração
 *   updated_at    DATETIME     — última atualização
 */
class Label_Table {

	const TABLE_VERSION = '1.1.0';
	const OPTION_KEY    = 'wc_melhor_envio_db_version';

	/**
	 * Retorna o nome completo da tabela com prefixo.
	 */
	public static function table_name(): string {
		global $wpdb;
		return $wpdb->prefix . 'me_labels';
	}

	/**
	 * install
	 * Cria ou atualiza a tabela. Chamar no activate do plugin.
	 */
	public static function install() {
		global $wpdb;

		if ( get_option( self::OPTION_KEY ) === self::TABLE_VERSION ) {
			return;
		}

		$table      = self::table_name();
		$charset_collate = $wpdb->get_charset_collate();

		$sql = "CREATE TABLE IF NOT EXISTS {$table} (
			id            BIGINT(20) UNSIGNED NOT NULL AUTO_INCREMENT,
			order_id      BIGINT(20) UNSIGNED NOT NULL,
			shipment_id   VARCHAR(100) DEFAULT '',
			protocol      VARCHAR(100) DEFAULT '',
			carrier       VARCHAR(100) DEFAULT '',
			customer_id   BIGINT(20) UNSIGNED DEFAULT 0,
			customer_email VARCHAR(190) DEFAULT '',
			shipping_real_cost DECIMAL(10,2) DEFAULT 0.00,
			shipping_charged DECIMAL(10,2) DEFAULT 0.00,
			service_fee DECIMAL(10,2) DEFAULT 0.00,
			margin DECIMAL(10,2) DEFAULT 0.00,
			carrier_id VARCHAR(100) DEFAULT '',
			service_id VARCHAR(100) DEFAULT '',
			service_name VARCHAR(190) DEFAULT '',
			destination_uf VARCHAR(10) DEFAULT '',
			destination_city VARCHAR(190) DEFAULT '',
			destination_postcode VARCHAR(20) DEFAULT '',
			postcode_prefix VARCHAR(10) DEFAULT '',
			status        VARCHAR(50)  DEFAULT 'pending',
			tracking      VARCHAR(255) DEFAULT '',
			pdf_path      TEXT,
			pdf_url       TEXT,
			print_url     TEXT,
			error_message TEXT,
			operator_id   BIGINT(20) UNSIGNED DEFAULT 0,
			generated_at  DATETIME DEFAULT NULL,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY   (id),
			UNIQUE KEY    order_id (order_id),
			KEY           status (status),
			KEY           carrier (carrier),
			KEY           customer_id (customer_id),
			KEY           destination_uf (destination_uf),
			KEY           postcode_prefix (postcode_prefix),
			KEY           generated_at (generated_at)
		) {$charset_collate};";

		require_once ABSPATH . 'wp-admin/includes/upgrade.php';
		dbDelta( $sql );

		update_option( self::OPTION_KEY, self::TABLE_VERSION );
	}

	/**
	 * upsert
	 * Insere ou atualiza o registro de uma etiqueta.
	 *
	 * @param array $data  Dados a gravar (colunas → valores)
	 * @return int|false   Rows affected ou false em erro
	 */
	public static function upsert( array $data ) {
		global $wpdb;

		$data = self::normalize_data( $data );
		if ( empty( $data['order_id'] ) ) {
			return false;
		}

		$data['updated_at'] = current_time( 'mysql' );

		$existing = self::find_by_order( absint( $data['order_id'] ) );

		if ( $existing ) {
			return $wpdb->update(
				self::table_name(),
				$data,
				[ 'order_id' => absint( $data['order_id'] ) ],
				self::format_array( $data ),
				[ '%d' ]
			);
		}

		if ( empty( $data['generated_at'] ) ) {
			$data['generated_at'] = current_time( 'mysql' );
		}

		return $wpdb->insert(
			self::table_name(),
			$data,
			self::format_array( $data )
		);
	}

	/**
	 * find_by_order
	 *
	 * @param int $order_id
	 * @return object|null
	 */
	public static function find_by_order( int $order_id ) {
		global $wpdb;

		return $wpdb->get_row(
			$wpdb->prepare(
				'SELECT * FROM ' . self::table_name() . ' WHERE order_id = %d LIMIT 1',
				$order_id
			)
		);
	}

	/**
	 * query
	 * Consulta com filtros opcionais.
	 *
	 * @param array $args {
	 *   @type string $status       Filtrar por status do pipeline
	 *   @type string $carrier      Filtrar por transportadora (LIKE)
	 *   @type string $from         Data início (Y-m-d)
	 *   @type string $to           Data fim (Y-m-d)
	 *   @type int    $limit        Máximo de registros (padrão 50)
	 *   @type int    $offset       Offset (paginação)
	 *   @type string $orderby      Coluna para ordenação (padrão generated_at)
	 *   @type string $order        ASC | DESC (padrão DESC)
	 * }
	 * @return object[]
	 */
	public static function query( array $args = [] ): array {
		global $wpdb;

		$table   = self::table_name();
		$where   = [];
		$values  = [];

		if ( ! empty( $args['status'] ) ) {
			$where[]  = 'status = %s';
			$values[] = sanitize_text_field( $args['status'] );
		}

		if ( ! empty( $args['carrier'] ) ) {
			$where[]  = 'carrier LIKE %s';
			$values[] = '%' . $wpdb->esc_like( sanitize_text_field( $args['carrier'] ) ) . '%';
		}

		if ( ! empty( $args['from'] ) ) {
			$where[]  = 'generated_at >= %s';
			$values[] = sanitize_text_field( $args['from'] ) . ' 00:00:00';
		}

		if ( ! empty( $args['to'] ) ) {
			$where[]  = 'generated_at <= %s';
			$values[] = sanitize_text_field( $args['to'] ) . ' 23:59:59';
		}

		$limit   = isset( $args['limit'] ) ? min( 500, max( 1, absint( $args['limit'] ) ) ) : 50;
		$offset  = isset( $args['offset'] ) ? absint( $args['offset'] ) : 0;

		$allowed_order_cols = [ 'id', 'order_id', 'status', 'carrier', 'generated_at', 'updated_at' ];
		$orderby = in_array( $args['orderby'] ?? '', $allowed_order_cols ) ? $args['orderby'] : 'generated_at';
		$order   = strtoupper( $args['order'] ?? 'DESC' ) === 'ASC' ? 'ASC' : 'DESC';

		$sql = "SELECT * FROM {$table}";

		if ( ! empty( $where ) ) {
			$sql .= ' WHERE ' . implode( ' AND ', $where );
		}

		$sql .= " ORDER BY {$orderby} {$order} LIMIT %d OFFSET %d";
		$values[] = $limit;
		$values[] = $offset;

		if ( ! empty( $values ) ) {
			$sql = $wpdb->prepare( $sql, ...$values );
		}

		return $wpdb->get_results( $sql ) ?: [];
	}

	/**
	 * count
	 * Conta registros com os mesmos filtros do query().
	 */
	public static function count( array $args = [] ): int {
		global $wpdb;

		$table  = self::table_name();
		$where  = [];
		$values = [];

		if ( ! empty( $args['status'] ) ) {
			$where[]  = 'status = %s';
			$values[] = sanitize_text_field( $args['status'] );
		}
		if ( ! empty( $args['carrier'] ) ) {
			$where[]  = 'carrier LIKE %s';
			$values[] = '%' . $wpdb->esc_like( sanitize_text_field( $args['carrier'] ) ) . '%';
		}
		if ( ! empty( $args['from'] ) ) {
			$where[]  = 'generated_at >= %s';
			$values[] = sanitize_text_field( $args['from'] ) . ' 00:00:00';
		}
		if ( ! empty( $args['to'] ) ) {
			$where[]  = 'generated_at <= %s';
			$values[] = sanitize_text_field( $args['to'] ) . ' 23:59:59';
		}

		$sql = "SELECT COUNT(*) FROM {$table}";
		if ( ! empty( $where ) ) {
			$sql .= ' WHERE ' . implode( ' AND ', $where );
		}

		if ( ! empty( $values ) ) {
			$sql = $wpdb->prepare( $sql, ...$values );
		}

		return (int) $wpdb->get_var( $sql );
	}

	/**
	 * delete_by_order
	 *
	 * @param int $order_id
	 * @return int|false
	 */
	public static function delete_by_order( int $order_id ) {
		global $wpdb;

		return $wpdb->delete( self::table_name(), [ 'order_id' => $order_id ], [ '%d' ] );
	}

	/**
	 * migrate_from_meta
	 * Migra dados existentes de post_meta para a tabela.
	 * Pode ser chamado via WP CLI ou pelo painel de ferramentas.
	 *
	 * @param int $batch  Quantos pedidos processar por vez
	 * @return int  Número de registros migrados
	 */
	public static function migrate_from_meta( int $batch = 100 ): int {
		$orders = wc_get_orders( [
			'limit'    => $batch,
			'return'   => 'objects',
			'meta_key' => '_melhor_envio_item_id',
		] );

		$migrated = 0;

		foreach ( $orders as $order ) {
			$existing = self::find_by_order( $order->get_id() );
			if ( $existing ) {
				continue; // já migrado
			}

			$carrier = '';
			foreach ( $order->get_items( 'shipping' ) as $item ) {
				if ( $item->get_meta( 'melhorenvio_method_id' ) ) {
					$carrier = $item->get_name();
					break;
				}
			}

			$generated_at = $order->get_meta( '_melhor_envio_pdf_downloaded_at' );
			$generated_at_mysql = $generated_at
				? gmdate( 'Y-m-d H:i:s', (int) $generated_at )
				: null;

			self::upsert( [
				'order_id'     => $order->get_id(),
				'shipment_id'  => (string) $order->get_meta( '_melhor_envio_item_id' ),
				'protocol'     => (string) $order->get_meta( '_melhor_envio_order_id' ),
				'carrier'      => $carrier,
				'status'       => (string) ( $order->get_meta( '_melhor_envio_label_status' ) ?: 'unknown' ),
				'tracking'     => (string) ( is_array( $order->get_meta( '_melhor_envio_tracking_codes' ) )
					? implode( ',', $order->get_meta( '_melhor_envio_tracking_codes' ) )
					: $order->get_meta( '_melhor_envio_tracking_codes' ) ),
				'pdf_path'     => (string) $order->get_meta( '_melhor_envio_pdf_local_path' ),
				'pdf_url'      => (string) $order->get_meta( '_melhor_envio_pdf_local_url' ),
				'print_url'    => (string) $order->get_meta( '_melhor_envio_print_url' ),
				'operator_id'  => (int) $order->get_meta( '_melhor_envio_requested_by' ),
				'generated_at' => $generated_at_mysql,
			] );

			$migrated++;
		}

		return $migrated;
	}


	private static function normalize_data( array $data ): array {
		if ( isset( $data['carrier_name'] ) && empty( $data['carrier'] ) ) { $data['carrier'] = $data['carrier_name']; }
		$allowed = [ 'order_id', 'shipment_id', 'protocol', 'carrier', 'customer_id', 'customer_email', 'shipping_real_cost', 'shipping_charged', 'service_fee', 'margin', 'carrier_id', 'service_id', 'service_name', 'destination_uf', 'destination_city', 'destination_postcode', 'postcode_prefix', 'status', 'tracking', 'pdf_path', 'pdf_url', 'print_url', 'error_message', 'operator_id', 'generated_at', 'updated_at' ];
		return array_intersect_key( $data, array_flip( $allowed ) );
	}

	/**
	 * format_array
	 * Retorna array de formatos MySQL para wpdb->insert/update.
	 */
	private static function format_array( array $data ): array {
		$int_cols = [ 'order_id', 'operator_id', 'customer_id' ];
		$float_cols = [ 'shipping_real_cost', 'shipping_charged', 'service_fee', 'margin' ];
		$formats  = [];

		foreach ( $data as $col => $val ) {
			if ( in_array( $col, $int_cols, true ) ) {
				$formats[] = '%d';
			} elseif ( in_array( $col, $float_cols, true ) ) {
				$formats[] = '%f';
			} else {
				$formats[] = '%s';
			}
		}

		return $formats;
	}
}
