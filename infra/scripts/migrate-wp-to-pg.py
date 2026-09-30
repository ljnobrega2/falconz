#!/usr/bin/env python3
"""
migrate-wp-to-pg.py — Migra dados reais do WordPress/WooCommerce (MySQL/MariaDB)
para o Postgres dos serviços Go. Substitui os .load do pgloader (gramática
incompatível com pgloader 3.6.10). Pequeno volume → cópia direta, mapeada por
nome de coluna, com coerção de tipos.

AUDIT-2026-06-18. SEM WordPress: este é o backfill único do estado atual do WP.
Uso:
  /tmp/senderzz-dev/venv/bin/python infra/scripts/migrate-wp-to-pg.py
Env (com defaults):
  MYSQL_HOST/PORT/USER/PASS/DB, PG_DSN
"""
import os, sys, json
from datetime import datetime, timezone
import pymysql, psycopg2
from psycopg2.extras import execute_values, Json

MY = dict(host=os.getenv("MYSQL_HOST","127.0.0.1"), port=int(os.getenv("MYSQL_PORT","3306")),
          user=os.getenv("MYSQL_USER","sz"), password=os.getenv("MYSQL_PASS","sz"),
          database=os.getenv("MYSQL_DB","u904932976_YNCRk"), charset="utf8mb4")
PG_DSN = os.getenv("PG_DSN","dbname=senderzz user=senderzz host=127.0.0.1 password=" + os.getenv("POSTGRES_PASSWORD",""))

# (mysql_table, pg_table, {mysql_col: pg_col renames})
PAIRS = [
    ("wp_tpc_carteira","tpc_carteira",{"updated_at":"created_at"}),
    ("wp_tpc_transacoes","tpc_transacoes",{}),
    ("wp_tpc_recargas","tpc_recargas",{}),
    ("wp_tpc_webhook_events","tpc_webhook_events",{}),
    ("wp_senderzz_portal_users","senderzz_portal_users",{"name":"nome"}),
    ("wp_senderzz_portal_user_classes","senderzz_portal_user_classes",{}),
    ("wp_senderzz_checkout_links","senderzz_checkout_links",{}),
    ("wp_senderzz_onboarding_requests","senderzz_onboarding_requests",{}),
    ("wp_senderzz_portal_audit_log","senderzz_portal_audit_log",{}),
    ("wp_sz_motoboy_cds","sz_motoboy_cds",{}),
    ("wp_sz_motoboys","sz_motoboys",{}),
    ("wp_sz_motoboy_zonas","sz_motoboy_zonas",{}),
    ("wp_sz_motoboy_cep_zonas","sz_motoboy_cep_zonas",{}),
    ("wp_sz_motoboy_pedidos","sz_motoboy_pedidos",{}),
    ("wp_sz_motoboy_audit","sz_motoboy_audit",{}),
    ("wp_sz_motoboy_fechamento","sz_motoboy_fechamento",{}),
    ("wp_sz_affiliates","senderzz_affiliates",{"producer_id":"produtor_id","user_id":"afiliado_id","commission_pct":"comissao_pct"}),
    ("wp_sz_affiliate_wallet","senderzz_affiliate_wallet",{}),
    ("wp_sz_affiliate_transactions","senderzz_affiliate_transactions",{}),
    ("wp_sz_affiliate_withdrawals","senderzz_affiliate_withdrawals",{}),
    ("wp_sz_cod_withdrawals","sz_cod_withdrawals",{}),
    ("wp_sz_cod_wallet_transactions","sz_cod_wallet_transactions",{"net":"amount"}),
    # Meta dos pedidos (1:1) e endereços (renomeados). Rodam DEPOIS de migrate_orders().
    ("wp_wc_orders_meta","sz_order_meta",{}),
    ("wp_wc_order_addresses","sz_order_addresses",
       {"address_type":"tipo","first_name":"nome","address_1":"logradouro","address_2":"complemento",
        "city":"cidade","state":"uf","postcode":"cep","country":"pais","phone":"telefone"}),
]

# Status WooCommerce → status sz_orders (CHECK: pending/processing/aguardando/on-hold/
# em_separacao/embalado/enviado/entregue/completo/cancelled/frustrado/reembolsado).
WC_STATUS = {
    "wc-completo":"completo","wc-completed":"completo","wc-agendado":"aguardando",
    "wc-frustrado":"frustrado","wc-processing":"processing","wc-pending":"pending",
    "wc-cancelled":"cancelled","wc-on-hold":"on-hold","wc-refunded":"reembolsado",
    "wc-em_rota":"enviado","wc-embalado":"embalado","wc-entregue":"entregue",
}
VALID_ORDER_ST = {"pending","processing","aguardando","on-hold","em_separacao","embalado",
                  "enviado","entregue","completo","cancelled","frustrado","reembolsado"}

# Transforma valores por (pg_table, pg_col): fn(val)->val. Resolve CHECK constraints.
# Obs: role de portal_users NÃO entra aqui — é derivado de múltiplos sinais (classe de
# entrega / vínculo afiliado / operator), ver compute_roles() + override em migrate_pair.
VALUE_MAP = {
    "sz_motoboy_audit": {"actor_tipo": lambda v: v if v in ("sistema","alan","motoboy","admin") else "sistema"},
    # status do afiliado: WP 'inactive' não existe no CHECK PG (pending/active/paused/revoked)
    "senderzz_affiliates": {"status": lambda v: {"inactive": "revoked"}.get(v, v)},
    # status da transação de comissão: WP 'applied'/'available' → 'approved' (CHECK PG: pending/approved/paid/reversed/cancelled)
    "senderzz_affiliate_transactions": {"status": lambda v: {"applied": "approved", "available": "approved"}.get(v, v)},
    # tipo da carteira COD: WP 'credit' = recebimento COD (CHECK PG: cod_received/withdrawal/adjustment/refund/fee)
    "sz_cod_wallet_transactions": {"type": lambda v: {"credit": "cod_received", "debit": "withdrawal"}.get(v, v)},
}

# Preenchido em main() via compute_roles(): {portal_users.id: role_real}.
ROLE_BY_ID = {}
# Colunas NOT NULL ausentes na origem → valor constante (pg_table: {pg_col: literal}).
DEFAULTS = {
    "senderzz_affiliates":       {"produto_id": 0},
    "senderzz_affiliate_wallet": {"updated_at": "now()"},
    # WP grava created_at='0000-00-00' em alguns links (→ NULL no coerce); PG é NOT NULL.
    "senderzz_checkout_links":   {"created_at": "now()"},
}

def pg_meta(pg, table):
    """Retorna {col: (data_type, is_generated)} do Postgres, ou None se a tabela não existe."""
    cur = pg.cursor()
    cur.execute("""SELECT column_name, data_type, is_generated, is_identity
                   FROM information_schema.columns
                   WHERE table_schema='public' AND table_name=%s ORDER BY ordinal_position""", (table,))
    rows = cur.fetchall(); cur.close()
    if not rows: return None
    return {r[0]: (r[1], r[2], r[3]) for r in rows}

def coerce(val, dtype):
    if val is None: return None
    if dtype == "boolean":
        if isinstance(val, bool): return val
        if isinstance(val,(int,)): return val != 0
        s = str(val).strip().lower()
        return s in ("1","true","t","yes","y")
    if dtype in ("timestamp without time zone","timestamp with time zone","date"):
        s = str(val)
        if s.startswith("0000-00-00") or s in ("", "0"): return None
        return val
    if dtype in ("json","jsonb"):
        if val in ("", None): return None
        if isinstance(val,(dict,list)): return Json(val)
        try: return Json(json.loads(val))
        except Exception: return None  # JSON inválido → NULL (não trava migração)
    return val

def migrate_pair(my, pg, mysql_t, pg_t, renames):
    meta = pg_meta(pg, pg_t)
    if meta is None:
        return f"SKIP {pg_t} (tabela PG não existe)"
    mcur = my.cursor()
    mcur.execute(f"SELECT column_name FROM information_schema.columns WHERE table_schema=%s AND table_name=%s", (MY["database"], mysql_t))
    mcols = [r[0] for r in mcur.fetchall()]
    if not mcols:
        mcur.close(); return f"SKIP {mysql_t} (tabela MySQL não existe)"
    # mapeia mysql_col -> pg_col, ignorando geradas e colunas que não existem no PG
    mapping = []  # (mysql_col, pg_col, pg_dtype)
    overriding = False
    for mc in mcols:
        pc = renames.get(mc, mc)
        if pc in meta and meta[pc][1] != "ALWAYS":  # pula colunas GERADAS (is_generated)
            mapping.append((mc, pc, meta[pc][0]))
            if meta[pc][2] == "YES":  # is_identity → precisa OVERRIDING SYSTEM VALUE
                overriding = True
    if not mapping:
        mcur.close(); return f"SKIP {mysql_t}→{pg_t} (0 colunas em comum)"
    sel = ", ".join(f"`{m}`" for m,_,_ in mapping)
    mcur.execute(f"SELECT {sel} FROM `{mysql_t}`")
    src = mcur.fetchall(); mcur.close()
    pgcols = [p for _,p,_ in mapping]
    dtypes = [d for _,_,d in mapping]
    vmap = VALUE_MAP.get(pg_t, {})
    rows = []
    for r in src:
        row = []
        for i,(_,pc,_) in enumerate(mapping):
            v = coerce(r[i], dtypes[i])
            if pc in vmap: v = vmap[pc](v)
            row.append(v)
        rows.append(row)
    defs = DEFAULTS.get(pg_t, {})
    # default p/ NULLs em colunas mapeadas (origem tem a coluna mas valor NULL)
    for ci, pc in enumerate(pgcols):
        if pc in defs:
            dv = datetime.now(timezone.utc) if defs[pc] == "now()" else defs[pc]
            for row in rows:
                if row[ci] is None: row[ci] = dv
    # colunas NOT NULL ausentes na origem → default constante
    extra = [c for c in defs if c not in pgcols]
    for c in extra:
        val = datetime.now(timezone.utc) if defs[c] == "now()" else defs[c]
        for row in rows: row.append(val)
    allcols = pgcols + extra
    # Override do role real de portal_users (derivado, não copiado da coluna bruta)
    if pg_t == "senderzz_portal_users" and "role" in allcols and "id" in allcols:
        ri, ii = allcols.index("role"), allcols.index("id")
        for row in rows:
            row[ri] = ROLE_BY_ID.get(int(row[ii]), "produtor")
    pcur = pg.cursor()
    if rows:
        collist = ", ".join(allcols)
        ov = "OVERRIDING SYSTEM VALUE " if overriding else ""
        # ON CONFLICT DO NOTHING: WP não tinha as UNIQUE de idempotência do schema Go
        # (ex: uq_transacao_ref). Dados históricos com refs repetidas → pula o conflitante.
        execute_values(pcur, f'INSERT INTO {pg_t} ({collist}) {ov}VALUES %s ON CONFLICT DO NOTHING', rows)
    # ressincroniza sequence do id (se houver coluna id serial/identity)
    if "id" in meta:
        pcur.execute(f"SELECT pg_get_serial_sequence('{pg_t}','id')")
        seq = pcur.fetchone()[0]
        if seq:
            pcur.execute(f"SELECT setval('{seq}', COALESCE((SELECT MAX(id) FROM {pg_t}),1))")
    pg.commit(); pcur.close()
    return f"OK   {mysql_t:32}→ {pg_t:30} {len(rows):4} linhas ({len(mapping)} cols)"

def reset_all(pg):
    """Trunca todos os alvos existentes (CASCADE) num único statement, preservando
    senderzz_admin_users (login). Remove o seed demo antes do backfill real."""
    cur = pg.cursor()
    targets = []
    for _, pt, _ in PAIRS + [("","sz_orders",""),("","sz_order_meta",""),("","sz_order_addresses",""),("","sz_order_items","")]:
        cur.execute("SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=%s", (pt,))
        if cur.fetchone(): targets.append(pt)
    targets = list(dict.fromkeys(targets))
    if targets:
        cur.execute("TRUNCATE TABLE " + ", ".join(targets) + " RESTART IDENTITY CASCADE")
    pg.commit(); cur.close()
    print(f"reset: {len(targets)} tabelas truncadas")

def compute_roles(my):
    """Role real de cada portal user (definição do dono, validada contra a listagem):
      - operator = OL (role bruto 'operator', habilitado por CD pelo admin)
      - produtor = dono de produto (tem classe de entrega própria, que contém produtos)
      - afiliado = vínculo ATIVO (status='active', não deletado) a um produtor
      - cliente  = nenhum dos acima (default — NÃO produtor)
    O role bruto do WP é NÃO confiável (Gabriel Campos vem 'client' mas é produtor;
    Raniele vem 'affiliate' mas não tem vínculo → cliente). Por isso derivamos."""
    cur = my.cursor()
    cur.execute("""
        SELECT pu.id, CASE
          WHEN pu.role='operator' THEN 'operator'
          WHEN pu.shipping_class_id>0
            OR EXISTS(SELECT 1 FROM wp_senderzz_portal_user_classes c
                      WHERE c.portal_user_id=pu.id AND c.shipping_class_id>0) THEN 'produtor'
          WHEN EXISTS(SELECT 1 FROM wp_sz_affiliates a
                      WHERE a.user_id=pu.wp_user_id AND a.status='active' AND a.deleted_at IS NULL) THEN 'afiliado'
          ELSE 'cliente' END
        FROM wp_senderzz_portal_users pu""")
    d = {int(r[0]): r[1] for r in cur.fetchall()}
    cur.close()
    return d

def migrate_shipping_classes(my, pg):
    """Classes de entrega = IDENTIDADE/marca do produtor no WP (taxonomia product_shipping_class).
    Substitui o seed genérico errado (Padrão/Frágil/...). Preserva term_id como id PG."""
    meta = pg_meta(pg, "senderzz_shipping_classes")
    if meta is None:
        return "SKIP shipping_classes (PG n/e)"
    mcur = my.cursor()
    mcur.execute("""SELECT t.term_id, t.name, t.slug FROM wp_terms t
                    JOIN wp_term_taxonomy tt ON tt.term_id=t.term_id
                    WHERE tt.taxonomy='product_shipping_class' ORDER BY t.term_id""")
    src = mcur.fetchall(); mcur.close()
    pcur = pg.cursor()
    pcur.execute("TRUNCATE TABLE senderzz_shipping_classes RESTART IDENTITY CASCADE")
    ov = "OVERRIDING SYSTEM VALUE " if meta.get("id", ("", "", "NO"))[2] == "YES" else ""
    for tid, name, slug in src:
        pcur.execute(
            f"INSERT INTO senderzz_shipping_classes (id, name, slug, active) {ov}"
            f"VALUES (%s,%s,%s,TRUE) ON CONFLICT DO NOTHING", (tid, name, slug))
    pcur.execute("SELECT pg_get_serial_sequence('senderzz_shipping_classes','id')")
    seq = pcur.fetchone()[0]
    if seq:
        pcur.execute(f"SELECT setval('{seq}', COALESCE((SELECT MAX(id) FROM senderzz_shipping_classes),1))")
    pg.commit(); pcur.close()
    return f"OK   shipping_classes: {len(src)} reais ({', '.join(s[1] for s in src)})"

def migrate_produtos(my, pg):
    """Produtos WooCommerce (wp_posts type=product, status=publish) → sz_products.
    Mapeia: id=post_ID (em wp_post_id), nome=post_title, preco=_price (meta),
    descricao=post_content, status='active'.
    produtor_id = portal_users.id DONO da classe de entrega do produto (a marca/
    identidade real do produtor, via product_shipping_class → portal_user_classes/
    portal_users.shipping_class_id). Fallback: portal_user do post_author; senão 1.
    categoria = nome da classe de entrega (quando houver).

    Dimensões físicas (v469 — regra do dono "produto tem SÓ altura/largura/
    comprimento/peso"): lidas de wp_postmeta por produto:
      altura=_height, largura=_width, comprimento=_length, peso=_weight (kg).
    Metas vazias/ausentes → NULL na coluna (NÃO inventar 0). As colunas
    altura/largura/comprimento/peso são NULLable (ver schema-fixes-v469).
    Se a tabela PG ainda não tiver essas colunas (schema v469 não aplicado),
    o INSERT cai num caminho sem dimensões — degradação graciosa.

    Idempotente via uq_products_wp_post_id + ON CONFLICT DO NOTHING."""
    meta = pg_meta(pg, "sz_products")
    if meta is None:
        return "SKIP produtos (sz_products PG n/e)"
    mcur = my.cursor()
    # produtos publicados (ignora trash/draft)
    mcur.execute("""SELECT ID, post_title, post_content, post_author
                    FROM wp_posts WHERE post_type='product' AND post_status='publish'
                    ORDER BY ID""")
    prods = mcur.fetchall()
    # _price por produto
    mcur.execute("""SELECT post_id, meta_value FROM wp_postmeta WHERE meta_key='_price'""")
    price = {int(r[0]): r[1] for r in mcur.fetchall()}
    # dimensões físicas (v469): _height/_width/_length em cm, _weight em kg.
    # dims[post_id] = {'_height':v, '_width':v, '_length':v, '_weight':v}
    mcur.execute("""SELECT post_id, meta_key, meta_value FROM wp_postmeta
                    WHERE meta_key IN ('_height','_width','_length','_weight')""")
    dims = {}
    for pid_, k, v in mcur.fetchall():
        dims.setdefault(int(pid_), {})[k] = v
    # classe de entrega (term) por produto, via product_shipping_class
    mcur.execute("""SELECT tr.object_id, t.term_id, t.name
                    FROM wp_term_relationships tr
                    JOIN wp_term_taxonomy tt ON tt.term_taxonomy_id=tr.term_taxonomy_id
                       AND tt.taxonomy='product_shipping_class'
                    JOIN wp_terms t ON t.term_id=tt.term_id""")
    sclass = {}  # post_id -> (term_id, name)
    for oid, tid, name in mcur.fetchall():
        sclass[int(oid)] = (int(tid), name)
    # dono da classe: shipping_class_id -> portal_users.id.
    # PRIORIDADE: dono PRIMÁRIO = portal_users.shipping_class_id (1 produtor por classe).
    # user_classes lista QUEM ACESSA a classe (N usuários) — só fallback, e MIN(id) p/
    # ser determinístico (evita pegar um afiliado/colaborador no lugar do dono).
    owner_by_class = {}
    mcur.execute("""SELECT shipping_class_id, id FROM wp_senderzz_portal_users
                    WHERE shipping_class_id>0 ORDER BY id""")
    for cid, puid in mcur.fetchall():
        owner_by_class.setdefault(int(cid), int(puid))   # primeiro (MIN id) é o dono primário
    # fallback: só considera portal_user_id que EXISTE em portal_users (a pivot
    # user_classes tem linhas órfãs de usuários deletados — JOIN as elimina).
    mcur.execute("""SELECT c.shipping_class_id, MIN(c.portal_user_id)
                    FROM wp_senderzz_portal_user_classes c
                    JOIN wp_senderzz_portal_users pu ON pu.id=c.portal_user_id
                    WHERE c.shipping_class_id>0
                    GROUP BY c.shipping_class_id""")
    for cid, puid in mcur.fetchall():
        owner_by_class.setdefault(int(cid), int(puid))   # fallback p/ classes sem dono primário
    # wp_user (post_author) -> portal_users.id (fallback)
    mcur.execute("""SELECT wp_user_id, id FROM wp_senderzz_portal_users WHERE wp_user_id>0""")
    pu_by_wp = {int(r[0]): int(r[1]) for r in mcur.fetchall()}
    mcur.close()

    def fprice(v):
        try: return float(v) if v not in (None, "") else 0.0
        except Exception: return 0.0

    # Dimensão: vazio/ausente → None (NULL no PG), NUNCA 0 (regra: não inventar).
    def fdim(v):
        try: return float(v) if v not in (None, "") else None
        except Exception: return None

    ov = "OVERRIDING SYSTEM VALUE " if meta.get("id", ("", "", "NO"))[2] == "YES" else ""
    # Só grava dimensões se o schema v469 já criou as colunas (degradação graciosa).
    has_dims = all(c in meta for c in ("altura", "largura", "comprimento", "peso"))
    pcur = pg.cursor(); n = 0; com_dim = 0
    # auto-truncate: reset_all() NÃO inclui sz_products; trunca aqui p/ ser idempotente
    # (sem isso, ON CONFLICT DO NOTHING preservaria produtor_id de uma corrida anterior).
    pcur.execute("TRUNCATE TABLE sz_products RESTART IDENTITY CASCADE")
    for pid, title, content, author in prods:
        pid = int(pid)
        sc = sclass.get(pid)  # (term_id, name) ou None
        categoria = sc[1] if sc else None
        produtor_id = (owner_by_class.get(sc[0]) if sc else None) \
                      or pu_by_wp.get(int(author or 0)) or 1
        d = dims.get(pid, {})
        altura      = fdim(d.get("_height"))
        largura     = fdim(d.get("_width"))
        comprimento = fdim(d.get("_length"))
        peso        = fdim(d.get("_weight"))
        if any(x is not None for x in (altura, largura, comprimento, peso)):
            com_dim += 1
        if has_dims:
            pcur.execute(
                f"""INSERT INTO sz_products
                    (wp_post_id, produtor_id, nome, preco, descricao, categoria, status,
                     altura, largura, comprimento, peso)
                    VALUES (%s,%s,%s,%s,%s,%s,'active',%s,%s,%s,%s) ON CONFLICT DO NOTHING""",
                (pid, produtor_id, (title or "").strip() or f"Produto {pid}",
                 fprice(price.get(pid)), (content or None), categoria,
                 altura, largura, comprimento, peso))
        else:
            pcur.execute(
                f"""INSERT INTO sz_products
                    (wp_post_id, produtor_id, nome, preco, descricao, categoria, status)
                    VALUES (%s,%s,%s,%s,%s,%s,'active') ON CONFLICT DO NOTHING""",
                (pid, produtor_id, (title or "").strip() or f"Produto {pid}",
                 fprice(price.get(pid)), (content or None), categoria))
        n += 1
    pcur.execute("SELECT pg_get_serial_sequence('sz_products','id')")
    seq = pcur.fetchone()[0]
    if seq:
        pcur.execute(f"SELECT setval('{seq}', COALESCE((SELECT MAX(id) FROM sz_products),1))")
    pg.commit(); pcur.close()
    dim_nota = f", {com_dim} com dimensões" if has_dims else " (colunas de dimensão ausentes — rode schema-fixes-v469)"
    return f"OK   produtos: {n} publicados → sz_products{dim_nota} ({', '.join((p[1] or '').strip() for p in prods)})"

def migrate_order_items(my, pg):
    """Itens de pedido WooCommerce (wp_woocommerce_order_items type=line_item +
    itemmeta) → sz_order_items. É o que preenche a coluna 'Produto' em Comissões
    (a query lê sz_order_items.nome via t.order_id). WC order_id == sz_orders.id,
    então o vínculo casa direto. Roda DEPOIS de migrate_orders().
      order_id   = i.order_id
      produto_id = itemmeta _product_id
      nome       = i.order_item_name
      quantidade = itemmeta _qty
      preco_unit = _line_total / qty   ; subtotal = _line_total
    Só insere itens cujo order_id existe em sz_orders (evita violar FK/órfãos)."""
    meta = pg_meta(pg, "sz_order_items")
    if meta is None:
        return "SKIP order_items (sz_order_items PG n/e)"
    mcur = my.cursor()
    mcur.execute("""SELECT order_item_id, order_id, order_item_name
                    FROM wp_woocommerce_order_items
                    WHERE order_item_type='line_item' ORDER BY order_item_id""")
    items = mcur.fetchall()
    if not items:
        mcur.close(); return "SKIP order_items (0 line_items no WP)"
    ids = [int(i[0]) for i in items]
    # itemmeta relevante num único SELECT
    fmt = ",".join(["%s"] * len(ids))
    mcur.execute(f"""SELECT order_item_id, meta_key, meta_value
                     FROM wp_woocommerce_order_itemmeta
                     WHERE order_item_id IN ({fmt})
                       AND meta_key IN ('_product_id','_qty','_line_total','_line_subtotal')""", ids)
    im = {}
    for iid, k, v in mcur.fetchall():
        im.setdefault(int(iid), {})[k] = v
    mcur.close()
    # quais order_id existem em sz_orders (não inserir órfãos)
    pcur = pg.cursor()
    pcur.execute("SELECT id FROM sz_orders")
    valid = {int(r[0]) for r in pcur.fetchall()}

    def fnum(v):
        try: return float(v) if v not in (None, "") else 0.0
        except Exception: return 0.0
    def inum(v):
        try: return int(float(v)) if v not in (None, "") else 0
        except Exception: return 0

    n = 0; skipped = 0
    for iid, oid, name in items:
        oid = int(oid)
        if oid not in valid:
            skipped += 1; continue
        m = im.get(int(iid), {})
        qty = inum(m.get("_qty")) or 1
        total = fnum(m.get("_line_total")) or fnum(m.get("_line_subtotal"))
        unit = (total / qty) if qty else total
        pcur.execute(
            """INSERT INTO sz_order_items
               (order_id, produto_id, nome, quantidade, preco_unit, subtotal)
               VALUES (%s,%s,%s,%s,%s,%s) ON CONFLICT DO NOTHING""",
            (oid, inum(m.get("_product_id")), (name or "").strip() or "Item",
             qty, round(unit, 2), round(total, 2)))
        n += 1
    pcur.execute("SELECT pg_get_serial_sequence('sz_order_items','id')")
    seq = pcur.fetchone()[0]
    if seq:
        pcur.execute(f"SELECT setval('{seq}', COALESCE((SELECT MAX(id) FROM sz_order_items),1))")
    pg.commit(); pcur.close()
    tail = f", {skipped} órfãos puláveis" if skipped else ""
    return f"OK   order_items: {n} itens → sz_order_items{tail}"

def migrate_orders(my, pg):
    """Pedidos WooCommerce (wp_wc_orders + meta + endereços) → sz_orders, promovendo
    a meta financeira (comissão/taxas/afiliado/produtor/classe) para colunas."""
    def f(v):
        try: return float(v) if v not in (None, "") else 0.0
        except Exception: return 0.0
    def i(v):
        try: return int(float(v)) if v not in (None, "") else 0
        except Exception: return 0
    mcur = my.cursor()
    mcur.execute("SELECT order_id, meta_key, meta_value FROM wp_wc_orders_meta")
    meta = {}
    for oid, k, v in mcur.fetchall():
        meta.setdefault(int(oid), {})[k] = v
    mcur.execute("SELECT order_id, address_type, first_name, last_name FROM wp_wc_order_addresses")
    addr = {}
    for oid, typ, fn, ln in mcur.fetchall():
        addr.setdefault(int(oid), {})[typ] = (fn, ln)
    mcur.execute("SELECT id, status, currency, total_amount, customer_id, billing_email, date_created_gmt, payment_method FROM wp_wc_orders")
    rows = mcur.fetchall(); mcur.close()
    pcur = pg.cursor()
    pcur.execute("TRUNCATE TABLE sz_orders RESTART IDENTITY CASCADE")
    n = 0
    for oid, status, currency, total, cust, bemail, created, pay in rows:
        m = meta.get(int(oid), {})
        st = WC_STATUS.get(status, (status or "").replace("wc-", ""))
        if st not in VALID_ORDER_ST: st = "pending"
        ba = addr.get(int(oid), {}).get("billing") or addr.get(int(oid), {}).get("shipping")
        cname = (str(ba[0] or "") + " " + str(ba[1] or "")).strip() if ba else ""
        aff = m.get("_sz_affiliate_user_id") or m.get("_sz_affiliate_id")
        pcur.execute("""INSERT INTO sz_orders
            (id, wp_order_id, order_number, user_id, produtor_id, affiliate_id, status, total,
             affiliate_amount, producer_net, senderzz_fee, delivery_fee, transaction_fee,
             shipping_class_id, customer_name, billing_email, currency, payment_method, payment_status, created_at)
            OVERRIDING SYSTEM VALUE
            VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s) ON CONFLICT DO NOTHING""",
            (oid, oid, str(oid), i(cust) or i(m.get("_senderzz_customer_id")), i(m.get("_sz_aff_producer_id")),
             (i(aff) or None), st, f(total),
             f(m.get("_sz_aff_commission")), f(m.get("_sz_aff_net")), f(m.get("_senderzz_service_fee")),
             f(m.get("_sz_taxa_entrega")), f(m.get("_sz_aff_transaction_fee")),
             i(m.get("_senderzz_product_shipping_class_id")), cname,
             (bemail or m.get("_senderzz_customer_email") or ""), (currency or "BRL"),
             (pay or ""), ("paid" if st in ("completo", "entregue") else "pending"), created))
        n += 1
    pcur.execute("SELECT pg_get_serial_sequence('sz_orders','id')")
    seq = pcur.fetchone()[0]
    if seq:
        pcur.execute(f"SELECT setval('{seq}', COALESCE((SELECT MAX(id) FROM sz_orders),1))")
    pg.commit(); pcur.close()
    return f"OK   orders: {n} pedidos → sz_orders (comissão/taxas/afiliado/produtor promovidos)"

def main():
    global ROLE_BY_ID
    my = pymysql.connect(**MY)
    pg = psycopg2.connect(PG_DSN)
    reset_all(pg)
    ROLE_BY_ID = compute_roles(my)
    print(f"roles derivados: {len(ROLE_BY_ID)} usuários")
    print(migrate_shipping_classes(my, pg))
    print(migrate_produtos(my, pg))
    print(migrate_orders(my, pg))          # trunca sz_orders CASCADE (limpa order_items)
    # HPOS-FIX: dumps com HPOS não têm wp_woocommerce_order_items (itens vivem em
    # wp_wc_order_product_lookup). NÃO deixar o erro halt o migrador — o loop PAIRS
    # (usuários/carteira/afiliados/motoboy) é crítico p/ login funcionar.
    try:
        print(migrate_order_items(my, pg))
    except Exception as e:
        pg.rollback()
        print(f"SKIP order_items (HPOS sem wp_woocommerce_order_items): {str(e)[:90]} — seguindo p/ PAIRS")
    for mt, pt, rn in PAIRS:
        try:
            print(migrate_pair(my, pg, mt, pt, rn))
        except Exception as e:
            pg.rollback()
            print(f"ERRO {mt}→{pt}: {str(e)[:160]}")
    my.close(); pg.close()

if __name__ == "__main__":
    main()
