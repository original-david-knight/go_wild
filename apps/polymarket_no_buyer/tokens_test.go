package main

import (
	"testing"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestDecodeBinaryTokens_MapsYesNoByIndex(t *testing.T) {
	const yesID = "71902000000000000000000000000000000000000000000000000000000000000000000000000"
	const noID = "52114000000000000000000000000000000000000000000000000000000000000000000000000"

	cases := []struct {
		name     string
		outcomes string
	}{
		{"canonical", `["Yes","No"]`},
		{"lower", `["yes","no"]`},
		{"upper", `["YES","NO"]`},
		{"mixed", `["yEs","No"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := polymarket.Market{
				ConditionID:  "0xabc",
				Outcomes:     tc.outcomes,
				ClobTokenIDs: `["` + yesID + `","` + noID + `"]`,
			}
			tokens, reason := decodeBinaryTokens(m)
			if reason != "" {
				t.Fatalf("unexpected skip reason %q", reason)
			}
			if tokens.YesTokenID != yesID {
				t.Errorf("YesTokenID = %q, want %q", tokens.YesTokenID, yesID)
			}
			if tokens.NoTokenID != noID {
				t.Errorf("NoTokenID = %q, want %q", tokens.NoTokenID, noID)
			}
		})
	}
}

func TestDecodeBinaryTokens_HonorsArrayOrder(t *testing.T) {
	// NO appears first: the mapping must follow the index alignment, not assume a
	// fixed YES-first layout.
	m := polymarket.Market{
		ConditionID:  "0xabc",
		Outcomes:     `["No","Yes"]`,
		ClobTokenIDs: `["111","222"]`,
	}
	tokens, reason := decodeBinaryTokens(m)
	if reason != "" {
		t.Fatalf("unexpected skip reason %q", reason)
	}
	if tokens.NoTokenID != "111" || tokens.YesTokenID != "222" {
		t.Errorf("got no=%q yes=%q, want no=111 yes=222", tokens.NoTokenID, tokens.YesTokenID)
	}
}

func TestDecodeBinaryTokens_Rejections(t *testing.T) {
	cases := []struct {
		name       string
		outcomes   string
		tokenIDs   string
		wantReason skipReason
	}{
		{"empty outcomes", ``, `["1","2"]`, skipOutcomesMissing},
		{"empty token ids", `["Yes","No"]`, ``, skipTokenIDsMissing},
		{"unparseable outcomes", `["Yes",`, `["1","2"]`, skipOutcomesUnparsed},
		{"unparseable token ids", `["Yes","No"]`, `["1",`, skipTokenIDsUnparsed},
		{"one outcome", `["Yes"]`, `["1","2"]`, skipNotTwoOutcomes},
		{"three outcomes", `["Yes","No","Maybe"]`, `["1","2","3"]`, skipNotTwoOutcomes},
		{"one token id", `["Yes","No"]`, `["1"]`, skipNotTwoTokenIDs},
		{"three token ids", `["Yes","No","Maybe"]`, `["1","2","3"]`, skipNotTwoOutcomes},
		{"three token ids only", `["Yes","No"]`, `["1","2","3"]`, skipNotTwoTokenIDs},
		{"empty token id string", `["Yes","No"]`, `["1",""]`, skipTokenIDsMissing},
		{"duplicate token ids", `["Yes","No"]`, `["1","1"]`, skipDuplicateTokenIDs},
		{"duplicate outcomes", `["Yes","Yes"]`, `["1","2"]`, skipDuplicateOutcomes},
		{"non yes/no labels", `["Up","Down"]`, `["1","2"]`, skipNotYesNoLabels},
		{"one valid one not", `["Yes","Maybe"]`, `["1","2"]`, skipNotYesNoLabels},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Guard against a panic on any malformed input.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("decodeBinaryTokens panicked on %q/%q: %v", tc.outcomes, tc.tokenIDs, r)
				}
			}()
			m := polymarket.Market{
				ConditionID:  "0xabc",
				Outcomes:     tc.outcomes,
				ClobTokenIDs: tc.tokenIDs,
			}
			tokens, reason := decodeBinaryTokens(m)
			if reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReason)
			}
			if (tokens != binaryTokens{}) {
				t.Errorf("expected empty tokens on rejection, got %+v", tokens)
			}
		})
	}
}
