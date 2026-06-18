<?php

namespace WC_MelhorEnvio\Traits;

use WC_Logger;

trait Logger {

	/**
	 * $log
	 *
	 * @var WC_Logger
	 */
	public static $log;

	public $source;

	public function log( $message, $level = 'info' ) {
		$integration = get_option( 'woocommerce_wc-melhor-envio_settings', [] );

		if ( isset( $integration['log'] ) && 'yes' !== $integration['log'] ) {
			return;
		}

		$this->source = 'wc-melhor-envio';

		$message = is_string( $message ) ? $message : print_r( $message, true );

		if ( ! isset( self::$log ) ) {
			self::$log = wc_get_logger();
		}

		self::$log->log( $level, $message, array( 'source' => $this->source ) );
	}
}
