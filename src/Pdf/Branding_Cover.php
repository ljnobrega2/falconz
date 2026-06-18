<?php

namespace WC_MelhorEnvio\Pdf;

use Exception;
use WC_MelhorEnvio\Traits\Helpers;
use WC_MelhorEnvio\Traits\Logger;

/**
 * Branding_Cover
 *
 * Gera uma folha de capa de branding para ser incluída antes da etiqueta.
 * A capa exibe: logo da loja, mensagem personalizada, dados do pedido e
 * informações de contato/social.
 *
 * Configurável via filtros WordPress:
 *   - wc_melhor_envio_cover_logo       → URL da logo
 *   - wc_melhor_envio_cover_message    → Mensagem personalizada (HTML permitido)
 *   - wc_melhor_envio_cover_color      → Cor principal (#hex)
 *   - wc_melhor_envio_cover_footer     → Texto do rodapé
 */
class Branding_Cover {
	use Helpers, Logger;

	protected $logger = 'wc-melhor-envio-cover';

	/**
	 * generate
	 * Gera a capa de branding para o pedido.
	 *
	 * @param \WC_Order $order
	 * @return string  Caminho absoluto do PDF gerado
	 * @throws Exception
	 */
	public function generate( $order ): string {
		$existing = $order->get_meta( '_melhor_envio_cover_path' );
		if ( $existing && file_exists( $existing ) ) {
			return $existing;
		}

		$upload_dir = wp_upload_dir();
		$subdir     = '/melhor-envio-labels/covers';
		$dir        = trailingslashit( $upload_dir['basedir'] ) . ltrim( $subdir, '/' );

		if ( ! file_exists( $dir ) ) {
			wp_mkdir_p( $dir );
		}

		$file_name = sprintf( 'capa-pedido-%d-%s.pdf', $order->get_id(), gmdate( 'Ymd' ) );
		$file_path = trailingslashit( $dir ) . $file_name;

		$html = $this->build_html( $order );

		if ( $this->render_dompdf( $html, $file_path ) || $this->render_mpdf( $html, $file_path ) ) {
			$this->save_meta( $order, $file_path, $upload_dir, $subdir, $file_name );
			return $file_path;
		}

		// Fallback HTML
		file_put_contents( $file_path, $html );
		$this->log( 'Capa gerada em HTML (sem biblioteca PDF). Instale dompdf ou mPDF para PDF real.' );
		$this->save_meta( $order, $file_path, $upload_dir, $subdir, $file_name );

		return $file_path;
	}


	private function build_html( $order ): string {
		$site_name   = get_bloginfo( 'name' );
		$logo_url    = apply_filters( 'wc_melhor_envio_cover_logo', '' );
		$message     = apply_filters(
			'wc_melhor_envio_cover_message',
			'<p>Obrigado pela sua compra! Esperamos que você adore seu pedido. 🎉</p><p>Em caso de dúvidas, entre em contato conosco.</p>'
		);
		$color       = apply_filters( 'wc_melhor_envio_cover_color', '#0073aa' );
		$footer_text = apply_filters(
			'wc_melhor_envio_cover_footer',
			get_bloginfo( 'url' ) . ' · ' . get_option( 'admin_email' )
		);

		$customer_name = $order->get_formatted_billing_full_name();
		$order_number  = $order->get_order_number();
		$order_date    = wc_format_datetime( $order->get_date_created() );

		$html = '<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: var(--sz-font);
    background: #fff;
    color: #333;
    height: 297mm;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 40px;
    text-align: center;
  }
  .top-bar {
    width: 100%;
    height: 8px;
    background: ' . esc_attr( $color ) . ';
    position: fixed;
    top: 0;
    left: 0;
  }
  .bottom-bar {
    width: 100%;
    height: 8px;
    background: ' . esc_attr( $color ) . ';
    position: fixed;
    bottom: 0;
    left: 0;
  }
  .logo { margin-bottom: 32px; }
  .logo img { max-height: 80px; max-width: 240px; }
  .logo-text { font-size: var(--sz-text-3xl); font-weight: 700; color: ' . esc_attr( $color ) . '; }
  .order-badge {
    background: ' . esc_attr( $color ) . ';
    color: #fff;
    border-radius: 50px;
    padding: 8px 28px;
    font-size: var(--sz-text-md);
    font-weight: 700;
    margin-bottom: 24px;
    display: inline-block;
  }
  .greeting {
    font-size: var(--sz-text-xl);
    font-weight: 700;
    margin-bottom: 8px;
    color: #111;
  }
  .message {
    font-size: var(--sz-text-md);
    color: #555;
    max-width: 400px;
    line-height: 1.7;
    margin: 0 auto 32px auto;
  }
  .info-box {
    border: 1px solid #e0e0e0;
    border-radius: 8px;
    padding: 16px 28px;
    margin-bottom: 32px;
    display: inline-block;
    min-width: 260px;
  }
  .info-box small { color: #999; font-size: var(--sz-text-sm); text-transform: none; letter-spacing:0; display: block; margin-bottom: 4px; }
  .info-box span { font-size: var(--sz-text-lg); font-weight: 700; color: #111; }
  .footer {
    font-size: var(--sz-text-sm);
    color: #aaa;
    margin-top: 20px;
  }
</style>
</head>
<body>
  <div class="top-bar"></div>';

		$html .= '<div class="logo">';
		if ( $logo_url ) {
			$html .= '<img src="' . esc_url( $logo_url ) . '" alt="' . esc_attr( $site_name ) . '">';
		} else {
			$html .= '<div class="logo-text">' . esc_html( $site_name ) . '</div>';
		}
		$html .= '</div>';

		$html .= '<div class="order-badge">Pedido #' . esc_html( $order_number ) . ' · ' . esc_html( $order_date ) . '</div>';
		$html .= '<div class="greeting">Olá, ' . esc_html( explode( ' ', $customer_name )[0] ) . '! 👋</div>';
		$html .= '<div class="message">' . wp_kses_post( $message ) . '</div>';

		$html .= '<div class="info-box">
			<small>Destinatário</small>
			<span>' . esc_html( $customer_name ) . '</span>
		</div>';

		$html .= '<div class="footer">' . wp_kses_post( $footer_text ) . '</div>';
		$html .= '<div class="bottom-bar"></div>';
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
			$this->log( 'Dompdf error (cover): ' . $e->getMessage() );
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
			$this->log( 'mPDF error (cover): ' . $e->getMessage() );
			return false;
		}
	}


	private function save_meta( $order, string $file_path, array $upload_dir, string $subdir, string $file_name ) {
		$file_url = trailingslashit( $upload_dir['baseurl'] ) . trim( $subdir, '/' ) . '/' . $file_name;
		$order->update_meta_data( '_melhor_envio_cover_path', $file_path );
		$order->update_meta_data( '_melhor_envio_cover_url', $file_url );
		$order->save();
	}
}
