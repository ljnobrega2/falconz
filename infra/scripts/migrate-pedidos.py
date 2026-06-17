#!/usr/bin/env python3
"""
migrate-pedidos.py — Migra wp_sz_motoboy_pedidos do MariaDB para o Postgres.

Uso no VPS:
  python3 infra/scripts/migrate-pedidos.py

Requer: pymysql, psycopg2-binary (pip install pymysql psycopg2-binary)
Ou via docker exec se libs não estiverem disponíveis:
  python3 infra/scripts/migrate-pedidos.py --via-docker

Variáveis de ambiente (fallback nos padrões do docker-compose dev):
  MYSQL_HOST, MYSQL_PORT, MYSQL_USER, MYSQL_PASS, MYSQL_DB
  PG_HOST, PG_PORT, PG_USER, PG_PASS, PG_DB
"""

import os
import sys

MYSQL_HOST = os.getenv("MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = int(os.getenv("MYSQL_PORT", "3307"))
MYSQL_USER = os.getenv("MYSQL_USER", "root")
MYSQL_PASS = os.getenv("MYSQL_PASS", "Root051194Lp!@")
MYSQL_DB   = os.getenv("MYSQL_DB",   "u904932976_YNCRk")

PG_HOST = os.getenv("PG_HOST", "127.0.0.1")
PG_PORT = int(os.getenv("PG_PORT", "5432"))
PG_USER = os.getenv("PG_USER", "senderzz")
PG_PASS = os.getenv("PG_PASS", "senderzz")
PG_DB   = os.getenv("PG_DB",   "senderzz")

# Colunas lidas do MySQL (em ordem)
MYSQL_COLS = [
    "id", "wc_order_id", "cd_id", "zona_id", "motoboy_id", "status",
    "dest_nome", "dest_telefone", "dest_cep", "dest_endereco",
    "dest_numero", "dest_complemento", "dest_bairro", "dest_cidade", "dest_uf",
    "dest_lat", "dest_lng",
    "dest_produto", "quantidade",
    "valor_pedido", "valor_taxa", "valor_taxa_frustrado",
    "pgto_dinheiro", "pgto_pix", "pgto_cartao",
    "recebedor_cpf", "recebedor_nome", "recebedor_tipo", "recebedor_assinatura",
    "baixa_por", "baixa_admin_user_id", "baixa_motoboy_id", "baixa_at",
    "entrega_foto", "comprovantes_count",
    "entrega_lat", "entrega_lng",
    "frustrado_motivo", "frustrado_observacao", "frustrado_isento",
    "reagendado_para",
    "ts_aprovado", "ts_embalado", "ts_em_rota", "ts_a_caminho",
    "ts_entregue", "ts_frustrado",
    "created_at", "updated_at",
]

# Colunas que existem no MySQL mas podem ser ausentes (adicionadas por ALTER)
# Se ausente, o MySQL retorna erro — capturado abaixo via coluna-a-coluna
OPTIONAL_MYSQL_COLS = {
    "dest_complemento", "dest_produto", "quantidade",
    "recebedor_tipo", "baixa_por", "baixa_admin_user_id",
    "baixa_motoboy_id", "baixa_at", "comprovantes_count",
    "recebedor_nome",
}

INSERT_SQL = """
INSERT INTO sz_motoboy_pedidos (
    id, wc_order_id, cd_id, zona_id, motoboy_id, status,
    dest_nome, dest_telefone, dest_cep, dest_endereco,
    dest_numero, dest_complemento, dest_bairro, dest_cidade, dest_uf,
    dest_lat, dest_lng,
    dest_produto, quantidade,
    valor_pedido, valor_taxa, valor_taxa_frustrado,
    pgto_dinheiro, pgto_pix, pgto_cartao,
    recebedor_cpf, recebedor_nome, recebedor_tipo, recebedor_assinatura,
    baixa_por, baixa_admin_user_id, baixa_motoboy_id, baixa_at,
    entrega_foto, comprovantes_count,
    entrega_lat, entrega_lng,
    frustrado_motivo, frustrado_observacao, frustrado_isento,
    reagendado_para,
    ts_aprovado, ts_embalado, ts_em_rota, ts_a_caminho,
    ts_entregue, ts_frustrado,
    created_at, updated_at
) OVERRIDING SYSTEM VALUE
VALUES (
    %(id)s, %(wc_order_id)s, %(cd_id)s, %(zona_id)s, %(motoboy_id)s, %(status)s,
    %(dest_nome)s, %(dest_telefone)s, %(dest_cep)s, %(dest_endereco)s,
    %(dest_numero)s, %(dest_complemento)s, %(dest_bairro)s, %(dest_cidade)s, %(dest_uf)s,
    %(dest_lat)s, %(dest_lng)s,
    %(dest_produto)s, %(quantidade)s,
    %(valor_pedido)s, %(valor_taxa)s, %(valor_taxa_frustrado)s,
    %(pgto_dinheiro)s, %(pgto_pix)s, %(pgto_cartao)s,
    %(recebedor_cpf)s, %(recebedor_nome)s, %(recebedor_tipo)s, %(recebedor_assinatura)s,
    %(baixa_por)s, %(baixa_admin_user_id)s, %(baixa_motoboy_id)s, %(baixa_at)s,
    %(entrega_foto)s, %(comprovantes_count)s,
    %(entrega_lat)s, %(entrega_lng)s,
    %(frustrado_motivo)s, %(frustrado_observacao)s, %(frustrado_isento)s,
    %(reagendado_para)s,
    %(ts_aprovado)s, %(ts_embalado)s, %(ts_em_rota)s, %(ts_a_caminho)s,
    %(ts_entregue)s, %(ts_frustrado)s,
    %(created_at)s, %(updated_at)s
)
ON CONFLICT (wc_order_id) DO NOTHING
"""


def via_docker():
    """Migra via docker exec (sem libs Python no host)."""
    import subprocess
    import json

    def mysql_exec(sql):
        r = subprocess.run(
            ["docker", "exec", "senderzz-mariadb",
             "mysql", "-u", MYSQL_USER, f"-p{MYSQL_PASS}", MYSQL_DB,
             "--batch", "--silent", "--default-character-set=utf8",
             "-N", "-e", sql],
            capture_output=True, text=True,
        )
        return r.stdout.strip()

    def pg_exec(sql):
        r = subprocess.run(
            ["docker", "exec", "senderzz-postgres",
             "psql", "-U", PG_USER, "-d", PG_DB, "-c", sql],
            capture_output=True, text=True,
        )
        if r.returncode != 0:
            raise RuntimeError(r.stderr.strip())
        return r.stdout.strip()

    # Descobre colunas disponíveis no MySQL
    cols_raw = mysql_exec(
        "SELECT COLUMN_NAME FROM information_schema.COLUMNS "
        f"WHERE TABLE_SCHEMA='{MYSQL_DB}' AND TABLE_NAME='wp_sz_motoboy_pedidos' "
        "ORDER BY ORDINAL_POSITION"
    )
    available = set(cols_raw.splitlines())
    cols = [c for c in MYSQL_COLS if c in available]
    missing = [c for c in MYSQL_COLS if c not in available]
    if missing:
        print(f"[aviso] colunas ausentes no MySQL (serão NULL): {missing}")

    select_cols = ", ".join(f"`{c}`" for c in cols)
    rows_raw = mysql_exec(f"SELECT {select_cols} FROM wp_sz_motoboy_pedidos")

    ok = err = skip = 0
    for line in rows_raw.splitlines():
        vals = line.split("\t")
        row = dict(zip(cols, vals))
        # Garante todas as colunas esperadas (NULL para ausentes)
        for c in MYSQL_COLS:
            if c not in row:
                row[c] = None
            elif row[c] == "NULL" or row[c] == "":
                row[c] = None
        # frustrado_isento TINYINT(1) → boolean
        if row.get("frustrado_isento") is not None:
            row["frustrado_isento"] = row["frustrado_isento"] not in ("0", None)
        # NULL para motoboy_id=0
        for nullable_int in ("motoboy_id", "baixa_admin_user_id", "baixa_motoboy_id"):
            if row.get(nullable_int) == "0":
                row[nullable_int] = None

        pg_vals = []
        pg_cols = []
        for c in cols:
            pg_cols.append(c)
            v = row[c]
            pg_vals.append("NULL" if v is None else f"'{str(v).replace(chr(39), chr(39)*2)}'")

        # Adiciona colunas ausentes do MySQL como NULL
        for c in MYSQL_COLS:
            if c not in cols:
                pg_cols.append(c)
                pg_vals.append("NULL")

        col_str = ", ".join(pg_cols)
        val_str = ", ".join(pg_vals)
        sql = (
            f"INSERT INTO sz_motoboy_pedidos ({col_str}) "
            f"OVERRIDING SYSTEM VALUE VALUES ({val_str}) "
            f"ON CONFLICT (wc_order_id) DO NOTHING"
        )
        try:
            out = pg_exec(sql)
            if "INSERT 0 0" in out:
                skip += 1
            else:
                ok += 1
        except Exception as e:
            print(f"[erro] pedido id={row.get('id')} wc={row.get('wc_order_id')}: {e}")
            err += 1

    print(f"\nResultado: {ok} inseridos, {skip} já existiam (skip), {err} erros")


def via_libs():
    """Migra usando pymysql + psycopg2."""
    import pymysql
    import psycopg2
    import psycopg2.extras

    my = pymysql.connect(
        host=MYSQL_HOST, port=MYSQL_PORT,
        user=MYSQL_USER, password=MYSQL_PASS, database=MYSQL_DB,
        charset="utf8mb4", cursorclass=pymysql.cursors.DictCursor,
    )
    pg = psycopg2.connect(
        host=PG_HOST, port=PG_PORT,
        user=PG_USER, password=PG_PASS, dbname=PG_DB,
    )

    with my.cursor() as cur:
        # Descobre colunas disponíveis
        cur.execute(
            "SELECT COLUMN_NAME FROM information_schema.COLUMNS "
            f"WHERE TABLE_SCHEMA=%s AND TABLE_NAME='wp_sz_motoboy_pedidos' "
            "ORDER BY ORDINAL_POSITION",
            (MYSQL_DB,),
        )
        available = {r["COLUMN_NAME"] for r in cur.fetchall()}
        missing = [c for c in MYSQL_COLS if c not in available]
        if missing:
            print(f"[aviso] colunas ausentes no MySQL (serão NULL): {missing}")

        cols = [c for c in MYSQL_COLS if c in available]
        cur.execute(f"SELECT {', '.join(f'`{c}`' for c in cols)} FROM wp_sz_motoboy_pedidos")
        rows = cur.fetchall()

    ok = err = skip = 0
    with pg.cursor() as pgcur:
        for row in rows:
            # Completa colunas ausentes como None
            full = {c: row.get(c) for c in MYSQL_COLS}
            # NULL para motoboy_id/baixa_*_id = 0
            for f in ("motoboy_id", "baixa_admin_user_id", "baixa_motoboy_id"):
                if full.get(f) == 0:
                    full[f] = None
            try:
                pgcur.execute(INSERT_SQL, full)
                if pgcur.rowcount == 0:
                    skip += 1
                else:
                    ok += 1
            except Exception as e:
                pg.rollback()
                print(f"[erro] id={row.get('id')} wc={row.get('wc_order_id')}: {e}")
                err += 1
                continue
            pg.commit()

    pg.close()
    my.close()
    print(f"\nResultado: {ok} inseridos, {skip} já existiam (skip), {err} erros")


if __name__ == "__main__":
    if "--via-docker" in sys.argv:
        print("[migrate-pedidos] modo docker exec (sem libs Python)")
        via_docker()
    else:
        try:
            via_libs()
        except ImportError:
            print("[migrate-pedidos] pymysql/psycopg2 não encontrados — usando docker exec")
            via_docker()
