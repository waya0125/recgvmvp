// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package channel

import "testing"

func TestParse(t *testing.T) {
	chT, err := Parse("25")
	if err != nil || chT.System != ISDBT || chT.UHF != 25 {
		t.Fatalf("Parse 25 = %+v, err=%v", chT, err)
	}

	chBS, err := Parse("BS15_0")
	if err != nil || chBS.System != ISDBS || chBS.FreqKHz != 11996000 || chBS.TSID != 0x40f1 {
		t.Fatalf("Parse BS15_0 = %+v, err=%v", chBS, err)
	}

	chBS1, err := Parse("bs1_1")
	if err != nil || chBS1.Name != "BS01_1" || chBS1.FreqKHz != 11727480 || chBS1.TSID != 0x4011 {
		t.Fatalf("Parse bs1_1 = %+v, err=%v", chBS1, err)
	}

	chCS, err := Parse("CS4")
	if err != nil || chCS.System != ISDBS || chCS.FreqKHz != 12331000 || chCS.TSID != 0x7040 {
		t.Fatalf("Parse CS4 = %+v, err=%v", chCS, err)
	}

	for _, bad := range []string{"", "12", "BS02_0", "CS3", "abc"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
