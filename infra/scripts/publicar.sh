#!/usr/bin/env bash
# Publica no VPS E registra no repositorio, num movimento so.
#
# Existe para que nao haja caminho que desalinhe. Publicar sem commitar deixa o
# servidor com codigo que ninguem mais tem; commitar sem publicar deixa o
# repositorio prometendo o que nao esta no ar. As duas coisas ja aconteceram
# aqui ao mesmo tempo, e custaram um dia para desfazer.
#
# Uso:
#   bash infra/scripts/publicar.sh admin-ui "mensagem do commit"
#   bash infra/scripts/publicar.sh falk-admin "mensagem do commit"
#   bash infra/scripts/publicar.sh --so-conferir
#
# O servidor e um clone deste repositorio: publicar e `git pull` la dentro,
# seguido da reconstrucao do servico. Nada de copiar arquivo solto por rsync —
# foi assim que a divergencia nasceu, e foi assim que uma copia local antiga
# sobrescreveu um arquivo mais novo do servidor.
set -euo pipefail

HOST=${FALK_HOST:-vps-senderzz}
CHAVE=${FALK_KEY:-$HOME/.ssh/senderzz_vps}
RAIZ=${FALK_ROOT:-/opt/falk}
COMPOSE="$RAIZ/infra/docker/docker-compose.falk.yml"

cd "$(dirname "$0")/../.."
remoto() { ssh -i "$CHAVE" -o BatchMode=yes "$HOST" "$@"; }

if [ "${1:-}" = "--so-conferir" ]; then
  bash infra/scripts/conferir-publicado.sh
  exit $?
fi

SERVICO=${1:-}
MENSAGEM=${2:-}
if [ -z "$SERVICO" ] || [ -z "$MENSAGEM" ]; then
  echo "uso: publicar.sh <servico> \"mensagem do commit\"" >&2
  echo "     servicos: admin-ui portal-ui checkout-ui falk-admin falk-portal falk-orders falk-labels falk-wallet" >&2
  exit 2
fi

echo "==> 1/4 registrando no repositorio"
if [ -n "$(git status --porcelain)" ]; then
  git add -A
  git commit -q -m "$MENSAGEM"
else
  echo "    nada novo para commitar; seguindo com o que ja esta no HEAD"
fi

echo "==> 2/4 enviando ao GitHub"
git push -q origin HEAD

echo "==> 3/4 atualizando o servidor a partir do repositorio"
remoto "cd $RAIZ && git fetch -q --depth 1 origin main && git checkout -q -f FETCH_HEAD -- . && git reset -q --mixed FETCH_HEAD"

echo "==> 4/4 reconstruindo $SERVICO"
remoto "cd $RAIZ/infra/docker && docker compose -f $COMPOSE build $SERVICO && docker compose -f $COMPOSE up -d $SERVICO"

echo "==> conferindo o alinhamento"
bash infra/scripts/conferir-publicado.sh
