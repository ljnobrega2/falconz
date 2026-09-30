package handlers

import (
	"testing"
	"time"
)

func timeInSaoPaulo(year, month, day, hour, minute int) time.Time {
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, tzSaoPaulo)
}

func TestPermiteEntregaHoje(t *testing.T) {
	today := timeInSaoPaulo(2026, 8, 7, 0, 0)

	tests := []struct {
		name string
		z    zoneInfo
		hour int
		want bool
	}{
		{
			name: "segunda a sabado ate 21h",
			z:    zoneInfo{Dias: []int{1, 2, 3, 4, 5, 6}, Cutoffs: map[int]string{1: "21:00", 2: "21:00", 3: "21:00", 4: "21:00", 5: "21:00", 6: "21:00"}},
			hour: 20,
			want: true,
		},
		{
			name: "ate exatamente 21h",
			z:    zoneInfo{Dias: []int{1, 2, 3, 4, 5, 6}, Cutoffs: map[int]string{1: "21:00", 2: "21:00", 3: "21:00", 4: "21:00", 5: "21:00", 6: "21:00"}},
			hour: 21,
			want: true,
		},
		{
			name: "depois de 21h",
			z:    zoneInfo{Dias: []int{1, 2, 3, 4, 5, 6}, Cutoffs: map[int]string{1: "21:00", 2: "21:00", 3: "21:00", 4: "21:00", 5: "21:00", 6: "21:00"}},
			hour: 22,
			want: false,
		},
		{
			name: "fechamento diferente",
			z:    zoneInfo{Dias: []int{1, 2, 3, 4, 5, 6}, Cutoffs: map[int]string{1: "20:00", 2: "21:00", 3: "21:00", 4: "21:00", 5: "21:00", 6: "21:00"}},
			hour: 20,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := timeInSaoPaulo(2026, 8, 7, tt.hour, 0)
			if got := permiteEntregaHoje(tt.z, now, today); got != tt.want {
				t.Fatalf("permiteEntregaHoje() = %v, want %v", got, tt.want)
			}
		})
	}
}
