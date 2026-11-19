// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package saigawa

import "testing"

func TestFreqTableAndLookup(t *testing.T) {
	if len(freqTable) != 0x71 {
		t.Fatalf("freqTable len = %d, want 113", len(freqTable))
	}
	e25, err := LookupFreq(545142857)
	if err != nil {
		t.Fatalf("LookupFreq(25ch): %v", err)
	}
	if e25.freqHz != 545143000 {
		t.Fatalf("LookupFreq(25ch) = %d, want 545143000", e25.freqHz)
	}
	if _, err := LookupFreq(80_000_000); err == nil {
		t.Fatal("expected error for out-of-range frequency")
	}
}
