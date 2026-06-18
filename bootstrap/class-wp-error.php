<?php
/**
 * Senderzz Standalone — WP_Error drop-in.
 */

if ( class_exists( 'WP_Error' ) ) return;

class WP_Error {

    private array $errors = [];
    private array $error_data = [];

    public function __construct( string|int $code = '', string $message = '', mixed $data = '' ) {
        if ( $code !== '' ) {
            $this->errors[ $code ][] = $message;
            if ( $data !== '' ) {
                $this->error_data[ $code ] = $data;
            }
        }
    }

    public function get_error_code(): string|int {
        return array_key_first( $this->errors ) ?? '';
    }

    public function get_error_codes(): array {
        return array_keys( $this->errors );
    }

    public function get_error_message( string|int $code = '' ): string {
        if ( $code === '' ) $code = $this->get_error_code();
        return $this->errors[ $code ][0] ?? '';
    }

    public function get_error_messages( string|int $code = '' ): array {
        if ( $code === '' ) return array_merge( ...array_values( $this->errors ) );
        return $this->errors[ $code ] ?? [];
    }

    public function get_error_data( string|int $code = '' ): mixed {
        if ( $code === '' ) $code = $this->get_error_code();
        return $this->error_data[ $code ] ?? null;
    }

    public function has_errors(): bool {
        return ! empty( $this->errors );
    }

    public function add( string|int $code, string $message, mixed $data = '' ): void {
        $this->errors[ $code ][] = $message;
        if ( $data !== '' ) $this->error_data[ $code ] = $data;
    }

    public function add_data( mixed $data, string|int $code = '' ): void {
        if ( $code === '' ) $code = $this->get_error_code();
        $this->error_data[ $code ] = $data;
    }

    public function remove( string|int $code ): void {
        unset( $this->errors[ $code ], $this->error_data[ $code ] );
    }

    public function merge_from( WP_Error $error ): void {
        foreach ( $error->get_error_codes() as $code ) {
            foreach ( $error->get_error_messages( $code ) as $msg ) {
                $this->add( $code, $msg, $error->get_error_data( $code ) );
            }
        }
    }
}

function is_wp_error( mixed $thing ): bool {
    return $thing instanceof WP_Error && $thing->has_errors();
}
