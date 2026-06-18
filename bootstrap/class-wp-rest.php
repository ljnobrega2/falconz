<?php
/**
 * Senderzz Standalone — WP_REST_Request + WP_REST_Response drop-ins.
 */

if ( class_exists( 'WP_REST_Request' ) ) return;

class WP_REST_Request {

    private string $method;
    private string $route;
    private array  $params     = [];
    private array  $headers    = [];
    private ?string $body_raw  = null;
    private array  $body_json  = [];
    private array  $url_params = [];

    public function __construct( string $method = 'GET', string $route = '' ) {
        $this->method = strtoupper( $method );
        $this->route  = $route;
    }

    public static function from_globals(): static {
        $req = new static(
            $_SERVER['REQUEST_METHOD'] ?? 'GET',
            $_SERVER['REQUEST_URI']    ?? '/'
        );
        // Headers
        foreach ( $_SERVER as $k => $v ) {
            if ( str_starts_with( $k, 'HTTP_' ) ) {
                $name = str_replace( '_', '-', substr( $k, 5 ) );
                $req->headers[ strtolower( $name ) ] = $v;
            }
        }
        // Content-Type header
        if ( isset( $_SERVER['CONTENT_TYPE'] ) ) {
            $req->headers['content-type'] = $_SERVER['CONTENT_TYPE'];
        }
        // Body
        $req->body_raw = file_get_contents( 'php://input' ) ?: null;
        if ( $req->body_raw && str_contains( $req->headers['content-type'] ?? '', 'application/json' ) ) {
            $req->body_json = json_decode( $req->body_raw, true ) ?? [];
        }
        // Query + POST params
        $req->params = array_merge( $_GET, $_POST );

        return $req;
    }

    public function get_method(): string { return $this->method; }
    public function get_route():  string { return $this->route; }

    public function get_param( string $key ): mixed {
        return $this->url_params[ $key ] ?? $this->body_json[ $key ] ?? $this->params[ $key ] ?? null;
    }

    public function get_params(): array {
        return array_merge( $this->params, $this->body_json, $this->url_params );
    }

    public function get_json_params(): array { return $this->body_json; }
    public function get_body(): ?string      { return $this->body_raw; }
    public function get_query_params(): array { return $_GET; }

    public function get_header( string $key ): ?string {
        return $this->headers[ strtolower( $key ) ] ?? null;
    }

    public function set_param( string $key, mixed $value ): void {
        $this->params[ $key ] = $value;
    }

    public function set_url_params( array $params ): void {
        $this->url_params = $params;
    }

    /** Allow array-access style: $request['key'] */
    public function offsetGet( mixed $offset ): mixed { return $this->get_param( (string) $offset ); }
    public function offsetExists( mixed $offset ): bool { return $this->get_param( (string) $offset ) !== null; }
    public function offsetSet( mixed $offset, mixed $value ): void { $this->set_param( (string) $offset, $value ); }
    public function offsetUnset( mixed $offset ): void { unset( $this->params[ $offset ] ); }
}

class WP_REST_Response {

    private mixed $data;
    private int   $status;
    private array $headers;

    public function __construct( mixed $data = null, int $status = 200, array $headers = [] ) {
        $this->data    = $data;
        $this->status  = $status;
        $this->headers = $headers;
    }

    public function get_data():    mixed { return $this->data; }
    public function get_status():  int   { return $this->status; }
    public function get_headers(): array { return $this->headers; }

    public function set_data( mixed $data ): void     { $this->data   = $data; }
    public function set_status( int $status ): void   { $this->status = $status; }

    public function header( string $key, string $value, bool $replace = true ): void {
        $this->headers[ $key ] = $value;
    }

    public function is_error(): bool { return $this->data instanceof WP_Error; }

    public function send(): never {
        http_response_code( $this->status );
        header( 'Content-Type: application/json; charset=UTF-8' );
        foreach ( $this->headers as $k => $v ) {
            header( "{$k}: {$v}" );
        }
        if ( $this->data instanceof WP_Error ) {
            echo json_encode( [
                'code'    => $this->data->get_error_code(),
                'message' => $this->data->get_error_message(),
                'data'    => $this->data->get_error_data() ?? [ 'status' => $this->status ],
            ] );
        } else {
            echo json_encode( $this->data );
        }
        exit;
    }
}

function rest_ensure_response( mixed $data ): WP_REST_Response {
    if ( $data instanceof WP_REST_Response ) return $data;
    if ( $data instanceof WP_Error ) return new WP_REST_Response( $data, 500 );
    return new WP_REST_Response( $data );
}

function rest_url( string $path = '' ): string {
    $base = rtrim( defined( 'SZ_REST_URL' ) ? SZ_REST_URL : ( ( $_SERVER['REQUEST_SCHEME'] ?? 'https' ) . '://' . ( $_SERVER['HTTP_HOST'] ?? 'localhost' ) . '/wp-json/' ), '/' );
    return $base . '/' . ltrim( $path, '/' );
}
