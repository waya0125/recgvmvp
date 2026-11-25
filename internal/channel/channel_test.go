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

// TMCC の一覧は、recfriio issue #4 と recgvmvp issue #1 の実機ログのもの（2026-11-25）。
func TestPickTSIDBySlot(t *testing.T) {
	bs09 := []uint16{0x4090, 0x4092}
	bs23 := []uint16{0x4770, 0x4971, 0x4973}
	cases := []struct {
		ch   string
		ids  []uint16
		want uint16
		ok   bool
	}{
		{"BS09_0", bs09, 0x4090, true},
		{"BS09_1", bs09, 0x4092, true},
		{"BS09_2", bs09, 0, false},
		{"BS23_0", bs23, 0x4770, true},
		{"BS23_1", bs23, 0x4971, true},
		{"BS23_2", bs23, 0x4973, true},
		{"BS15_2", []uint16{0x40f1, 0x40f2, 0x48f3, 0xffff}, 0x48f3, true},
		{"BS23_1", []uint16{0x4770, 0xfff9, 0x4971}, 0x4971, true}, // 空きは数えない
		{"BS01_0", nil, 0, false},
	}
	for _, c := range cases {
		ch, err := Parse(c.ch)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := ch.PickTSID(c.ids)
		if got != c.want || ok != c.ok {
			t.Errorf("%s %#04x: got %#04x %v, want %#04x %v", c.ch, c.ids, got, ok, c.want, c.ok)
		}
	}
}

func TestPickTSIDExplicitAndCS(t *testing.T) {
	ch, _ := Parse("BS09_0")
	ch.TSID, ch.ExplicitTSID = 0x4092, true
	if got, ok := ch.PickTSID([]uint16{0x4090, 0x4092}); !ok || got != 0x4092 {
		t.Fatalf("explicit: got %#04x %v", got, ok)
	}
	if _, ok := ch.PickTSID([]uint16{0x4090}); ok {
		t.Fatal("explicit TSID not in list must fail")
	}

	cs, _ := Parse("CS4")
	if got, ok := cs.PickTSID([]uint16{0x7040}); !ok || got != 0x7040 {
		t.Fatalf("CS4 exact: got %#04x %v", got, ok)
	}
	if got, ok := cs.PickTSID([]uint16{0x6040}); !ok || got != 0x6040 {
		t.Fatalf("CS4 network moved: got %#04x %v", got, ok)
	}
	if _, ok := cs.PickTSID([]uint16{0x7060}); ok {
		t.Fatal("CS4 with other TS must fail")
	}
}

// 表は recisdb 1.3.2 同梱の dvbv5_channels_isdbs.conf と合わせる（issue #1）。
func TestBSTableMatchesConf(t *testing.T) {
	for name, want := range map[string]uint16{
		"BS03_1": 0x4631, "BS03_2": 0x4632, "BS09_1": 0x4092, "BS13_2": 0x46d2,
		"BS15_2": 0x48f3, "BS23_1": 0x4971, "BS23_2": 0x4973,
	} {
		ch, err := Parse(name)
		if err != nil || ch.TSID != want {
			t.Errorf("%s: TSID %#04x, want %#04x (err=%v)", name, ch.TSID, want, err)
		}
	}
}
