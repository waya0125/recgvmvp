// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package sonylj

import (
	"encoding/binary"
	"fmt"

	"github.com/waya0125/recgvmvp/internal/saa7164usb"
)

// InitISDBSDemod は BS・110度CS 復調器（unit 4）の初期レジスタ設定を行う（PhilMAS64.sys の InitIsdbsDemod）。
func InitISDBSDemod(c *saa7164usb.Client) error {
	regs := [][2]byte{
		{0x01, 0x90}, {0x03, 0x00}, {0x04, 0x02}, {0x06, 0x40},
		{0x07, 0x41}, {0x08, 0x00}, {0x09, 0x00}, {0x0a, 0xff},
		{0x0c, 0x59}, {0x0d, 0xf2}, {0x0e, 0xf0}, {0x0f, 0x50},
		{0x10, 0xb2}, {0x11, 0x00}, {0x12, 0x30}, {0x13, 0x80},
		{0x14, 0x00}, {0x15, 0x00}, {0x17, 0x00}, {0x1a, 0x00},
		{0x1b, 0x00}, {0x1c, 0xaa}, {0x1d, 0xaa}, {0x1e, 0x20},
		{0x1f, 0xaa}, {0x20, 0x00}, {0x38, 0x40}, {0x39, 0x10},
		{0x3b, 0x90}, {0x51, 0xb0}, {0x52, 0x89}, {0x53, 0xb3},
		{0x5a, 0x2d}, {0x5b, 0xd3}, {0x85, 0x69}, {0x87, 0x00},
		{0x8d, 0x00}, {0x8e, 0x00}, {0xa3, 0x11}, {0xa4, 0x00},
		{0xa5, 0x40}, {0xa6, 0x04},
	}
	for _, kv := range regs {
		if err := demodS(c, kv[0], kv[1]); err != nil {
			return fmt.Errorf("BS 復調初期化（0x%02x）: %w", kv[0], err)
		}
	}
	return nil
}

// InitISDBSTuner は BS・110度CS チューナーの初期レジスタ設定を行う（PhilMAS64.sys の InitIsdbsTuner）。
func InitISDBSTuner(c *saa7164usb.Client) error {
	if err := tunerS(c, 0x09, 0x03); err != nil {
		return err
	}
	if err := tunerS(c, 0x06, 0x2a, 0xe8, 0x84); err != nil {
		return err
	}
	if err := tunerS(c, 0x0a, 0x20, 0x78, 0x1e, 0x0a); err != nil {
		return err
	}
	if err := tunerS(c, 0x0f, 0x08, 0x0f); err != nil {
		return err
	}
	if err := tunerS(c, 0x18, 0x40); err != nil {
		return err
	}
	saa7164usb.Sleep(10)
	return nil
}

// WakeISDBS は地デジ側をスリープにして BS・110度CS 側を起こす（ChannelDecoder::EnableISDBS(1)）。
func WakeISDBS(c *saa7164usb.Client) error {
	for _, kv := range [][2]byte{{0x0f, 0x1c}, {0x1c, 0x33}, {0x1e, 0xaa}, {0x1f, 0xa8}} {
		if err := demod(c, kv[0], kv[1]); err != nil {
			return fmt.Errorf("地デジ復調スリープ（0x%02x）: %w", kv[0], err)
		}
	}
	for _, kv := range [][2]byte{{0x1e, 0x20}, {0x1c, 0x2a}, {0x1f, 0x88}} {
		if err := demodS(c, kv[0], kv[1]); err != nil {
			return fmt.Errorf("BS 復調起床（0x%02x）: %w", kv[0], err)
		}
	}
	if err := tunerS(c, 0x04, 0x00); err != nil {
		return fmt.Errorf("BS チューナー（0x04）: %w", err)
	}
	if err := demod(c, 0x03, 0x90); err != nil {
		return err
	}
	if err := tuner(c, 0x05, 0x00); err != nil {
		return err
	}
	if err := tunerS(c, 0x02, 0x00, 0x00); err != nil {
		return err
	}
	if err := tunerS(c, 0x19, 0x60); err != nil {
		return err
	}
	if err := tunerS(c, 0x20, 0x00); err != nil {
		return err
	}
	if err := tunerS(c, 0x0e, 0x6f); err != nil {
		return err
	}
	return demodS(c, 0x17, 0x00)
}

// SetISDBSTSID は BS・110度CS の TSID を設定する。
func SetISDBSTSID(c *saa7164usb.Client, tsid uint16) error {
	if err := demodS(c, 0x90, byte(tsid)); err != nil {
		return err
	}
	return demodS(c, 0x8f, byte(tsid>>8))
}

// TuneISDBS は BS・110度CS の周波数（kHz、例: 11727480）と TSID に合わせる（SetIsdbsFrequency）。
func TuneISDBS(c *saa7164usb.Client, freqKHz uint32, tsid uint16) error {
	const lnbKHz = 10678000
	if freqKHz <= lnbKHz {
		return fmt.Errorf("BS/CS 周波数が範囲外 (%d kHz)", freqKHz)
	}
	ifMHz := (freqKHz - lnbKHz) / 1000
	if ifMHz < 1040 || ifMHz > 2060 {
		return fmt.Errorf("BS/CS IF 周波数が範囲外 (%d MHz)", ifMHz)
	}

	if tsid != 0 {
		if err := SetISDBSTSID(c, tsid); err != nil {
			return err
		}
	}

	var band byte
	var mul uint16 = 1
	var reg5 byte
	switch {
	case ifMHz < 1075:
		band = 0x00
		mul = 2
		reg5 = 8
	case ifMHz < 1100:
		band = 0x00
	case ifMHz < 1200:
		band = 0x20
	case ifMHz < 1400:
		band = 0x40
	case ifMHz < 1500:
		band = 0x50
	case ifMHz < 1600:
		band = 0x60
	default:
		band = 0x70
	}

	pll := mul * uint16(ifMHz)
	if err := tunerS(c, 0x00, byte(pll>>8), byte(pll), band, reg5); err != nil {
		return err
	}
	if err := tunerS(c, 0x02, band|0x02); err != nil {
		return err
	}
	return demodS(c, 0x03, 0x01)
}

// LockISDBS は BS・110度CS の復調がロックしているかを返す（reg 0xc3 の bit4 == 0）。
func LockISDBS(c *saa7164usb.Client) (locked bool, rc3 byte, err error) {
	v, err := c.I2CRead(DemodS, 0xc3, 1)
	if err != nil {
		return false, 0, err
	}
	rc3 = v[0]
	return rc3&0x10 == 0, rc3, nil
}

// ReadTSIDs は現在の BS/CS トランスポンダに含まれる有効な TSID 一覧（reg 0xce、最大 8 個）を読む。
func ReadTSIDs(c *saa7164usb.Client) ([]uint16, error) {
	v, err := c.I2CRead(DemodS, 0xce, 16)
	if err != nil {
		return nil, err
	}
	var ids []uint16
	for i := 0; i+1 < len(v); i += 2 {
		id := binary.BigEndian.Uint16(v[i : i+2])
		if id != 0x0000 && id != 0xffff {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// CNRawISDBS は BS・110度CS 復調の C/N 生値（reg 0xbc..0xbd の 16 ビットビッグエンディアン）を読む。
func CNRawISDBS(c *saa7164usb.Client) (uint16, error) {
	v, err := c.I2CRead(DemodS, 0xbc, 2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(v[:2]), nil
}
