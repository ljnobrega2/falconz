package handlers

import "testing"

func TestStockAvailableForSale(t *testing.T) {
	tests := []struct {
		name     string
		onHand   int64
		reserved int64
		want     int64
	}{
		{name: "estoque Egipzya", onHand: 990, reserved: 4, want: 986},
		{name: "sem reservas", onHand: 25, reserved: 0, want: 25},
		{name: "reserva maior que saldo", onHand: 2, reserved: 3, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stockAvailableForSale(tt.onHand, tt.reserved); got != tt.want {
				t.Fatalf("stockAvailableForSale(%d, %d) = %d; esperava %d", tt.onHand, tt.reserved, got, tt.want)
			}
		})
	}
}
