<?php
/**
 * Senderzz — Integrações de entrada
 * Receptor universal de pedidos externos: recebe webhook, mapeia campos e cria pedido de expedição normal.
 */
defined('ABSPATH') || exit;
if ( defined('SENDERZZ_INTEGRATIONS_LOADED') ) return;
define('SENDERZZ_INTEGRATIONS_LOADED', true);

function senderzz_int_table(): string { global $wpdb; return $wpdb->prefix . 'senderzz_integrations'; }
function senderzz_int_log_table(): string { global $wpdb; return $wpdb->prefix . 'senderzz_integration_log'; }

function senderzz_int_install_tables(): void {
    if ( get_option( 'senderzz_integrations_db_v358_done' ) ) return;
    global $wpdb;
    static $ran = false; if ($ran) return; $ran = true;
    require_once ABSPATH . 'wp-admin/includes/upgrade.php';
    $charset = $wpdb->get_charset_collate();
    $table = senderzz_int_table();
    $log = senderzz_int_log_table();
    dbDelta("CREATE TABLE {$table} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        portal_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        name VARCHAR(120) NOT NULL DEFAULT 'Integração padrão',
        token VARCHAR(96) NOT NULL,
        active TINYINT(1) NOT NULL DEFAULT 1,
        auto_cheapest TINYINT(1) NOT NULL DEFAULT 1,
        require_paid TINYINT(1) NOT NULL DEFAULT 1,
        ignore_duplicates TINYINT(1) NOT NULL DEFAULT 1,
        mapping_json LONGTEXT NULL,
        last_payload_json LONGTEXT NULL,
        last_received_at DATETIME NULL,
        last_status VARCHAR(40) NOT NULL DEFAULT '',
        last_error TEXT NULL,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NULL,
        PRIMARY KEY (id),
        UNIQUE KEY uniq_token (token),
        KEY idx_user (user_id),
        KEY idx_active (active)
    ) {$charset};");
    dbDelta("CREATE TABLE {$log} (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        integration_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        external_order_id VARCHAR(160) NOT NULL DEFAULT '',
        wc_order_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
        status VARCHAR(40) NOT NULL DEFAULT 'received',
        message TEXT NULL,
        payload_json LONGTEXT NULL,
        mapped_json LONGTEXT NULL,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id),
        KEY idx_integration (integration_id),
        KEY idx_user (user_id),
        KEY idx_ext (external_order_id),
        KEY idx_created (created_at)
    ) {$charset};");
    update_option( 'senderzz_integrations_db_v358_done', current_time( 'mysql' ), false );
}
add_action('init', 'senderzz_int_install_tables', 12);

function senderzz_int_path_get($data, string $path) {
    $path = trim($path);
    if ($path === '') return null;
    $cur = $data;
    foreach (explode('.', $path) as $part) {
        if ($part === '') continue;
        if (is_array($cur) && array_key_exists($part, $cur)) { $cur = $cur[$part]; continue; }
        if (is_object($cur) && isset($cur->{$part})) { $cur = $cur->{$part}; continue; }
        if (is_array($cur) && ctype_digit($part) && array_key_exists((int)$part, $cur)) { $cur = $cur[(int)$part]; continue; }
        return null;
    }
    return $cur;
}
function senderzz_int_flatten($data, string $prefix = ''): array {
    $out = [];
    if (!is_array($data)) return $out;
    foreach ($data as $k => $v) {
        $key = $prefix === '' ? (string)$k : $prefix . '.' . $k;
        if (is_array($v) && count($v) && count($out) < 250) $out += senderzz_int_flatten($v, $key);
        else $out[$key] = is_scalar($v) || $v === null ? $v : wp_json_encode($v, JSON_UNESCAPED_UNICODE);
    }
    return $out;
}
function senderzz_int_aliases(): array {
    return [
        'pedido.id_externo'=>['pedido.id_externo','order_id','pedido_id','order_number','internal_order_id','id','external_order_id'],
        'pedido.status_pagamento'=>['pedido.status_pagamento','payment_status','status_pagamento','status','payment.status'],
        'pedido.total'=>['pedido.total','total','valor_total','amount','order_total'],
        'pedido.frete'=>['pedido.frete','shipping','frete','shipping_total'],
        'cliente.nome'=>['cliente.nome','customer.name','customer.nome','cliente.name','nome'],
        'cliente.telefone'=>['cliente.telefone','customer.phone','customer.telefone','cliente.phone','phone','telefone'],
        'cliente.email'=>['cliente.email','customer.email','email'],
        'cliente.documento'=>['cliente.documento','cliente.cpf_cnpj','customer.documento','customer.cpf_cnpj','documento','cpf_cnpj','cpf','cnpj'],
        'endereco.cep'=>['endereco.cep','shipping_address.zip','shipping_address.cep','endereco_entrega.zip','endereco_entrega.cep','address.zip','address.zipcode','address.cep'],
        'endereco.rua'=>['endereco.rua','shipping_address.street','shipping_address.rua','endereco_entrega.street','endereco_entrega.rua','address.street','address.rua'],
        'endereco.numero'=>['endereco.numero','shipping_address.number','shipping_address.numero','endereco_entrega.number','endereco_entrega.numero','address.number','address.numero'],
        'endereco.complemento'=>['endereco.complemento','shipping_address.complement','shipping_address.complemento','endereco_entrega.complement','endereco_entrega.complemento','address.complement','address.complemento'],
        'endereco.bairro'=>['endereco.bairro','shipping_address.neighborhood','shipping_address.bairro','endereco_entrega.neighborhood','endereco_entrega.bairro','address.neighborhood','address.district','address.bairro'],
        'endereco.cidade'=>['endereco.cidade','shipping_address.city','shipping_address.cidade','endereco_entrega.city','endereco_entrega.cidade','address.city','address.cidade'],
        'endereco.estado'=>['endereco.estado','shipping_address.state','shipping_address.estado','endereco_entrega.state','endereco_entrega.estado','address.state','address.estado','uf'],
        'endereco.pais'=>['endereco.pais','shipping_address.country','shipping_address.pais','endereco_entrega.country','endereco_entrega.pais','address.country','address.pais'],
        'produtos'=>['products','produtos','items'],
        'produto.sku'=>['products.0.sku','produtos.0.sku','items.0.sku','products.0.product_id','produtos.0.product_id'],
        'produto.nome'=>['products.0.name','products.0.nome','produtos.0.name','produtos.0.nome','items.0.name','items.0.nome'],
        'produto.quantidade'=>['products.0.quantity','products.0.quantidade','produtos.0.quantity','produtos.0.quantidade','items.0.quantity','items.0.qty'],
        'produto.valor_unitario'=>['products.0.unit_price','products.0.valor_unitario','produtos.0.unit_price','produtos.0.valor_unitario','items.0.price'],
        'produto.peso'=>['products.0.weight','products.0.peso','produtos.0.weight','produtos.0.peso','items.0.weight'],
        'produto.altura'=>['products.0.height','products.0.altura','produtos.0.height','produtos.0.altura','items.0.height'],
        'produto.largura'=>['products.0.width','products.0.largura','produtos.0.width','produtos.0.largura','items.0.width'],
        'produto.comprimento'=>['products.0.length','products.0.comprimento','produtos.0.length','produtos.0.comprimento','items.0.length'],
        'pedido.observacoes'=>['pedido.observacoes','notes','observacoes','delivery_reference','referencia_entrega','complementos_operacionais'],
    ];
}
function senderzz_int_first_value(array $payload, array $paths) {
    foreach ($paths as $path) {
        $v = senderzz_int_path_get($payload, $path);
        if ($v !== null && $v !== '') return $v;
    }
    return null;
}
function senderzz_int_canonical_payload(array $payload): array {
    $out = [];
    foreach (senderzz_int_aliases() as $canonical => $paths) {
        $v = senderzz_int_first_value($payload, $paths);
        if ($v === null || $v === '') continue;
        $parts = explode('.', $canonical);
        $ref =& $out;
        foreach ($parts as $i => $part) {
            if ($i === count($parts)-1) { $ref[$part] = $v; break; }
            if (!isset($ref[$part]) || !is_array($ref[$part])) $ref[$part] = [];
            $ref =& $ref[$part];
        }
        unset($ref);
    }
    return $out ?: $payload;
}
function senderzz_int_flatten_unique($data): array {
    return senderzz_int_flatten(senderzz_int_canonical_payload(is_array($data) ? $data : []));
}
function senderzz_int_default_mapping(): array {
    return [
        'external_order_id'=>'pedido.id_externo','payment_status'=>'pedido.status_pagamento','customer_name'=>'cliente.nome','customer_phone'=>'cliente.telefone','customer_email'=>'cliente.email','customer_document'=>'cliente.documento',
        'postcode'=>'endereco.cep','street'=>'endereco.rua','number'=>'endereco.numero','complement'=>'endereco.complemento','district'=>'endereco.bairro','city'=>'endereco.cidade','state'=>'endereco.estado',
        'items'=>'produtos','sku'=>'produto.sku','product_name'=>'produto.nome','quantity'=>'produto.quantidade','price'=>'produto.valor_unitario','weight'=>'produto.peso','height'=>'produto.altura','width'=>'produto.largura','length'=>'produto.comprimento'
    ];
}
function senderzz_int_num($value, float $fallback = 0.0): float {
    if ($value === null || $value === '') return $fallback;
    return (float) str_replace(',', '.', (string) $value);
}
function senderzz_int_product_from_sku(string $sku) {
    $sku = trim((string)$sku);
    if ($sku === '' || !function_exists('wc_get_product')) return null;

    // 1) Primeiro tenta SKU real do WooCommerce.
    if (function_exists('wc_get_product_id_by_sku')) {
        $product_id = (int) wc_get_product_id_by_sku($sku);
        if ($product_id) {
            $product = wc_get_product($product_id);
            if ($product) return $product;
        }
    }

    // 2) Muitos integradores enviam product_id no campo usado como SKU.
    // Se for numérico, tenta como ID do produto também.
    if (ctype_digit($sku)) {
        $product = wc_get_product((int)$sku);
        if ($product) return $product;
    }

    return null;
}
function senderzz_int_enrich_item_from_senderzz(array $item, int $default_class_id = 0): array {
    $item['sku'] = sanitize_text_field((string)($item['sku'] ?? ''));
    $product = senderzz_int_product_from_sku($item['sku']);
    if ($product) {
        $item['name'] = function_exists('senderzz_clean_product_label') ? senderzz_clean_product_label( $product->get_name() ) : $product->get_name();
        if (senderzz_int_num($item['price'] ?? null, 0) <= 0) $item['price'] = (float) wc_get_price_excluding_tax($product);

        // Produto Senderzz é a fonte oficial para cotação: usa medidas do cadastro quando existirem.
        $p_weight = senderzz_int_num($product->get_weight(), 0);
        $p_height = senderzz_int_num($product->get_height(), 0);
        $p_width  = senderzz_int_num($product->get_width(), 0);
        $p_length = senderzz_int_num($product->get_length(), 0);
        if ($p_weight > 0) $item['weight'] = $p_weight;
        if ($p_height > 0) $item['height'] = $p_height;
        if ($p_width  > 0) $item['width']  = $p_width;
        if ($p_length > 0) $item['length'] = $p_length;

        $item['product_id'] = (int) $product->get_id();
        $item['shipping_class_id'] = (int) $product->get_shipping_class_id();
        $item['_senderzz_enriched_from_sku'] = true;
        $item['_senderzz_source'] = 'SKU/ID Senderzz';
    }
    if (empty($item['shipping_class_id']) && $default_class_id > 0) $item['shipping_class_id'] = $default_class_id;
    $item['name'] = sanitize_text_field((string)($item['name'] ?: 'Produto'));
    $item['quantity'] = max(1, absint($item['quantity'] ?? 1));
    $item['price'] = senderzz_int_num($item['price'] ?? 0, 0);
    $item['weight'] = senderzz_int_num($item['weight'] ?? 0, 0);
    $item['height'] = senderzz_int_num($item['height'] ?? 0, 0);
    $item['width'] = senderzz_int_num($item['width'] ?? 0, 0);
    $item['length'] = senderzz_int_num($item['length'] ?? 0, 0);
    return $item;
}
function senderzz_int_apply_mapping(array $payload, array $mapping, int $default_class_id = 0): array {
    $defaults = senderzz_int_default_mapping();
    $g = function($key, $fallback='') use ($payload, $mapping, $defaults) {
        $paths = [];
        if (!empty($mapping[$key])) $paths[] = (string)$mapping[$key];
        if (!empty($defaults[$key])) $paths[] = (string)$defaults[$key];
        foreach ($paths as $path) {
            $v = senderzz_int_path_get($payload, $path);
            if ($v !== null && $v !== '') return $v;
        }
        return $fallback;
    };
    $items_raw = senderzz_int_path_get($payload, (string)($mapping['items'] ?? ''));
    if (!is_array($items_raw) && !empty($defaults['items'])) $items_raw = senderzz_int_path_get($payload, (string)$defaults['items']);
    $items = [];
    if (is_array($items_raw) && isset($items_raw[0]) && is_array($items_raw[0])) {
        foreach ($items_raw as $it) {
            $items[] = senderzz_int_enrich_item_from_senderzz([
                'sku' => sanitize_text_field((string)($it['sku'] ?? $it['product_id'] ?? $it['id'] ?? $it['codigo'] ?? '')),
                'name'=> sanitize_text_field((string)($it['name'] ?? $it['title'] ?? $it['nome'] ?? '')),
                'quantity'=> max(1, absint($it['quantity'] ?? $it['qty'] ?? $it['quantidade'] ?? 1)),
                'price'=> senderzz_int_num($it['price'] ?? $it['unit_price'] ?? $it['valor_unitario'] ?? $it['value'] ?? $it['valor'] ?? 0, 0),
                'weight'=> senderzz_int_num($it['weight'] ?? $it['peso'] ?? 0, 0),
                'height'=> senderzz_int_num($it['height'] ?? $it['altura'] ?? 0, 0),
                'width'=> senderzz_int_num($it['width'] ?? $it['largura'] ?? 0, 0),
                'length'=> senderzz_int_num($it['length'] ?? $it['comprimento'] ?? 0, 0),
            ], $default_class_id);
        }
    }
    if (!$items) {
        $items[] = senderzz_int_enrich_item_from_senderzz([
            'sku'=>(string)$g('sku'),
            'name'=>(string)$g('product_name',''),
            'quantity'=>max(1,absint($g('quantity',1))),
            'price'=>senderzz_int_num($g('price',0), 0),
            'weight'=>senderzz_int_num($g('weight',0), 0),
            'height'=>senderzz_int_num($g('height',0), 0),
            'width'=>senderzz_int_num($g('width',0), 0),
            'length'=>senderzz_int_num($g('length',0), 0),
        ], $default_class_id);
    }
    return [
        'external_order_id'=>sanitize_text_field((string)$g('external_order_id')),
        'payment_status'=>sanitize_key((string)$g('payment_status','paid')),
        'customer'=>['name'=>sanitize_text_field((string)$g('customer_name')), 'phone'=>preg_replace('/\D+/', '', (string)$g('customer_phone')), 'email'=>sanitize_email((string)$g('customer_email')), 'document'=>preg_replace('/\D+/', '', (string)$g('customer_document'))],
        'address'=>senderzz_int_enrich_address_via_cep(['postcode'=>preg_replace('/\D+/', '', (string)$g('postcode')), 'street'=>sanitize_text_field((string)$g('street')), 'number'=>sanitize_text_field((string)$g('number')), 'complement'=>sanitize_text_field((string)$g('complement')), 'district'=>sanitize_text_field((string)$g('district')), 'city'=>sanitize_text_field((string)$g('city')), 'state'=>strtoupper(sanitize_text_field((string)$g('state')))]),
        'items'=>$items,
    ];
}
function senderzz_int_enrich_address_via_cep(array $address): array {
    $cep = preg_replace('/\D+/', '', $address['postcode'] ?? '');
    if (strlen($cep) !== 8) return $address;
    // Só consulta se street estiver vazio
    if (!empty($address['street'])) return $address;
    $response = wp_remote_get("https://viacep.com.br/ws/{$cep}/json/", ['timeout'=>5,'sslverify'=>false]);
    if (is_wp_error($response)) return $address;
    $body = json_decode(wp_remote_retrieve_body($response), true);
    if (empty($body) || !empty($body['erro'])) return $address;
    if (empty($address['street']) && !empty($body['logradouro']))   $address['street']   = sanitize_text_field($body['logradouro']);
    if (empty($address['district']) && !empty($body['bairro']))     $address['district'] = sanitize_text_field($body['bairro']);
    if (empty($address['city']) && !empty($body['localidade']))     $address['city']     = sanitize_text_field($body['localidade']);
    if (empty($address['state']) && !empty($body['uf']))            $address['state']    = strtoupper($body['uf']);
    return $address;
}

function senderzz_int_validate_mapped_order(array $mapped): array {
    $missing = [];
    if (empty($mapped['external_order_id'])) $missing[] = 'ID externo do pedido';
    if (empty($mapped['customer']['name'])) $missing[] = 'nome do cliente';
    if (empty($mapped['customer']['phone'])) $missing[] = 'telefone';
    foreach (['postcode'=>'CEP','city'=>'cidade','state'=>'estado'] as $k=>$label) {
        if (empty($mapped['address'][$k])) $missing[] = $label;
    }
    foreach (($mapped['items'] ?? []) as $idx=>$it) {
        $n = $idx + 1;
        if (empty($it['sku']) && empty($it['name'])) $missing[] = "item {$n}: SKU ou nome";
        // Medidas não são validadas aqui — a cotação ME API usa fallback mínimo se vier zerado
    }
    return $missing;
}

function senderzz_int_get_or_create_for_user(int $wallet_user_id, int $portal_user_id=0): array {
    global $wpdb; senderzz_int_install_tables();
    $row = $wpdb->get_row($wpdb->prepare("SELECT * FROM ".senderzz_int_table()." WHERE user_id=%d ORDER BY id ASC LIMIT 1", $wallet_user_id), ARRAY_A);
    if ($row) return $row;
    $token = 'sz_' . wp_generate_password(48, false, false);
    $wpdb->insert(senderzz_int_table(), ['user_id'=>$wallet_user_id,'portal_user_id'=>$portal_user_id,'token'=>$token,'mapping_json'=>wp_json_encode(senderzz_int_default_mapping()),'created_at'=>current_time('mysql'),'updated_at'=>current_time('mysql')], ['%d','%d','%s','%s','%s','%s']);
    return $wpdb->get_row($wpdb->prepare("SELECT * FROM ".senderzz_int_table()." WHERE id=%d", (int)$wpdb->insert_id), ARRAY_A) ?: [];
}

function senderzz_int_quote_cheapest(array $mapped, int $class_id = 0): array {
    // Cotação direta via ME API — independente de sessão WC, funciona em contexto REST
    $token    = function_exists('senderzz_get_me_token') ? senderzz_get_me_token() : (string)get_option('tpc_me_token','');
    $api_base = defined('TPC_ME_API') ? rtrim(constant('TPC_ME_API'),'/') : 'https://www.melhorenvio.com.br/api/v2';
    $from_cep = (string)get_option('woocommerce_store_postcode','');
    // Tentar pegar CEP de origem das configurações do plugin de frete
    if (!$from_cep) {
        $from_cep = (string)get_option('tpc_cep_origem', '');
    }
    $from_cep = preg_replace('/\D+/', '', $from_cep);
    $to_cep   = preg_replace('/\D+/', '', $mapped['address']['postcode'] ?? '');
    if (!$token || !$from_cep || !$to_cep) {
        return ['_senderzz_no_rate'=>true,'_senderzz_debug'=>'Token ME, CEP origem ou CEP destino ausente. token='.(!$token?'vazio':'ok').' from='.$from_cep.' to='.$to_cep];
    }
    // Montar produtos para cotação
    $products = [];
    foreach ($mapped['items'] as $it) {
        $qty = max(1,(int)($it['quantity']??1));
        // Tenta enriquecer medidas do produto WC se zeradas
        $it_weight = senderzz_int_num($it['weight']??0,0);
        $it_width  = senderzz_int_num($it['width']??0,0);
        $it_height = senderzz_int_num($it['height']??0,0);
        $it_length = senderzz_int_num($it['length']??0,0);
        if (($it_weight <= 0 || $it_width <= 0) && !empty($it['sku'])) {
            $wc_prod = senderzz_int_product_from_sku((string)$it['sku']);
            if ($wc_prod) {
                if ($it_weight <= 0) $it_weight = (float)$wc_prod->get_weight();
                if ($it_width  <= 0) $it_width  = (float)$wc_prod->get_width();
                if ($it_height <= 0) $it_height = (float)$wc_prod->get_height();
                if ($it_length <= 0) $it_length = (float)$wc_prod->get_length();
            }
        }
        // Converter unidades WC para kg/cm se necessario
        $wc_weight_unit = get_option('woocommerce_weight_unit', 'kg');
        $wc_dim_unit    = get_option('woocommerce_dimension_unit', 'cm');
        if ($wc_weight_unit === 'lbs' && $it_weight > 0) $it_weight = $it_weight * 0.453592;
        elseif ($wc_weight_unit === 'oz' && $it_weight > 0) $it_weight = $it_weight * 0.0283495;
        elseif ($wc_weight_unit === 'g' && $it_weight > 0) $it_weight = $it_weight / 1000;
        if ($wc_dim_unit === 'in' && $it_width > 0)  { $it_width = $it_width * 2.54; $it_height = $it_height * 2.54; $it_length = $it_length * 2.54; }
        elseif ($wc_dim_unit === 'yd' && $it_width > 0) { $it_width = $it_width * 91.44; $it_height = $it_height * 91.44; $it_length = $it_length * 91.44; }
        elseif ($wc_dim_unit === 'mm' && $it_width > 0) { $it_width = $it_width / 10; $it_height = $it_height / 10; $it_length = $it_length / 10; }
        elseif ($wc_dim_unit === 'm' && $it_width > 0) { $it_width = $it_width * 100; $it_height = $it_height * 100; $it_length = $it_length * 100; }
        $products[] = [
            'id'             => 1,
            'width'          => max(1,(int)$it_width),
            'height'         => max(1,(int)$it_height),
            'length'         => max(1,(int)$it_length),
            'weight'         => max(0.1,(float)$it_weight),
            'insurance_value'=> 1.00,
            'quantity'       => $qty,
        ];
    }
    $body = ['from'=>['postal_code'=>$from_cep],'to'=>['postal_code'=>$to_cep],'products'=>$products,'options'=>['receipt'=>false,'own_hand'=>false,'insurance_value'=>1]];
    $response = wp_remote_post($api_base.'/me/shipment/calculate', [
        'headers' => ['Authorization'=>'Bearer '.$token,'Content-Type'=>'application/json','Accept'=>'application/json','User-Agent'=>'Senderzz/Integration'],
        'body'    => wp_json_encode($body, JSON_UNESCAPED_UNICODE),
        'timeout' => 15,
    ]);
    if (is_wp_error($response)) return ['_senderzz_no_rate'=>true,'_senderzz_debug'=>'Erro HTTP ME API: '.$response->get_error_message()];
    $data = json_decode(wp_remote_retrieve_body($response), true);
    if (!is_array($data)) return ['_senderzz_no_rate'=>true,'_senderzz_debug'=>'Resposta inválida da ME API.'];
    // Filtrar: só com preço, sem erro, não é Mini Envios (#17), não é motoboy
    $rates = [];
    foreach ($data as $method) {
        if (empty($method['price']) || !empty($method['error'])) continue;
        $name  = strtolower(remove_accents((string)($method['name']??'')));
        $label = ($method['company']['name']??$method['company']??'').' '.(string)($method['name']??'');
        if (strpos($name,'motoboy')!==false) continue;
        if (($method['id']??0)==17) continue; // Mini Envios
        $rates[] = ['id'=>'melhorenvio_'.$method['id'],'label'=>trim($label),'cost'=>(float)$method['price'],'meta'=>['melhorenvio_service_id'=>$method['id'],'melhorenvio_company'=>$method['company']['name']??'','melhorenvio_delivery_time'=>$method['delivery_time']??'']];
    }
    usort($rates, fn($a,$b)=>$a['cost']<=>$b['cost']);
    if (!empty($rates[0])) {
        $best = $rates[0];
        // Aplicar markup da classe de entrega
        $shipping_class_id = 0;
        foreach (($mapped['items'] ?? []) as $it) {
            if (!empty($it['shipping_class_id'])) { $shipping_class_id = (int)$it['shipping_class_id']; break; }
        }
        $real_cost = $best['cost'];
        if (function_exists('senderzz_apply_markup') && $real_cost > 0) {
            $charged = senderzz_apply_markup($real_cost, $shipping_class_id);
            $best['meta']['melhorenvio_original_cost'] = $real_cost;
            $best['cost'] = $charged;
        }
        $best['_senderzz_debug'] = ['from'=>$from_cep,'to'=>$to_cep,'total_cotacoes'=>count($rates),'markup_class'=>$shipping_class_id,'real_cost'=>$real_cost];
        return $best;
    }
    return ['_senderzz_no_rate'=>true,'_senderzz_debug'=>'Sem frete válido da ME API. from='.$from_cep.' to='.$to_cep.' total_respostas='.count($data)];
}


function senderzz_int_process_payload_for_integration(array $payload, array $integration, ?array $mapping = null): array {
    $mapping = is_array($mapping) ? $mapping : json_decode((string)($integration['mapping_json'] ?? ''), true);
    if (!is_array($mapping)) $mapping = senderzz_int_default_mapping();
    // Usa portal_user_id para buscar classes via tabela multi-class
    $class_ids = [];
    if (function_exists('sz_get_user_class_ids') && !empty($integration['portal_user_id'])) {
        $class_ids = sz_get_user_class_ids((int)$integration['portal_user_id']);
    }
    if (empty($class_ids)) {
        $class_ids = function_exists('senderzz_rest_owned_shipping_class_ids') ? senderzz_rest_owned_shipping_class_ids((int)$integration['user_id']) : [];
    }
    $default_class_id = (int)($class_ids[0] ?? 0);
    $canonical_payload = senderzz_int_canonical_payload($payload);
    $mapping_payload = array_replace_recursive($payload, $canonical_payload);
    $mapped = senderzz_int_apply_mapping($mapping_payload, $mapping, $default_class_id);
    // Se payment_status vier vazio, assume como pago (integração de entrada — pagamento já processado na origem)
    $ps = strtolower((string)$mapped['payment_status']);
    $paid = ($ps === '' || in_array($ps, ['paid','pago','approved','aprovado','confirmed','confirmado','processing'], true));
    $status = 'received'; $message = 'Recebido.'; $order_id = 0; $result = [];
    if (!empty($integration['require_paid']) && !$paid) {
        $status = 'ignored';
        $message = 'Pagamento não confirmado.';
    } else {
        $result = senderzz_int_create_order($mapped, $integration);
        $status = !empty($result['ok']) ? (!empty($result['duplicate']) ? 'duplicate' : 'created') : (strpos((string)($result['message'] ?? ''), 'Pendente de integração:') === 0 ? 'pending' : 'error');
        $message = (string)($result['message'] ?? '');
        $order_id = (int)($result['order_id'] ?? 0);
    }
    return [
        'canonical_payload' => $canonical_payload,
        'mapped' => $mapped,
        'status' => $status,
        'message' => $message,
        'order_id' => $order_id,
        'ok' => !in_array($status, ['error','pending'], true),
        'result' => $result,
    ];
}

function senderzz_int_mapped_preview(array $payload, array $integration, ?array $mapping = null): array {
    $mapping = is_array($mapping) ? $mapping : json_decode((string)($integration['mapping_json'] ?? ''), true);
    if (!is_array($mapping)) $mapping = senderzz_int_default_mapping();
    // Usa portal_user_id para buscar classes via tabela multi-class
    $class_ids = [];
    if (function_exists('sz_get_user_class_ids') && !empty($integration['portal_user_id'])) {
        $class_ids = sz_get_user_class_ids((int)$integration['portal_user_id']);
    }
    if (empty($class_ids)) {
        $class_ids = function_exists('senderzz_rest_owned_shipping_class_ids') ? senderzz_rest_owned_shipping_class_ids((int)$integration['user_id']) : [];
    }
    $default_class_id = (int)($class_ids[0] ?? 0);
    $canonical_payload = senderzz_int_canonical_payload($payload);
    $mapping_payload = array_replace_recursive($payload, $canonical_payload);
    $mapped = senderzz_int_apply_mapping($mapping_payload, $mapping, $default_class_id);
    foreach (($mapped['items'] ?? []) as $i => $item) {
        if (!empty($item['_senderzz_enriched_from_sku'])) {
            $mapped['items'][$i]['_origem'] = 'SKU/ID Senderzz';
            $mapped['items'][$i]['_medidas'] = 'puxadas do cadastro do produto';
        } else {
            $mapped['items'][$i]['_origem'] = 'fallback do payload';
            $mapped['items'][$i]['_alerta'] = 'produto não localizado pelo SKU/ID recebido';
        }
    }
    return $mapped;
}

function senderzz_int_create_order(array $mapped, array $integration): array {
    if (!function_exists('wc_create_order')) return ['ok'=>false,'message'=>'WooCommerce indisponível.'];
    $addr = $mapped['address']; $cust = $mapped['customer'];
    foreach (['external_order_id','customer','address','items'] as $k) { if (empty($mapped[$k])) return ['ok'=>false,'message'=>'Dados obrigatórios ausentes: '.$k]; }
    if (empty($cust['name']) || empty($addr['postcode']) || empty($addr['city']) || empty($addr['state'])) return ['ok'=>false,'message'=>'Cliente/endereço incompleto.'];
    global $wpdb;
    if (!empty($integration['ignore_duplicates']) && !empty($mapped['external_order_id'])) {
        $existing = wc_get_orders(['limit'=>1,'return'=>'ids','meta_key'=>'_senderzz_external_order_id','meta_value'=>$mapped['external_order_id']]);
        if ($existing) return ['ok'=>true,'duplicate'=>true,'order_id'=>(int)$existing[0],'message'=>'Pedido duplicado ignorado.'];
    }
    // Usa portal_user_id para buscar classes via tabela multi-class
    $class_ids = [];
    if (function_exists('sz_get_user_class_ids') && !empty($integration['portal_user_id'])) {
        $class_ids = sz_get_user_class_ids((int)$integration['portal_user_id']);
    }
    if (empty($class_ids)) {
        $class_ids = function_exists('senderzz_rest_owned_shipping_class_ids') ? senderzz_rest_owned_shipping_class_ids((int)$integration['user_id']) : [];
    }
    $class_id = (int)($class_ids[0] ?? 0);
    // Fallback: pegar shipping_class_id do primeiro item mapeado
    if (!$class_id) {
        foreach (($mapped['items'] ?? []) as $it) {
            if (!empty($it['shipping_class_id'])) { $class_id = (int)$it['shipping_class_id']; break; }
        }
    }
    $missing = senderzz_int_validate_mapped_order($mapped);
    if ($missing) return ['ok'=>false,'message'=>'Pendente de integração: dados ausentes para cotação — ' . implode(', ', array_unique($missing)) . '.'];
    $cheapest = senderzz_int_quote_cheapest($mapped, $class_id);
    $frete_pendente = !empty($cheapest['_senderzz_no_rate']);
    if ($frete_pendente) {
        // Sem cotação disponível no contexto REST — cria pedido com frete pendente
        $cheapest = ['id'=>'senderzz_pendente','label'=>'Frete a calcular','cost'=>0,'meta'=>[],'_senderzz_debug'=>$cheapest['_senderzz_debug'] ?? ''];
    }
    $order = wc_create_order(['customer_id'=>(int)$integration['user_id']]);
    if (is_wp_error($order)) return ['ok'=>false,'message'=>$order->get_error_message()];
    $name_parts = preg_split('/\s+/', trim($cust['name']), 2);
    $billing = ['first_name'=>$name_parts[0] ?? $cust['name'], 'last_name'=>$name_parts[1] ?? '', 'email'=>$cust['email'], 'phone'=>$cust['phone'], 'address_1'=>$addr['street'].($addr['number']?' '.$addr['number']:''), 'address_2'=>$addr['complement'], 'city'=>$addr['city'], 'state'=>$addr['state'], 'postcode'=>$addr['postcode'], 'country'=>'BR'];
    $order->set_address($billing, 'billing'); $order->set_address($billing, 'shipping');
    foreach ($mapped['items'] as $it) {
        $product_id = 0;
        if (!empty($it['sku']) && function_exists('wc_get_product_id_by_sku')) $product_id = (int) wc_get_product_id_by_sku($it['sku']);
        $product = $product_id ? wc_get_product($product_id) : null;
        if (!$product && !empty($it['sku']) && function_exists('senderzz_int_product_from_sku')) $product = senderzz_int_product_from_sku((string)$it['sku']);
        if (!$product) { $product = new WC_Product_Simple(); $product->set_name($it['name'] ?: 'Produto'); $product->set_regular_price((string)(float)$it['price']); $product->set_price((string)(float)$it['price']); $product->set_weight((string)senderzz_int_num($it['weight'] ?? 0,0)); $product->set_height((string)senderzz_int_num($it['height'] ?? 0,0)); $product->set_width((string)senderzz_int_num($it['width'] ?? 0,0)); $product->set_length((string)senderzz_int_num($it['length'] ?? 0,0)); $item_class_id=(int)($it['shipping_class_id'] ?? 0); if($item_class_id)$product->set_shipping_class_id($item_class_id); elseif($class_id)$product->set_shipping_class_id($class_id); }
        $order->add_product($product, max(1,(int)$it['quantity']), ['subtotal'=>(float)$it['price']*max(1,(int)$it['quantity']), 'total'=>(float)$it['price']*max(1,(int)$it['quantity'])]);
    }
    $shipping = new WC_Order_Item_Shipping();
    $shipping->set_method_title($cheapest['label'] ?: 'Frete Senderzz'); $shipping->set_method_id($cheapest['id'] ?: 'senderzz_cheapest'); $shipping->set_total((float)$cheapest['cost']);
    foreach (($cheapest['meta'] ?? []) as $mk=>$mv) $shipping->add_meta_data($mk, $mv, true);
    $order->add_item($shipping);
    $order->update_meta_data('_senderzz_origin','integration');
    $order->update_meta_data('_senderzz_integration_id',(int)$integration['id']);
    $order->update_meta_data('_senderzz_external_order_id',$mapped['external_order_id']);
    $order->update_meta_data('_senderzz_payment_type','prepaid');
    $order->update_meta_data('_senderzz_owner_user_id',(int)$integration['user_id']);
    // Integração = expedição. Não cria linha em sz_motoboy_pedidos e não usa status/fluxo motoboy.
    $order->update_meta_data('_senderzz_delivery_mode','expedicao');
    $order->update_meta_data('_senderzz_order_kind','expedicao');
    $order->update_meta_data('_senderzz_integration_flow','expedicao');
    $order->delete_meta_data('_senderzz_motoboy_flow_status');
    if ($class_id) $order->update_meta_data('_senderzz_product_shipping_class_id',$class_id);
    $order->update_meta_data('_senderzz_cheapest_shipping_rate', wp_json_encode($cheapest, JSON_UNESCAPED_UNICODE));
    $order->calculate_totals();
    // O motor de expedição/etiqueta do plugin dispara ao entrar em aprovado.
    // Se não houve cotação (contexto REST sem sessão WC), fica pendente para o admin cotar/aprovar.
    if ($frete_pendente) {
        $order->set_status('on-hold');
        $order->save();
        $order->update_status('on-hold', 'Pedido criado por Integração Senderzz. Frete a calcular — aguardando aprovação.');
        $order->save();
        return ['ok'=>true,'order_id'=>$order->get_id(),'message'=>'Pedido criado. Frete a calcular — aguardando aprovação.','shipping'=>$cheapest];
    }
    $order->set_status('on-hold');
    $order->save();
    $order->update_status('on-hold', 'Pedido criado por Integração Senderzz. Aguardando aprovação do produtor.');
    $order->save();
    if (function_exists('wp_schedule_single_event')) {
        wp_schedule_single_event(time() + 5, 'senderzz_auto_generate_label', [(int)$order->get_id()]);
    }
    return ['ok'=>true,'order_id'=>$order->get_id(),'message'=>'Pedido de expedição criado e enviado para geração de etiqueta.','shipping'=>$cheapest];
}

function senderzz_int_rest_receive(WP_REST_Request $req): WP_REST_Response {
    global $wpdb; senderzz_int_install_tables();

    // Aceita token via URL (retrocompatibilidade) OU via header Authorization: Bearer <token>
    $token = sanitize_text_field( (string) $req['token'] );
    if ( $token === '' ) {
        $auth_header = isset( $_SERVER['HTTP_AUTHORIZATION'] ) ? sanitize_text_field( wp_unslash( $_SERVER['HTTP_AUTHORIZATION'] ) ) : '';
        if ( preg_match( '/^Bearer\s+([A-Za-z0-9_\-]+)$/i', $auth_header, $m ) ) {
            $token = $m[1];
        }
    }
    if ( $token === '' ) {
        return new WP_REST_Response( ['ok'=>false,'message'=>'Token de integração ausente.'], 401 );
    }

    $int = $wpdb->get_row($wpdb->prepare("SELECT * FROM ".senderzz_int_table()." WHERE token=%s AND active=1 LIMIT 1", $token), ARRAY_A);
    if (!$int) {
        return new WP_REST_Response(['ok'=>false,'message'=>'Integração inválida ou pausada.'], 404);
    }
    $payload = $req->get_json_params(); if (!is_array($payload)) $payload = $req->get_body_params(); if (!is_array($payload)) $payload = [];
    $mapping = json_decode((string)($int['mapping_json'] ?? ''), true); if (!is_array($mapping)) $mapping = senderzz_int_default_mapping();
    $processed = senderzz_int_process_payload_for_integration($payload, $int, $mapping);
    $canonical_payload = $processed['canonical_payload'];
    $mapped = $processed['mapped'];
    $status = $processed['status'];
    $message = $processed['message'];
    $order_id = (int)$processed['order_id'];
    $wpdb->insert(senderzz_int_log_table(), ['integration_id'=>(int)$int['id'],'user_id'=>(int)$int['user_id'],'external_order_id'=>$mapped['external_order_id'],'wc_order_id'=>$order_id,'status'=>$status,'message'=>$message,'payload_json'=>wp_json_encode($canonical_payload,JSON_UNESCAPED_UNICODE),'mapped_json'=>wp_json_encode($mapped,JSON_UNESCAPED_UNICODE),'created_at'=>current_time('mysql')], ['%d','%d','%s','%d','%s','%s','%s','%s','%s']);
    $wpdb->update(senderzz_int_table(), ['last_payload_json'=>wp_json_encode($canonical_payload,JSON_UNESCAPED_UNICODE),'last_received_at'=>current_time('mysql'),'last_status'=>$status,'last_error'=>$status==='error'?$message:'','updated_at'=>current_time('mysql')], ['id'=>(int)$int['id']], ['%s','%s','%s','%s','%s'], ['%d']);
    return new WP_REST_Response(['ok'=>!in_array($status, ['error','pending'], true),'status'=>$status,'message'=>$message,'order_id'=>$order_id], in_array($status, ['error','pending'], true)?422:200);
}
add_action('rest_api_init', function(){
    register_rest_route('senderzz/v1', '/integrations/(?P<token>[a-zA-Z0-9_\-]+)', ['methods'=>'POST','callback'=>'senderzz_int_rest_receive','permission_callback'=>'__return_true']);
});