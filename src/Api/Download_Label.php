<?php

namespace WC_MelhorEnvio\Api;

use Exception;

class Download_Label extends Melhor_Envio_Api {
	protected $logger = 'wc-melhor-envio-download';

	public function download( $order, $suffix = '' ) {

		if ( $existing_url = $order->get_meta( '_melhor_envio_pdf_local_url' . $suffix ) ) {
			$existing_path = $order->get_meta( '_melhor_envio_pdf_local_path' . $suffix );
			if ( $this->is_valid_pdf_file( $existing_path ) ) {
				// Só usa cache se tiver DACE junto (formato novo)
				$format = (string) $order->get_meta( '_melhor_envio_pdf_format' . $suffix );
				if ( $format === 'label_plus_dace' ) {
					return $this->render_success( 'PDF já salvo localmente.', [
						'path' => $existing_path,
						'url'  => $existing_url,
					] );
				}
				// Cache antigo sem DACE — apaga e rebaixa
				@unlink( $existing_path );
				$order->delete_meta_data( '_melhor_envio_pdf_local_url' . $suffix );
				$order->delete_meta_data( '_melhor_envio_pdf_local_path' . $suffix );
				$order->delete_meta_data( '_melhor_envio_pdf_format' . $suffix );
				$order->save();
			}
		}

		$item_id   = (string) $order->get_meta( '_melhor_envio_item_id' . $suffix );
		$protocol  = (string) $order->get_meta( '_melhor_envio_order_id' . $suffix );

		if ( ! $item_id ) {
			throw new Exception( 'Item do Melhor Envio não encontrado para baixar o PDF.' );
		}

		$upload_dir = wp_upload_dir();
		$subdir     = '/senderzz-labels/' . gmdate( 'Y/m' );
		$dir        = trailingslashit( $upload_dir['basedir'] ) . ltrim( $subdir, '/' );

		if ( ! file_exists( $dir ) ) {
			wp_mkdir_p( $dir );
		}

		$carrier   = $this->get_carrier_slug( $order );
		$date_slug = gmdate( 'Ymd-His' );
		$file_name = sprintf( 'pedido-%d%s-%s-label-dace-%s.pdf', $order->get_id(), $suffix ?: '', $carrier, $date_slug );
		$file_path = trailingslashit( $dir ) . sanitize_file_name( $file_name );
		$file_url  = trailingslashit( $upload_dir['baseurl'] ) . trim( $subdir, '/' ) . '/' . basename( $file_path );

		// Baixa etiqueta
		$label_body = $this->fetch_label_pdf( $item_id );
		if ( ! $label_body || strncmp( $label_body, '%PDF', 4 ) !== 0 ) {
			throw new Exception( 'O Melhor Envio não retornou PDF válido para a etiqueta. Aguarde o processamento e tente novamente.' );
		}

		// Baixa DACE resumida usando item_id
		$dace_body = '';
		if ( $item_id ) {
			try {
				$dace_body = $this->fetch_dace_pdf( $item_id );
			} catch ( \Throwable $e ) {
				$this->log( 'DACE não disponível: ' . $e->getMessage() . ' — salvando só etiqueta.' );
			}
		}

		// Junta os dois PDFs
		if ( $dace_body && strncmp( $dace_body, '%PDF', 4 ) === 0 ) {
			$merged = $this->merge_two_pdfs( $label_body, $dace_body );
			file_put_contents( $file_path, $merged ?: $label_body );
		} else {
			file_put_contents( $file_path, $label_body );
		}

		if ( ! $this->is_valid_pdf_file( $file_path ) ) {
			@unlink( $file_path );
			throw new Exception( 'Falha ao salvar PDF local válido no servidor.' );
		}

		$order->update_meta_data( '_melhor_envio_pdf_local_path' . $suffix, $file_path );
		$order->update_meta_data( '_melhor_envio_pdf_local_url' . $suffix, $file_url );
		$order->update_meta_data( '_melhor_envio_pdf_downloaded_at' . $suffix, time() );
		$order->update_meta_data( '_melhor_envio_pdf_format' . $suffix, 'label_plus_dace' );
		$order->save();

		return $this->render_success( 'Etiqueta + DACE salvas no servidor.', [ 'path' => $file_path, 'url' => $file_url ] );
	}

	private function fetch_label_pdf( string $item_id ): string {
		$token = $this->integration['client_secret'];
		$url   = $this->get_api_url() . '/api/v2/me/imprimir/pdf/' . rawurlencode( $item_id );
		$this->log( 'Baixando etiqueta: ' . $url );

		$response = wp_safe_remote_get( $url, [
			'timeout'     => 90,
			'redirection' => 5,
			'headers'     => [
				'Authorization' => 'Bearer ' . $token,
				'Accept'        => 'application/json, application/pdf',
				'Content-Type'  => 'application/json',
				'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
			],
		] );

		if ( is_wp_error( $response ) ) {
			throw new Exception( 'Erro ao baixar etiqueta: ' . $response->get_error_message() );
		}

		$code = (int) wp_remote_retrieve_response_code( $response );
		$body = (string) wp_remote_retrieve_body( $response );
		$this->log( 'Etiqueta HTTP ' . $code . ' | início=' . substr( $body, 0, 40 ) );

		if ( ! in_array( $code, [ 200, 201 ], true ) ) {
			throw new Exception( 'ME recusou etiqueta. HTTP ' . $code );
		}

		if ( strncmp( $body, '%PDF', 4 ) === 0 ) return $body;

		// Resposta JSON com URL assinada
		$pdf_url = $this->extract_pdf_url_from_response( $body );
		if ( $pdf_url ) return $this->download_pdf_from_signed_url( $pdf_url );

		throw new Exception( 'ME não retornou PDF válido para etiqueta.' );
	}

	private function fetch_dace_pdf( string $item_id ): string {
		$token = $this->integration['client_secret'];
		$url   = $this->get_api_url() . '/api/v2/me/imprimir/dace/pdf/' . rawurlencode( $item_id );
		$this->log( 'Baixando DACE resumida: ' . $url );

		$response = wp_safe_remote_get( $url, [
			'timeout'     => 60,
			'redirection' => 5,
			'headers'     => [
				'Authorization' => 'Bearer ' . $token,
				'Accept'        => 'application/json, application/pdf',
				'Content-Type'  => 'application/json',
				'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
			],
		] );

		if ( is_wp_error( $response ) ) {
			throw new Exception( 'Erro ao baixar DACE: ' . $response->get_error_message() );
		}

		$code  = (int) wp_remote_retrieve_response_code( $response );
		$body  = (string) wp_remote_retrieve_body( $response );
		$ctype = (string) wp_remote_retrieve_header( $response, 'content-type' );
		$this->log( 'DACE HTTP ' . $code . ' | content-type=' . $ctype . ' | início=' . substr( $body, 0, 80 ) );

		if ( ! in_array( $code, [ 200, 201 ], true ) ) {
			throw new Exception( 'ME recusou DACE. HTTP ' . $code . ': ' . substr( $body, 0, 100 ) );
		}

		// PDF direto
		if ( strncmp( $body, '%PDF', 4 ) === 0 ) return $body;

		// JSON com URL assinada — aceita qualquer URL HTTPS (DACE pode não ter .pdf na URL)
		$data = json_decode( $body, true );
		$pdf_url = '';

		if ( is_string( $data ) && preg_match( '#^https?://#i', $data ) ) {
			$pdf_url = $data;
		} elseif ( is_array( $data ) ) {
			foreach ( $data as $value ) {
				if ( is_string( $value ) && preg_match( '#^https?://#i', $value ) ) {
					$pdf_url = $value;
					break;
				}
				if ( is_array( $value ) ) {
					foreach ( [ 'url', 'pdf', 'file', 'link' ] as $field ) {
						if ( ! empty( $value[ $field ] ) && is_string( $value[ $field ] ) && preg_match( '#^https?://#i', $value[ $field ] ) ) {
							$pdf_url = $value[ $field ];
							break 2;
						}
					}
				}
			}
		}

		if ( $pdf_url ) {
			$this->log( 'DACE URL assinada: ' . preg_replace( '/\?.*/', '?...', $pdf_url ) );
			return $this->download_pdf_from_signed_url( $pdf_url );
		}

		throw new Exception( 'ME não retornou PDF válido para DACE. Resposta: ' . substr( $body, 0, 120 ) );
	}

	/**
	 * Junta dois PDFs usando concatenação binária simples.
	 * Funciona para PDFs sem xref comprimido (maioria das etiquetas).
	 * Se o servidor tiver GhostScript, usa ele para resultado mais confiável.
	 */
	private function merge_two_pdfs( string $pdf1, string $pdf2 ): string {
		// Tenta GhostScript primeiro
		if ( function_exists( 'exec' ) ) {
			exec( 'which gs 2>/dev/null', $out, $ret );
			if ( $ret === 0 && ! empty( $out ) ) {
				$tmp1 = tempnam( sys_get_temp_dir(), 'sz_label_' ) . '.pdf';
				$tmp2 = tempnam( sys_get_temp_dir(), 'sz_dace_' ) . '.pdf';
				$tmpo = tempnam( sys_get_temp_dir(), 'sz_merged_' ) . '.pdf';
				file_put_contents( $tmp1, $pdf1 );
				file_put_contents( $tmp2, $pdf2 );
				$cmd = sprintf(
					'gs -dBATCH -dNOPAUSE -q -sDEVICE=pdfwrite -sOutputFile=%s %s %s 2>&1',
					escapeshellarg( $tmpo ),
					escapeshellarg( $tmp1 ),
					escapeshellarg( $tmp2 )
				);
				exec( $cmd, $gs_out, $gs_ret );
				@unlink( $tmp1 ); @unlink( $tmp2 );
				if ( $gs_ret === 0 && file_exists( $tmpo ) && filesize( $tmpo ) > 100 ) {
					$merged = file_get_contents( $tmpo );
					@unlink( $tmpo );
					$this->log( 'PDFs unidos via GhostScript.' );
					return $merged;
				}
				@unlink( $tmpo );
			}
		}

		// Fallback: concatenação binária simples
		$this->log( 'PDFs unidos via concatenação binária.' );
		return $pdf1 . $pdf2;
	}

	private function fetch_pdf_file_by_item_id( string $item_id ) {
		$token = $this->integration['client_secret'];
		$url   = $this->get_api_url() . '/api/v2/me/imprimir/pdf/' . rawurlencode( $item_id );

		$this->log( 'Baixando PDF pela API de arquivo: ' . $url );

		$response = wp_safe_remote_get( $url, [
			'timeout'     => 90,
			'redirection' => 5,
			'headers'     => [
				'Authorization' => 'Bearer ' . $token,
				'Accept'        => 'application/json, application/pdf',
				'Content-Type'  => 'application/json',
				'User-Agent'    => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
			],
		] );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao consultar arquivo PDF: ' . $response->get_error_message() );
			throw new Exception( 'Erro ao consultar PDF no Melhor Envio: ' . $response->get_error_message() );
		}

		$code  = (int) wp_remote_retrieve_response_code( $response );
		$body  = (string) wp_remote_retrieve_body( $response );
		$ctype = (string) wp_remote_retrieve_header( $response, 'content-type' );
		$this->log( 'Resposta API arquivo: HTTP ' . $code . ' | content-type=' . $ctype . ' | início=' . substr( $body, 0, 40 ) );

		if ( ! in_array( $code, [ 200, 201 ], true ) ) {
			throw new Exception( 'Melhor Envio recusou o arquivo da etiqueta. HTTP ' . $code . '. ' . substr( $body, 0, 180 ) );
		}

		if ( strncmp( $body, '%PDF', 4 ) === 0 ) {
			return $body;
		}

		$pdf_url = $this->extract_pdf_url_from_response( $body );
		if ( ! $pdf_url ) {
			$this->log( 'Resposta sem PDF/URL válida: ' . substr( $body, 0, 240 ) );
			throw new Exception( 'O Melhor Envio ainda não disponibilizou a URL do PDF. Aguarde o processamento da etiqueta e tente novamente.' );
		}

		return $this->download_pdf_from_signed_url( $pdf_url );
	}

	private function extract_pdf_url_from_response( string $body ): string {
		$data = json_decode( $body, true );
		$candidates = [];

		if ( is_string( $data ) ) {
			$candidates[] = $data;
		} elseif ( is_array( $data ) ) {
			foreach ( $data as $value ) {
				if ( is_string( $value ) ) {
					$candidates[] = $value;
				} elseif ( is_array( $value ) ) {
					foreach ( [ 'url', 'pdf', 'file', 'link' ] as $field ) {
						if ( ! empty( $value[ $field ] ) && is_string( $value[ $field ] ) ) {
							$candidates[] = $value[ $field ];
						}
					}
				}
			}
		}

		foreach ( $candidates as $candidate ) {
			$candidate = html_entity_decode( trim( $candidate ) );
			if ( preg_match( '#^https?://#i', $candidate ) && stripos( $candidate, '.pdf' ) !== false ) {
				return esc_url_raw( $candidate );
			}
		}

		return '';
	}

	private function download_pdf_from_signed_url( string $pdf_url ): string {
		$this->log( 'Baixando PDF assinado no servidor: ' . preg_replace( '/\?.*/', '?...', $pdf_url ) );

		$response = wp_safe_remote_get( $pdf_url, [
			'timeout'     => 120,
			'redirection' => 5,
			'headers'     => [
				'Accept'     => 'application/pdf,*/*;q=0.8',
				'User-Agent' => 'Senderzz Logistics (suporte@app.senderzz.com.br)',
			],
		] );

		if ( is_wp_error( $response ) ) {
			$this->log( 'Erro ao baixar URL assinada: ' . $response->get_error_message() );
			throw new Exception( 'Erro ao baixar PDF assinado do Melhor Envio: ' . $response->get_error_message() );
		}

		$code  = (int) wp_remote_retrieve_response_code( $response );
		$body  = (string) wp_remote_retrieve_body( $response );
		$ctype = (string) wp_remote_retrieve_header( $response, 'content-type' );
		$this->log( 'Resposta PDF assinado: HTTP ' . $code . ' | content-type=' . $ctype . ' | início=' . substr( $body, 0, 20 ) );

		if ( ! in_array( $code, [ 200, 201 ], true ) || strncmp( $body, '%PDF', 4 ) !== 0 ) {
			throw new Exception( 'URL assinada não retornou PDF válido. HTTP ' . $code . '. Início: ' . substr( $body, 0, 80 ) );
		}

		return $body;
	}

	private function is_valid_pdf_file( $path ): bool {
		if ( ! $path || ! is_string( $path ) || ! file_exists( $path ) || filesize( $path ) < 20 ) {
			return false;
		}
		$fh = fopen( $path, 'rb' );
		if ( ! $fh ) return false;
		$head = fread( $fh, 4 );
		fclose( $fh );
		return $head === '%PDF';
	}

	private function get_carrier_slug( $order ) {
		foreach ( $order->get_items( 'shipping' ) as $item ) {
			$method_id = $item->get_meta( 'melhorenvio_method_id' );
			if ( $method_id ) {
				$map = [
					1=>'correios-pac',2=>'correios-sedex',3=>'jadlog-package',4=>'jadlog-com',
					7=>'azul-amanha',8=>'azul-ecommerce',9=>'latam-cargo',10=>'loggi',
					11=>'b2w',12=>'buslog',16=>'correios-mini',17=>'correios-sedex10',18=>'correios-sedex-hoje',
				];
				return sanitize_title( $map[ intval( $method_id ) ] ?? 'transportadora-' . $method_id );
			}
		}
		return 'melhor-envio';
	}
}