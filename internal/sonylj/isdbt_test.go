// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package sonylj

import "testing"

func TestFrequencyCode(t *testing.T) {
	// USB の記録（ドライバーの読み込み時）: 13ch で 0x14 ← 0x49、0x15 ← 0x76
	hz, err := UHFChannelHz(13)
	if err != nil {
		t.Fatal(err)
	}
	if got := FrequencyCode(hz); got != 0x7649 {
		t.Fatalf("13ch: got 0x%04x, want 0x7649", got)
	}
	hz27, _ := UHFChannelHz(27)
	if hz27 != 557142857 {
		t.Fatalf("27ch: %d", hz27)
	}
}
