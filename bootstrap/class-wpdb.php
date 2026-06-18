<?php
/**
 * Senderzz Standalone — $wpdb replacement using PDO.
 * Drop-in compatible with WordPress wpdb API.
 */

if ( class_exists( 'wpdb' ) ) return;

class wpdb {

    public string $prefix;
    public int    $insert_id = 0;
    public int    $rows_affected = 0;
    public string $last_query   = '';
    public ?string $last_error  = null;

    private \PDO $pdo;
    private static array $cache = [];

    public function __construct( string $dbuser, string $dbpassword, string $dbname, string $dbhost ) {
        $dsn = "mysql:host={$dbhost};dbname={$dbname};charset=utf8mb4";
        $this->pdo = new \PDO( $dsn, $dbuser, $dbpassword, [
            \PDO::ATTR_ERRMODE            => \PDO::ERRMODE_EXCEPTION,
            \PDO::ATTR_DEFAULT_FETCH_MODE => \PDO::FETCH_OBJ,
            \PDO::MYSQL_ATTR_INIT_COMMAND => "SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci",
        ] );
        $this->prefix = defined( 'DB_PREFIX' ) ? DB_PREFIX : 'wp_';
    }

    public function get_charset_collate(): string {
        return 'DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci';
    }

    /**
     * prepare() — identical signature to WP's wpdb::prepare().
     * Supports %d, %f, %s, %i (identifier).
     */
    public function prepare( string $query, ...$args ): string {
        if ( isset( $args[0] ) && is_array( $args[0] ) ) {
            $args = $args[0];
        }
        $i = 0;
        return preg_replace_callback(
            '/%(d|f|s|i)/',
            function ( $m ) use ( &$args, &$i ) {
                $val = $args[ $i++ ] ?? null;
                return match ( $m[1] ) {
                    'd' => (int) $val,
                    'f' => (float) $val,
                    'i' => '`' . str_replace( '`', '', (string) $val ) . '`',
                    default => $this->pdo->quote( (string) $val ),
                };
            },
            $query
        );
    }

    public function query( string $sql ): int|false {
        $this->last_query = $sql;
        try {
            $stmt = $this->pdo->query( $sql );
            $this->rows_affected = $stmt->rowCount();
            $this->insert_id = (int) $this->pdo->lastInsertId();
            $this->last_error = null;
            return $this->rows_affected;
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }

    public function get_results( string $sql, string $output = OBJECT ): array {
        $this->last_query = $sql;
        try {
            $stmt = $this->pdo->query( $sql );
            $rows = $stmt->fetchAll( \PDO::FETCH_OBJ );
            if ( $output === ARRAY_A ) {
                return array_map( fn( $r ) => (array) $r, $rows );
            }
            return $rows;
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return [];
        }
    }

    public function get_row( string $sql, string $output = OBJECT, int $y = 0 ) {
        $this->last_query = $sql;
        try {
            $stmt = $this->pdo->query( $sql );
            $row  = $stmt->fetch( \PDO::FETCH_OBJ );
            if ( ! $row ) return null;
            if ( $output === ARRAY_A ) return (array) $row;
            return $row;
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return null;
        }
    }

    public function get_var( string $sql, int $x = 0, int $y = 0 ): mixed {
        $this->last_query = $sql;
        try {
            $stmt = $this->pdo->query( $sql );
            $row  = $stmt->fetch( \PDO::FETCH_NUM );
            return $row ? ( $row[ $x ] ?? null ) : null;
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return null;
        }
    }

    public function get_col( string $sql, int $x = 0 ): array {
        $this->last_query = $sql;
        try {
            $stmt = $this->pdo->query( $sql );
            return $stmt->fetchAll( \PDO::FETCH_COLUMN, $x );
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return [];
        }
    }

    public function insert( string $table, array $data, $format = null ): int|false {
        $cols = implode( ', ', array_map( fn( $k ) => "`{$k}`", array_keys( $data ) ) );
        $vals = implode( ', ', array_fill( 0, count( $data ), '?' ) );
        $sql  = "INSERT INTO `{$table}` ({$cols}) VALUES ({$vals})";
        try {
            $stmt = $this->pdo->prepare( $sql );
            $stmt->execute( array_values( $data ) );
            $this->insert_id     = (int) $this->pdo->lastInsertId();
            $this->rows_affected = $stmt->rowCount();
            $this->last_error    = null;
            return $this->rows_affected;
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }

    public function update( string $table, array $data, array $where, $format = null, $where_format = null ): int|false {
        $set  = implode( ', ', array_map( fn( $k ) => "`{$k}` = ?", array_keys( $data ) ) );
        $cond = implode( ' AND ', array_map( fn( $k ) => "`{$k}` = ?", array_keys( $where ) ) );
        $sql  = "UPDATE `{$table}` SET {$set} WHERE {$cond}";
        try {
            $stmt = $this->pdo->prepare( $sql );
            $stmt->execute( [ ...array_values( $data ), ...array_values( $where ) ] );
            $this->rows_affected = $stmt->rowCount();
            $this->last_error    = null;
            return $this->rows_affected;
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }

    public function delete( string $table, array $where, $where_format = null ): int|false {
        $cond = implode( ' AND ', array_map( fn( $k ) => "`{$k}` = ?", array_keys( $where ) ) );
        $sql  = "DELETE FROM `{$table}` WHERE {$cond}";
        try {
            $stmt = $this->pdo->prepare( $sql );
            $stmt->execute( array_values( $where ) );
            $this->rows_affected = $stmt->rowCount();
            $this->last_error    = null;
            return $this->rows_affected;
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }

    public function replace( string $table, array $data, $format = null ): int|false {
        $cols = implode( ', ', array_map( fn( $k ) => "`{$k}`", array_keys( $data ) ) );
        $vals = implode( ', ', array_fill( 0, count( $data ), '?' ) );
        $sql  = "REPLACE INTO `{$table}` ({$cols}) VALUES ({$vals})";
        try {
            $stmt = $this->pdo->prepare( $sql );
            $stmt->execute( array_values( $data ) );
            $this->insert_id     = (int) $this->pdo->lastInsertId();
            $this->rows_affected = $stmt->rowCount();
            return $this->rows_affected;
        } catch ( \PDOException $e ) {
            $this->last_error = $e->getMessage();
            return false;
        }
    }

    /** Pass-through for raw PDO when needed. */
    public function pdo(): \PDO { return $this->pdo; }
}
