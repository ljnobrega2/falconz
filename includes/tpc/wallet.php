<?php
/**
 * wallet.php
 *
 * Operações atômicas na carteira.
 * Suporta saldo disponível + saldo reservado para evitar débito indevido.
 */
defined( 'ABSPATH' ) || exit;
if ( defined( 'SENDERZZ_TPC_WALLET_LOADED' ) ) return;
define( 'SENDERZZ_TPC_WALLET_LOADED', true );

function tpc_wallet_meta_payload( array $meta = [] ): array {
    $meta['actor_id']   = get_current_user_id() ?: null;
    $meta['ip_address'] = $_SERVER['REMOTE_ADDR'] ?? null;
    return $meta;
}

function tpc_creditar( int $user_id, float $valor, string $descricao, array $meta = [] ): int|false {
    return tpc_movimentar( $user_id, 'credito', $valor, $descricao, $meta );
}

function tpc_debitar( int $user_id, float $valor, string $descricao, array $meta = [] ): int|false {
    return tpc_movimentar( $user_id, 'debito', $valor, $descricao, $meta );
}

function tpc_tem_saldo( int $user_id, float $valor ): bool {
    return ( function_exists( 'tpc_get_saldo_disponivel' ) ? tpc_get_saldo_disponivel( $user_id ) : tpc_get_saldo( $user_id ) ) >= $valor;
}

function tpc_movimentar( int $user_id, string $tipo, float $valor, string $descricao, array $meta = [] ): int|false {
    global $wpdb;

    if ( $valor <= 0 || ! in_array( $tipo, [ 'credito', 'debito' ], true ) ) return false;
    $meta = tpc_wallet_meta_payload( $meta );

    // Idempotência: se referencia já existe como transação confirmada, retorna o ID existente
    // sem debitar novamente. Protege contra duplo clique e requisições paralelas.
    if ( ! empty( $meta['referencia'] ) ) {
        $existing_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}tpc_transacoes
             WHERE user_id = %d AND referencia = %s AND tipo = %s AND status = 'confirmado'
             LIMIT 1",
            $user_id,
            $meta['referencia'],
            $tipo
        ) );
        if ( $existing_id ) {
            return $existing_id;
        }
    }

    $wpdb->query( 'START TRANSACTION' );

    $wpdb->query( $wpdb->prepare(
        "INSERT INTO {$wpdb->prefix}tpc_carteira (user_id, saldo, saldo_reservado)
         VALUES (%d, 0.00, 0.00)
         ON DUPLICATE KEY UPDATE id = id",
        $user_id
    ) );

    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT saldo, saldo_reservado FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d FOR UPDATE",
        $user_id
    ) );

    // Segunda checagem de idempotência dentro da transação (proteção contra race condition)
    if ( ! empty( $meta['referencia'] ) ) {
        $existing_id = (int) $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}tpc_transacoes
             WHERE user_id = %d AND referencia = %s AND tipo = %s AND status = 'confirmado'
             LIMIT 1",
            $user_id,
            $meta['referencia'],
            $tipo
        ) );
        if ( $existing_id ) {
            $wpdb->query( 'ROLLBACK' );
            return $existing_id;
        }
    }

    $saldo_atual     = $row ? (float) $row->saldo : 0.00;
    $saldo_reservado = $row && isset( $row->saldo_reservado ) ? (float) $row->saldo_reservado : 0.00;
    $disponivel      = round( $saldo_atual - $saldo_reservado, 2 );

    if ( $tipo === 'debito' && $disponivel < $valor ) {
        $wpdb->query( 'ROLLBACK' );
        return false;
    }

    $saldo_novo = $tipo === 'credito' ? round( $saldo_atual + $valor, 2 ) : round( $saldo_atual - $valor, 2 );

    $ok_wallet = $wpdb->update(
        $wpdb->prefix . 'tpc_carteira',
        [ 'saldo' => $saldo_novo ],
        [ 'user_id' => $user_id ],
        [ '%f' ],
        [ '%d' ]
    );

    $ok_tx = $wpdb->insert(
        $wpdb->prefix . 'tpc_transacoes',
        [
            'user_id'      => $user_id,
            'tipo'         => $tipo,
            'valor'        => $valor,
            'saldo_apos'   => $saldo_novo,
            'descricao'    => substr( $descricao, 0, 255 ),
            'referencia'   => $meta['referencia'] ?? null,
            'order_id'     => $meta['order_id'] ?? null,
            'me_order_id'  => $meta['me_order_id'] ?? null,
            'status'       => 'confirmado',
            'meta_json'    => wp_json_encode( $meta, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ),
            'actor_id'     => $meta['actor_id'] ?? null,
            'ip_address'   => $meta['ip_address'] ?? null,
        ],
        [ '%d', '%s', '%f', '%f', '%s', '%s', '%d', '%s', '%s', '%s', '%d', '%s' ]
    );

    if ( $ok_wallet === false || ! $ok_tx ) {
        $wpdb->query( 'ROLLBACK' );
        return false;
    }

    $transacao_id = (int) $wpdb->insert_id;
    $wpdb->query( 'COMMIT' );

    do_action( 'tpc_pos_movimentacao', $user_id, $tipo, $valor, $saldo_novo, $transacao_id, $meta );
    // Double-write Go (Fase 2 migração strangler fig)
    do_action( 'tpc_transacao_inserida', $transacao_id, $user_id, $tipo, array_merge( $meta, [
        'valor'       => $valor,
        'saldo_apos'  => $saldo_novo,
        'descricao'   => substr( $descricao, 0, 255 ),
        'referencia'  => $meta['referencia'] ?? null,
        'order_id'    => $meta['order_id'] ?? null,
        'status'      => 'confirmado',
    ] ) );
    do_action( 'tpc_saldo_atualizado', $user_id, $saldo_novo, $saldo_novo );
    return $transacao_id;
}

function tpc_reservar( int $user_id, float $valor, string $descricao, array $meta = [] ): int|false {
    global $wpdb;
    if ( $valor <= 0 || ! $user_id ) return false;
    $meta = tpc_wallet_meta_payload( $meta );

    if ( ! empty( $meta['referencia'] ) ) {
        $existente = $wpdb->get_var( $wpdb->prepare(
            "SELECT id FROM {$wpdb->prefix}tpc_transacoes WHERE user_id = %d AND referencia = %s AND status = 'pendente' LIMIT 1",
            $user_id,
            $meta['referencia']
        ) );
        if ( $existente ) return (int) $existente;
    }

    $wpdb->query( 'START TRANSACTION' );
    $wpdb->query( $wpdb->prepare(
        "INSERT INTO {$wpdb->prefix}tpc_carteira (user_id, saldo, saldo_reservado)
         VALUES (%d, 0.00, 0.00)
         ON DUPLICATE KEY UPDATE id = id",
        $user_id
    ) );

    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT saldo, saldo_reservado FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d FOR UPDATE",
        $user_id
    ) );

    $saldo      = $row ? (float) $row->saldo : 0.00;
    $reservado  = $row && isset( $row->saldo_reservado ) ? (float) $row->saldo_reservado : 0.00;
    $disponivel = round( $saldo - $reservado, 2 );

    if ( $disponivel < $valor ) {
        $wpdb->query( 'ROLLBACK' );
        return false;
    }

    $ok_wallet = $wpdb->update(
        $wpdb->prefix . 'tpc_carteira',
        [ 'saldo_reservado' => round( $reservado + $valor, 2 ) ],
        [ 'user_id' => $user_id ],
        [ '%f' ],
        [ '%d' ]
    );

    $ok_tx = $wpdb->insert(
        $wpdb->prefix . 'tpc_transacoes',
        [
            'user_id'     => $user_id,
            'tipo'        => 'debito',
            'valor'       => $valor,
            'saldo_apos'  => $saldo,
            'descricao'   => substr( $descricao, 0, 255 ),
            'referencia'  => $meta['referencia'] ?? null,
            'order_id'    => $meta['order_id'] ?? null,
            'me_order_id' => $meta['me_order_id'] ?? null,
            'status'      => 'pendente',
            'meta_json'   => wp_json_encode( $meta, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ),
            'actor_id'    => $meta['actor_id'] ?? null,
            'ip_address'  => $meta['ip_address'] ?? null,
        ],
        [ '%d', '%s', '%f', '%f', '%s', '%s', '%d', '%s', '%s', '%s', '%d', '%s' ]
    );

    if ( $ok_wallet === false || ! $ok_tx ) {
        $wpdb->query( 'ROLLBACK' );
        return false;
    }

    $tx_id = (int) $wpdb->insert_id;
    $wpdb->query( 'COMMIT' );
    do_action( 'tpc_saldo_reservado', $user_id, $valor, $tx_id, $meta );
    return $tx_id;
}

function tpc_liberar_reserva( int $tx_id, string $motivo = '' ): bool {
    global $wpdb;
    if ( ! $tx_id ) return false;

    $wpdb->query( 'START TRANSACTION' );
    $tx = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}tpc_transacoes WHERE id = %d FOR UPDATE",
        $tx_id
    ), ARRAY_A );

    if ( ! $tx || $tx['status'] !== 'pendente' || $tx['tipo'] !== 'debito' ) {
        $wpdb->query( 'ROLLBACK' );
        return false;
    }

    $user_id = (int) $tx['user_id'];
    $valor   = (float) $tx['valor'];
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT saldo_reservado FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d FOR UPDATE",
        $user_id
    ) );
    $reservado = $row && isset( $row->saldo_reservado ) ? (float) $row->saldo_reservado : 0.00;

    $ok_wallet = $wpdb->update(
        $wpdb->prefix . 'tpc_carteira',
        [ 'saldo_reservado' => max( 0, round( $reservado - $valor, 2 ) ) ],
        [ 'user_id' => $user_id ],
        [ '%f' ],
        [ '%d' ]
    );
    $ok_tx = $wpdb->update(
        $wpdb->prefix . 'tpc_transacoes',
        [ 'status' => 'cancelado', 'descricao' => substr( $tx['descricao'] . ( $motivo ? ' | Liberado: ' . $motivo : '' ), 0, 255 ) ],
        [ 'id' => $tx_id, 'status' => 'pendente' ],
        [ '%s', '%s' ],
        [ '%d', '%s' ]
    );

    if ( $ok_wallet === false || ! $ok_tx ) {
        $wpdb->query( 'ROLLBACK' );
        return false;
    }

    $wpdb->query( 'COMMIT' );
    do_action( 'tpc_reserva_liberada', $user_id, $valor, $tx_id, $motivo );
    return true;
}

function tpc_debitar_reserva( int $tx_id, array $meta = [] ): bool {
    global $wpdb;
    if ( ! $tx_id ) return false;
    $meta = tpc_wallet_meta_payload( $meta );

    $wpdb->query( 'START TRANSACTION' );
    $tx = $wpdb->get_row( $wpdb->prepare(
        "SELECT * FROM {$wpdb->prefix}tpc_transacoes WHERE id = %d FOR UPDATE",
        $tx_id
    ), ARRAY_A );

    if ( ! $tx ) { $wpdb->query( 'ROLLBACK' ); return false; }
    if ( $tx['status'] === 'confirmado' ) { $wpdb->query( 'COMMIT' ); return true; }
    if ( $tx['status'] !== 'pendente' || $tx['tipo'] !== 'debito' ) { $wpdb->query( 'ROLLBACK' ); return false; }

    $user_id = (int) $tx['user_id'];
    $valor   = (float) $tx['valor'];
    $row = $wpdb->get_row( $wpdb->prepare(
        "SELECT saldo, saldo_reservado FROM {$wpdb->prefix}tpc_carteira WHERE user_id = %d FOR UPDATE",
        $user_id
    ) );
    $saldo     = $row ? (float) $row->saldo : 0.00;
    $reservado = $row && isset( $row->saldo_reservado ) ? (float) $row->saldo_reservado : 0.00;

    if ( $saldo < $valor || $reservado < $valor ) {
        $wpdb->query( 'ROLLBACK' );
        return false;
    }

    $saldo_novo = round( $saldo - $valor, 2 );
    $reservado_novo = max( 0, round( $reservado - $valor, 2 ) );

    $ok_wallet = $wpdb->update(
        $wpdb->prefix . 'tpc_carteira',
        [ 'saldo' => $saldo_novo, 'saldo_reservado' => $reservado_novo ],
        [ 'user_id' => $user_id ],
        [ '%f', '%f' ],
        [ '%d' ]
    );

    $merged_meta = array_merge( json_decode( $tx['meta_json'] ?? '[]', true ) ?: [], $meta );
    $ok_tx = $wpdb->update(
        $wpdb->prefix . 'tpc_transacoes',
        [
            'status'      => 'confirmado',
            'saldo_apos'  => $saldo_novo,
            'me_order_id' => $merged_meta['me_order_id'] ?? $tx['me_order_id'],
            'meta_json'   => wp_json_encode( $merged_meta, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES ),
        ],
        [ 'id' => $tx_id, 'status' => 'pendente' ],
        [ '%s', '%f', '%s', '%s' ],
        [ '%d', '%s' ]
    );

    if ( $ok_wallet === false || ! $ok_tx ) {
        $wpdb->query( 'ROLLBACK' );
        return false;
    }

    $wpdb->query( 'COMMIT' );
    do_action( 'tpc_reserva_debitada', $user_id, $valor, $tx_id, $merged_meta );
    return true;
}

