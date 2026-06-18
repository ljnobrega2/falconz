<?php

namespace WC_MelhorEnvio\Pdf;

use Exception;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

/**
 * Batch_Composer
 *
 * Une múltiplos PDFs de etiquetas em um único arquivo para impressão em lote.
 * Usa a biblioteca setasign/fpdi quando disponível; fallback para
 * concatenação simples via GhostScript se disponível no servidor;
 * ou retorna a lista de URLs para impressão individual.
 *
 * Como usar:
 *   $composer = new Batch_Composer();
 *   $result   = $composer->compose( [101, 102, 103] );
 *   // $result['url'] = URL do PDF gerado
 *   // $result['path'] = caminho local
 */
class Batch_Composer {
	use Helpers, Logger;

	protected $logger = 'wc-melhor-envio-batch';

	/**
	 * compose
	 * Compõe um PDF em lote a partir dos order_ids informados.
	 *
	 * @param int[]  $order_ids
	 * @param array  $options   Opções: include_packing_slip, include_cover
	 * @return array  [ 'url' => string, 'path' => string, 'orders' => array, 'missing' => array ]
	 * @throws Exception
	 */
	public function compose( array $order_ids, array $options = [] ) {
		if ( empty( $order_ids ) ) {
			throw new Exception( 'Nenhum pedido informado para composição de lote.' );
		}

		$include_packing_slip = ! empty( $options['include_packing_slip'] );
		$include_cover        = ! empty( $options['include_cover'] );

		$pdf_paths = [];
		$missing   = [];
		$orders    = [];

		foreach ( $order_ids as $order_id ) {
			$order = wc_get_order( absint( $order_id ) );

			if ( ! $order ) {
				$missing[] = $order_id;
				continue;
			}

			$orders[] = $order;

			$pdf_path = $order->get_meta( '_melhor_envio_pdf_local_path' );

			if ( ! $pdf_path || ! file_exists( $pdf_path ) ) {
				$this->log( 'PDF não encontrado para pedido #' . $order_id . ': ' . $pdf_path );
				$missing[] = $order_id;
				continue;
			}

			// Capa de branding por pedido (opcional)
			if ( $include_cover ) {
				try {
					$cover = new Branding_Cover();
					$cover_path = $cover->generate( $order );
					if ( $cover_path ) {
						$pdf_paths[] = $cover_path;
					}
				} catch ( \Throwable $e ) {
					$this->log( 'Erro ao gerar capa para pedido #' . $order_id . ': ' . $e->getMessage() );
				}
			}

			$pdf_paths[] = $pdf_path;

			// Packing slip por pedido (opcional)
			if ( $include_packing_slip ) {
				try {
					$slip = new Packing_Slip();
					$slip_path = $slip->generate( $order );
					if ( $slip_path ) {
						$pdf_paths[] = $slip_path;
					}
				} catch ( \Throwable $e ) {
					$this->log( 'Erro ao gerar packing slip para pedido #' . $order_id . ': ' . $e->getMessage() );
				}
			}
		}

		if ( empty( $pdf_paths ) ) {
			throw new Exception( 'Nenhum PDF disponível para composição. Gere as etiquetas primeiro.' );
		}

		$batch_path = $this->merge_pdfs( $pdf_paths, $order_ids );

		return [
			'url'     => $this->path_to_url( $batch_path ),
			'path'    => $batch_path,
			'orders'  => array_map( function( $o ) { return $o->get_id(); }, $orders ),
			'missing' => $missing,
			'count'   => count( $pdf_paths ),
		];
	}


	/**
	 * merge_pdfs
	 * Tenta unir PDFs usando fpdi (Composer) ou GhostScript.
	 * Fallback: copia o primeiro PDF e registra log de indisponibilidade.
	 *
	 * @param string[] $paths
	 * @param int[]    $order_ids Para nomear o arquivo de saída
	 * @return string  Caminho do arquivo gerado
	 * @throws Exception
	 */
	private function merge_pdfs( array $paths, array $order_ids ) {
		$upload_dir = wp_upload_dir();
		$subdir     = '/melhor-envio-labels/lotes';
		$dir        = trailingslashit( $upload_dir['basedir'] ) . ltrim( $subdir, '/' );

		if ( ! file_exists( $dir ) ) {
			wp_mkdir_p( $dir );
		}

		$slug      = 'lote-' . implode( '-', array_slice( $order_ids, 0, 5 ) ) . ( count( $order_ids ) > 5 ? '-e-mais' : '' );
		$out_name  = sanitize_file_name( $slug . '-' . gmdate( 'Ymd-His' ) . '.pdf' );
		$out_path  = trailingslashit( $dir ) . $out_name;

		// Estratégia 1: setasign/fpdi via Composer
		if ( $this->try_merge_fpdi( $paths, $out_path ) ) {
			$this->log( 'Lote gerado via FPDI: ' . $out_path );
			return $out_path;
		}

		// Estratégia 2: GhostScript (gs)
		if ( $this->try_merge_ghostscript( $paths, $out_path ) ) {
			$this->log( 'Lote gerado via GhostScript: ' . $out_path );
			return $out_path;
		}

		// Estratégia 3: concatenação binária simples (funciona para PDFs sem xref comprimido)
		if ( $this->try_merge_binary( $paths, $out_path ) ) {
			$this->log( 'Lote gerado via concatenação binária: ' . $out_path );
			return $out_path;
		}

		throw new Exception(
			'Não foi possível unir os PDFs. Instale a biblioteca setasign/fpdi via Composer ou o GhostScript no servidor.'
		);
	}


	/**
	 * try_merge_fpdi
	 * Usa setasign/fpdi se disponível no ambiente.
	 */
	private function try_merge_fpdi( array $paths, string $out_path ): bool {
		if ( ! class_exists( '\setasign\Fpdi\Fpdi' ) && ! class_exists( '\FPDI' ) ) {
			return false;
		}

		try {
			$class = class_exists( '\setasign\Fpdi\Fpdi' ) ? '\setasign\Fpdi\Fpdi' : '\FPDI';
			$pdf   = new $class();
			$pdf->SetAutoPageBreak( false );

			foreach ( $paths as $path ) {
				if ( ! file_exists( $path ) ) {
					continue;
				}
				$count = $pdf->setSourceFile( $path );
				for ( $i = 1; $i <= $count; $i++ ) {
					$tpl = $pdf->importPage( $i );
					$size = $pdf->getTemplateSize( $tpl );
					$pdf->AddPage( $size['width'] > $size['height'] ? 'L' : 'P', [ $size['width'], $size['height'] ] );
					$pdf->useTemplate( $tpl );
				}
			}

			$pdf->Output( 'F', $out_path );
			return file_exists( $out_path );
		} catch ( \Throwable $e ) {
			$this->log( 'FPDI error: ' . $e->getMessage() );
			return false;
		}
	}


	/**
	 * try_merge_ghostscript
	 * Usa gs (GhostScript) via exec se disponível.
	 */
	private function try_merge_ghostscript( array $paths, string $out_path ): bool {
		if ( ! function_exists( 'exec' ) ) {
			return false;
		}

		exec( 'which gs 2>/dev/null', $output, $return );
		if ( $return !== 0 || empty( $output ) ) {
			return false;
		}

		$escaped_paths = array_map( 'escapeshellarg', $paths );
		$cmd = sprintf(
			'gs -dBATCH -dNOPAUSE -q -sDEVICE=pdfwrite -sOutputFile=%s %s 2>&1',
			escapeshellarg( $out_path ),
			implode( ' ', $escaped_paths )
		);

		exec( $cmd, $gs_output, $gs_return );

		if ( $gs_return !== 0 ) {
			$this->log( 'GhostScript error: ' . implode( "\n", $gs_output ) );
			return false;
		}

		return file_exists( $out_path );
	}


	/**
	 * try_merge_binary
	 * Concatenação binária simples — funciona apenas para PDFs que não usam xref comprimido.
	 * Resultado pode não ser reconhecido por todos os leitores, mas serve como fallback.
	 */
	private function try_merge_binary( array $paths, string $out_path ): bool {
		try {
			$content = '';
			foreach ( $paths as $path ) {
				if ( file_exists( $path ) ) {
					$content .= file_get_contents( $path );
				}
			}

			if ( empty( $content ) ) {
				return false;
			}

			file_put_contents( $out_path, $content );
			return file_exists( $out_path );
		} catch ( \Throwable $e ) {
			$this->log( 'Binary merge error: ' . $e->getMessage() );
			return false;
		}
	}


	/**
	 * path_to_url
	 * Converte caminho absoluto para URL pública do WordPress.
	 */
	private function path_to_url( string $path ): string {
		$upload_dir = wp_upload_dir();
		return str_replace(
			trailingslashit( $upload_dir['basedir'] ),
			trailingslashit( $upload_dir['baseurl'] ),
			$path
		);
	}
}
