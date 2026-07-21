package main

import (
	"testing"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestGammaNoMidpoint(t *testing.T) {
	cases := []struct {
		name     string
		outcomes string
		prices   string
		wantPx   float64
		wantOK   bool
	}{
		{"no second", `["Yes","No"]`, `["0.0235","0.9765"]`, 0.9765, true},
		{"no first", `["No","Yes"]`, `["0.93","0.07"]`, 0.93, true},
		{"case insensitive", `["yes","no"]`, `["0.2","0.8"]`, 0.8, true},
		{"non-binary up/down", `["Up","Down"]`, `["0.5","0.5"]`, 0, false},
		{"missing prices", `["Yes","No"]`, ``, 0, false},
		{"price out of range", `["Yes","No"]`, `["0","1"]`, 0, false},
		{"three outcomes", `["A","B","No"]`, `["0.3","0.3","0.4"]`, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			px, ok := gammaNoMidpoint(polymarket.Market{Outcomes: tc.outcomes, OutcomePrices: tc.prices})
			if ok != tc.wantOK || (ok && px != tc.wantPx) {
				t.Errorf("got (%v, %v), want (%v, %v)", px, ok, tc.wantPx, tc.wantOK)
			}
		})
	}
}
