#!/usr/bin/env bash
# Confere se o repositorio bate com o que esta publicado no VPS.
#
# Existe porque o contrario ja aconteceu e custou caro: codigo commitado que
# nunca subiu, e arquivo editado direto no servidor que nunca voltou para o
# repositorio. Nos dois casos o repositorio deixa de dizer a verdade sobre o
# que esta no ar — e quando o servidor morre, ninguem sabe o que se perdeu.
#
# Uso: bash infra/scripts/conferir-publicado.sh
# Saida 0 = igual. Saida 1 = ha divergencia (lista os arquivos).
set -uo pipefail

HOST=${FALK_HOST:-vps-senderzz}
CHAVE=${FALK_KEY:-$HOME/.ssh/senderzz_vps}
RAIZ=${FALK_ROOT:-/opt/falk}
PASTAS="admin-ui/src portal-ui/src checkout-ui/src go infra"
# Ruido que nao deve entrar no repositorio: backups feitos no servidor e
# segredos (o .env fica no .gitignore de proposito).
IGNORAR='node_modules|/dist/|\.git/|\.bak|\.backup|codex-backup|/\.env$'
# Ferramentas que rodam NA MAQUINA de quem publica, nunca no servidor. Nao sao
# divergencia: e onde elas devem estar. Qualquer outra coisa so no repositorio
# significa codigo que nunca subiu, e isso o script precisa acusar.
LOCAIS='infra/scripts/conferir-publicado.sh|infra/scripts/deploy-admin.sh'

cd "$(dirname "$0")/../.." || exit 2
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

ssh -i "$CHAVE" -o BatchMode=yes "$HOST" \
  "cd $RAIZ && find $PASTAS -type f 2>/dev/null | grep -vE '$IGNORAR' | sort | xargs md5sum 2>/dev/null" \
  > "$tmp/servidor.raw"
find $PASTAS -type f 2>/dev/null | grep -vE "$IGNORAR" | grep -vE "$LOCAIS" | sort \
  | xargs md5 -r 2>/dev/null > "$tmp/repo.raw"

python3 - "$tmp/servidor.raw" "$tmp/repo.raw" <<'PY'
import sys

def carrega(caminho, sep):
    fora = {}
    for linha in open(caminho, encoding="utf-8", errors="replace"):
        linha = linha.rstrip("\n")
        if not linha:
            continue
        # md5sum separa com dois espacos; md5 -r do macOS, com um.
        h, _, arquivo = linha.partition(sep)
        arquivo = arquivo.strip()
        if arquivo:
            fora[arquivo] = h.strip()
    return fora

servidor = carrega(sys.argv[1], "  ")
repo = carrega(sys.argv[2], " ")

difere = sorted(f for f in servidor.keys() & repo.keys() if servidor[f] != repo[f])
so_servidor = sorted(servidor.keys() - repo.keys())
so_repo = sorted(repo.keys() - servidor.keys())

if not (difere or so_servidor or so_repo):
    print(f"Repositorio e producao estao iguais ({len(servidor)} arquivos conferidos).")
    raise SystemExit(0)

print("DIVERGENCIA entre o repositorio e o que esta publicado:\n")
for f in difere:
    print("  conteudo diferente:", f)
for f in so_servidor:
    print("  so no servidor:    ", f)
for f in so_repo:
    print("  so no repositorio: ", f)
print()
print("Servidor mais novo -> traga o arquivo para o repositorio e faca commit.")
print("Repositorio mais novo -> publique, ou desfaca se nao era para ir.")
raise SystemExit(1)
PY
