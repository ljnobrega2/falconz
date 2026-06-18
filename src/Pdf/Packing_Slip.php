<?php

namespace WC_MelhorEnvio\Pdf;

use Exception;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

/**
 * Packing_Slip
 *
 * Gera um romaneio (packing slip) em PDF para um pedido WooCommerce.
 * Usa a biblioteca dompdf se disponível; fallback para HTML simples
 * convertido em PDF via ob_start + file_put_contents.
 *
 * Armazena o arquivo em /uploads/melhor-envio-labels/packing-slips/
 * e salva o caminho no meta _melhor_envio_packing_slip_path do pedido.
 */
class Packing_Slip {
	use Helpers, Logger;

	protected $logger = 'wc-melhor-envio-packing-slip';

	/**
	 * generate
	 * Gera o packing slip para o pedido e retorna o caminho do arquivo.
	 *
	 * @param \WC_Order $order
	 * @return string  Caminho absoluto do PDF gerado
	 * @throws Exception
	 */
	public function generate( $order ): string {
		$existing = $order->get_meta( '_melhor_envio_packing_slip_path' );
		if ( $existing && file_exists( $existing ) && filesize( $existing ) > 100 ) {
			// Verifica se é PDF real (não HTML salvo como .pdf)
			$fh   = fopen( $existing, 'rb' );
			$head = $fh ? fread( $fh, 4 ) : '';
			if ( $fh ) fclose( $fh );
			if ( $head === '%PDF' ) {
				return $existing;
			}
			// Era HTML — apaga e regera
			@unlink( $existing );
			$order->delete_meta_data( '_melhor_envio_packing_slip_path' );
			$order->delete_meta_data( '_melhor_envio_packing_slip_url' );
			$order->save();
		}

		$upload_dir = wp_upload_dir();
		$subdir     = '/melhor-envio-labels/packing-slips';
		$dir        = trailingslashit( $upload_dir['basedir'] ) . ltrim( $subdir, '/' );

		if ( ! file_exists( $dir ) ) {
			wp_mkdir_p( $dir );
		}

		$file_name = sprintf( 'dace-pedido-%d-%s.pdf', $order->get_id(), gmdate( 'Ymd' ) );
		$file_path = trailingslashit( $dir ) . $file_name;

		$html = $this->build_html( $order );

		// Estratégia 1: dompdf
		if ( $this->render_dompdf( $html, $file_path ) ) {
			$this->save_meta( $order, $file_path, $upload_dir, $subdir, $file_name );
			return $file_path;
		}

		// Estratégia 2: mPDF
		if ( $this->render_mpdf( $html, $file_path ) ) {
			$this->save_meta( $order, $file_path, $upload_dir, $subdir, $file_name );
			return $file_path;
		}

		// Fallback: PDF nativo sem biblioteca externa
		if ( $this->render_native_pdf( $order, $file_path ) ) {
			$this->save_meta( $order, $file_path, $upload_dir, $subdir, $file_name );
			return $file_path;
		}

		throw new Exception( 'Não foi possível gerar o DACE. Instale dompdf ou mPDF via Composer.' );
	}


	/**
	 * build_html
	 * Monta o HTML do packing slip com os dados do pedido.
	 */
	private function build_html( $order ): string {
		$items_html = '';
		foreach ( $order->get_items() as $item ) {
			$product = $item->get_product();
			$sku     = $product ? $product->get_sku() : '';
			$items_html .= sprintf(
				'<tr>
					<td style="padding:4px 8px; border:1px solid #ddd;">%s</td>
					<td style="padding:4px 8px; border:1px solid #ddd; text-align:center;">%s</td>
					<td style="padding:4px 8px; border:1px solid #ddd; text-align:center;">%d</td>
					<td style="padding:4px 8px; border:1px solid #ddd; text-align:right;">%s</td>
				</tr>',
				esc_html( $item->get_name() ),
				esc_html( $sku ),
				$item->get_quantity(),
				wc_price( $item->get_total() )
			);
		}

		$shipping_method = '';
		foreach ( $order->get_items( 'shipping' ) as $s ) {
			$shipping_method = $s->get_name();
			break;
		}

		$site_name = get_bloginfo( 'name' );
		$logo_url  = apply_filters( 'wc_melhor_envio_packing_slip_logo', '' );

		$html = '<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
  body { font-family: var(--sz-font); font-size: var(--sz-text-meta); color: #333; margin: 0; padding: 20px; }
  h1   { font-size: var(--sz-text-lg); margin: 0 0 4px 0; }
  h2   { font-size: var(--sz-text-md); margin: 0 0 16px 0; color: #555; }
  .header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 20px; border-bottom: 2px solid #333; padding-bottom: 12px; }
  .logo img { max-height: 60px; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 16px; }
  th { background: #f0f0f0; padding: 6px 8px; border: 1px solid #ddd; text-align: left; font-size: var(--sz-text-sm); text-transform: none; }
  .section-title { font-size: var(--sz-text-base); font-weight: 700; margin: 16px 0 6px 0; border-bottom: 1px solid #ccc; padding-bottom: 4px; }
  .info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-bottom: 16px; }
  .info-box { border: 1px solid #ddd; padding: 10px; border-radius: 4px; }
  .info-box strong { display: block; margin-bottom: 4px; font-size: var(--sz-text-sm); text-transform: none; color: #888; }
  .totals td { padding: 4px 8px; }
  .totals .grand-total { font-weight: 700; font-size: var(--sz-text-md); border-top: 2px solid #333; }
  .footer { margin-top: 24px; font-size: var(--sz-text-xs); color: #999; text-align: center; border-top: 1px solid #eee; padding-top: 8px; }
</style>
</head>
<body>';

		$html .= '<div class="header">';
		if ( $logo_url ) {
			$html .= '<div class="logo"><img src="' . esc_url( $logo_url ) . '" alt="' . esc_attr( $site_name ) . '"></div>';
		} else {
			$html .= '<div><strong style="font-size:var(--sz-text-lg)">' . esc_html( $site_name ) . '</strong></div>';
		}
		$html .= '<div style="text-align:right">
			<h1>DACE - DECLARAÇÃO DE CONTEÚDO</h1>
			<h2>Pedido #' . $order->get_order_number() . '</h2>
			<small>Data: ' . wc_format_datetime( $order->get_date_created() ) . '</small>
		</div></div>';

		// Informações do pedido
		$html .= '<div class="info-grid">';
		$html .= '<div class="info-box"><strong>Dados do Destinatário</strong>';
		$html .= esc_html( $order->get_formatted_billing_full_name() ) . '<br>';
		if ( $order->get_billing_company() ) {
			$html .= esc_html( $order->get_billing_company() ) . '<br>';
		}
		$html .= esc_html( $order->get_billing_email() ) . '<br>';
		$html .= esc_html( $order->get_billing_phone() );
		$html .= '</div>';

		$html .= '<div class="info-box"><strong>Endereço de Entrega</strong>';
		$html .= $order->get_formatted_shipping_address()
			? wp_strip_all_tags( $order->get_formatted_shipping_address() )
			: wp_strip_all_tags( $order->get_formatted_billing_address() );
		$html .= '</div>';
		$html .= '</div>';

		// Método de envio e rastreio
		$tracking = $order->get_meta( '_melhor_envio_tracking_codes' );
		$html .= '<div class="info-grid">';
		$html .= '<div class="info-box"><strong>Método de Envio</strong>' . esc_html( $shipping_method ) . '</div>';
		$html .= '<div class="info-box"><strong>Código de Rastreio</strong>';
		if ( $tracking ) {
			if ( is_array( $tracking ) ) {
				$html .= implode( '<br>', array_map( 'esc_html', $tracking ) );
			} else {
				$html .= esc_html( $tracking );
			}
		} else {
			$html .= '—';
		}
		$html .= '</div></div>';

		// Produtos
		$html .= '<div class="section-title">Itens Declarados</div>';
		$html .= '<table>
			<thead><tr>
				<th>Produto</th>
				<th style="text-align:center">SKU</th>
				<th style="text-align:center">Qtd</th>
				<th style="text-align:right">Valor declarado</th>
			</tr></thead>
			<tbody>' . $items_html . '</tbody>
		</table>';

		// Totais
		$html .= '<table class="totals" style="width:300px; margin-left:auto">';
		$html .= '<tr><td>Subtotal:</td><td style="text-align:right">' . wc_price( $order->get_subtotal() ) . '</td></tr>';
		if ( $order->get_shipping_total() > 0 ) {
			$html .= '<tr><td>Frete:</td><td style="text-align:right">' . wc_price( $order->get_shipping_total() ) . '</td></tr>';
		}
		if ( $order->get_discount_total() > 0 ) {
			$html .= '<tr><td>Desconto:</td><td style="text-align:right">-' . wc_price( $order->get_discount_total() ) . '</td></tr>';
		}
		$html .= '<tr class="grand-total"><td>Total:</td><td style="text-align:right">' . $order->get_formatted_order_total() . '</td></tr>';
		$html .= '</table>';

		// Observações
		$notes = wc_get_order_notes( [ 'order_id' => $order->get_id(), 'type' => 'customer' ] );
		if ( ! empty( $notes ) ) {
			$html .= '<div class="section-title">Observações do Cliente</div>';
			foreach ( $notes as $note ) {
				$html .= '<p>' . esc_html( $note->content ) . '</p>';
			}
		}

		$html .= '<div class="section-title">Declaração</div><p style="font-size:var(--sz-text-sm);line-height:1.45">Declaro que o conteúdo informado corresponde aos itens do pedido acima e que este documento acompanha a postagem para conferência logística.</p>';

		$html .= '<div class="footer">DACE gerada automaticamente por ' . esc_html( $site_name ) . ' · ' . gmdate( 'd/m/Y H:i' ) . '</div>';
		$html .= '</body></html>';

		return $html;
	}


	private function render_dompdf( string $html, string $out_path ): bool {
		if ( ! class_exists( '\Dompdf\Dompdf' ) ) {
			return false;
		}

		try {
			$dompdf = new \Dompdf\Dompdf( [ 'isRemoteEnabled' => true ] );
			$dompdf->loadHtml( $html );
			$dompdf->setPaper( 'A4', 'portrait' );
			$dompdf->render();
			file_put_contents( $out_path, $dompdf->output() );
			return file_exists( $out_path );
		} catch ( \Throwable $e ) {
			$this->log( 'Dompdf error: ' . $e->getMessage() );
			return false;
		}
	}


	private function render_mpdf( string $html, string $out_path ): bool {
		if ( ! class_exists( '\Mpdf\Mpdf' ) ) {
			return false;
		}

		try {
			$mpdf = new \Mpdf\Mpdf();
			$mpdf->WriteHTML( $html );
			$mpdf->Output( $out_path, 'F' );
			return file_exists( $out_path );
		} catch ( \Throwable $e ) {
			$this->log( 'mPDF error: ' . $e->getMessage() );
			return false;
		}
	}


	private function save_meta( $order, string $file_path, array $upload_dir, string $subdir, string $file_name ) {
		$file_url = trailingslashit( $upload_dir['baseurl'] ) . trim( $subdir, '/' ) . '/' . $file_name;
		$order->update_meta_data( '_melhor_envio_packing_slip_path', $file_path );
		$order->update_meta_data( '_melhor_envio_packing_slip_url', $file_url );
		$order->save();
	}

	/**
	 * render_native_pdf
	 * Gera PDF real usando apenas PHP puro — sem biblioteca externa.
	 * Cria um PDF A4 válido com os dados do pedido.
	 */
	private function render_native_pdf( $order, string $out_path ): bool {
		try {
			// Coleta dados do pedido
			$order_number = $order->get_order_number();
			$date         = wc_format_datetime( $order->get_date_created() );
			$site_name    = get_bloginfo( 'name' );

			$dest_name    = $order->get_formatted_billing_full_name();
			$dest_email   = $order->get_billing_email();
			$dest_phone   = $order->get_billing_phone();

			$ship_addr = $order->get_formatted_shipping_address()
				? wp_strip_all_tags( $order->get_formatted_shipping_address() )
				: wp_strip_all_tags( $order->get_formatted_billing_address() );
			$ship_addr = str_replace( "\n", ', ', $ship_addr );

			$shipping_method = '';
			foreach ( $order->get_items( 'shipping' ) as $s ) {
				$shipping_method = $s->get_name();
				break;
			}

			$tracking = $order->get_meta( '_melhor_envio_tracking_codes' );
			if ( is_array( $tracking ) ) $tracking = implode( ', ', $tracking );
			$tracking = $tracking ?: '—';

			// Monta linhas de itens
			$items_lines = [];
			$subtotal    = 0;
			foreach ( $order->get_items() as $item ) {
				$product = $item->get_product();
				$sku     = $product ? $product->get_sku() : '';
				$qty     = $item->get_quantity();
				$total   = $item->get_total();
				$subtotal += $total;
				$items_lines[] = [
					'name'  => $item->get_name(),
					'sku'   => $sku,
					'qty'   => $qty,
					'total' => 'R$ ' . number_format( $total, 2, ',', '.' ),
				];
			}

			$total_fmt    = 'R$ ' . number_format( $order->get_total(), 2, ',', '.' );
			$shipping_fmt = $order->get_shipping_total() > 0 ? 'R$ ' . number_format( $order->get_shipping_total(), 2, ',', '.' ) : '';
			$discount_fmt = $order->get_discount_total() > 0 ? 'R$ ' . number_format( $order->get_discount_total(), 2, ',', '.' ) : '';

			// Gera PDF usando TCPDF se disponível, senão usa gerador nativo mínimo
			if ( class_exists( 'TCPDF' ) ) {
				return $this->render_tcpdf( $out_path, $order_number, $date, $site_name, $dest_name, $dest_email, $dest_phone, $ship_addr, $shipping_method, $tracking, $items_lines, $total_fmt, $shipping_fmt, $discount_fmt );
			}

			// Gerador PDF mínimo nativo (RFC 3200 compliant, sem dependências)
			return $this->render_minimal_pdf( $out_path, $order_number, $date, $site_name, $dest_name, $dest_email, $dest_phone, $ship_addr, $shipping_method, $tracking, $items_lines, $total_fmt, $shipping_fmt, $discount_fmt );

		} catch ( \Throwable $e ) {
			$this->log( 'Native PDF error: ' . $e->getMessage() );
			return false;
		}
	}

	private function render_minimal_pdf( string $out_path, string $order_number, string $date, string $site_name, string $dest_name, string $dest_email, string $dest_phone, string $ship_addr, string $shipping_method, string $tracking, array $items_lines, string $total_fmt, string $shipping_fmt, string $discount_fmt ): bool {

		// Helper para limpar caracteres não-Latin1
		$c = function( string $s ): string {
			return iconv( 'UTF-8', 'ISO-8859-1//TRANSLIT//IGNORE', $s ) ?: $s;
		};

		// Estrutura PDF mínima válida
		$objects  = [];
		$offsets  = [];
		$obj_count = 0;

		$add = function( string $content ) use ( &$objects, &$obj_count ) {
			$obj_count++;
			$objects[ $obj_count ] = $content;
			return $obj_count;
		};

		// Página A4: 595 x 842 pts
		$W = 595; $H = 842;
		$margin = 40;
		$y = $H - $margin; // começa do topo (coordenadas PDF são de baixo para cima)

		// Constrói stream de conteúdo
		$stream = '';

		// Fonte
		$stream .= "BT\n";

		$line = function( string $text, int $x, int &$y, int $size = 10, bool $bold = false ) use ( &$stream, $W, $c ) {
			$text = $c( $text );
			$font = $bold ? '/F2' : '/F1';
			$stream_local = "{$font} {$size} Tf\n{$x} {$y} Td\n({$text}) Tj\nT*\n";
			$stream .= $stream_local;
			$y -= (int) ( $size * 1.4 );
		};

		$stream .= "ET\n";

		// Rebuild sem BT/ET aninhados — constrói linha a linha
		$stream = '';
		$lines  = [];

		$addLine = function( string $text, int $x, int $y_pos, int $size = 10, bool $bold = false ) use ( &$lines, $c ) {
			$text = str_replace( ['(', ')', '\\'], ['\\(', '\\)', '\\\\'], $c( $text ) );
			$font = $bold ? '/F2' : '/F1';
			$lines[] = "BT {$font} {$size} Tf {$x} {$y_pos} Td ({$text}) Tj ET";
		};

		$addRect = function( int $x, int $y_pos, int $w, int $h ) use ( &$lines ) {
			$lines[] = "{$x} {$y_pos} {$w} {$h} re S";
		};

		$addLine2 = function( int $x1, int $y1, int $x2, int $y2 ) use ( &$lines ) {
			$lines[] = "{$x1} {$y1} m {$x2} {$y2} l S";
		};

		// Título
		$y = 800;
		$addLine( 'DACE - DECLARACAO DE CONTEUDO', $margin, $y, 16, true );
		$y -= 22;
		$addLine( "Pedido #{$order_number}  |  {$date}", $margin, $y, 10 );
		$y -= 14;
		$addLine( $site_name, $margin, $y, 9 );
		$y -= 18;
		$addLine2( $margin, $y, $W - $margin, $y );
		$y -= 16;

		// Destinatário
		$addLine( 'Destinatario:', $margin, $y, 9, true );
		$y -= 13;
		$addLine( $dest_name, $margin, $y, 10 );
		$y -= 13;
		if ( $dest_email ) { $addLine( $dest_email, $margin, $y, 9 ); $y -= 13; }
		if ( $dest_phone ) { $addLine( $dest_phone, $margin, $y, 9 ); $y -= 13; }
		$y -= 4;

		// Endereço de entrega
		$addLine( 'Endereco de Entrega:', $margin, $y, 9, true );
		$y -= 13;
		// quebra endereço longo
		$addr_parts = explode( ',', $ship_addr );
		$addr_line  = '';
		foreach ( $addr_parts as $part ) {
			if ( strlen( $addr_line . $part ) > 80 ) {
				$addLine( trim( $addr_line ), $margin, $y, 9 ); $y -= 13;
				$addr_line = $part . ',';
			} else {
				$addr_line .= $part . ',';
			}
		}
		if ( $addr_line ) { $addLine( rtrim( trim( $addr_line ), ',' ), $margin, $y, 9 ); $y -= 13; }
		$y -= 4;

		// Frete e rastreio
		$addLine( "Metodo de Envio: {$shipping_method}", $margin, $y, 9 );
		$y -= 13;
		$addLine( "Rastreio: {$tracking}", $margin, $y, 9 );
		$y -= 18;
		$addLine2( $margin, $y, $W - $margin, $y );
		$y -= 14;

		// Cabeçalho tabela
		$addLine( 'Produto', $margin, $y, 9, true );
		$addLine( 'SKU', 310, $y, 9, true );
		$addLine( 'Qtd', 400, $y, 9, true );
		$addLine( 'Valor', 460, $y, 9, true );
		$y -= 12;
		$addLine2( $margin, $y, $W - $margin, $y );
		$y -= 12;

		foreach ( $items_lines as $item ) {
			$name = mb_substr( $item['name'], 0, 55 );
			$addLine( $name,         $margin, $y, 9 );
			$addLine( $item['sku'],  310,     $y, 9 );
			$addLine( (string) $item['qty'], 400, $y, 9 );
			$addLine( $item['total'], 460,    $y, 9 );
			$y -= 13;
		}

		$y -= 6;
		$addLine2( $margin, $y, $W - $margin, $y );
		$y -= 14;

		// Totais
		if ( $shipping_fmt ) { $addLine( "Frete: {$shipping_fmt}", 400, $y, 9 ); $y -= 13; }
		if ( $discount_fmt ) { $addLine( "Desconto: -{$discount_fmt}", 400, $y, 9 ); $y -= 13; }
		$addLine( "Total: {$total_fmt}", 400, $y, 10, true );
		$y -= 20;

		// Declaração
		$addLine2( $margin, $y, $W - $margin, $y );
		$y -= 14;
		$addLine( 'Declaracao:', $margin, $y, 9, true );
		$y -= 13;
		$addLine( 'Declaro que o conteudo informado corresponde aos itens do pedido acima.', $margin, $y, 8 );
		$y -= 12;
		$addLine( 'Este documento acompanha a postagem para conferencia logistica.', $margin, $y, 8 );
		$y -= 20;
		$addLine( 'DACE gerada por ' . $site_name . ' em ' . gmdate( 'd/m/Y H:i' ), $margin, $y, 8 );

		$stream = implode( "\n", $lines );
		$stream_len = strlen( $stream );

		// Monta objetos PDF
		$pdf = "%PDF-1.4\n";

		$obj_offsets = [];
		$obj_num     = 1;

		// Obj 1: catalog
		$obj_offsets[1] = strlen( $pdf );
		$pdf .= "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n";

		// Obj 2: pages
		$obj_offsets[2] = strlen( $pdf );
		$pdf .= "2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n";

		// Obj 3: page
		$obj_offsets[3] = strlen( $pdf );
		$pdf .= "3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 {$W} {$H}] /Contents 4 0 R /Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> >>\nendobj\n";

		// Obj 4: content stream
		$obj_offsets[4] = strlen( $pdf );
		$pdf .= "4 0 obj\n<< /Length {$stream_len} >>\nstream\n{$stream}\nendstream\nendobj\n";

		// Obj 5: font Helvetica
		$obj_offsets[5] = strlen( $pdf );
		$pdf .= "5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>\nendobj\n";

		// Obj 6: font Helvetica-Bold
		$obj_offsets[6] = strlen( $pdf );
		$pdf .= "6 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>\nendobj\n";

		// xref
		$xref_offset = strlen( $pdf );
		$pdf .= "xref\n0 7\n";
		$pdf .= "0000000000 65535 f \n";
		foreach ( $obj_offsets as $n => $off ) {
			$pdf .= str_pad( $off, 10, '0', STR_PAD_LEFT ) . " 00000 n \n";
		}

		$pdf .= "trailer\n<< /Size 7 /Root 1 0 R >>\n";
		$pdf .= "startxref\n{$xref_offset}\n%%EOF\n";

		file_put_contents( $out_path, $pdf );
		return file_exists( $out_path ) && filesize( $out_path ) > 100;
	}

	private function render_tcpdf( string $out_path, ...$args ): bool {
		// Placeholder — se TCPDF estiver instalado, delega para dompdf/mPDF acima.
		return false;
	}
}