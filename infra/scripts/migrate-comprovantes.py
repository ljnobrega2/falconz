#!/usr/bin/env python3
"""
migrate-comprovantes.py — Migra wp_sz_motoboy_comprovantes do MariaDB para o Postgres.

Companheiro de migrate-pedidos.py. Roda DEPOIS dos pedidos (os comprovantes
referenciam pedido_id / wc_order_id já presentes em sz_motoboy_pedidos).

Uso no VPS:
  python3 infra/scripts/migrate-comprovantes.py

Uso local (sem docker, clientes mysql/psql nativos via socket/TCP):
  python3 infra/scripts/migrate-comprovantes.py --via-cli

Requer (modo padrão): pymysql, psycopg2-binary
Modo --via-cli: usa os binários `mysql` e `psql` já no PATH (nenhuma lib Python).

Variáveis de ambiente (fallback nos padrões do docker-compose dev):
  MYSQL_HOST, MYSQL_PORT, MYSQL_USER, MYSQL_PASS, MYSQL_DB
  PG_HOST, PG_PORT, PG_USER, PG_PASS, PG_DB

DIFERENÇAS-CHAVE vs migrate-pedidos.py (não copiar o ON CONFLICT de lá):
  - A tabela de comprovantes NÃO tem índice único em wc_order_id
    (idx_comprovantes_order é btree comum). Usar ON CONFLICT (wc_order_id)
    daria erro 42P10 (no unique/exclusion constraint matching).
  - Existe N:1 entre comprovantes e pedido: o mesmo wc_order_id pode ter
    vários comprovantes (ex.: order 1498 tem 2). O conflito tem que ser por
    `id` (PK), não por wc_order_id, senão perde-se linha.
  - id é `generated always as identity` no Postgres → precisa de
    OVERRIDING SYSTEM VALUE para preservar o id de origem, e bump da
    sequence ao final para os próximos inserts da app não colidirem.
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

# Colunas lidas do MySQL (em ordem) — espelha includes/motoboy/database.php:351.
MYSQL_COLS = [
    "id", "pedido_id", "wc_order_id", "motoboy_id",
    "tipo_pgto", "foto_url", "foto_path", "baixa_por", "created_at",
]

INSERT_SQL = """
INSERT INTO sz_motoboy_comprovantes (
    id, pedido_id, wc_order_id, motoboy_id,
    tipo_pgto, foto_url, foto_path, baixa_por, created_at
) OVERRIDING SYSTEM VALUE
VALUES (
    %(id)s, %(pedido_id)s, %(wc_order_id)s, %(motoboy_id)s,
    %(tipo_pgto)s, %(foto_url)s, %(foto_path)s, %(baixa_por)s, %(created_at)s
)
ON CONFLICT (id) DO NOTHING
"""

# Após inserir com id preservado, alinha a sequence de identity ao MAX(id)
# para que próximos inserts da aplicação não colidam em id já existente.
RESYNC_SEQ_SQL = """
SELECT setval(
    pg_get_serial_sequence('sz_motoboy_comprovantes', 'id'),
    GREATEST((SELECT COALESCE(MAX(id), 1) FROM sz_motoboy_comprovantes), 1)
)
"""


def via_cli():
    """Migra via binários `mysql` e `psql` (sem libs Python, sem docker)."""
    import subprocess

    def mysql_exec(sql):
        cmd = ["mysql"]
        if MYSQL_USER:
            cmd += ["-u", MYSQL_USER]
        if MYSQL_PASS:
            cmd += [f"-p{MYSQL_PASS}"]
        if MYSQL_HOST:
            cmd += ["-h", MYSQL_HOST, "-P", str(MYSQL_PORT)]
        cmd += [MYSQL_DB, "--batch", "--silent", "-N",
                "--default-character-set=utf8mb4", "-e", sql]
        r = subprocess.run(cmd, capture_output=True, text=True)
        if r.returncode != 0:
            raise RuntimeError(r.stderr.strip())
        return r.stdout.rstrip("\n")

    def pg_exec(sql, capture=True):
        dsn = f"postgresql://{PG_USER}:{PG_PASS}@{PG_HOST}:{PG_PORT}/{PG_DB}"
        r = subprocess.run(
            ["psql", dsn, "-v", "ON_ERROR_STOP=1", "-t", "-A", "-c", sql],
            capture_output=True, text=True,
        )
        if r.returncode != 0:
            raise RuntimeError(r.stderr.strip())
        return r.stdout.strip() if capture else ""

    select_cols = ", ".join(f"`{c}`" for c in MYSQL_COLS)
    rows_raw = mysql_exec(f"SELECT {select_cols} FROM wp_sz_motoboy_comprovantes ORDER BY id")

    ok = err = skip = 0
    for line in rows_raw.splitlines():
        if not line:
            continue
        vals = line.split("\t")
        row = dict(zip(MYSQL_COLS, vals))

        cols, parts = [], []
        for c in MYSQL_COLS:
            v = row.get(c)
            cols.append(c)
            if v is None or v == "NULL":
                parts.append("NULL")
            else:
                parts.append("'" + str(v).replace("'", "''") + "'")
        col_str = ", ".join(cols)
        val_str = ", ".join(parts)
        sql = (
            f"INSERT INTO sz_motoboy_comprovantes ({col_str}) "
            f"OVERRIDING SYSTEM VALUE VALUES ({val_str}) "
            f"ON CONFLICT (id) DO NOTHING"
        )
        try:
            # psql não retorna rowcount em modo -t/-A de forma simples;
            # usamos RETURNING para distinguir insert de skip.
            out = pg_exec(sql.replace("DO NOTHING", "DO NOTHING RETURNING id"))
            if out:
                ok += 1
            else:
                skip += 1
        except Exception as e:
            print(f"[erro] comprovante id={row.get('id')} wc={row.get('wc_order_id')}: {e}")
            err += 1

    try:
        pg_exec(RESYNC_SEQ_SQL)
    except Exception as e:
        print(f"[aviso] falha ao re-sincronizar sequence: {e}")

    print(f"\nResultado: {ok} inseridos, {skip} já existiam (skip), {err} erros")


def via_libs():
    """Migra usando pymysql + psycopg2."""
    import pymysql
    import psycopg2

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
        cur.execute(
            f"SELECT {', '.join(f'`{c}`' for c in MYSQL_COLS)} "
            "FROM wp_sz_motoboy_comprovantes ORDER BY id"
        )
        rows = cur.fetchall()

    ok = err = skip = 0
    with pg.cursor() as pgcur:
        for row in rows:
            full = {c: row.get(c) for c in MYSQL_COLS}
            try:
                pgcur.execute(INSERT_SQL, full)
                if pgcur.rowcount == 0:
                    skip += 1
                else:
                    ok += 1
                pg.commit()
            except Exception as e:
                pg.rollback()
                print(f"[erro] id={row.get('id')} wc={row.get('wc_order_id')}: {e}")
                err += 1

        try:
            pgcur.execute(RESYNC_SEQ_SQL)
            pg.commit()
        except Exception as e:
            pg.rollback()
            print(f"[aviso] falha ao re-sincronizar sequence: {e}")

    pg.close()
    my.close()
    print(f"\nResultado: {ok} inseridos, {skip} já existiam (skip), {err} erros")


if __name__ == "__main__":
    if "--via-cli" in sys.argv:
        print("[migrate-comprovantes] modo CLI (mysql/psql nativos)")
        via_cli()
    else:
        try:
            via_libs()
        except ImportError:
            print("[migrate-comprovantes] pymysql/psycopg2 não encontrados — usando CLI")
            via_cli()
