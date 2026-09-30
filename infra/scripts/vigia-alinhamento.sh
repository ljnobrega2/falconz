#!/usr/bin/env bash
# Roda NO SERVIDOR, por cron. Verifica se o que esta em /opt/falk continua
# sendo o que o repositorio diz — e grita quando alguem edita arquivo direto
# no servidor sem passar pelo repositorio.
#
# Essa edicao direta e a origem silenciosa da divergencia: funciona na hora,
# ninguem registra, e meses depois o repositorio nao descreve mais a producao.
# Com o VPS ligado o tempo todo, esta e a unica checagem que nao depende de
# alguem lembrar de rodar.
#
# Instalar:  bash infra/scripts/vigia-alinhamento.sh --instalar
# Conferir:  bash infra/scripts/vigia-alinhamento.sh
#
# Avisa no Discord se FALK_WEBHOOK estiver definido em /opt/falk/.vigia-env;
# sem isso, so escreve no diario.
set -uo pipefail
RAIZ=${FALK_ROOT:-/opt/falk}
DIARIO=${FALK_LOG:-/var/log/falk-alinhamento.log}
[ -f "$RAIZ/.vigia-env" ] && . "$RAIZ/.vigia-env"

if [ "${1:-}" = "--instalar" ]; then
  linha="17 8 * * * bash $RAIZ/infra/scripts/vigia-alinhamento.sh >/dev/null 2>&1  # falk-alinhamento"
  ( crontab -l 2>/dev/null | grep -v 'falk-alinhamento'; echo "$linha" ) | crontab -
  echo "vigia instalado: roda todo dia as 08:17"
  exit 0
fi

cd "$RAIZ" || exit 2
git fetch -q --depth 1 origin main 2>/dev/null

# Arquivos RASTREADOS que foram mexidos no servidor sem passar pelo repositorio.
# Nao rastreado (.env, backups, node_modules) nao conta: nunca foi para o git.
mexidos=$(git status --porcelain 2>/dev/null | grep -E '^ ?M' | awk '{print $2}')
atras=$(git rev-list --count HEAD..FETCH_HEAD 2>/dev/null || echo 0)

quando=$(date '+%Y-%m-%d %H:%M')
if [ -z "$mexidos" ] && [ "$atras" = "0" ]; then
  echo "$quando  ok: servidor igual ao repositorio" >> "$DIARIO"
  exit 0
fi

recado="[FALK] servidor e repositorio divergiram"
[ -n "$mexidos" ] && recado="$recado
Editado direto no servidor, sem passar pelo repositorio:
$(echo "$mexidos" | sed 's/^/  - /')"
[ "$atras" != "0" ] && recado="$recado
O servidor esta $atras commit(s) atras do GitHub."

echo "$quando  DIVERGENCIA" >> "$DIARIO"
echo "$recado" | sed 's/^/    /' >> "$DIARIO"
echo "$recado"

if [ -n "${FALK_WEBHOOK:-}" ]; then
  payload=$(printf '%s' "$recado" | python3 -c 'import json,sys; print(json.dumps({"content": sys.stdin.read()[:1900]}))')
  curl -s -m 15 -H 'content-type: application/json' -d "$payload" "$FALK_WEBHOOK" >/dev/null 2>&1
fi
exit 1
