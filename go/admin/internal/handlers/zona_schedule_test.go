// Testes unitários dos helpers PUROS de zona_schedule.go (sem DB, sem rede).
//
// O que é genuinamente puro e crítico (a correção da feature vive aqui):
//   - parseDiasFuncionamento — CSV de DOW → []int (default seg–sáb)
//   - parseCutoffs           — JSON objeto E array → [7]string (default 21:00)
//   - dateAllowed            — predicado de elegibilidade (DOW ∈ dias + cutoff D−1)
//
// A convenção dos dias (0=domingo..6=sábado, PHP date('w')/Postgres EXTRACT(DOW)) e o
// cutoff "dia anterior" (sábado até sexta às 18:00) são exatamente o que estes testes
// fixam — os builds verdes (go build/tsc/go vet) NÃO exercitam esta aritmética.
//
// package handlers (mesmo pacote, alcança não-exportados).
package handlers

import (
	"testing"
	"time"
)

func TestParseDiasFuncionamento(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"1,2,3,4,5,6", []int{1, 2, 3, 4, 5, 6}}, // seg–sáb
		{"6", []int{6}},                          // só sábado
		{"0", []int{0}},                          // só domingo (convenção: 0=dom, não 7)
		{"", []int{1, 2, 3, 4, 5, 6}},            // vazio → default seg–sáb
		{"6,1,1,3", []int{1, 3, 6}},              // dedup + ordena
		{"7,9,x,2", []int{2}},                    // descarta inválidos (7 não existe)
		{"  2 , 4 ", []int{2, 4}},                // tolera espaços
	}
	for _, c := range cases {
		got := parseDiasFuncionamento(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("parseDiasFuncionamento(%q) = %v, quer %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("parseDiasFuncionamento(%q) = %v, quer %v", c.in, got, c.want)
			}
		}
	}
}

func TestParseCutoffs(t *testing.T) {
	// Objeto {"0":"21:00",…} — formato default gravado por wp_json_encode (database.php).
	obj := `{"0":"21:00","1":"21:00","2":"21:00","3":"21:00","4":"21:00","5":"18:00","6":"18:00"}`
	got := parseCutoffs(obj)
	if got[5] != "18:00" || got[6] != "18:00" || got[0] != "21:00" {
		t.Fatalf("parseCutoffs(objeto) = %v, esperava sex/sáb 18:00 e dom 21:00", got)
	}

	// Array ["21:00",…] — formato legado.
	arr := `["08:00","09:00","10:00","11:00","12:00","13:00","14:00"]`
	got = parseCutoffs(arr)
	if got[0] != "08:00" || got[6] != "14:00" {
		t.Fatalf("parseCutoffs(array) = %v, esperava [0]=08:00 [6]=14:00", got)
	}

	// Vazio / nulo → todos 21:00 (tolerância: zona sem regra não quebra).
	for _, raw := range []string{"", "   ", "null", "garbage"} {
		got = parseCutoffs(raw)
		for d := 0; d < 7; d++ {
			if got[d] != "21:00" {
				t.Fatalf("parseCutoffs(%q)[%d] = %q, quer 21:00", raw, d, got[d])
			}
		}
	}

	// Horário inválido dentro do objeto → cai p/ 21:00 só naquele dia.
	got = parseCutoffs(`{"3":"99:99","4":"07:30"}`)
	if got[3] != "21:00" || got[4] != "07:30" {
		t.Fatalf("parseCutoffs(inválido) = %v, esperava [3]=21:00 [4]=07:30", got)
	}
}

// TestDateAllowed exercita o predicado de elegibilidade — a função que vira o gate do
// backend e espelha o FalkDatePicker. Usa o fuso SP (tzSaoPauloAdmin) p/ now/datas, de
// forma que o teste é determinístico independente do fuso da máquina de CI.
func TestDateAllowed(t *testing.T) {
	// Zona: entrega seg–sáb (1..6), cutoff 18:00 em TODOS os dias (sábado até sexta 18:00).
	z := zoneSchedule{
		HasSchedule: true,
		Dias:        []int{1, 2, 3, 4, 5, 6},
		Cutoffs:     [7]string{"18:00", "18:00", "18:00", "18:00", "18:00", "18:00", "18:00"},
	}

	// Datas fixas: 2026-06-27 é SÁBADO; 2026-06-28 é DOMINGO (zona NÃO entrega domingo).
	sat := time.Date(2026, 6, 27, 0, 0, 0, 0, tzSaoPauloAdmin)
	sun := time.Date(2026, 6, 28, 0, 0, 0, 0, tzSaoPauloAdmin)
	if sat.Weekday() != time.Saturday || sun.Weekday() != time.Sunday {
		t.Fatalf("fixtures de data erradas: 27=%v 28=%v", sat.Weekday(), sun.Weekday())
	}

	// 1) Domingo NÃO está em dias → bloqueado, independentemente do horário.
	if ok, motivo := z.dateAllowed(sun, time.Date(2026, 6, 20, 9, 0, 0, 0, tzSaoPauloAdmin)); ok {
		t.Fatalf("domingo deveria ser bloqueado (fora dos dias), motivo=%q", motivo)
	}

	// Deadline do sábado = sexta (2026-06-26) às 18:00.
	// 2) Sábado, agora ANTES do cutoff (sexta 17:59) → permitido.
	if ok, motivo := z.dateAllowed(sat, time.Date(2026, 6, 26, 17, 59, 0, 0, tzSaoPauloAdmin)); !ok {
		t.Fatalf("sábado antes do cutoff deveria ser permitido, motivo=%q", motivo)
	}
	// 3) Sábado, agora 1 minuto APÓS o cutoff (sexta 18:01) → bloqueado.
	//    Esta é a fronteira que prova a semântica "dia anterior" + o cutoff exato.
	if ok, _ := z.dateAllowed(sat, time.Date(2026, 6, 26, 18, 1, 0, 0, tzSaoPauloAdmin)); ok {
		t.Fatalf("sábado 1min após o cutoff (sexta 18:01) deveria ser bloqueado")
	}
	// 4) Sábado, agora EXATAMENTE no cutoff (sexta 18:00) → permitido (now ≤ deadline).
	if ok, motivo := z.dateAllowed(sat, time.Date(2026, 6, 26, 18, 0, 0, 0, tzSaoPauloAdmin)); !ok {
		t.Fatalf("sábado exatamente no cutoff deveria ser permitido (≤), motivo=%q", motivo)
	}

	// 5) Fail-open: sem schedule (zona ausente) → SEMPRE permitido, qualquer data/hora.
	noSched := zoneSchedule{HasSchedule: false}
	if ok, _ := noSched.dateAllowed(sun, time.Date(2026, 6, 26, 23, 59, 0, 0, tzSaoPauloAdmin)); !ok {
		t.Fatalf("sem schedule deveria permitir tudo (fail-open)")
	}
}
