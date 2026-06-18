<?php
/**
 * Senderzz — Módulo Motoboy
 * Router: roteamento automático CEP → CD → Zona → Motoboy
 */
if ( ! defined( 'ABSPATH' ) ) exit;


if ( ! function_exists( 'sz_motoboy_tracking_url' ) ) {
    // V-SEC-03: URL canônica de rastreio motoboy — sempre inclui ?key= para habilitar reagendamento.
    function sz_motoboy_tracking_url( int $wc_order_id ): string {
        $order = function_exists( 'wc_get_order' ) ? wc_get_order( $wc_order_id ) : null;
        $key   = ( $order instanceof \WC_Order ) ? $order->get_order_key() : '';
        return add_query_arg(
            [ 'pedido' => $wc_order_id, 'key' => $key ],
            home_url( '/rastreio-motoboy/' )
        );
    }
}

if ( ! function_exists( 'sz_motoboy_sanitize_zone_days' ) ) {
    function sz_motoboy_sanitize_zone_days( $days ): string {
        if ( is_string( $days ) ) $days = preg_split( '/[^0-6]+/', $days, -1, PREG_SPLIT_NO_EMPTY );
        if ( ! is_array( $days ) ) $days = [];
        $clean = [];
        foreach ( $days as $day ) {
            $day = (string) (int) $day;
            if ( in_array( $day, [ '0','1','2','3','4','5','6' ], true ) ) $clean[] = $day;
        }
        $clean = array_values( array_unique( $clean ) );
        sort( $clean, SORT_NUMERIC );
        return $clean ? implode( ',', $clean ) : '1,2,3,4,5,6';
    }
}

if ( ! function_exists( 'sz_motoboy_zone_days_array' ) ) {
    function sz_motoboy_zone_days_array( $days ): array {
        return array_map( 'intval', explode( ',', sz_motoboy_sanitize_zone_days( $days ) ) );
    }
}

if ( ! function_exists( 'sz_motoboy_zone_days_label' ) ) {
    function sz_motoboy_zone_days_label( $days ): string {
        $names = [ 0 => 'Dom', 1 => 'Seg', 2 => 'Ter', 3 => 'Qua', 4 => 'Qui', 5 => 'Sex', 6 => 'Sáb' ];
        $arr = sz_motoboy_zone_days_array( $days );
        return implode( ', ', array_map( fn( $d ) => $names[ $d ] ?? (string) $d, $arr ) );
    }
}



if ( ! function_exists( 'sz_motoboy_sanitize_single_cutoff_time' ) ) {
    /** Sanitiza um único horário limite da zona. */
    function sz_motoboy_sanitize_single_cutoff_time( $time, string $default = '21:00' ): string {
        $time = trim( (string) $time );
        if ( preg_match( '/^(\d{1,2}):(\d{2})$/', $time, $m ) ) {
            $h = max( 0, min( 23, (int) $m[1] ) );
            $i = max( 0, min( 59, (int) $m[2] ) );
            return sprintf( '%02d:%02d', $h, $i );
        }
        return $default;
    }
}

if ( ! function_exists( 'sz_motoboy_single_cutoff_payload' ) ) {
    /**
     * Cria o payload legado de cutoff_horarios usando UM horário único.
     * O banco continua aceitando JSON por compatibilidade, mas a operação agora é simples:
     * zona selecionada = dias de atendimento + um único horário limite para todos os dias ativos.
     */
    function sz_motoboy_single_cutoff_payload( $time ): string {
        $time = sz_motoboy_sanitize_single_cutoff_time( $time );
        return sz_motoboy_sanitize_zone_cutoffs( [
            0 => $time, 1 => $time, 2 => $time, 3 => $time, 4 => $time, 5 => $time, 6 => $time,
        ] );
    }
}

if ( ! function_exists( 'sz_motoboy_zone_single_cutoff_time' ) ) {
    /** Retorna o horário único exibido para a zona, usando o primeiro dia ativo como referência. */
    function sz_motoboy_zone_single_cutoff_time( $cutoffs, $days = '' ): string {
        $arr = sz_motoboy_zone_cutoffs_array( $cutoffs );
        $active = sz_motoboy_zone_days_array( $days );
        foreach ( $active as $d ) {
            if ( isset( $arr[ (string) $d ] ) ) return sz_motoboy_sanitize_single_cutoff_time( $arr[ (string) $d ] );
        }
        foreach ( $arr as $v ) return sz_motoboy_sanitize_single_cutoff_time( $v );
        return '21:00';
    }
}

if ( ! function_exists( 'sz_motoboy_sanitize_zone_cutoffs' ) ) {
    /**
     * Horário limite por dia de entrega.
     * Ex: dia 6 (sábado) = 18:00 significa que sábado só pode ser agendado
     * até sexta às 18:00. Padrão: todos os dias 21:00 do dia anterior.
     */
    function sz_motoboy_sanitize_zone_cutoffs( $cutoffs ): string {
        if ( is_string( $cutoffs ) ) {
            $decoded = json_decode( $cutoffs, true );
            if ( is_array( $decoded ) ) { $cutoffs = $decoded; }
        }
        if ( ! is_array( $cutoffs ) ) $cutoffs = [];
        $clean = [];
        for ( $d = 0; $d <= 6; $d++ ) {
            $raw = isset( $cutoffs[ $d ] ) ? (string) $cutoffs[ $d ] : ( isset( $cutoffs[ (string) $d ] ) ? (string) $cutoffs[ (string) $d ] : '21:00' );
            $raw = trim( $raw );
            if ( preg_match( '/^(\d{1,2}):(\d{2})$/', $raw, $m ) ) {
                $h = max( 0, min( 23, (int) $m[1] ) );
                $i = max( 0, min( 59, (int) $m[2] ) );
                $clean[ (string) $d ] = sprintf( '%02d:%02d', $h, $i );
            } else {
                $clean[ (string) $d ] = '21:00';
            }
        }
        return wp_json_encode( $clean );
    }
}

if ( ! function_exists( 'sz_motoboy_zone_cutoffs_array' ) ) {
    function sz_motoboy_zone_cutoffs_array( $cutoffs ): array {
        $json = sz_motoboy_sanitize_zone_cutoffs( $cutoffs );
        $arr = json_decode( $json, true );
        return is_array( $arr ) ? $arr : [ '0'=>'21:00','1'=>'21:00','2'=>'21:00','3'=>'21:00','4'=>'21:00','5'=>'21:00','6'=>'21:00' ];
    }
}

if ( ! function_exists( 'sz_motoboy_zone_cutoff_label' ) ) {
    function sz_motoboy_zone_cutoff_label( $cutoffs, $days = '' ): string {
        $time = function_exists( 'sz_motoboy_zone_single_cutoff_time' )
            ? sz_motoboy_zone_single_cutoff_time( $cutoffs, $days )
            : '21:00';
        return 'Limite até ' . $time . ' do dia anterior';
    }
}

if ( ! function_exists( 'sz_motoboy_weekday_full_name' ) ) {
    function sz_motoboy_weekday_full_name( int $dow ): string {
        $names = [ 0 => 'domingo', 1 => 'segunda-feira', 2 => 'terça-feira', 3 => 'quarta-feira', 4 => 'quinta-feira', 5 => 'sexta-feira', 6 => 'sábado' ];
        return $names[ $dow ] ?? 'dia informado';
    }
}

if ( ! function_exists( 'sz_motoboy_previous_weekday_full_name' ) ) {
    function sz_motoboy_previous_weekday_full_name( int $dow ): string {
        $prev = ( $dow + 6 ) % 7;
        return sz_motoboy_weekday_full_name( $prev );
    }
}


if ( ! function_exists( 'sz_motoboy_format_time_commercial' ) ) {
    function sz_motoboy_format_time_commercial( string $time ): string {
        $time = sz_motoboy_sanitize_single_cutoff_time( $time );
        $h = (int) substr( $time, 0, 2 );
        $m = (int) substr( $time, 3, 2 );
        return $m === 0 ? sprintf( '%dh', $h ) : sprintf( '%dh%02d', $h, $m );
    }
}

if ( ! function_exists( 'sz_motoboy_zone_cutoff_commercial_message' ) ) {
    /**
     * Copy pública/comercial para checkout: explica a regra da zona identificada pelo CEP,
     * sem termos técnicos como cutoff ou configuração interna.
     */
    function sz_motoboy_zone_cutoff_commercial_message( $days, $cutoffs = '', string $zona_nome = '' ): string {
        $active = sz_motoboy_zone_days_array( $days );
        $time   = function_exists( 'sz_motoboy_zone_single_cutoff_time' ) ? sz_motoboy_zone_single_cutoff_time( $cutoffs, $days ) : '21:00';
        $prefix = $zona_nome ? 'Região de ' . $zona_nome . ': ' : '';

        if ( count( $active ) === 1 ) {
            $dow = (int) $active[0];
            $delivery = sz_motoboy_weekday_full_name( $dow );
            $limitday = sz_motoboy_previous_weekday_full_name( $dow );
            return $prefix . 'entregamos na ' . $delivery . '. Para receber nessa rota, finalize o pedido até ' . $limitday . ' às ' . sz_motoboy_format_time_commercial( $time ) . '.';
        }

        $label = sz_motoboy_zone_days_label( $days );
        if ( $label === 'Seg, Ter, Qua, Qui, Sex, Sáb' ) {
            return $prefix . 'entregas de segunda a sábado. Para receber no próximo dia disponível, finalize até ' . sz_motoboy_format_time_commercial( $time ) . ' do dia anterior.';
        }

        return $prefix . 'entregas em ' . $label . '. Finalize até ' . sz_motoboy_format_time_commercial( $time ) . ' do dia anterior para garantir a próxima rota disponível.';
    }
}

if ( ! function_exists( 'sz_motoboy_zone_date_is_allowed' ) ) {
    function sz_motoboy_zone_date_is_allowed( string $ymd, $days, $cutoffs = '' ): bool {
        if ( ! preg_match( '/^\d{4}-\d{2}-\d{2}$/', $ymd ) ) return false;
        $allowed = sz_motoboy_zone_days_array( $days );
        $cut = sz_motoboy_zone_cutoffs_array( $cutoffs );
        try {
            $tz = new DateTimeZone( 'America/Sao_Paulo' );
            $now = new DateTimeImmutable( 'now', $tz );
            $target = new DateTimeImmutable( $ymd . ' 00:00:00', $tz );
        } catch ( Throwable $e ) {
            return false;
        }
        $today = new DateTimeImmutable( $now->format( 'Y-m-d' ) . ' 00:00:00', $tz );
        if ( $target <= $today ) return false;
        $dow = (int) $target->format( 'w' );
        if ( ! in_array( $dow, $allowed, true ) ) return false;
        $time = $cut[ (string) $dow ] ?? '21:00';
        $deadline = $target->modify( '-1 day' )->setTime( (int) substr( $time, 0, 2 ), (int) substr( $time, 3, 2 ), 0 );
        return $now <= $deadline;
    }
}

if ( ! function_exists( 'sz_motoboy_zone_next_dates' ) ) {
    function sz_motoboy_zone_next_dates( $days, int $qty = 3, $cutoffs = '' ): array {
        $allowed = sz_motoboy_zone_days_array( $days );
        $cut = sz_motoboy_zone_cutoffs_array( $cutoffs );
        $dates = [];
        try {
            $tz = new DateTimeZone( 'America/Sao_Paulo' );
            $now = new DateTimeImmutable( 'now', $tz );
            $tomorrow = $now->modify( '+1 day' )->format( 'Y-m-d' );
        } catch ( Throwable $e ) {
            $tz = wp_timezone();
            $now = new DateTimeImmutable( 'now', $tz );
            $tomorrow = wp_date( 'Y-m-d', strtotime( '+1 day' ) );
        }
        $short = [ 'DOM','SEG','TER','QUA','QUI','SEX','SÁB' ];
        $full  = [ 'Domingo','Segunda-feira','Terça-feira','Quarta-feira','Quinta-feira','Sexta-feira','Sábado' ];
        $meses = [ 1=>'Jan',2=>'Fev',3=>'Mar',4=>'Abr',5=>'Mai',6=>'Jun',7=>'Jul',8=>'Ago',9=>'Set',10=>'Out',11=>'Nov',12=>'Dez' ];
        for ( $i = 1; count( $dates ) < $qty && $i < 45; $i++ ) {
            $dt = $now->modify( '+' . $i . ' days' );
            $dow = (int) $dt->format( 'w' );
            if ( ! in_array( $dow, $allowed, true ) ) continue;
            $time = $cut[ (string) $dow ] ?? '21:00';
            $deadline = $dt->modify( '-1 day' )->setTime( (int) substr( $time, 0, 2 ), (int) substr( $time, 3, 2 ), 0 );
            if ( $now > $deadline ) continue;
            $ymd = $dt->format( 'Y-m-d' );
            $label = ( $ymd === $tomorrow ) ? 'Amanhã' : $full[ $dow ];
            $dates[] = [
                'value' => $ymd,
                'dow'   => $short[ $dow ],
                'full'  => $label,
                'label' => $label,
                'day'   => $dt->format( 'd' ),
                'month' => $meses[ (int) $dt->format( 'n' ) ],
                'cutoff'=> $time,
            ];
        }
        return $dates;
    }
}

/**
 * Resolve zona a partir de CEP (apenas dígitos, 8 chars).
 * Retorna array ['cd_id'=>x,'zona_id'=>y] ou null.
 */
function sz_motoboy_resolver_zona( string $cep ): ?array {
    global $wpdb;
    $cep = preg_replace( '/\D/', '', $cep );
    if ( strlen( $cep ) !== 8 ) return null;

    $p = $wpdb->prefix;
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT z.id AS zona_id, z.cd_id, z.nome AS zona_nome, z.dias_funcionamento, z.cutoff_horarios
           FROM {$p}sz_motoboy_cep_zonas cz
           JOIN {$p}sz_motoboy_zonas z ON z.id = cz.zona_id AND z.ativo = 1
          WHERE cz.cep_inicio <= %s AND cz.cep_fim >= %s
          LIMIT 1",
        $cep, $cep
    ) );

    if ( ! $row ) return null;
    return [ 'cd_id' => (int) $row->cd_id, 'zona_id' => (int) $row->zona_id, 'zona_nome' => sanitize_text_field( (string) ( $row->zona_nome ?? '' ) ), 'dias_funcionamento' => sz_motoboy_sanitize_zone_days( $row->dias_funcionamento ?? '' ), 'cutoff_horarios' => sz_motoboy_sanitize_zone_cutoffs( $row->cutoff_horarios ?? '' ) ];
}

/**
 * Seleciona motoboy ativo da zona com menos pedidos em aberto hoje.
 * Fallback: se zona vazia, busca motoboy do CD com faixa de CEP mais próxima.
 * Retorna motoboy_id ou null.
 */
function sz_motoboy_selecionar_motoboy( int $zona_id, int $cd_id, string $cep = '' ): ?int {
    global $wpdb;
    $p       = $wpdb->prefix;
    $hoje_br = ( new DateTimeImmutable( 'now', new DateTimeZone( 'America/Sao_Paulo' ) ) )->format( 'Y-m-d' );

    // Tentativa 1: motoboy da zona (principal ou pivot) com menor carga
    $pivot_table = $p . 'sz_motoboy_zona_pivot';
    $has_pivot = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $pivot_table ) );

    // Consulta inclui pivot de múltiplas zonas
    $zona_id_int = (int) $zona_id;
    $cd_id_int   = (int) $cd_id;
    if ( $has_pivot ) {
        $row = $wpdb->get_row(
            "SELECT m.id, COUNT(mp.id) AS pedidos_abertos
               FROM {$p}sz_motoboys m
               LEFT JOIN {$p}sz_motoboy_pedidos mp
                      ON mp.motoboy_id = m.id
                     AND mp.status IN ('embalado','em_rota')
                     AND DATE(mp.created_at) = '{$hoje_br}'
              WHERE m.cd_id = {$cd_id_int}
                AND m.ativo = 1
                AND (m.zona_id = {$zona_id_int}
                     OR EXISTS (SELECT 1 FROM {$pivot_table} pv WHERE pv.motoboy_id = m.id AND pv.zona_id = {$zona_id_int}))
              GROUP BY m.id
              ORDER BY pedidos_abertos ASC
              LIMIT 1"
        ); // phpcs:ignore -- IDs inteiros sanitizados acima
    } else {
        $row = $wpdb->get_row( $wpdb->prepare(
            "SELECT m.id, COUNT(mp.id) AS pedidos_abertos
               FROM {$p}sz_motoboys m
               LEFT JOIN {$p}sz_motoboy_pedidos mp
                      ON mp.motoboy_id = m.id
                     AND mp.status IN ('embalado','em_rota')
                     AND DATE(mp.created_at) = %s
              WHERE m.zona_id = %d AND m.cd_id = %d AND m.ativo = 1
              GROUP BY m.id ORDER BY pedidos_abertos ASC LIMIT 1",
            $hoje_br, $zona_id, $cd_id
        ) );
    }

    if ( $row ) return (int) $row->id;

    // Fallback: nenhum motoboy na zona — busca qualquer motoboy ativo do CD
    // ordenado por proximidade numérica de CEP (motoboy cujo zona_id cobre o CEP mais próximo)
    if ( $cep ) {
        $cep_num = (int) preg_replace( '/\D/', '', $cep );

        $fallback = $wpdb->get_row( $wpdb->prepare(
            "SELECT m.id,
                    ABS( ( CAST(cz.cep_inicio AS UNSIGNED) + CAST(cz.cep_fim AS UNSIGNED) ) / 2 - %d ) AS distancia_cep,
                    COUNT(mp.id) AS pedidos_abertos
               FROM {$p}sz_motoboys m
               JOIN {$p}sz_motoboy_zonas z    ON z.id = m.zona_id AND z.ativo = 1
               JOIN {$p}sz_motoboy_cep_zonas cz ON cz.zona_id = z.id
               LEFT JOIN {$p}sz_motoboy_pedidos mp
                      ON mp.motoboy_id = m.id
                     AND mp.status IN ('embalado','em_rota')
                     AND DATE(mp.created_at) = %s
              WHERE m.cd_id = %d
                AND m.ativo = 1
              GROUP BY m.id, cz.id
              ORDER BY pedidos_abertos ASC, distancia_cep ASC
              LIMIT 1",
            $hoje_br, $cep_num, $cd_id
        ) );

        if ( $fallback ) {
            sz_motoboy_audit( [
                'motoboy_id' => (int) $fallback->id,
                'acao'       => 'fallback_zona',
                'meta'       => [ 'zona_original' => $zona_id, 'cep' => $cep, 'cd_id' => $cd_id ],
            ] );
            return (int) $fallback->id;
        }
    }

    // Último recurso: qualquer motoboy ativo do CD com menor carga
    $last = $wpdb->get_row( $wpdb->prepare(
        "SELECT m.id, COUNT(mp.id) AS pedidos_abertos
           FROM {$p}sz_motoboys m
           LEFT JOIN {$p}sz_motoboy_pedidos mp
                  ON mp.motoboy_id = m.id
                 AND mp.status IN ('embalado','em_rota')
                 AND DATE(mp.created_at) = %s
          WHERE m.cd_id = %d AND m.ativo = 1
          GROUP BY m.id
          ORDER BY pedidos_abertos ASC
          LIMIT 1",
        $hoje_br, $cd_id
    ) );

    return $last ? (int) $last->id : null;
}

/**
 * Cria entrada em sz_motoboy_pedidos para um pedido WooCommerce.
 * Retorna ID inserido ou false.
 */
function sz_motoboy_criar_pedido( int $wc_order_id ): int|false {
    global $wpdb;

    $order = wc_get_order( $wc_order_id );
    if ( ! $order ) return false;

    // Já existe?
    $existe = $wpdb->get_var( $wpdb->prepare(
        "SELECT id FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d",
        $wc_order_id
    ) );
    if ( $existe ) return (int) $existe;

    $cep = preg_replace( '/\D/', '', $order->get_shipping_postcode() ?: $order->get_billing_postcode() );
    $zona = sz_motoboy_resolver_zona( $cep );

    /*
     * COD Motoboy precisa nascer na fila operacional mesmo quando ainda não há
     * zona/faixa de CEP configurada. Antes o insert abortava quando $zona era
     * vazio; por isso pedidos HPOS wc-agendado/cod apareciam no portal do
     * produtor, mas nunca chegavam ao OL. Aqui a linha é criada como agendada
     * com cd_id/zona_id = 0 e motoboy_id nulo. A distribuição por CEP continua
     * sendo aplicada quando existir zona válida.
     */
    $cd_id      = is_array( $zona ) ? (int) ( $zona['cd_id'] ?? 0 ) : 0;
    $zona_id    = is_array( $zona ) ? (int) ( $zona['zona_id'] ?? 0 ) : 0;
    // v349: pedido COD nasce sem motoboy por padrão. A escolha deve ser feita
    // pelo OL/admin ou assumida pelo motoboy no fluxo correto; nunca pré-selecionar Alan/outro.
    $motoboy_id = null;

    $valor_pedido = (float) $order->get_total();
    $valor_taxa   = (float) get_option( 'sz_motoboy_taxa_entrega', 25.00 );
    $dest_nome    = trim( $order->get_shipping_first_name() . ' ' . $order->get_shipping_last_name() );
    if ( $dest_nome === '' ) {
        $dest_nome = trim( $order->get_billing_first_name() . ' ' . $order->get_billing_last_name() );
    }

    $row = [
        'wc_order_id'  => $wc_order_id,
        'cd_id'        => $cd_id,
        'zona_id'      => $zona_id,
        'motoboy_id'   => $motoboy_id,
        'status'       => 'agendado',
        'dest_nome'    => $dest_nome,
        'dest_telefone'=> $order->get_billing_phone(),
        'dest_cep'     => $cep,
        'dest_endereco'=> (function() use ( $order ): string {
            $addr1 = trim( $order->get_shipping_address_1() ?: $order->get_billing_address_1() );
            // Remove número do final da rua se vier junto (ex: "Rua Tal, 9 B" → "Rua Tal")
            // Padrão: rua seguida de vírgula + número OU rua seguida de espaço + número no final
            if ( preg_match( '/^(.*?),\s*\d+/u', $addr1, $m ) ) {
                return trim( $m[1] );
            }
            return $addr1;
        })(),
        'dest_numero'  => (function() use ( $order ): string {
            // Prioridade 1: meta BR dedicada (_shipping_number / _billing_number)
            $num = (string) (
                $order->get_meta( '_shipping_number', true ) ?:
                $order->get_meta( '_billing_number',  true ) ?: ''
            );
            if ( $num !== '' ) return $num;
            // Prioridade 2: extrair do address_1 após a vírgula (ex: "Rua Tal, 9 B" → "9")
            $addr1 = trim( $order->get_shipping_address_1() ?: $order->get_billing_address_1() );
            if ( preg_match( '/,\s*(\d+\w*)/u', $addr1, $m ) ) {
                return $m[1];
            }
            // Prioridade 3: address_2 APENAS se começar com dígito
            $addr2 = trim( (string) ( $order->get_shipping_address_2() ?: $order->get_billing_address_2() ) );
            if ( $addr2 !== '' && preg_match( '/^(\d+\w*)/u', $addr2, $m ) ) {
                return $m[1];
            }
            return '';
        })(),
        'dest_complemento' => (function() use ( $order ): string {
            // Prioridade 1: meta BR dedicada
            $comp = (string) (
                $order->get_meta( '_shipping_complement', true ) ?:
                $order->get_meta( '_billing_complement',  true ) ?: ''
            );
            if ( $comp !== '' ) return $comp;
            // Prioridade 2: texto após o número no address_1 (ex: "Rua Tal, 9 B" → "B")
            $addr1 = trim( $order->get_shipping_address_1() ?: $order->get_billing_address_1() );
            if ( preg_match( '/,\s*\d+\w*\s+(.*)/u', $addr1, $m ) ) {
                return trim( $m[1] );
            }
            // Prioridade 3: address_2 — se não começa com dígito é 100% complemento
            $addr2 = trim( (string) ( $order->get_shipping_address_2() ?: $order->get_billing_address_2() ) );
            if ( $addr2 === '' ) return '';
            if ( preg_match( '/^\d+\w*\s+(.*)/u', $addr2, $m ) ) return trim( $m[1] );
            if ( ! preg_match( '/^\d/u', $addr2 ) ) return $addr2;
            return '';
        })(),
        'dest_bairro'  => $order->get_meta('_billing_neighborhood') ?: $order->get_meta('_shipping_neighborhood') ?: '',
        'dest_cidade'  => $order->get_shipping_city() ?: $order->get_billing_city(),
        'dest_produto' => (function() use ($order): string {
            // Regra Senderzz: quantidade + nome, uma vez só (sem itens após a vírgula).
            if ( function_exists( 'senderzz_order_primary_item_label' ) ) {
                $label = senderzz_order_primary_item_label( $order );
                if ( $label !== '' ) return $label;
            }
            foreach ( $order->get_items() as $item ) {
                $qty  = (int) $item->get_quantity();
                $name = function_exists( 'senderzz_clean_product_label' ) ? senderzz_clean_product_label( $item->get_name() ) : trim( $item->get_name() );
                if ( $name === '' ) continue;
                return ( max( 1, $qty ) ) . 'x ' . $name;
            }
            return '';
        })(),
        'quantidade'   => (function() use ($order): int {
            $total = 0;
            foreach ( $order->get_items() as $item ) $total += (int) $item->get_quantity();
            return $total;
        })(),
        'dest_uf'      => $order->get_shipping_state() ?: $order->get_billing_state(),
        'valor_pedido' => $valor_pedido,
        'valor_taxa'   => $valor_taxa,
        'ts_aprovado'  => sz_motoboy_now_mysql(),
    ];

    // v320: protege contra bancos antigos com colunas faltantes. Antes, uma coluna
    // ausente como `quantidade` fazia o insert inteiro falhar e o OL ficava vazio.
    $table_pedidos = $wpdb->prefix . 'sz_motoboy_pedidos';
    $cols = $wpdb->get_col( "SHOW COLUMNS FROM {$table_pedidos}", 0 );
    if ( is_array( $cols ) && $cols ) {
        $row = array_intersect_key( $row, array_flip( $cols ) );
    }
    $ok = $wpdb->insert( $table_pedidos, $row );
    if ( ! $ok ) {
        if ( function_exists( 'senderzz_me_log' ) ) {
            senderzz_me_log( 'motoboy.insert_failed', [ 'wc_order_id' => $wc_order_id, 'db_error' => $wpdb->last_error ] );
        }
        return false;
    }

    $pedido_id = (int) $wpdb->insert_id;

    // Double-write: replica para serviço Go durante janela de migração (Fase 1).
    do_action( 'sz_motoboy_pedido_criado', $pedido_id, $wc_order_id, $row );

    sz_motoboy_audit( [
        'pedido_id'   => $pedido_id,
        'motoboy_id'  => $motoboy_id,
        'acao'        => 'pedido_criado',
        'para_status' => 'agendado',
        'meta'        => [
            'wc_order_id'     => $wc_order_id,
            'zona_id'         => $zona_id,
            'affiliate_id'    => (int) $order->get_meta( '_sz_affiliate_id', true ),
            'affiliate_name'  => (string) $order->get_meta( '_sz_aff_name', true ),
        ],
    ] );

    return $pedido_id;
}

/**
 * Muda status de um pedido motoboy com auditoria.
 * $actor_tipo: 'sistema' | 'alan' | 'motoboy'
 */
function sz_motoboy_mudar_status( int $pedido_id, string $novo_status, array $extra = [], string $actor_tipo = 'sistema', ?int $actor_id = null ): bool {
    global $wpdb;
    $p = $wpdb->prefix;

    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$p}sz_motoboy_pedidos WHERE id = %d",
        $pedido_id
    ) );
    if ( ! $pedido ) return false;

    if ( function_exists( 'sz_mbc_validate_transition' ) ) {
        $sz_mbc_valid = sz_mbc_validate_transition( $pedido_id, $novo_status, $extra, $actor_tipo, $actor_id );
        if ( is_wp_error( $sz_mbc_valid ) ) {
            if ( function_exists( 'senderzz_me_log' ) ) {
                senderzz_me_log( 'motoboy.custody_transition_blocked', [
                    'pedido_id' => $pedido_id,
                    'novo_status' => $novo_status,
                    'actor_tipo' => $actor_tipo,
                    'actor_id' => $actor_id,
                    'error' => $sz_mbc_valid->get_error_message(),
                ] );
            }
            return false;
        }
    }

    $de_status = $pedido->status;
    $ts_field  = 'ts_' . $novo_status;

    $data = array_merge( [ 'status' => $novo_status ], $extra );
    unset( $data['qr_validated'], $data['package_code'], $data['custody_note'] );
    $sz_mb_cols = $wpdb->get_col( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos", 0 );
    if ( is_array( $sz_mb_cols ) && $sz_mb_cols ) {
        $data = array_intersect_key( $data, array_flip( $sz_mb_cols ) );
    }
    if ( in_array( $ts_field, [ 'ts_aprovado','ts_embalado','ts_em_rota','ts_a_caminho','ts_entregue','ts_frustrado' ], true ) ) {
        $existing_ts = isset( $pedido->{$ts_field} ) ? trim( (string) $pedido->{$ts_field} ) : '';
        // Timestamp de evento é histórico: grava uma vez no ato da mudança e não sobrescreve em reprocessos/ações repetidas.
        if ( $existing_ts === '' || $existing_ts === '0000-00-00 00:00:00' ) {
            $data[ $ts_field ] = sz_motoboy_now_mysql();
        }
    }

    // v315: a baixa final (entregue/frustrado) deve ficar vinculada ao motoboy
    // logado que executou a ação no app. Antes, frustrados herdavam o motoboy_id
    // operacional antigo e podiam cair todos na carteira do Alan.
    if ( in_array( $novo_status, [ 'entregue', 'frustrado' ], true ) ) {
        $responsavel_baixa = 0;
        if ( $actor_tipo === 'motoboy' && $actor_id ) {
            $responsavel_baixa = (int) $actor_id;
        } elseif ( ! empty( $extra['motoboy_id'] ) ) {
            $responsavel_baixa = (int) $extra['motoboy_id'];
        } elseif ( ! empty( $pedido->motoboy_id ) ) {
            $responsavel_baixa = (int) $pedido->motoboy_id;
        }
        if ( $responsavel_baixa > 0 ) {
            $data['motoboy_id']       = $responsavel_baixa;
            $data['baixa_motoboy_id'] = $responsavel_baixa;
        }
        if ( empty( $data['baixa_at'] ) ) {
            $data['baixa_at'] = sz_motoboy_now_mysql();
        }
    }

    $wpdb->update( $p . 'sz_motoboy_pedidos', $data, [ 'id' => $pedido_id ] );

    // Sincroniza o espelho no Woo/HPOS para que Portal do produtor, OL e app
    // mostrem o mesmo estado Motoboy imediatamente. Mantém COD Motoboy isolado
    // de Expedição/Melhor Envio: apenas grava metas e status Woo compatíveis.
    $wc_order_id = (int) ( $pedido->wc_order_id ?? 0 );
    if ( $wc_order_id > 0 && function_exists( 'wc_get_order' ) ) {
        $order = wc_get_order( $wc_order_id );
        if ( $order instanceof \WC_Order ) {
            $now_mysql = sz_motoboy_now_mysql();
            $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
            $order->update_meta_data( '_senderzz_motoboy_flow_status', $novo_status );
            $order->update_meta_data( '_senderzz_motoboy_status', $novo_status );
            $order->update_meta_data( '_senderzz_motoboy_status_updated_at', $now_mysql );
            $order->update_meta_data( '_senderzz_motoboy_' . $novo_status . '_at', $now_mysql );
            $responsavel_meta = 0;
            if ( $actor_tipo === 'motoboy' && $actor_id ) {
                $responsavel_meta = (int) $actor_id;
            } elseif ( ! empty( $data['motoboy_id'] ) ) {
                $responsavel_meta = (int) $data['motoboy_id'];
            } elseif ( ! empty( $pedido->motoboy_id ) ) {
                $responsavel_meta = (int) $pedido->motoboy_id;
            }
            if ( $responsavel_meta > 0 ) {
                $mb_nome = (string) $wpdb->get_var( $wpdb->prepare( "SELECT nome FROM {$p}sz_motoboys WHERE id = %d", $responsavel_meta ) );
                $order->update_meta_data( '_senderzz_motoboy_id',              $responsavel_meta );
                $order->update_meta_data( '_sz_motoboy_id',                    $responsavel_meta );
                $order->update_meta_data( '_motoboy_user_id',                  $responsavel_meta );
                $order->update_meta_data( '_senderzz_motoboy_responsavel_id',   $responsavel_meta );
                $order->update_meta_data( '_senderzz_motoboy_entregador_id',    $responsavel_meta );
                if ( in_array( $novo_status, [ 'entregue', 'frustrado' ], true ) ) {
                    $order->update_meta_data( '_senderzz_motoboy_baixa_motoboy_id', $responsavel_meta );
                }
                if ( $mb_nome !== '' ) {
                    $order->update_meta_data( '_senderzz_motoboy_name',             $mb_nome );
                    $order->update_meta_data( '_sz_motoboy_name',                   $mb_nome );
                    $order->update_meta_data( '_motoboy_name',                      $mb_nome );
                    $order->update_meta_data( '_senderzz_motoboy_responsavel_nome',  $mb_nome );
                    $order->update_meta_data( '_senderzz_motoboy_entregador_nome',   $mb_nome );
                }
            }
            if ( $novo_status === 'em_rota' ) {
                if ( ! $order->get_meta( '_senderzz_motoboy_em_rota_at', true ) ) { $order->update_meta_data( '_senderzz_motoboy_em_rota_at', $now_mysql ); }
                // Não troca o status Woo para um status de Expedição. O estado operacional
                // de rota fica no espelho Motoboy (_senderzz_motoboy_flow_status).
            } elseif ( $novo_status === 'embalado' ) {
                if ( ! $order->get_meta( '_senderzz_motoboy_embalado_at', true ) ) { $order->update_meta_data( '_senderzz_motoboy_embalado_at', $now_mysql ); }
                if ( ! $order->has_status( 'embalado' ) ) {
                    $order->update_status( 'embalado', 'Senderzz COD Motoboy: pedido embalado.' );
                }
            } elseif ( $novo_status === 'entregue' ) {
                if ( ! $order->get_meta( '_senderzz_motoboy_entregue_at', true ) ) { $order->update_meta_data( '_senderzz_motoboy_entregue_at', $now_mysql ); }
                $order->update_meta_data( '_senderzz_cod_payment_method_label', 'Cash On Delivery' );
                if ( method_exists( $order, 'set_payment_method_title' ) ) {
                    $order->set_payment_method_title( 'Cash On Delivery' );
                }
                if ( method_exists( $order, 'set_payment_method' ) ) {
                    $order->set_payment_method( 'cod' );
                }
                if ( ! $order->has_status( 'completo' ) ) {
                    $order->update_status( 'completo', 'Senderzz COD Motoboy: pedido completo via Cash On Delivery.' );
                }
            } elseif ( $novo_status === 'frustrado' ) {
                if ( ! $order->get_meta( '_senderzz_motoboy_frustrado_at', true ) ) { $order->update_meta_data( '_senderzz_motoboy_frustrado_at', $now_mysql ); }
                if ( ! $order->has_status( 'frustrado' ) ) {
                    $order->update_status( 'frustrado', 'Senderzz COD Motoboy: tentativa de entrega frustrada.' );
                }
            } elseif ( $novo_status === 'cancelado' ) {
                if ( ! $order->has_status( 'cancelled' ) ) {
                    $order->update_status( 'cancelled', 'Senderzz COD Motoboy: pedido cancelado.' );
                }
            }
            $order->save();
        }
    }

    sz_motoboy_audit( [
        'pedido_id'   => $pedido_id,
        'motoboy_id'  => $pedido->motoboy_id,
        'actor_tipo'  => $actor_tipo,
        'actor_id'    => $actor_id,
        'acao'        => 'status_alterado',
        'de_status'   => $de_status,
        'para_status' => $novo_status,
        'meta'        => $extra,
    ] );

    // Notificação WhatsApp/SMS ao cliente (hook extensível)
    // IMPORTANTE: após o update acima, o objeto $pedido ainda contém os dados antigos
    // (ex.: motoboy_id anterior). A carteira do motoboy depende do motoboy atual.
    // Por isso recarregamos a linha atualizada antes de disparar o hook.
    $pedido_atual = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$p}sz_motoboy_pedidos WHERE id = %d",
        $pedido_id
    ) );
    if ( $pedido_atual ) {
        $pedido = $pedido_atual;
    }

    do_action( 'sz_motoboy_status_changed', $pedido_id, $de_status, $novo_status, $pedido );

    return true;
}

/**
 * Verifica frustração: primeira é isenta, segunda em diante cobra taxa.
 */
function sz_motoboy_calcular_taxa_frustrado( int $wc_order_id ): array {
    global $wpdb;
    $count = (int) $wpdb->get_var( $wpdb->prepare(
        "SELECT COUNT(*) FROM {$wpdb->prefix}sz_motoboy_pedidos
          WHERE wc_order_id = %d AND status IN ('frustrado')",
        $wc_order_id
    ) );
    $isento = ( $count === 0 );
    // 1ª frustração: produtor isento (R$ 0), motoboy recebe taxa de tentativa
    // 2ª+ frustração: produtor paga sz_aff_producer_frustration_penalty (padrão R$ 8,00)
    $taxa = $isento ? 0.00 : (float) get_option( 'sz_aff_producer_frustration_penalty', 8.00 );
    return [ 'isento' => $isento, 'taxa' => $taxa, 'count' => $count ];
}

/**
 * COD Motoboy isolado: cria/roteia somente pedidos marcados como motoboy.
 * Não usa Melhor Envio, Expedição, etiqueta ou transportadora.
 */
function sz_motoboy_order_is_cod_motoboy( $order ): bool {
    if ( ! $order ) return false;

    // 1) Marcação direta Senderzz/Woo.
    $direct_keys = [
        '_senderzz_delivery_mode', '_senderzz_shipping_type', '_senderzz_order_type',
        '_senderzz_tipo_entrega', '_senderzz_checkout_tipo', '_senderzz_delivery_type',
        '_senderzz_motoboy_flow_status', '_senderzz_motoboy_status', '_sz_motoboy_status',
    ];
    foreach ( $direct_keys as $key ) {
        $v = strtolower( trim( (string) $order->get_meta( $key, true ) ) );
        if ( $v !== '' && strpos( $v, 'motoboy' ) !== false ) return true;
        if ( in_array( $key, [ '_senderzz_motoboy_flow_status', '_senderzz_motoboy_status', '_sz_motoboy_status' ], true ) && $v !== '' ) return true;
    }

    // 2) Frete: cobre exatamente o caso da tela WooCommerce "via 🛵 Motoboy Senderzz".
    foreach ( $order->get_items( 'shipping' ) as $shipping_item ) {
        $hay = strtolower( implode( ' ', [
            (string) $shipping_item->get_method_id(),
            (string) $shipping_item->get_method_title(),
            (string) $shipping_item->get_name(),
            wp_json_encode( $shipping_item->get_meta_data() ),
        ] ) );
        if ( strpos( $hay, 'motoboy' ) !== false || strpos( $hay, 'senderzz' ) !== false ) return true;
    }

    // 3) Link/token de checkout. Em FunnelKit alguns pedidos salvam só o token/id do link.
    global $wpdb;
    $links_table = $wpdb->prefix . 'senderzz_checkout_links';
    $tokens = array_filter( array_unique( array_map( 'trim', [
        (string) $order->get_meta( '_senderzz_offer_token', true ),
        (string) $order->get_meta( '_senderzz_checkout_token', true ),
        (string) $order->get_meta( 'senderzz_offer_token', true ),
        (string) $order->get_meta( 'senderzz_checkout_token', true ),
    ] ) ) );
    foreach ( $tokens as $token ) {
        $tipo = (string) $wpdb->get_var( $wpdb->prepare( "SELECT tipo FROM {$links_table} WHERE token = %s LIMIT 1", $token ) );
        if ( strtolower( $tipo ) === 'motoboy' ) return true;
    }

    $link_ids = array_filter( array_unique( array_map( 'absint', [
        $order->get_meta( '_senderzz_checkout_link_id', true ),
        $order->get_meta( '_senderzz_offer_link_id', true ),
        $order->get_meta( 'senderzz_checkout_link_id', true ),
        $order->get_meta( 'senderzz_offer_link_id', true ),
    ] ) ) );
    foreach ( $link_ids as $link_id ) {
        $tipo = (string) $wpdb->get_var( $wpdb->prepare( "SELECT tipo FROM {$links_table} WHERE id = %d LIMIT 1", $link_id ) );
        if ( strtolower( $tipo ) === 'motoboy' ) return true;
    }

    // 4) Último fallback seguro: texto de método/observação/meta do pedido contendo Motoboy Senderzz.
    $hay = strtolower( implode( ' ', [
        (string) $order->get_shipping_method(),
        (string) $order->get_customer_note(),
        (string) $order->get_payment_method(),
        (string) $order->get_payment_method_title(),
    ] ) );
    return ( strpos( $hay, 'motoboy' ) !== false );
}


/**
 * Garante que pedidos COD Motoboy recentes tenham linha operacional.
 * Usado pelo Admin COD e pelo painel OL para recuperar pedidos criados por
 * FunnelKit/HPOS quando algum hook de checkout/status não disparou.
 */
function sz_motoboy_backfill_recent_orders( int $limit = 120 ): int {
    if ( ! function_exists( 'wc_get_order' ) ) return 0;

    global $wpdb;
    $limit = max( 50, min( 1000, $limit ) );
    $ids = [];

    // A) Woo API/HPOS oficial.
    if ( function_exists( 'wc_get_orders' ) ) {
        $wc_ids = wc_get_orders( [
            'limit'   => $limit,
            'orderby' => 'date',
            'order'   => 'DESC',
            'return'  => 'ids',
            'status'  => array_keys( wc_get_order_statuses() ),
        ] );
        foreach ( (array) $wc_ids as $id ) $ids[] = absint( $id );
    }

    // B) HPOS direto, sem filtrar payment_method/status. O print mostra pedidos wc-agendado
    // via Motoboy Senderzz; alguns ambientes não devolvem esses status pela query Woo.
    $hpos = $wpdb->prefix . 'wc_orders';
    if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $hpos ) ) ) {
        $direct_ids = $wpdb->get_col( $wpdb->prepare(
            "SELECT id FROM {$hpos} WHERE type = 'shop_order' ORDER BY id DESC LIMIT %d",
            $limit
        ) );
        foreach ( (array) $direct_ids as $id ) $ids[] = absint( $id );
    }

    // C) Fallback legado wp_posts.
    $posts_ids = $wpdb->get_col( $wpdb->prepare(
        "SELECT ID FROM {$wpdb->posts} WHERE post_type = 'shop_order' ORDER BY ID DESC LIMIT %d",
        $limit
    ) );
    foreach ( (array) $posts_ids as $id ) $ids[] = absint( $id );

    $ids = array_values( array_unique( array_filter( $ids ) ) );
    rsort( $ids, SORT_NUMERIC );

    $created = 0;
    foreach ( $ids as $order_id ) {
        $order = wc_get_order( $order_id );
        if ( ! $order || ! sz_motoboy_order_is_cod_motoboy( $order ) ) continue;

        $before = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}sz_motoboy_pedidos WHERE wc_order_id = %d LIMIT 1",
            $order_id
        ) );

        $pid = sz_motoboy_criar_pedido( $order_id );
        if ( $pid ) {
            $order->update_meta_data( '_senderzz_delivery_mode', 'motoboy' );
            if ( ! $order->get_meta( '_senderzz_motoboy_flow_status', true ) ) {
                $order->update_meta_data( '_senderzz_motoboy_flow_status', 'agendado' );
            }
            $order->save();
            if ( ! $before ) $created++;
        }
    }

    if ( function_exists( 'senderzz_me_log' ) ) {
        senderzz_me_log( 'motoboy.backfill_recent_orders', [ 'scanned' => count( $ids ), 'created' => $created, 'limit' => $limit ] );
    }
    return $created;
}

function sz_motoboy_maybe_create_from_order( $order_id ): void {
    $order = function_exists( 'wc_get_order' ) ? wc_get_order( $order_id ) : null;
    if ( ! $order || ! sz_motoboy_order_is_cod_motoboy( $order ) ) return;
    sz_motoboy_criar_pedido( (int) $order_id );
}

add_action( 'woocommerce_order_status_agendado', 'sz_motoboy_maybe_create_from_order', 20 );
add_action( 'woocommerce_checkout_order_processed', 'sz_motoboy_maybe_create_from_order', 60 );
add_action( 'woocommerce_order_status_aprovado', 'sz_motoboy_maybe_create_from_order', 20 );


/**
 * Se a baixa for feita direto pelo admin/WooCommerce para wc-completo,
 * sincroniza o pedido motoboy para "entregue" e dispara o hook
 * sz_motoboy_status_changed, que credita a wallet do motoboy.
 */
add_action( 'woocommerce_order_status_changed', function( int $order_id, string $old_status, string $new_status ): void {
    if ( $new_status !== 'completo' ) return;
    if ( ! function_exists( 'wc_get_order' ) ) return;

    $order = wc_get_order( $order_id );
    if ( ! $order || ! sz_motoboy_order_is_cod_motoboy( $order ) ) return;

    global $wpdb;
    $table = $wpdb->prefix . 'sz_motoboy_pedidos';
    $pedido = $wpdb->get_row( $wpdb->prepare(
        "SELECT id, status FROM {$table} WHERE wc_order_id = %d ORDER BY id DESC LIMIT 1",
        $order_id
    ) );

    if ( ! $pedido || $pedido->status === 'entregue' ) return;

    sz_motoboy_mudar_status( (int) $pedido->id, 'entregue', [], 'admin_wc', get_current_user_id() );
}, 25, 3 );

/**
 * Geofence: cron a cada 1 min verifica motoboys próximos ao destino.
 */
add_action( 'sz_motoboy_geofence_check', function() {
    global $wpdb;
    $p = $wpdb->prefix;

    $pedidos = $wpdb->get_results(
        "SELECT mp.id, mp.motoboy_id, mp.dest_lat, mp.dest_lng
           FROM {$p}sz_motoboy_pedidos mp
          WHERE mp.status = 'em_rota'
            AND mp.dest_lat IS NOT NULL"
    );

    foreach ( $pedidos as $pedido ) {
        $mb = $wpdb->get_row( $wpdb->prepare(
            "SELECT ultimo_lat, ultimo_lng FROM {$p}sz_motoboys WHERE id = %d",
            $pedido->motoboy_id
        ) );
        if ( ! $mb || ! $mb->ultimo_lat ) continue;

        $dist = sz_motoboy_distancia_metros(
            (float) $mb->ultimo_lat, (float) $mb->ultimo_lng,
            (float) $pedido->dest_lat, (float) $pedido->dest_lng
        );

        if ( $dist <= 500 ) {
            // COD Motoboy isolado: não usa etapa automática a_caminho; permanece em_rota até entrega/frustração.
        }
    }
} );

if ( ! wp_next_scheduled( 'sz_motoboy_geofence_check' ) ) {
    wp_schedule_event( time(), 'sz_motoboy_1min', 'sz_motoboy_geofence_check' );
}

add_filter( 'cron_schedules', function( $schedules ) {
    if ( empty( $schedules['sz_motoboy_1min'] ) ) {
        $schedules['sz_motoboy_1min'] = [
            'interval' => 60,
            'display'  => 'Senderzz Motoboy — 1 minuto',
        ];
    }
    return $schedules;
} );

/**
 * Haversine: distância em metros entre dois pontos GPS.
 */
function sz_motoboy_distancia_metros( float $lat1, float $lng1, float $lat2, float $lng2 ): float {
    $r  = 6371000;
    $d1 = deg2rad( $lat2 - $lat1 );
    $d2 = deg2rad( $lng2 - $lng1 );
    $a  = sin( $d1/2 ) ** 2 + cos( deg2rad($lat1) ) * cos( deg2rad($lat2) ) * sin( $d2/2 ) ** 2;
    return $r * 2 * atan2( sqrt($a), sqrt(1-$a) );
}
