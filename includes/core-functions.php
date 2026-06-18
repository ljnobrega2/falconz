<?php
if ( defined( 'SENDERZZ_CORE_FUNCTIONS_LOADED' ) ) return;
define( 'SENDERZZ_CORE_FUNCTIONS_LOADED', true );
if ( ! defined( 'ABSPATH' ) ) {
	exit;
}

function wc_melhor_envio_add_tracking_code( $order, $tracking_code, $save = false ) {
	$tracking_codes = wc_melhor_envio_get_tracking_codes( $order );

	if ( false !== array_search( $tracking_code, $tracking_codes ) ) {
		return false;
	}

	$tracking_codes[] = $tracking_code;

	$order->add_meta_data( '_melhor_envio_tracking_codes', $tracking_codes, true );

	do_action( 'wc_melhor_envio_new_tracking_code', $tracking_code, $order );

	$order->add_order_note( sprintf( __( 'Código de rastreio do Melhor Envio adicionado: %s', 'wc-melhor-envio' ),
		$tracking_code ) );

	$mailer       = WC()->mailer();
	$notification = $mailer->emails['melhor_envio_tracking_code'] ?? null;

	if ( $notification && $notification->is_enabled() ) {
		$notification->trigger( $order, $tracking_code );
	}

	if ( $save ) {
		$order->save();
	}

	return true;
}

function wc_melhor_envio_get_tracking_codes( $order ) {
	$tracking_codes = $order->get_meta( '_melhor_envio_tracking_codes' );

	return array_filter( is_array( $tracking_codes ) ? $tracking_codes : [ $tracking_codes ] );
}


function wc_melhor_envio_get_tracking_url( $tracking_code ) {
	return sprintf( 'https://www.melhorrastreio.com.br/meu-rastreio/%s', $tracking_code );
}


function wc_melhor_envio_get_formatted_tracking_url( $tracking_code, $button_text = '', $class = '' ) {
	return sprintf(
		'<a href="%s" target="_blank" class="melhor-envio-tracking-button %s">%s</a>',
		wc_melhor_envio_get_tracking_url( $tracking_code ),
		$class,
		$button_text ? $button_text : $tracking_code
	);
}


function wc_melhor_envio_get_account_methods() {
	$value = get_option( 'wc_melhor_envio_methods' );

	if ( ! is_array( $value ) ) {
		return wc_melhor_envio_update_account_methods( false );
	}

	// backward compatibility
	foreach ( $value as $method_id => $data ) {
		if ( is_string( $data ) ) {
			return wc_melhor_envio_update_account_methods( false );
		}
	}

	return $value;
}

function wc_melhor_envio_update_account_methods( $throw = true ) {
	$url      = 'https://www.melhorenvio.com.br';
	$endpoint = '/api/v2/me/shipment/services';

	$args = array(
		'timeout' => 10,
		'headers' => array(
			'Accept'       => 'application/json',
			'Content-Type' => 'application/json',
		),
		'method'  => 'GET',
	);

	try {

		$response = wp_remote_post( $url . $endpoint, $args );

		if ( is_wp_error( $response ) ) {
			return $response->get_error_message();
		}

		if ( 200 !== $response['response']['code'] ) {
			return 'Resposta inválida do Melhor Envio. Código retornado: ' . '';
		}

		$methods = json_decode( $response['body'] );

		$values = [];

		foreach ( $methods as $method ) {
			$values[ $method->id ] = [
				'id'      => $method->id,
				'name'    => $method->name,
				'company' => $method->company->name,
			];
		}

		update_option( 'wc_melhor_envio_methods', $values );

		return $values;

	} catch ( \Exception $e ) {
		if ( $throw ) {
			throw new \Exception( $e->getMessage() );
		}

		return $e->getMessage();
	}
}


function wc_melhor_envio_enabled_reverse_logistic() {
	return apply_filters( 'wc_melhor_envio_enabled_reverse_logistic', false );
}


function wc_melhor_envio_get_script_url() {
	return WC_Melhor_Envio::plugin_url() . '/assets/js/orders-1.7.4.min.js';
}


/**
 * Retorna os IDs de métodos do ME que estão com "Ativar este método" marcado
 * em pelo menos uma zona de envio do WooCommerce.
 * Usado para filtrar a lista de seleção no painel e no portal.
 *
 * @return int[]  Lista de method_ids habilitados (ex: [1, 3, 7])
 */
function senderzz_get_enabled_me_method_ids(): array {
    static $cache = null;
    if ( $cache !== null ) return $cache;

    $enabled = [];

    if ( ! function_exists( 'WC' ) || ! WC()->shipping ) {
        $cache = $enabled;
        return $cache;
    }

    // Itera todas as zonas de envio incluindo a zona "rest of the world" (zone_id=0)
    $zones = WC_Shipping_Zones::get_zones();
    $zones[] = [ 'zone_id' => 0 ]; // zona padrão

    foreach ( $zones as $zone_data ) {
        $zone = new WC_Shipping_Zone( $zone_data['zone_id'] );
        foreach ( $zone->get_shipping_methods( false ) as $method ) {
            // Só processa instâncias do plugin Melhor Envio
            if ( strpos( $method->id, 'melhor_envio' ) === false ) continue;

            // A opção 'labels' armazena [ method_id => ['enabled' => true/false, ...] ]
            $labels = $method->get_option( 'labels' );
            if ( ! is_array( $labels ) ) continue;

            foreach ( $labels as $mid => $data ) {
                if ( ! empty( $data['enabled'] ) ) {
                    $enabled[] = (int) $mid;
                }
            }
        }
    }

    $cache = array_values( array_unique( $enabled ) );
    return $cache;
}


// ─── Helpers globais de fuso horário Brasil (America/Sao_Paulo) ──────────────

if ( ! function_exists( 'sz_br_tz' ) ) {
    function sz_br_tz(): DateTimeZone {
        return new DateTimeZone( 'America/Sao_Paulo' );
    }
}

if ( ! function_exists( 'sz_br_now' ) ) {
    /** Retorna DateTimeImmutable em horário de Brasília. */
    function sz_br_now(): DateTimeImmutable {
        return new DateTimeImmutable( 'now', sz_br_tz() );
    }
}

if ( ! function_exists( 'sz_br_now_mysql' ) ) {
    /** Retorna string MySQL em horário de Brasília. */
    function sz_br_now_mysql(): string {
        return sz_br_now()->format( 'Y-m-d H:i:s' );
    }
}

if ( ! function_exists( 'sz_br_format' ) ) {
    /**
     * Formata uma data/timestamp em horário de Brasília.
     * Aceita: string MySQL, timestamp int, ou DateTimeInterface.
     */
    function sz_br_format( $value, string $format = 'd/m/Y H:i' ): string {
        if ( ! $value ) return '—';
        try {
            if ( $value instanceof DateTimeInterface ) {
                $dt = DateTimeImmutable::createFromInterface( $value )->setTimezone( sz_br_tz() );
            } elseif ( is_int( $value ) || ctype_digit( (string) $value ) ) {
                $dt = ( new DateTimeImmutable( '@' . (int) $value ) )->setTimezone( sz_br_tz() );
            } else {
                $str = trim( (string) $value );
                // Datas já em horário BR (gravadas com sz_br_now_mysql) — não converter
                // Datas UTC teriam sufixo Z ou +00:00
                $is_utc = ( substr($str,-1) === 'Z' || strpos($str,'+00:00')!==false || strpos($str,'UTC')!==false );
                if ( $is_utc ) {
                    $dt = ( new DateTimeImmutable( $str ) )->setTimezone( sz_br_tz() );
                } else {
                    $dt = new DateTimeImmutable( $str, sz_br_tz() );
                }
            }
            return $dt->format( $format );
        } catch ( \Exception $e ) {
            return (string) $value;
        }
    }
}
