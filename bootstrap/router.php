<?php
/**
 * Senderzz Standalone — REST Router.
 *
 * Captures all register_rest_route() calls, then dispatches
 * the current HTTP request to the matching callback.
 *
 * Usage:
 *   All plugin includes run register_rest_route() during bootstrap.
 *   Then call SZ_Router::dispatch() to handle the request.
 */

if ( class_exists( 'SZ_Router' ) ) return;

class SZ_Router {

    /** @var array<array{namespace:string, route:string, method:string, callback:callable, permission_callback:callable|null, pattern:string, param_names:string[]}> */
    private static array $routes = [];

    public static function add( string $namespace, string $route, array $args ): void {
        $methods = strtoupper( $args['methods'] ?? 'GET' );
        foreach ( explode( ',', $methods ) as $method ) {
            $method = trim( $method );
            // Convert WP-style route params (?P<name>\w+) to named captures
            $param_names = [];
            $pattern = preg_replace_callback(
                '/\(\?P<([^>]+)>[^)]+\)/',
                function ( $m ) use ( &$param_names ) {
                    $param_names[] = $m[1];
                    return '([^/]+)';
                },
                $route
            );
            self::$routes[] = [
                'namespace'            => $namespace,
                'route'                => $route,
                'method'               => $method,
                'callback'             => $args['callback'],
                'permission_callback'  => $args['permission_callback'] ?? null,
                'pattern'              => '#^/' . trim( $namespace, '/' ) . $pattern . '$#',
                'param_names'          => $param_names,
            ];
        }
    }

    public static function dispatch(): never {
        $method  = strtoupper( $_SERVER['REQUEST_METHOD'] ?? 'GET' );
        $uri     = strtok( $_SERVER['REQUEST_URI'] ?? '/', '?' );

        // Strip /wp-json prefix if present
        $uri = preg_replace( '#^/wp-json#', '', $uri );

        // CORS
        header( 'Access-Control-Allow-Origin: *' );
        header( 'Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS' );
        header( 'Access-Control-Allow-Headers: Content-Type, X-WP-Nonce, Authorization' );
        if ( $method === 'OPTIONS' ) { http_response_code( 200 ); exit; }

        foreach ( self::$routes as $route ) {
            if ( $route['method'] !== $method ) continue;
            if ( ! preg_match( $route['pattern'], $uri, $matches ) ) continue;

            $request = WP_REST_Request::from_globals();
            $request->set_url_params( array_combine(
                $route['param_names'],
                array_slice( $matches, 1 )
            ) );

            // Permission check
            $perm = $route['permission_callback'];
            if ( $perm && $perm !== '__return_true' ) {
                $allowed = is_string( $perm ) ? $perm( $request ) : $perm( $request );
                if ( is_wp_error( $allowed ) ) {
                    http_response_code( $allowed->get_error_data()['status'] ?? 403 );
                    header( 'Content-Type: application/json; charset=UTF-8' );
                    echo json_encode( [
                        'code'    => $allowed->get_error_code(),
                        'message' => $allowed->get_error_message(),
                        'data'    => $allowed->get_error_data(),
                    ] );
                    exit;
                }
                if ( $allowed === false ) {
                    http_response_code( 403 );
                    header( 'Content-Type: application/json; charset=UTF-8' );
                    echo json_encode( [ 'code' => 'rest_forbidden', 'message' => 'Acesso negado.', 'data' => [ 'status' => 403 ] ] );
                    exit;
                }
            }

            // Call route callback
            $cb       = $route['callback'];
            $response = is_string( $cb ) ? $cb( $request ) : $cb( $request );
            $response = rest_ensure_response( $response );
            $response->send();
        }

        // 404
        http_response_code( 404 );
        header( 'Content-Type: application/json; charset=UTF-8' );
        echo json_encode( [ 'code' => 'rest_no_route', 'message' => 'Rota não encontrada.', 'data' => [ 'status' => 404 ] ] );
        exit;
    }

    public static function routes(): array { return self::$routes; }
}

function register_rest_route( string $namespace, string $route, array $args, bool $override = false ): bool {
    if ( isset( $args[0] ) ) {
        // Multiple methods array
        foreach ( $args as $endpoint ) {
            SZ_Router::add( $namespace, $route, $endpoint );
        }
    } else {
        SZ_Router::add( $namespace, $route, $args );
    }
    return true;
}
