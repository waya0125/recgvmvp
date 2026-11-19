// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package channel は地デジ（UHF 13〜62）・BS（BS01_0 等）・110度CS（CS2〜CS24）のチャンネル名を周波数・TSID へ解決する。
package channel

import (
	"fmt"
	"strconv"
	"strings"
)

// System は放送方式。
type System int

const (
	ISDBT System = iota
	ISDBS
)

// Channel は選局先の情報。
type Channel struct {
	Name    string
	System  System
	UHF     int    // ISDB-T の物理チャンネル (13..62)
	FreqKHz uint32 // ISDB-S の RF 周波数 (kHz)
	TSID    uint16 // ISDB-S の TSID (0 なら未指定)
}

var bsTSIDMap = map[string]uint16{
	"BS01_0": 0x4010, // BS朝日
	"BS01_1": 0x4011, // BS-TBS
	"BS01_2": 0x4012, // BSテレ東
	"BS03_0": 0x4030, // WOWOWプライム
	"BS03_1": 0x4031, // BS-TBS / BSジャパン旧等
	"BS03_2": 0x4032, // NHK BSプレミアム等
	"BS05_0": 0x4450, // WOWOWライブ
	"BS05_1": 0x4451, // WOWOWシネマ
	"BS07_0": 0x4470, // スターチャンネル
	"BS09_0": 0x4090, // BS11
	"BS09_1": 0x4091, // スターチャンネル / BS12等
	"BS09_2": 0x4092, // BS12 トゥエルビ
	"BS11_0": 0x46b0, // FOX / BSスカパー等
	"BS11_1": 0x46b1, // 放送大学
	"BS11_2": 0x46b2, // BS釣りビジョン等
	"BS13_0": 0x40d0, // BS日テレ
	"BS13_1": 0x40d1, // BSフジ
	"BS13_2": 0x40d2, // BSアニマックス等
	"BS15_0": 0x40f1, // NHK BS1
	"BS15_1": 0x40f2, // NHK BS / BSプレミアム
	"BS17_0": 0x4310, // 地デジ難視等
	"BS19_0": 0x4730, // J SPORTS 1/2/3/4
	"BS19_1": 0x4731,
	"BS19_2": 0x4732,
	"BS19_3": 0x4733,
	"BS21_0": 0x4750, // IMAGICA / 日本映画等
	"BS21_1": 0x4751,
	"BS21_2": 0x4752,
	"BS23_0": 0x4770, // ディズニー / BS松竹等
	"BS23_1": 0x4771,
	"BS23_2": 0x4772,
}

var csTSIDMap = map[int]uint16{
	2:  0x6020,
	4:  0x7040,
	6:  0x7060,
	8:  0x7080,
	10: 0x70a0,
	12: 0x70c0,
	14: 0x70e0,
	16: 0x7100,
	18: 0x7120,
	20: 0x7140,
	22: 0x7160,
	24: 0x7180,
}

// Parse はチャンネル文字列（"25", "BS15_0", "BS1_0", "CS4" 等）を Channel に変換する。
func Parse(s string) (Channel, error) {
	u := strings.ToUpper(strings.TrimSpace(s))
	if u == "" {
		return Channel{}, fmt.Errorf("チャンネルが空")
	}
	if strings.HasPrefix(u, "BS") {
		rest := u[2:]
		var tp, slot int
		if i := strings.IndexByte(rest, '_'); i >= 0 {
			var err1, err2 error
			tp, err1 = strconv.Atoi(rest[:i])
			slot, err2 = strconv.Atoi(rest[i+1:])
			if err1 != nil || err2 != nil || slot < 0 || slot > 7 {
				return Channel{}, fmt.Errorf("BS チャンネル %q が不正", s)
			}
		} else {
			var err error
			tp, err = strconv.Atoi(rest)
			if err != nil {
				return Channel{}, fmt.Errorf("BS チャンネル %q が不正", s)
			}
		}
		if tp < 1 || tp > 23 || tp%2 == 0 {
			return Channel{}, fmt.Errorf("BS トランスポンダ番号は 1〜23 の奇数（%q）", s)
		}
		norm := fmt.Sprintf("BS%02d_%d", tp, slot)
		freq := uint32(11727480 + ((tp-1)/2)*38360)
		tsid := bsTSIDMap[norm]
		return Channel{Name: norm, System: ISDBS, FreqKHz: freq, TSID: tsid}, nil
	}
	if strings.HasPrefix(u, "CS") {
		nd, err := strconv.Atoi(u[2:])
		if err != nil || nd < 2 || nd > 24 || nd%2 != 0 {
			return Channel{}, fmt.Errorf("CS チャンネルは CS2〜CS24 の偶数（%q）", s)
		}
		freq := uint32(12291000 + ((nd-2)/2)*40000)
		return Channel{Name: fmt.Sprintf("CS%d", nd), System: ISDBS, FreqKHz: freq, TSID: csTSIDMap[nd]}, nil
	}
	n, err := strconv.Atoi(u)
	if err != nil || n < 13 || n > 62 {
		return Channel{}, fmt.Errorf("地デジは 13〜62、BS は BS01_0 等、CS は CS2〜CS24 を指定（%q）", s)
	}
	return Channel{Name: strconv.Itoa(n), System: ISDBT, UHF: n}, nil
}
