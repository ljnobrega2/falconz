<?php
/**
 * Senderzz — Módulo Motoboy
 * Database: criação e migração das tabelas
 */
if ( ! defined( 'ABSPATH' ) ) exit;


if ( ! function_exists( 'sz_motoboy_br_timezone' ) ) {
    /** Timezone oficial da operação COD Motoboy. */
    function sz_motoboy_br_timezone(): DateTimeZone {
        return new DateTimeZone( 'America/Sao_Paulo' );
    }
}

if ( ! function_exists( 'sz_motoboy_now_mysql' ) ) {
    /** Agora em horário do Brasil, sem depender do timezone global do WordPress/servidor. */
    function sz_motoboy_now_mysql(): string {
        return ( new DateTimeImmutable( 'now', sz_motoboy_br_timezone() ) )->format( 'Y-m-d H:i:s' );
    }
}

if ( ! function_exists( 'sz_motoboy_format_br_datetime' ) ) {
    /** Formata DATETIME da operação Motoboy como horário de Brasília. */
    function sz_motoboy_format_br_datetime( $value, string $format = 'd/m/Y H:i' ): string {
        $value = trim( (string) $value );
        if ( $value === '' || $value === '0000-00-00 00:00:00' ) return '';
        try {
            $dt = DateTimeImmutable::createFromFormat( 'Y-m-d H:i:s', $value, sz_motoboy_br_timezone() );
            if ( ! $dt ) $dt = new DateTimeImmutable( $value, sz_motoboy_br_timezone() );
            return $dt->format( $format );
        } catch ( Throwable $e ) {
            return '';
        }
    }
}


if ( ! function_exists( 'sz_mb_mask_cpf' ) ) {
    function sz_mb_mask_cpf( string $cpf ): string {
        $d = preg_replace( '/\D+/', '', $cpf );
        if ( strlen( $d ) !== 11 ) return $cpf;
        return substr( $d, 0, 3 ) . '.' . substr( $d, 3, 3 ) . '.' . substr( $d, 6, 3 ) . '-' . substr( $d, 9, 2 );
    }
}

if ( ! function_exists( 'sz_motoboy_normalize_legacy_utc_timestamps' ) ) {
    /**
     * v62: corrige transições Motoboy gravadas em UTC/servidor nas versões anteriores.
     * Regra conservadora: só soma 3h em ts_* que ficaram antes do created_at da própria linha,
     * caso típico do bug visto no timeline (embalado/em_rota retroagindo para o dia anterior).
     */
    function sz_motoboy_normalize_legacy_utc_timestamps(): void {
        if ( get_option( 'sz_motoboy_br_timestamps_v62_done' ) ) return;
        global $wpdb;
        $table = $wpdb->prefix . 'sz_motoboy_pedidos';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) !== $table ) return;
        foreach ( [ 'ts_aprovado', 'ts_embalado', 'ts_em_rota', 'ts_a_caminho', 'ts_entregue', 'ts_frustrado' ] as $col ) {
            $wpdb->query( "UPDATE {$table} SET {$col} = DATE_ADD({$col}, INTERVAL 3 HOUR) WHERE {$col} IS NOT NULL AND {$col} <> '0000-00-00 00:00:00' AND created_at IS NOT NULL AND {$col} < created_at AND TIMESTAMPDIFF(HOUR, {$col}, created_at) BETWEEN 1 AND 12" );
        }
        update_option( 'sz_motoboy_br_timestamps_v62_done', sz_motoboy_now_mysql(), false );
    }
    add_action( 'init', 'sz_motoboy_normalize_legacy_utc_timestamps', 20 );
}



if ( ! function_exists( 'sz_motoboy_normalize_legacy_utc_timestamps_v65' ) ) {
    /**
     * v65: corrige timestamps Motoboy que foram gravados em UTC antes da padronização Brasil.
     * Conservador: só ajusta etapas que aparecem antes da criação do pedido ou antes da etapa anterior.
     */
    function sz_motoboy_normalize_legacy_utc_timestamps_v65(): void {
        if ( get_option( 'sz_motoboy_br_timestamps_v65_done' ) ) return;
        global $wpdb;
        $table = $wpdb->prefix . 'sz_motoboy_pedidos';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) !== $table ) return;
        $cols = [ 'ts_embalado', 'ts_em_rota', 'ts_a_caminho', 'ts_entregue', 'ts_frustrado' ];
        foreach ( $cols as $col ) {
            $wpdb->query( "UPDATE {$table} SET {$col} = DATE_ADD({$col}, INTERVAL 3 HOUR) WHERE {$col} IS NOT NULL AND {$col} <> '0000-00-00 00:00:00' AND created_at IS NOT NULL AND {$col} < created_at AND TIMESTAMPDIFF(HOUR, {$col}, created_at) BETWEEN 1 AND 12" );
        }
        $wpdb->query( "UPDATE {$table} SET ts_entregue = DATE_ADD(ts_entregue, INTERVAL 3 HOUR) WHERE ts_entregue IS NOT NULL AND ts_entregue <> '0000-00-00 00:00:00' AND ts_em_rota IS NOT NULL AND ts_entregue < ts_em_rota AND TIMESTAMPDIFF(HOUR, ts_entregue, ts_em_rota) BETWEEN 1 AND 12" );
        update_option( 'sz_motoboy_br_timestamps_v65_done', sz_motoboy_now_mysql(), false );
    }
    add_action( 'init', 'sz_motoboy_normalize_legacy_utc_timestamps_v65', 21 );
}

define( 'SZ_MOTOBOY_DB_VERSION', '1.0.5' );


if ( ! function_exists( 'sz_motoboy_migrate_zone_operation_days' ) ) {
    /** Garante a coluna de dias de funcionamento nas zonas antigas. */
    function sz_motoboy_migrate_zone_operation_days(): void {
        if ( get_option( 'sz_motoboy_zone_schema_v358_done' ) ) return;
        global $wpdb;
        $table = $wpdb->prefix . 'sz_motoboy_zonas';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) ) !== $table ) return;
        $col = $wpdb->get_var( "SHOW COLUMNS FROM {$table} LIKE 'dias_funcionamento'" );
        if ( ! $col ) {
            $wpdb->query( "ALTER TABLE {$table} ADD dias_funcionamento VARCHAR(20) NOT NULL DEFAULT '1,2,3,4,5,6' AFTER descricao" );
        }
        $wpdb->query( "UPDATE {$table} SET dias_funcionamento = '1,2,3,4,5,6' WHERE dias_funcionamento IS NULL OR dias_funcionamento = ''" );
        $col_cutoff = $wpdb->get_var( "SHOW COLUMNS FROM {$table} LIKE 'cutoff_horarios'" );
        if ( ! $col_cutoff ) {
            $wpdb->query( "ALTER TABLE {$table} ADD cutoff_horarios TEXT NULL AFTER dias_funcionamento" );
        }
        $default_cutoff = function_exists( 'sz_motoboy_sanitize_zone_cutoffs' ) ? sz_motoboy_sanitize_zone_cutoffs( [] ) : '{"0":"21:00","1":"21:00","2":"21:00","3":"21:00","4":"21:00","5":"21:00","6":"21:00"}';
        $wpdb->query( $wpdb->prepare( "UPDATE {$table} SET cutoff_horarios = %s WHERE cutoff_horarios IS NULL OR cutoff_horarios = ''", $default_cutoff ) );
        update_option( 'sz_motoboy_zone_schema_v358_done', sz_motoboy_now_mysql(), false );
    }
    add_action( 'init', 'sz_motoboy_migrate_zone_operation_days', 5 );
}


if ( ! function_exists( 'sz_motoboy_seed_sp_agenda_zones_v354' ) ) {
    /**
     * v354: agenda comercial por CEP, conforme mapa Senderzz.
     * - Sexta: Santos, Cubatão, Praia Grande, São Vicente. Limite quinta 12h.
     * - Sábado: Mairiporã, Franco da Rocha, Francisco Morato, Cajamar, Caieiras,
     *   Jundiaí, Jacareí, São José dos Campos, Taubaté, Pindamonhangaba. Limite sexta 12h.
     *
     * Observação técnica: o modelo usa o horário limite no dia anterior da entrega.
     * Então Friday/Saturday com cutoff 12:00 atende exatamente a regra pedida.
     */
    function sz_motoboy_seed_sp_agenda_zones_v354(): void {
        if ( get_option( 'sz_motoboy_sp_agenda_zones_v354_done' ) ) return;
        global $wpdb;
        $p = $wpdb->prefix;
        $cd_table   = $p . 'sz_motoboy_cds';
        $zone_table = $p . 'sz_motoboy_zonas';
        $cep_table  = $p . 'sz_motoboy_cep_zonas';
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $cd_table ) ) !== $cd_table ) return;
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $zone_table ) ) !== $zone_table ) return;
        if ( $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $cep_table ) ) !== $cep_table ) return;

        $cd_id = (int) $wpdb->get_var( "SELECT id FROM {$cd_table} WHERE ativo=1 AND (nome='SP' OR nome LIKE '%São Paulo%' OR cidade='São Paulo') ORDER BY id ASC LIMIT 1" );
        if ( $cd_id <= 0 ) {
            $cd_id = (int) $wpdb->get_var( "SELECT id FROM {$cd_table} WHERE ativo=1 ORDER BY id ASC LIMIT 1" );
        }
        if ( $cd_id <= 0 ) {
            $wpdb->insert( $cd_table, [
                'nome' => 'SP',
                'cidade' => 'São Paulo',
                'uf' => 'SP',
                'endereco' => '',
            ], [ '%s', '%s', '%s', '%s' ] );
            $cd_id = (int) $wpdb->insert_id;
        }
        if ( $cd_id <= 0 ) return;

        $make_cutoff = static function( string $time ): string {
            if ( function_exists( 'sz_motoboy_single_cutoff_payload' ) ) return sz_motoboy_single_cutoff_payload( $time );
            $time = preg_match( '/^\d{2}:\d{2}$/', $time ) ? $time : '21:00';
            return wp_json_encode( [ '0'=>$time, '1'=>$time, '2'=>$time, '3'=>$time, '4'=>$time, '5'=>$time, '6'=>$time ] );
        };

        $items = [
            [ 'Santos',               '11000001', '11249999', '5', '12:00' ],
            [ 'Cubatão',              '11500001', '11599999', '5', '12:00' ],
            [ 'Praia Grande',         '11700001', '11729999', '5', '12:00' ],
            [ 'São Vicente',          '11300001', '11399999', '5', '12:00' ],
            [ 'Mairiporã',            '07600001', '07699999', '6', '12:00' ],
            [ 'Franco da Rocha',      '07800001', '07899999', '6', '12:00' ],
            [ 'Francisco Morato',     '07900001', '07999999', '6', '12:00' ],
            [ 'Cajamar',              '07750001', '07799999', '6', '12:00' ],
            [ 'Caieiras',             '07700001', '07749999', '6', '12:00' ],
            [ 'Jundiaí',              '13200001', '13219999', '6', '12:00' ],
            [ 'Jacareí',              '12300001', '12349999', '6', '12:00' ],
            [ 'São José dos Campos',  '12200001', '12249999', '6', '12:00' ],
            [ 'Taubaté',              '12000001', '12119999', '6', '12:00' ],
            [ 'Pindamonhangaba',      '12400001', '12449999', '6', '12:00' ],
        ];

        foreach ( $items as $item ) {
            [ $nome, $cep_inicio, $cep_fim, $dias, $cutoff ] = $item;
            $cutoff_json = $make_cutoff( $cutoff );

            $zona_id = (int) $wpdb->get_var( $wpdb->prepare( "SELECT id FROM {$zone_table} WHERE nome=%s AND ativo=1 ORDER BY id ASC LIMIT 1", $nome ) );
            if ( $zona_id > 0 ) {
                $wpdb->update( $zone_table, [
                    'cd_id' => $cd_id,
                    'dias_funcionamento' => $dias,
                    'cutoff_horarios' => $cutoff_json,
                    'ativo' => 1,
                ], [ 'id' => $zona_id ], [ '%d', '%s', '%s', '%d' ], [ '%d' ] );
            } else {
                $wpdb->insert( $zone_table, [
                    'cd_id' => $cd_id,
                    'nome' => $nome,
                    'descricao' => '',
                    'dias_funcionamento' => $dias,
                    'cutoff_horarios' => $cutoff_json,
                    'ativo' => 1,
                ], [ '%d', '%s', '%s', '%s', '%s', '%d' ] );
                $zona_id = (int) $wpdb->insert_id;
            }
            if ( $zona_id <= 0 ) continue;

            // Remove faixas conflitantes para que o CEP resolva de forma única.
            $wpdb->query( $wpdb->prepare(
                "DELETE FROM {$cep_table}
                  WHERE zona_id <> %d
                    AND NOT (cep_fim < %s OR cep_inicio > %s)",
                $zona_id, $cep_inicio, $cep_fim
            ) );
            $wpdb->delete( $cep_table, [ 'zona_id' => $zona_id ], [ '%d' ] );
            $wpdb->insert( $cep_table, [
                'zona_id' => $zona_id,
                'cep_inicio' => $cep_inicio,
                'cep_fim' => $cep_fim,
            ], [ '%d', '%s', '%s' ] );
        }

        update_option( 'sz_motoboy_sp_agenda_zones_v354_done', sz_motoboy_now_mysql(), false );
    }
    add_action( 'init', 'sz_motoboy_seed_sp_agenda_zones_v354', 15 );
}

function sz_motoboy_create_tables() {
    global $wpdb;
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    $charset = $wpdb->get_charset_collate();
    $p       = $wpdb->prefix;

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboy_cds (
        id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        nome       VARCHAR(100)    NOT NULL,
        cidade     VARCHAR(100)    NOT NULL,
        uf         CHAR(2)         NOT NULL DEFAULT 'SP',
        endereco   VARCHAR(255)    NULL,
        lat        DECIMAL(10,7)   NULL,
        lng        DECIMAL(10,7)   NULL,
        ativo      TINYINT(1)      NOT NULL DEFAULT 1,
        created_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboy_zonas (
        id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        cd_id      BIGINT UNSIGNED NOT NULL,
        nome       VARCHAR(100)    NOT NULL,
        descricao  VARCHAR(255)    NULL,
        dias_funcionamento VARCHAR(20) NOT NULL DEFAULT '1,2,3,4,5,6',
        cutoff_horarios TEXT NULL,
        ativo      TINYINT(1)      NOT NULL DEFAULT 1,
        created_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_cd_id (cd_id)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboy_cep_zonas (
        id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        zona_id    BIGINT UNSIGNED NOT NULL,
        cep_inicio CHAR(8)         NOT NULL,
        cep_fim    CHAR(8)         NOT NULL,
        created_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_zona_id (zona_id),
        KEY idx_cep (cep_inicio, cep_fim)
    ) $charset;" );

    // Tabela pivot: motoboy pode atuar em múltiplas zonas
    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboy_zona_pivot (
        motoboy_id BIGINT UNSIGNED NOT NULL,
        zona_id    BIGINT UNSIGNED NOT NULL,
        PRIMARY KEY (motoboy_id, zona_id),
        KEY idx_zona (zona_id)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboys (
        id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        cd_id        BIGINT UNSIGNED NOT NULL,
        zona_id      BIGINT UNSIGNED NULL,
        nome         VARCHAR(100)    NOT NULL,
        telefone     VARCHAR(20)     NOT NULL,
        cpf          VARCHAR(14)     NULL,
        email        VARCHAR(100)    NULL,
        tipo_pgto    ENUM('clt','pj','autonomo') NOT NULL DEFAULT 'autonomo',
        ativo        TINYINT(1)      NOT NULL DEFAULT 1,
        token_app    VARCHAR(64)     NULL,
        pin_hash     VARCHAR(255)    NULL,  -- bcrypt hash do PIN de acesso (4-8 dígitos)
        ultimo_lat   DECIMAL(10,7)   NULL,
        ultimo_lng   DECIMAL(10,7)   NULL,
        ultimo_ping  DATETIME        NULL,
        created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_cd_id (cd_id),
        KEY idx_zona_id (zona_id),
        UNIQUE KEY uk_token (token_app)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboy_pedidos (
        id                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        wc_order_id          BIGINT UNSIGNED NOT NULL,
        cd_id                BIGINT UNSIGNED NOT NULL,
        zona_id              BIGINT UNSIGNED NOT NULL,
        motoboy_id           BIGINT UNSIGNED NULL,
        status               ENUM('agendado','embalado','em_rota','entregue','frustrado','cancelado','aprovado','a_caminho','reagendado') NOT NULL DEFAULT 'agendado',
        dest_nome            VARCHAR(150)    NULL,
        dest_telefone        VARCHAR(20)     NULL,
        dest_cep             CHAR(8)         NOT NULL,
        dest_endereco        VARCHAR(255)    NULL,
        dest_numero          VARCHAR(20)     NULL,
        dest_complemento     VARCHAR(100)    NULL,
        dest_produto         VARCHAR(500)    NULL COMMENT 'Resumo dos itens: nome x qty',
        dest_bairro          VARCHAR(100)    NULL,
        dest_cidade          VARCHAR(100)    NULL,
        dest_uf              CHAR(2)         NULL,
        dest_lat             DECIMAL(10,7)   NULL,
        dest_lng             DECIMAL(10,7)   NULL,
        valor_pedido         DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        valor_taxa           DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        valor_taxa_frustrado DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        pgto_dinheiro        DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        pgto_pix             DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        pgto_cartao          DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        recebedor_cpf        VARCHAR(14)     NULL,
        recebedor_nome       VARCHAR(150)    NULL,
        recebedor_tipo       ENUM('cliente','terceiro') NULL DEFAULT 'cliente' COMMENT 'cliente=destinatário, terceiro=outra pessoa',
        recebedor_assinatura LONGTEXT        NULL,
        baixa_por            ENUM('motoboy','admin') NULL DEFAULT 'motoboy' COMMENT 'Quem deu a baixa',
        baixa_admin_user_id  BIGINT UNSIGNED NULL COMMENT 'WP user_id se baixa foi por admin',
        baixa_motoboy_id     BIGINT UNSIGNED NULL COMMENT 'Motoboy que recebe o valor (pode diferir do motoboy_id quando baixa por admin)',
        baixa_at             DATETIME        NULL COMMENT 'Data/hora exata da baixa',
        entrega_foto         VARCHAR(255)    NULL COMMENT 'foto principal legado',
        comprovantes_count   TINYINT         NOT NULL DEFAULT 0 COMMENT 'qtd de comprovantes enviados',
        entrega_lat          DECIMAL(10,7)   NULL,
        entrega_lng          DECIMAL(10,7)   NULL,
        frustrado_motivo     VARCHAR(255)    NULL,
        frustrado_observacao TEXT            NULL,
        frustrado_isento     TINYINT(1)      NOT NULL DEFAULT 0,
        reagendado_para      DATE            NULL,
        ts_aprovado          DATETIME        NULL,
        ts_embalado          DATETIME        NULL,
        ts_em_rota           DATETIME        NULL,
        ts_a_caminho         DATETIME        NULL,
        ts_entregue          DATETIME        NULL,
        ts_frustrado         DATETIME        NULL,
        created_at           DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at           DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        UNIQUE KEY uk_wc_order (wc_order_id),
        KEY idx_motoboy_id (motoboy_id),
        KEY idx_status (status),
        KEY idx_cd_zona (cd_id, zona_id),
        KEY idx_created (created_at)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboy_comprovantes (
        id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        pedido_id    BIGINT UNSIGNED NOT NULL COMMENT 'ID de sz_motoboy_pedidos',
        wc_order_id  BIGINT UNSIGNED NOT NULL,
        motoboy_id   BIGINT UNSIGNED NOT NULL,
        tipo_pgto    VARCHAR(20) NOT NULL DEFAULT 'dinheiro' COMMENT 'dinheiro|pix|cartao',
        foto_url     VARCHAR(500) NOT NULL,
        foto_path    VARCHAR(500) NOT NULL,
        baixa_por    ENUM('motoboy','admin') NOT NULL DEFAULT 'motoboy',
        created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_pedido (pedido_id),
        KEY idx_order (wc_order_id)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboy_fechamento (
        id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        motoboy_id         BIGINT UNSIGNED NOT NULL,
        cd_id              BIGINT UNSIGNED NOT NULL,
        data_fechamento    DATE            NOT NULL,
        total_pedidos      INT UNSIGNED    NOT NULL DEFAULT 0,
        total_entregues    INT UNSIGNED    NOT NULL DEFAULT 0,
        total_frustrados   INT UNSIGNED    NOT NULL DEFAULT 0,
        total_dinheiro     DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        total_pix          DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        total_cartao       DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        total_a_repassar   DECIMAL(10,2)   NOT NULL DEFAULT 0.00,
        repasse_confirmado TINYINT(1)      NOT NULL DEFAULT 0,
        repasse_ts         DATETIME        NULL,
        alan_confirmou     TINYINT(1)      NOT NULL DEFAULT 0,
        alan_ts            DATETIME        NULL,
        obs                TEXT            NULL,
        created_at         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        UNIQUE KEY uk_motoboy_data (motoboy_id, data_fechamento),
        KEY idx_cd_id (cd_id),
        KEY idx_data (data_fechamento)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_motoboy_audit (
        id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        pedido_id   BIGINT UNSIGNED NULL,
        motoboy_id  BIGINT UNSIGNED NULL,
        actor_tipo  ENUM('sistema','alan','motoboy','admin') NOT NULL DEFAULT 'sistema',
        actor_id    BIGINT UNSIGNED NULL,
        acao        VARCHAR(100)    NOT NULL,
        de_status   VARCHAR(50)     NULL,
        para_status VARCHAR(50)     NULL,
        meta_json   LONGTEXT        NULL,
        ip_address  VARCHAR(64)     NULL,
        created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_pedido (pedido_id),
        KEY idx_motoboy (motoboy_id),
        KEY idx_created (created_at)
    ) $charset;" );

    // ── Suporte / Tickets ────────────────────────────────────────────────────
    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_portal_tickets (
        id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        portal_user_id  BIGINT UNSIGNED NOT NULL COMMENT 'ID em sz_portal_users',
        assunto         VARCHAR(200)    NOT NULL,
        categoria       ENUM('financeiro','pedido','tecnico','outro') NOT NULL DEFAULT 'outro',
        status          ENUM('aberto','em_analise','respondido','fechado') NOT NULL DEFAULT 'aberto',
        prioridade      ENUM('baixa','normal','alta') NOT NULL DEFAULT 'normal',
        created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        fechado_at      DATETIME        NULL,
        PRIMARY KEY (id),
        KEY idx_user   (portal_user_id),
        KEY idx_status (status),
        KEY idx_created (created_at)
    ) $charset;" );

    dbDelta( "CREATE TABLE IF NOT EXISTS {$p}sz_portal_ticket_msgs (
        id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        ticket_id   BIGINT UNSIGNED NOT NULL,
        autor_tipo  ENUM('cliente','admin') NOT NULL DEFAULT 'cliente',
        autor_id    BIGINT UNSIGNED NULL COMMENT 'portal_user_id (cliente) ou wp_user_id (admin)',
        autor_nome  VARCHAR(100)    NULL,
        mensagem    TEXT            NOT NULL,
        created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_ticket (ticket_id),
        KEY idx_created (created_at)
    ) $charset;" );
    // ── /Suporte / Tickets ───────────────────────────────────────────────────


    // Migração segura para operação COD Motoboy isolada.
    // Mantém status legados no ENUM para não quebrar histórico, mas o fluxo novo usa:
    // agendado -> embalado -> em_rota -> entregue/frustrado/cancelado.
    $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos MODIFY status ENUM('agendado','embalado','em_rota','entregue','frustrado','cancelado','aprovado','a_caminho','reagendado') NOT NULL DEFAULT 'agendado'" );

    // v63: nome do recebedor para prova de entrega COD. Seguro para bancos já existentes.
    $has_recebedor_nome = $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'recebedor_nome'" );
    if ( ! $has_recebedor_nome ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD recebedor_nome VARCHAR(150) NULL AFTER recebedor_cpf" );
    }

    // ── Migration v252: colunas novas em sz_motoboy_pedidos ─────────────────
    // dbDelta não faz ALTER em tabelas existentes — migrations explícitas obrigatórias.

    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'dest_produto'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN dest_produto VARCHAR(500) NULL COMMENT 'Resumo dos itens: nome x qty' AFTER dest_lng" );
    }

    // v320: alguns bancos antigos não tinham a coluna quantidade; o insert do backfill falhava
    // e o Painel do Operador ficava zerado mesmo com pedidos Woo Motoboy existentes.
    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'quantidade'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN quantidade INT UNSIGNED NOT NULL DEFAULT 0 AFTER dest_produto" );
    }

    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'recebedor_tipo'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN recebedor_tipo ENUM('cliente','terceiro') NULL DEFAULT 'cliente' COMMENT 'cliente=destinatário, terceiro=outra pessoa' AFTER recebedor_nome" );
    }

    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'baixa_por'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN baixa_por ENUM('motoboy','admin') NULL DEFAULT 'motoboy' COMMENT 'Quem deu a baixa' AFTER recebedor_tipo" );
    }

    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'baixa_admin_user_id'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN baixa_admin_user_id BIGINT UNSIGNED NULL COMMENT 'WP user_id se baixa foi por admin' AFTER baixa_por" );
    }

    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'baixa_motoboy_id'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN baixa_motoboy_id BIGINT UNSIGNED NULL COMMENT 'Motoboy que recebe o valor' AFTER baixa_admin_user_id" );
    }

    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'baixa_at'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN baixa_at DATETIME NULL COMMENT 'Data/hora exata da baixa' AFTER baixa_motoboy_id" );
    }

    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'comprovantes_count'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN comprovantes_count TINYINT NOT NULL DEFAULT 0 COMMENT 'Qtd de comprovantes enviados' AFTER entrega_foto" );
    }
    // ── /Migration v252 ──────────────────────────────────────────────────────

    // Migration: coluna pin_hash para autenticação segura do motoboy no PWA
    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboys LIKE 'pin_hash'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboys ADD COLUMN pin_hash VARCHAR(255) NULL COMMENT 'bcrypt hash do PIN de acesso' AFTER token_app" );
    }

    // Migration: dest_complemento separado do número
    if ( ! $wpdb->get_var( "SHOW COLUMNS FROM {$p}sz_motoboy_pedidos LIKE 'dest_complemento'" ) ) {
        $wpdb->query( "ALTER TABLE {$p}sz_motoboy_pedidos ADD COLUMN dest_complemento VARCHAR(100) NULL COMMENT 'Complemento do endereço' AFTER dest_numero" );
    }

    update_option( 'sz_motoboy_db_version', SZ_MOTOBOY_DB_VERSION );
}

function sz_motoboy_audit( $args = [] ) {
    global $wpdb;
    $wpdb->insert( $wpdb->prefix . 'sz_motoboy_audit', [
        'pedido_id'   => $args['pedido_id']   ?? null,
        'motoboy_id'  => $args['motoboy_id']  ?? null,
        'actor_tipo'  => $args['actor_tipo']  ?? 'sistema',
        'actor_id'    => $args['actor_id']    ?? null,
        'acao'        => $args['acao']        ?? '',
        'de_status'   => $args['de_status']   ?? null,
        'para_status' => $args['para_status'] ?? null,
        'meta_json'   => isset( $args['meta'] ) ? wp_json_encode( $args['meta'] ) : null,
        'ip_address'  => $_SERVER['REMOTE_ADDR'] ?? null,
    ] );
}
