<?php

namespace WC_MelhorEnvio\Portal;

/**
 * Portal_Auth
 *
 * Sistema de autenticação do painel do cliente.
 * - Cadastro com email + senha (bcrypt)
 * - Login com 2FA por email
 * - Sessão com "lembrar por X dias"
 * - Zero dependência do sistema de usuários do WordPress
 */
class Portal_Auth {

	const TABLE       = 'senderzz_portal_users';
	const TABLE_2FA   = 'senderzz_portal_2fa';
	const TABLE_SESS  = 'senderzz_portal_sessions';
	const COOKIE_NAME = 'senderzz_portal_session';

	/**
	 * Armazena apenas hash do token de sessão no banco para reduzir impacto de vazamento.
	 * Compatível com sessões antigas: consultas aceitam token puro OU hash.
	 */
	public static function hash_session_token( string $token ): string {
		$token = sanitize_text_field( $token );
		$salt  = defined( 'AUTH_SALT' ) && AUTH_SALT ? AUTH_SALT : wp_salt( 'auth' );
		return hash_hmac( 'sha256', $token, $salt );
	}

	public static function session_token_values( string $token ): array {
		$token = sanitize_text_field( $token );
		if ( $token === '' ) return [ '', '' ];
		return [ $token, self::hash_session_token( $token ) ];
	}

	/**
	 * Instala as tabelas necessárias.
	 */
	public static function install(): void {
		global $wpdb;
		$charset = $wpdb->get_charset_collate();

		$sql = "
		CREATE TABLE IF NOT EXISTS {$wpdb->prefix}" . self::TABLE . " (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			email VARCHAR(191) NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			shipping_class_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			name VARCHAR(191) NULL,
			parent_user_id BIGINT UNSIGNED NULL,
			permissions LONGTEXT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			last_login_at DATETIME NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY email (email),
			KEY parent_user_id (parent_user_id),
			KEY shipping_class_id (shipping_class_id)
		) {$charset};

		CREATE TABLE IF NOT EXISTS {$wpdb->prefix}" . self::TABLE_2FA . " (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			user_id BIGINT UNSIGNED NOT NULL,
			code VARCHAR(8) NOT NULL,
			expires_at DATETIME NOT NULL,
			used TINYINT(1) NOT NULL DEFAULT 0,
			PRIMARY KEY (id),
			KEY user_id (user_id)
		) {$charset};

		CREATE TABLE IF NOT EXISTS {$wpdb->prefix}" . self::TABLE_SESS . " (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			user_id BIGINT UNSIGNED NOT NULL,
			token VARCHAR(64) NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY token (token),
			KEY user_id (user_id)
		) {$charset};
		";

		require_once ABSPATH . 'wp-admin/includes/upgrade.php';
		dbDelta( $sql );

		// Migração segura para instalações antigas do portal.
		$users_table = $wpdb->prefix . self::TABLE;
		$columns = $wpdb->get_col( "DESC {$users_table}", 0 );
		$columns = is_array( $columns ) ? $columns : [];

		$maybe_add = function( string $column, string $definition ) use ( $wpdb, $users_table, $columns ) {
			if ( ! in_array( $column, $columns, true ) ) {
				$wpdb->query( "ALTER TABLE {$users_table} ADD {$definition}" );
			}
		};

		$maybe_add( 'name', "name VARCHAR(191) NULL AFTER shipping_class_id" );
		$maybe_add( 'parent_user_id', "parent_user_id BIGINT UNSIGNED NULL AFTER name" );
		$maybe_add( 'permissions', "permissions LONGTEXT NULL AFTER parent_user_id" );
		$maybe_add( 'status', "status VARCHAR(20) NOT NULL DEFAULT 'active' AFTER permissions" );
		$maybe_add( 'last_login_at', "last_login_at DATETIME NULL AFTER status" );
		$maybe_add( 'role',       "role VARCHAR(30) NOT NULL DEFAULT 'client' AFTER last_login_at" );
		$maybe_add( 'wp_user_id', "wp_user_id BIGINT UNSIGNED NULL AFTER role" );
		$maybe_add( 'require_2fa', "require_2fa TINYINT(1) NOT NULL DEFAULT 1 AFTER wp_user_id" );

		// Migração segura para sessões antigas: padroniza em token, aceita legado session_token.
		$sess_table = $wpdb->prefix . self::TABLE_SESS;
		$sess_exists = ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $sess_table ) ) === $sess_table );
		if ( $sess_exists ) {
			$sess_cols = $wpdb->get_col( "DESC {$sess_table}", 0 );
			$sess_cols = is_array( $sess_cols ) ? $sess_cols : [];
			if ( ! in_array( 'token', $sess_cols, true ) && in_array( 'session_token', $sess_cols, true ) ) {
				$wpdb->query( "ALTER TABLE {$sess_table} CHANGE session_token token VARCHAR(64) NOT NULL" );
			} elseif ( ! in_array( 'token', $sess_cols, true ) ) {
				$wpdb->query( "ALTER TABLE {$sess_table} ADD token VARCHAR(64) NOT NULL AFTER user_id" );
			}
		}

		// Back-fill wp_user_id for existing rows
		$wpdb->query(
			"UPDATE {$wpdb->prefix}senderzz_portal_users pu
			  INNER JOIN {$wpdb->users} wu ON wu.user_email COLLATE utf8mb4_unicode_ci = pu.email COLLATE utf8mb4_unicode_ci
			  SET pu.wp_user_id = wu.ID
			  WHERE pu.wp_user_id IS NULL"
		);

		// Index
		$has_idx = $wpdb->get_var( "SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='{$wpdb->prefix}senderzz_portal_users' AND INDEX_NAME='idx_wp_user_id'" );
		if ( ! $has_idx ) {
			$wpdb->query( "ALTER TABLE {$wpdb->prefix}senderzz_portal_users ADD INDEX idx_wp_user_id (wp_user_id)" );
		}

		// Registrar cron de limpeza de sessões expiradas
		if ( ! wp_next_scheduled( 'senderzz_portal_cleanup_sessions' ) ) {
			wp_schedule_event( time(), 'twicedaily', 'senderzz_portal_cleanup_sessions' );
		}
	}

	/**
	 * Cria um usuário do portal (feito pelo admin).
	 */
	public static function create_user( string $email, string $password, $shipping_class_ids = 0 ): array {
		global $wpdb;
		$table = $wpdb->prefix . self::TABLE;

		$email = sanitize_email( $email );
		if ( ! is_email( $email ) ) {
			return [ 'success' => false, 'message' => 'Email inválido.' ];
		}

		$exists = $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$table} WHERE email = %s", $email ) );
		if ( $exists ) {
			return [ 'success' => false, 'message' => 'Email já cadastrado.' ];
		}

		// Normalize: aceita int ou array de IDs.
		$class_ids_arr = is_array( $shipping_class_ids ) ? array_map( 'intval', $shipping_class_ids ) : [ (int) $shipping_class_ids ];
		$class_ids_arr = array_values( array_unique( array_filter( $class_ids_arr, fn($id) => $id >= 0 ) ) );
		$primary_class = ! empty( $class_ids_arr ) ? $class_ids_arr[0] : 0;

		$hash = wp_hash_password( $password );
		$wpdb->insert( $table, [
			'email'             => $email,
			'password_hash'     => $hash,
			'shipping_class_id' => $primary_class,
			'name'              => '',
			'parent_user_id'    => null,
			'permissions'       => wp_json_encode( [
				'wallet'  => true,
				'approve' => true,
				'cancel'  => true,
				'links'   => true,
			] ),
			'status'            => 'active',
			'role'              => 'client',
			'wp_user_id'        => get_user_by( 'email', $email ) ? get_user_by( 'email', $email )->ID : null,
		] );

		if ( ! $wpdb->insert_id ) {
			return [ 'success' => false, 'message' => 'Erro ao criar usuário.' ];
		}

		$new_user_id = (int) $wpdb->insert_id;
		// Sincroniza tabela multi-class
		if ( function_exists( 'sz_set_user_class_ids' ) ) {
			sz_set_user_class_ids( $new_user_id, $class_ids_arr );
		}

		return [ 'success' => true, 'user_id' => $new_user_id ];
	}

	/**
	 * Step 1 do login: valida email+senha.
	 * - Se require_2fa = 1 (padrão): gera código 2FA e retorna temp_token.
	 * - Se require_2fa = 0 (usuário desativou): cria sessão direto sem 2FA.
	 * Sessões duram 1 dia por padrão; usuário não escolhe mais o prazo no login.
	 */
	public static function login_step1( string $email, string $password ): array {
		global $wpdb;
		$table = $wpdb->prefix . self::TABLE;

		$email = sanitize_email( $email );

		// ── Rate limit: máx 5 tentativas por IP + email por 15 minutos ────────
		// Usa chave composta IP+email para não bloquear IPs compartilhados globalmente
		// e não revelar se o email existe (chave baseada em hash do email, não do usuário real).
		$ip        = sanitize_text_field( $_SERVER['REMOTE_ADDR'] ?? '0.0.0.0' );
		$rl_key    = 'sz_login_rl_' . md5( $ip . '|' . strtolower( $email ) );
		$attempts  = (int) get_transient( $rl_key );
		if ( $attempts >= 5 ) {
			return [ 'success' => false, 'message' => 'Muitas tentativas. Aguarde alguns minutos e tente novamente.' ];
		}

		$user = $wpdb->get_row( $wpdb->prepare( "SELECT * FROM {$table} WHERE email = %s", $email ) );

		// Credenciais inválidas OU conta inativa: mesma mensagem genérica para não revelar existência de conta
		$credenciais_invalidas = ( ! $user || ! wp_check_password( $password, $user->password_hash ) );
		$conta_inativa         = ( $user && isset( $user->status ) && $user->status !== 'active' );

		if ( $credenciais_invalidas || $conta_inativa ) {
			// Incrementa contador apenas em falha real de credenciais (não em conta inativa)
			// para não revelar enumeração por comportamento diferente do rate limit
			if ( $credenciais_invalidas ) {
				set_transient( $rl_key, $attempts + 1, 15 * MINUTE_IN_SECONDS );
			}
			return [ 'success' => false, 'message' => 'Email ou senha incorretos.' ];
		}

		// Login bem-sucedido: zera o contador de tentativas
		delete_transient( $rl_key );

		// Se 2FA desativado pelo usuário: cria sessão direto e retorna token de sessão
		$requires_2fa = ! isset( $user->require_2fa ) || (int) $user->require_2fa !== 0;
		if ( ! $requires_2fa ) {
			return self::create_session_for_user( (int) $user->id, $user->role ?: 'client' );
		}

		// 2FA ativado: gera e envia código
		$code       = str_pad( (string) random_int( 0, 999999 ), 6, '0', STR_PAD_LEFT );
		$expires    = gmdate( 'Y-m-d H:i:s', time() + 15 * MINUTE_IN_SECONDS );
		$table_2fa  = $wpdb->prefix . self::TABLE_2FA;

		$wpdb->insert( $table_2fa, [
			'user_id'    => (int) $user->id,
			'code'       => $code,
			'expires_at' => $expires,
			'used'       => 0,
		] );

		$temp_token = bin2hex( random_bytes( 20 ) );
		set_transient( 'senderzz_2fa_temp_' . $temp_token, (int) $user->id, 15 * MINUTE_IN_SECONDS );

		self::send_2fa_email( $user->email, $code );

		return [
			'success'    => true,
			'needs_2fa'  => true,
			'temp_token' => $temp_token,
			'message'    => 'Código enviado ao seu e-mail.',
		];
	}

	/**
	 * Cria sessão de 1 dia para o usuário (reutilizado por login direto e step2).
	 */
	private static function create_session_for_user( int $user_id, string $role ): array {
		global $wpdb;

		$session_token = bin2hex( random_bytes( 32 ) );
		$expires       = gmdate( 'Y-m-d H:i:s', time() + DAY_IN_SECONDS );

		// Housekeeping: remove sessões expiradas do usuário antes de criar a nova.
		$wpdb->query( $wpdb->prepare(
			"DELETE FROM {$wpdb->prefix}" . self::TABLE_SESS . " WHERE user_id = %d AND expires_at < NOW()",
			$user_id
		) );

		// Limite de sessões simultâneas: mantém no máximo 4 ativas (a nova será a 5ª).
		$max_concurrent = apply_filters( 'senderzz_portal_max_sessions', 5 );
		$active_sessions = (int) $wpdb->get_var( $wpdb->prepare(
			"SELECT COUNT(*) FROM {$wpdb->prefix}" . self::TABLE_SESS . " WHERE user_id = %d",
			$user_id
		) );
		if ( $active_sessions >= $max_concurrent ) {
			// Remove as sessões mais antigas para abrir espaço
			$wpdb->query( $wpdb->prepare(
				"DELETE FROM {$wpdb->prefix}" . self::TABLE_SESS . "
				 WHERE user_id = %d
				 ORDER BY created_at ASC
				 LIMIT %d",
				$user_id,
				$active_sessions - $max_concurrent + 1
			) );
		}

		$wpdb->insert( $wpdb->prefix . self::TABLE_SESS, [
			'user_id'    => $user_id,
			'token'      => self::hash_session_token( $session_token ),
			'expires_at' => $expires,
		] );

		if ( ! $wpdb->insert_id ) {
			return [ 'success' => false, 'message' => 'Erro ao iniciar sessão. Tente novamente.' ];
		}

		setcookie( self::COOKIE_NAME, $session_token, [
			'expires'  => time() + DAY_IN_SECONDS,
			'path'     => '/',
			'secure'   => is_ssl(),
			'httponly' => true,
			 'samesite' => 'Strict',
		] );

		$wpdb->update( $wpdb->prefix . self::TABLE, [ 'last_login_at' => current_time( 'mysql', true ) ], [ 'id' => $user_id ] );

		return [
			'success'       => true,
			'direct_login'  => true,
			'session_token' => $session_token,
			'role'          => $role,
			'message'       => 'Login realizado.',
		];
	}

	/**
	 * Step 2 do login: valida código 2FA e cria sessão de 1 dia.
	 */
	public static function login_step2( string $temp_token, string $code, int $remember_days = 1 ): array {
		global $wpdb;

		$data = get_transient( 'senderzz_2fa_temp_' . $temp_token );
		if ( ! $data ) {
			return [ 'success' => false, 'message' => 'Sessão expirada. Faça login novamente.' ];
		}

		// Compatibilidade com tokens gerados antes do patch: antes o transient guardava só o user_id.
		if ( is_numeric( $data ) ) {
			$data = [
				'user_id'  => (int) $data,
				'attempts' => 0,
			];
		}

		$user_id = isset( $data['user_id'] ) ? (int) $data['user_id'] : 0;
		if ( ! $user_id ) {
			delete_transient( 'senderzz_2fa_temp_' . $temp_token );
			return [ 'success' => false, 'message' => 'Sessão expirada. Faça login novamente.' ];
		}

		// S9: usar senderzz_login_get_client_ip() — valida proxy chain, evita IP spoofing via X-Forwarded-For cru
		$ip = function_exists( 'senderzz_login_get_client_ip' )
			? senderzz_login_get_client_ip()
			: sanitize_text_field( (string) ( $_SERVER['REMOTE_ADDR'] ?? '0.0.0.0' ) );

		$user_lock_key = 'sz_2fa_lock_user_' . $user_id;
		$ip_lock_key   = 'sz_2fa_lock_ip_' . md5( $ip );

		if ( ! empty( $data['blocked'] ) || get_transient( $user_lock_key ) || get_transient( $ip_lock_key ) ) {
			return [ 'success' => false, 'message' => 'Muitas tentativas. Aguarde alguns minutos.' ];
		}

		$table_2fa = $wpdb->prefix . self::TABLE_2FA;
		$row = $wpdb->get_row( $wpdb->prepare(
			"SELECT * FROM {$table_2fa} WHERE user_id = %d AND code = %s AND used = 0 AND expires_at > NOW() ORDER BY id DESC LIMIT 1",
			$user_id, $code
		) );

		if ( ! $row ) {
			$data['attempts'] = isset( $data['attempts'] ) ? (int) $data['attempts'] + 1 : 1;

			if ( $data['attempts'] >= 3 ) {
				$data['blocked'] = true;
				set_transient( $user_lock_key, 1, 10 * MINUTE_IN_SECONDS );
				set_transient( $ip_lock_key, 1, 10 * MINUTE_IN_SECONDS );
			}

			set_transient( 'senderzz_2fa_temp_' . $temp_token, $data, 5 * MINUTE_IN_SECONDS );
			error_log( '[SECURITY][2FA_FAIL] user_id=' . $user_id . ' ip=' . $ip . ' attempts=' . $data['attempts'] );

			return [
				'success' => false,
				'message' => ! empty( $data['blocked'] ) ? 'Muitas tentativas. Aguarde alguns minutos.' : 'Código inválido ou expirado.',
			];
		}

		// Marca como usado e remove dados temporários.
		$wpdb->update( $table_2fa, [ 'used' => 1 ], [ 'id' => $row->id ] );
		delete_transient( 'senderzz_2fa_temp_' . $temp_token );

		// Se o usuário acertou, remove locks para evitar travamento indevido após login legítimo.
		delete_transient( $user_lock_key );
		delete_transient( $ip_lock_key );

		// Busca role do usuário e preserva o fluxo original de criação de sessão.
		$user = $wpdb->get_row( $wpdb->prepare(
			"SELECT role FROM {$wpdb->prefix}" . self::TABLE . " WHERE id = %d",
			$user_id
		) );
		$role = $user ? ( $user->role ?: 'client' ) : 'client';

		return self::create_session_for_user( (int) $user_id, $role );
	}

	/**
	 * Valida sessão atual a partir do cookie.
	 * Retorna o usuário ou false.
	 */
	public static function get_current_user(): ?object {
		global $wpdb;

		$token = $_COOKIE[ self::COOKIE_NAME ] ?? '';
		if ( ! $token ) {
			return null;
		}

		$token = sanitize_text_field( $token );
		list( $token_raw, $token_hash ) = self::session_token_values( $token );
		$table_sess = $wpdb->prefix . self::TABLE_SESS;
		$table_user = $wpdb->prefix . self::TABLE;

		$session = $wpdb->get_row( $wpdb->prepare(
			"SELECT s.user_id FROM {$table_sess} s WHERE s.token IN (%s, %s) AND s.expires_at > NOW() LIMIT 1",
			$token_raw, $token_hash
		) );

		if ( ! $session ) {
			return null;
		}

		return $wpdb->get_row( $wpdb->prepare(
			"SELECT id, email, shipping_class_id, parent_user_id, permissions, name, status, last_login_at, role, wp_user_id, require_2fa FROM {$table_user} WHERE id = %d AND status = 'active'",
			$session->user_id
		) );
	}

	/**
	 * Logout — destroi cookie e sessão.
	 */
	public static function logout(): void {
		global $wpdb;

		$token = $_COOKIE[ self::COOKIE_NAME ] ?? '';
		if ( $token ) {
			list( $token_raw, $token_hash ) = self::session_token_values( sanitize_text_field( $token ) );
			$wpdb->query( $wpdb->prepare(
				"DELETE FROM {$wpdb->prefix}" . self::TABLE_SESS . " WHERE token IN (%s, %s)",
				$token_raw, $token_hash
			) );
		}

		unset( $_COOKIE[ self::COOKIE_NAME ] );

		$expire = time() - YEAR_IN_SECONDS;
		$secure = is_ssl();
		$domain = parse_url( home_url(), PHP_URL_HOST );

		foreach ( array_unique( [ '/', COOKIEPATH ?: '/', SITECOOKIEPATH ?: '/' ] ) as $path ) {
			setcookie( self::COOKIE_NAME, '', $expire, $path, '', $secure, true );
			if ( $domain ) {
				setcookie( self::COOKIE_NAME, '', $expire, $path, $domain, $secure, true );
				setcookie( self::COOKIE_NAME, '', $expire, $path, '.' . ltrim( $domain, '.' ), $secure, true );
			}
		}

		if ( PHP_VERSION_ID >= 70300 ) {
			setcookie( self::COOKIE_NAME, '', [
				'expires'  => $expire,
				'path'     => '/',
				'secure'   => $secure,
				'httponly' => true,
				'samesite' => 'Strict',
			] );
		}
	}


	/**
	 * Solicita recuperação de senha: gera token, envia e-mail.
	 * Retorna sempre a mesma mensagem para não revelar se o e-mail existe.
	 */
	public static function request_password_reset( string $email ): array {
		global $wpdb;
		$email = sanitize_email( $email );
		if ( ! is_email( $email ) ) {
			return [ 'success' => true, 'message' => 'Se este e-mail estiver cadastrado, você receberá as instruções em breve.' ];
		}

		$user = $wpdb->get_row( $wpdb->prepare(
			"SELECT id, email, status FROM {$wpdb->prefix}" . self::TABLE . " WHERE email = %s LIMIT 1",
			$email
		) );

		// Resposta genérica independente de existir ou não (anti-enumeração)
		if ( ! $user || $user->status !== 'active' ) {
			return [ 'success' => true, 'message' => 'Se este e-mail estiver cadastrado, você receberá as instruções em breve.' ];
		}

		// Rate limit: máx 3 resets por e-mail por hora
		$rate_key = 'sz_pwreset_rate_' . md5( $email );
		$attempts = (int) get_transient( $rate_key );
		if ( $attempts >= 3 ) {
			return [ 'success' => true, 'message' => 'Se este e-mail estiver cadastrado, você receberá as instruções em breve.' ];
		}
		set_transient( $rate_key, $attempts + 1, HOUR_IN_SECONDS );

		$token   = bin2hex( random_bytes( 32 ) );
		$expires = time() + 30 * MINUTE_IN_SECONDS;

		set_transient( 'sz_pwreset_' . $token, [
			'user_id' => (int) $user->id,
			'email'   => $email,
			'expires' => $expires,
		], 30 * MINUTE_IN_SECONDS );

		$portal_url = get_permalink( get_option( 'senderzz_portal_page_id' ) ) ?: home_url( '/meus-pedidos/' );
		$reset_url  = add_query_arg( [ 'sz_reset' => $token ], $portal_url );

		$subject  = 'Redefinição de senha — Senderzz';
		$logo_url = function_exists( 'senderzz_portal_logo_url' )
			? senderzz_portal_logo_url()
			: plugins_url( 'assets/images/senderzz-logo.png', dirname( __DIR__, 2 ) . '/senderzz-logistics.php' );
		$message = '
		<div style="font-family:var(--sz-font);max-width:520px;margin:0 auto;padding:32px;background:#f8fafc;border-radius:18px;border:1px solid #e5e7eb;">
			<div style="margin-bottom:18px;"><img src="' . esc_url( $logo_url ) . '" alt="Senderzz" style="display:block;height:30px;max-width:150px;width:auto;object-fit:contain;"></div>
			<h2 style="color:#111827;margin:0 0 8px;font-size:var(--sz-text-3xl);">Redefinir sua senha</h2>
			<p style="color:#475467;margin:0 0 24px;line-height:1.55;">Clique no botão abaixo para criar uma nova senha. O link expira em <strong>30 minutos</strong>.</p>
			<a href="' . esc_url( $reset_url ) . '" style="display:inline-block;background:#E8650A;color:#fff;text-decoration:none;padding:14px 24px;border-radius:12px;font-weight:700;font-size:var(--sz-text-md);">Criar nova senha</a>
			<p style="color:#667085;font-size:var(--sz-text-meta);line-height:1.5;margin-top:24px;">Se você não solicitou a redefinição, ignore este e-mail. Sua senha continua a mesma.</p>
			<p style="color:#98a2b3;font-size:var(--sz-text-sm);line-height:1.45;margin-top:14px;word-break:break-all;">Link: ' . esc_url( $reset_url ) . '</p>
		</div>';

		$html_ct = function() { return 'text/html'; };
		add_filter( 'wp_mail_content_type', $html_ct );
		wp_mail( $email, $subject, $message );
		remove_filter( 'wp_mail_content_type', $html_ct );

		return [ 'success' => true, 'message' => 'Se este e-mail estiver cadastrado, você receberá as instruções em breve.' ];
	}

	/**
	 * Valida token de reset — retorna dados do usuário ou false.
	 */
	public static function validate_reset_token( string $token ): ?array {
		if ( empty( $token ) ) return null;
		$data = get_transient( 'sz_pwreset_' . sanitize_text_field( $token ) );
		if ( ! $data || empty( $data['user_id'] ) || empty( $data['expires'] ) ) return null;
		if ( time() > (int) $data['expires'] ) {
			delete_transient( 'sz_pwreset_' . $token );
			return null;
		}
		return $data;
	}

	/**
	 * Completa o reset: troca a senha e invalida o token.
	 */
	public static function complete_password_reset( string $token, string $new_password ): array {
		$data = self::validate_reset_token( $token );
		if ( ! $data ) {
			return [ 'success' => false, 'message' => 'Link expirado ou inválido. Solicite um novo link.' ];
		}
		if ( strlen( $new_password ) < 8 ) {
			return [ 'success' => false, 'message' => 'A senha deve ter no mínimo 8 caracteres.' ];
		}

		global $wpdb;
		self::change_password( (int) $data['user_id'], $new_password );
		// Invalida todas as sessões ativas (segurança pós-reset)
		$wpdb->delete( $wpdb->prefix . self::TABLE_SESS, [ 'user_id' => (int) $data['user_id'] ] );
		delete_transient( 'sz_pwreset_' . $token );

		return [ 'success' => true, 'message' => 'Senha redefinida com sucesso. Faça login com sua nova senha.' ];
	}


	/**
	 * Reenvio de código 2FA — gera novo código para o temp_token existente.
	 * Rate limit: 1 reenvio por minuto por temp_token.
	 */
	public static function resend_2fa_code( string $temp_token ): array {
		if ( empty( $temp_token ) ) {
			return [ 'success' => false, 'message' => 'Sessão inválida. Faça login novamente.' ];
		}

		$data = get_transient( 'senderzz_2fa_temp_' . $temp_token );
		if ( ! $data ) {
			return [ 'success' => false, 'message' => 'Sessão expirada. Faça login novamente.' ];
		}

		if ( is_numeric( $data ) ) {
			$data = [ 'user_id' => (int) $data, 'attempts' => 0 ];
		}

		$user_id = (int) ( $data['user_id'] ?? 0 );
		if ( ! $user_id ) {
			return [ 'success' => false, 'message' => 'Sessão inválida. Faça login novamente.' ];
		}

		// Rate limit: 1 reenvio por minuto
		$rate_key = 'sz_2fa_resend_' . md5( $temp_token );
		if ( get_transient( $rate_key ) ) {
			return [ 'success' => false, 'message' => 'Aguarde 1 minuto antes de reenviar o código.' ];
		}
		set_transient( $rate_key, 1, MINUTE_IN_SECONDS );

		global $wpdb;
		$user = $wpdb->get_row( $wpdb->prepare(
			"SELECT email FROM {$wpdb->prefix}" . self::TABLE . " WHERE id = %d AND status = 'active'",
			$user_id
		) );

		if ( ! $user ) {
			return [ 'success' => false, 'message' => 'Usuário não encontrado.' ];
		}

		// Gera novo código
		$code      = str_pad( (string) random_int( 0, 999999 ), 6, '0', STR_PAD_LEFT );
		$expires   = gmdate( 'Y-m-d H:i:s', time() + 15 * MINUTE_IN_SECONDS );
		$table_2fa = $wpdb->prefix . self::TABLE_2FA;

		$wpdb->insert( $table_2fa, [
			'user_id'    => $user_id,
			'code'       => $code,
			'expires_at' => $expires,
			'used'       => 0,
		] );

		// Renova o temp_token por mais 15 minutos
		set_transient( 'senderzz_2fa_temp_' . $temp_token, $data, 15 * MINUTE_IN_SECONDS );

		self::send_2fa_email( $user->email, $code );

		return [ 'success' => true, 'message' => 'Novo código enviado ao seu e-mail.' ];
	}

	/**
	 * Limpa sessões expiradas de todos os usuários (chamado pelo cron).
	 */
	public static function cleanup_expired_sessions(): void {
		global $wpdb;
		$wpdb->query( "DELETE FROM {$wpdb->prefix}" . self::TABLE_SESS . " WHERE expires_at < NOW()" );
		// Limpa também códigos 2FA expirados
		$wpdb->query( "DELETE FROM {$wpdb->prefix}" . self::TABLE_2FA . " WHERE expires_at < NOW() OR used = 1" );
	}

	/**
	 * Altera senha do usuário.
	 */
	public static function change_password( int $user_id, string $new_password ): bool {
		global $wpdb;
		$rows = $wpdb->update(
			$wpdb->prefix . self::TABLE,
			[ 'password_hash' => wp_hash_password( $new_password ) ],
			[ 'id' => $user_id ]
		);
		return $rows !== false;
	}

	/**
	 * Ativa ou desativa o 2FA para o usuário.
	 */
	public static function set_require_2fa( int $user_id, bool $require ): bool {
		global $wpdb;
		$rows = $wpdb->update(
			$wpdb->prefix . self::TABLE,
			[ 'require_2fa' => $require ? 1 : 0 ],
			[ 'id' => $user_id ]
		);
		return $rows !== false;
	}

	/**
	 * Envia email com código 2FA.
	 */
	private static function send_2fa_email( string $to, string $code ): void {
		$subject  = 'Seu código de acesso Senderzz: ' . $code;
		$logo_url = function_exists( 'senderzz_portal_logo_url' )
			? senderzz_portal_logo_url()
			: plugins_url( 'assets/images/senderzz-logo.png', dirname( __DIR__, 2 ) . '/senderzz-logistics.php' );
		$message = '
		<div style="font-family:var(--sz-font);max-width:520px;margin:0 auto;padding:32px;background:#f8fafc;border-radius:18px;border:1px solid #e5e7eb;">
			<div style="margin-bottom:18px;"><img src="' . esc_url( $logo_url ) . '" alt="Senderzz" style="display:block;height:30px;max-width:150px;width:auto;object-fit:contain;"></div>
			<h2 style="color:#111827;margin:0 0 8px;font-size:var(--sz-text-3xl);">Código de verificação</h2>
			<p style="color:#475467;margin:0 0 22px;line-height:1.55;">Use o código abaixo para acessar o painel. Ele expira em <strong>15 minutos</strong>.</p>
			<div style="font-size:var(--sz-text-hero);font-weight:700;letter-spacing:.02em;color:#111827;text-align:center;padding:18px;background:#fff;border-radius:14px;border:1px solid #e5e7eb;">' . esc_html( $code ) . '</div>
			<p style="color:#667085;font-size:var(--sz-text-meta);line-height:1.5;margin-top:24px;">Se você não solicitou este código, ignore este e-mail.</p>
		</div>';

		$html_ct = function() { return 'text/html'; };
		add_filter( 'wp_mail_content_type', $html_ct );
		wp_mail( $to, $subject, $message );
		remove_filter( 'wp_mail_content_type', $html_ct );
	}
}
