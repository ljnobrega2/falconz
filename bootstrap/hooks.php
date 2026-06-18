<?php
/**
 * Senderzz Standalone — WordPress hooks system.
 * Implements add_action, add_filter, do_action, apply_filters, has_action, remove_action.
 */

if ( function_exists( 'add_action' ) ) return;

/** @var array<string, array<int, array<array{cb: callable, n: int}>>> */
$GLOBALS['_sz_hooks'] = [];

function add_action( string $hook, callable $cb, int $priority = 10, int $accepted_args = 1 ): bool {
    $GLOBALS['_sz_hooks'][ $hook ][ $priority ][] = [ 'cb' => $cb, 'n' => $accepted_args ];
    return true;
}

function add_filter( string $hook, callable $cb, int $priority = 10, int $accepted_args = 1 ): bool {
    return add_action( $hook, $cb, $priority, $accepted_args );
}

function do_action( string $hook, ...$args ): void {
    $hooks = $GLOBALS['_sz_hooks'][ $hook ] ?? [];
    ksort( $hooks );
    foreach ( $hooks as $priority => $cbs ) {
        foreach ( $cbs as $entry ) {
            call_user_func_array( $entry['cb'], array_slice( $args, 0, $entry['n'] ) );
        }
    }
}

function apply_filters( string $hook, mixed $value, ...$args ): mixed {
    $hooks = $GLOBALS['_sz_hooks'][ $hook ] ?? [];
    ksort( $hooks );
    foreach ( $hooks as $priority => $cbs ) {
        foreach ( $cbs as $entry ) {
            $value = call_user_func_array( $entry['cb'], array_slice( [ $value, ...$args ], 0, $entry['n'] ) );
        }
    }
    return $value;
}

function has_action( string $hook, $cb = false ): bool|int {
    $hooks = $GLOBALS['_sz_hooks'][ $hook ] ?? [];
    if ( ! $cb ) return ! empty( $hooks );
    foreach ( $hooks as $priority => $cbs ) {
        foreach ( $cbs as $entry ) {
            if ( $entry['cb'] === $cb ) return $priority;
        }
    }
    return false;
}

function has_filter( string $hook, $cb = false ): bool|int {
    return has_action( $hook, $cb );
}

function remove_action( string $hook, callable $cb, int $priority = 10 ): bool {
    if ( ! isset( $GLOBALS['_sz_hooks'][ $hook ][ $priority ] ) ) return false;
    $GLOBALS['_sz_hooks'][ $hook ][ $priority ] = array_filter(
        $GLOBALS['_sz_hooks'][ $hook ][ $priority ],
        fn( $e ) => $e['cb'] !== $cb
    );
    return true;
}

function remove_filter( string $hook, callable $cb, int $priority = 10 ): bool {
    return remove_action( $hook, $cb, $priority );
}

function doing_action( string $hook ): bool { return false; }
function current_action(): string { return ''; }
function did_action( string $hook ): int { return 0; }
