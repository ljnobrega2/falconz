#!/usr/bin/env bash
# Regenera a parte concatenada de docs/SENDERZZ-MASTER.md (preserva o topo manual).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
M="$ROOT/docs/SENDERZZ-MASTER.md"
MARK='<!-- CONCAT-BELOW'
# preserva tudo até (e incluindo) a linha-marcador
head -n "$(grep -n "$MARK" "$M" | head -1 | cut -d: -f1)" "$M" > "$M.tmp"
emit(){ local anchor="$1" title="$2" file="$3"
  [ -f "$ROOT/$file" ] || return 0
  { printf '\n\n---\n\n<a id="%s"></a>\n\n# 📄 %s\n\n> fonte: `%s`\n\n' "$anchor" "$title" "$file"
    cat "$ROOT/$file"; } >> "$M.tmp"; }
emit doc-auditoria "AUDITORIA-COMPLETA-2026-06-18" "AUDITORIA-COMPLETA-2026-06-18.md"
emit doc-roadmap-v2 "ROADMAP-V2"                   "ROADMAP-V2.md"
emit doc-diagnostico "DIAGNOSTICO-2026-06-17"      "DIAGNOSTICO-2026-06-17.md"
emit doc-migration  "MIGRATION-GO-CHECKLIST"       "docs/MIGRATION-GO-CHECKLIST.md"
emit doc-notas      "NOTAS-TECNICAS"               "NOTAS-TECNICAS.md"
emit doc-runbook    "RUNBOOK"                      "RUNBOOK.md"
emit doc-servidor   "SERVIDOR"                     "SERVIDOR.md"
mv "$M.tmp" "$M"
echo "master regenerado: $(wc -l < "$M") linhas"
