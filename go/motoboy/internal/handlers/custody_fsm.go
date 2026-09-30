// Package handlers — máquina de estados PURA da custódia/COD do motoboy.
//
// FEAT-GEOFENCE companion (custódia/COD): o motoboy carrega custódia física do
// pacote e, em COD, do VALOR a receber. O dinheiro é creditado/cobrado quando o
// pacote atinge um estado TERMINAL (entregue / devolvido / avariado). Se uma
// transição ilegal "pular" um estado ou se um estado terminal puder ser
// reentrado, o valor pode ser PERDIDO (terminal nunca alcançado) ou DUPLICADO
// (terminal alcançado duas vezes). Hoje essas regras vivem só em SQL inline
// (rota.go, motoboy_ops.go) e no PHP (sz_mbc_*); não havia função pura testável.
//
// Este arquivo extrai a tabela de transições legais como função PURA — uma
// MÁQUINA DE SEGURANÇA DE VALOR derivada do mapa de status do PHP
// (includes/senderzz-motoboy-custody.php) + a regra de produto de cancelamento:
//   - mapa status pedido → physical (sz_mbc_set_pedido_status / status_hook)
//   - frustrated → return_declared (sz_mbc_declare_return_by_qr)
//   - return_declared → available|damaged (sz_mbc_return_by_qr)
//   - guarda "return_not_declared": OL não confirma condição antes do motoboy
//     declarar (frustrated NÃO salta direto para available/damaged).
//
// IMPORTANTE — não é um port byte-a-byte: esta máquina é DELIBERADAMENTE MAIS
// ESTRITA que o runtime PHP. sz_mbc_validate_transition aceita 'cancelado' a
// partir de QUALQUER estado (e o status_hook aplica cancelado→cancelled sem
// olhar o estado atual), logo o PHP em runtime permite with_motoboy→cancelled.
// Aqui só reserved→cancelled é legal, encodando a regra de produto "cancelar só
// em agendado/embalado" (CLAUDE.md). Esta função CODIFICA E TESTA uma máquina
// segura; ela NÃO valida que os caminhos SQL ao vivo (rota.go/ol.go/PHP) a
// respeitam — é o gate puro plugável, ainda SEM caller em produção.
//
// NÃO muda nenhuma regra de dinheiro: só centraliza a transição para que o
// invariante "um valor → exatamente um terminal por caminho legal" seja coberto
// por teste. O caller continua dono da I/O (ler/gravar custódia).
package handlers

// CustodyState é o physical_status de sz_motoboy_stock_custody.
// Espelha os valores usados em sz_mbc_status_labels / sz_mbc_set_pedido_status.
type CustodyState string

const (
	// CustodyReserved — pacote reservado no CD (agendado/embalado). Sem custódia
	// física do motoboy ainda. Estado inicial efetivo após ensure.
	CustodyReserved CustodyState = "reserved"
	// CustodyWithMotoboy — em rota / a caminho: pacote (e valor COD) em custódia
	// física do motoboy.
	CustodyWithMotoboy CustodyState = "with_motoboy"
	// CustodyDelivered — entregue: terminal. Valor COD recebido pelo motoboy.
	CustodyDelivered CustodyState = "delivered"
	// CustodyFrustrated — tentativa frustrada: pacote permanece com o motoboy
	// (custódia física), valor NÃO recebido. Aguarda declaração de devolução.
	CustodyFrustrated CustodyState = "frustrated"
	// CustodyReturnDeclared — motoboy bipou o QR e declarou devolução; aguarda
	// conferência do OL. Pacote ainda fisicamente com o motoboy.
	CustodyReturnDeclared CustodyState = "return_declared"
	// CustodyAvailable — OL confirmou retorno vendável: terminal. Produto volta
	// ao estoque do CD.
	CustodyAvailable CustodyState = "available"
	// CustodyDamaged — OL confirmou avaria/perda: terminal. Perda operacional.
	CustodyDamaged CustodyState = "damaged"
	// CustodyCancelled — pedido cancelado antes de sair: terminal.
	CustodyCancelled CustodyState = "cancelled"
)

// transicoesLegais mapeia, para cada estado, o conjunto de destinos permitidos.
//
// FIEL ao PHP:
//   - status_hook map: reserved→with_motoboy, with_motoboy→delivered/frustrated,
//     reserved→cancelled (cancelado).
//   - sz_mbc_declare_return_by_qr: frustrated→return_declared.
//   - sz_mbc_return_by_qr: return_declared→available|damaged.
//
// Idempotência (auto-laço) é tratada à parte em PodeTransicionar — não vive
// neste mapa para deixar explícito que repetir a MESMA baixa não é um avanço.
var transicoesLegais = map[CustodyState]map[CustodyState]bool{
	CustodyReserved: {
		CustodyWithMotoboy: true, // em_rota (via QR)
		CustodyCancelled:   true, // cancelado
	},
	CustodyWithMotoboy: {
		CustodyDelivered:  true, // entregue (COD recebido)
		CustodyFrustrated: true, // frustrado (COD não recebido)
	},
	CustodyFrustrated: {
		CustodyReturnDeclared: true, // motoboy declara devolução
		// NÃO há frustrated→available/damaged: o OL só confirma condição DEPOIS
		// que o motoboy declara (sz_mbc_return_not_declared). Pular isso perderia
		// o passo de custódia e a trilha de auditoria do retorno.
	},
	CustodyReturnDeclared: {
		CustodyAvailable: true, // OL: retorno vendável
		CustodyDamaged:   true, // OL: avaria/perda
	},
	// delivered, available, damaged, cancelled são TERMINAIS: nenhum destino.
}

// estadosTerminais — onde o valor COD já foi resolvido. Reentrar = duplicar.
var estadosTerminais = map[CustodyState]bool{
	CustodyDelivered: true,
	CustodyAvailable: true,
	CustodyDamaged:   true,
	CustodyCancelled: true,
}

// EhTerminal reporta se o estado é terminal (valor COD já resolvido).
func EhTerminal(s CustodyState) bool {
	return estadosTerminais[s]
}

// PodeTransicionar reporta se a transição de->para é legal na máquina de
// custódia/COD.
//
// Regras de proteção do valor COD:
//   - Transição idempotente (de == para) em estado NÃO-terminal é permitida e
//     não-avanço (return_declared → return_declared = ok, espelha o early-return
//     idempotente de sz_mbc_declare_return_by_qr). Em estado TERMINAL, repetir
//     é BLOQUEADO (não reentra terminal → não re-credita/cobra o valor).
//   - A partir de um terminal não há saída (valor já resolvido).
//   - Qualquer par fora da tabela é ilegal (fail-closed): impede pular estados
//     e "teletransportar" valor.
func PodeTransicionar(de, para CustodyState) bool {
	// Idempotência: mesma posição. Permitida só fora de terminal (não reentra
	// terminal para não duplicar a resolução do valor).
	if de == para {
		return !estadosTerminais[de]
	}
	// Terminal não tem saída.
	if estadosTerminais[de] {
		return false
	}
	destinos, ok := transicoesLegais[de]
	if !ok {
		return false
	}
	return destinos[para]
}

// ResultadoTransicao descreve o veredito de uma tentativa de transição de
// custódia, com motivo legível (PT-BR) quando bloqueada.
type ResultadoTransicao struct {
	Permitida   bool
	Idempotente bool   // de == para em estado não-terminal (no-op seguro)
	Motivo      string // vazio quando permitida; PT-BR quando bloqueada
}

// AvaliarTransicaoCustodia devolve o veredito completo (para o caller logar /
// devolver mensagem). Mantém a mesma decisão de PodeTransicionar, adicionando o
// motivo do bloqueio.
func AvaliarTransicaoCustodia(de, para CustodyState) ResultadoTransicao {
	if de == para {
		if estadosTerminais[de] {
			return ResultadoTransicao{
				Permitida: false,
				Motivo:    "pacote já em estado terminal de custódia; não pode ser rebaixado novamente",
			}
		}
		return ResultadoTransicao{Permitida: true, Idempotente: true}
	}
	if estadosTerminais[de] {
		return ResultadoTransicao{
			Permitida: false,
			Motivo:    "pacote em estado terminal de custódia; sem novas transições",
		}
	}
	destinos, ok := transicoesLegais[de]
	if !ok || !destinos[para] {
		return ResultadoTransicao{
			Permitida: false,
			Motivo:    "transição de custódia não permitida (pularia um estado e arriscaria o valor COD)",
		}
	}
	return ResultadoTransicao{Permitida: true}
}

// PedidoStatusParaCustody mapeia o status do pedido motoboy (sz_motoboy_pedidos.
// status) para o physical_status de custódia correspondente.
//
// FIEL ao $map de sz_mbc_* (status_hook em senderzz-motoboy-custody.php):
//
//	agendado/reagendado/embalado → reserved
//	em_rota/a_caminho            → with_motoboy
//	entregue                     → delivered
//	frustrado                    → frustrated
//	cancelado                    → cancelled
//
// Retorna ok=false para status sem mapeamento de custódia (return_declared/
// available/damaged são transições de custódia, não status de pedido).
func PedidoStatusParaCustody(status string) (CustodyState, bool) {
	switch status {
	case "agendado", "reagendado", "embalado":
		return CustodyReserved, true
	case "em_rota", "a_caminho":
		return CustodyWithMotoboy, true
	case "entregue":
		return CustodyDelivered, true
	case "frustrado":
		return CustodyFrustrated, true
	case "cancelado":
		return CustodyCancelled, true
	}
	return "", false
}
