// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package sonylj は GV-MVP/VZ の中の Sony の地上・BS モジュール（SMT-LJ100/MJ110）を、
// SAA7164 の I2C の命令越しに操作する。
//
// 手順と値は、メーカーのドライバー（PhilMAS64_052B04BB.sys の ChannelDecoder::SetIsdbtFrequency・
// GetIsdbtLockState）を読んで、USB の記録（ドライバーの読み込み時の 13ch の選局）と照らし合わせたもの。
package sonylj

import (
	"fmt"

	"github.com/waya0125/recgvmvp/internal/saa7164usb"
)

// DemodS は BS・110度CS 側の復調の unit（部品の表の unit 4、devicetype 0x40）。
const DemodS = 4

// DemodT は地デジの復調の unit（部品の表の unit 5、devicetype 0x100）。
const DemodT = 5

// tunerAddr は地デジのチューナーの I2C アドレス。復調のレジスタ 0xFE 越しに書く。
const tunerAddr = 0xC0

// FrequencyCode は周波数（Hz）を、チューナーに書く 1/64 MHz 単位の値にする（ドライバーと同じ丸め方）。
func FrequencyCode(hz uint32) uint16 {
	khz := uint64(hz) / 1000
	return uint16((khz*64 + 500) / 1000)
}

// UHFChannelHz は地デジの UHF の物理チャンネル（13〜62）の中心周波数。
func UHFChannelHz(ch int) (uint32, error) {
	if ch < 13 || ch > 62 {
		return 0, fmt.Errorf("UHF のチャンネルは 13〜62（%d）", ch)
	}
	// 473 + 1/7 MHz から 6MHz おき
	return uint32(473000000 + 142857 + (ch-13)*6000000), nil
}

func demod(c *saa7164usb.Client, reg, val byte) error {
	return c.I2CWrite(DemodT, reg, []byte{val})
}

func tuner(c *saa7164usb.Client, reg, val byte) error {
	return c.I2CWrite(DemodT, 0xFE, []byte{tunerAddr, reg, val})
}

func demodS(c *saa7164usb.Client, reg, val byte) error {
	return c.I2CWrite(DemodS, reg, []byte{val})
}

func tunerS(c *saa7164usb.Client, reg byte, vals ...byte) error {
	buf := append([]byte{tunerAddr, reg}, vals...)
	return c.I2CWrite(DemodS, 0xFE, buf)
}

// InitISDBT はコールドスタート後の復調器初期化（PhilMAS64.sys の InitIsdbtDemod）と地デジ起床（WakeISDBT）を行う。
// これにより外部の .replay ファイルが無くても単体で地デジの初期化・選局ができる。
func InitISDBT(c *saa7164usb.Client, lna, att bool) error {
	// 復調をリセットして起こす（Windows のドライバーの読み込み時と同じ: GPIO のピン 0 を 0 → 約 80ms → 1、約 1 秒待つ）。
	// これが無いと、挿したばかりの機械では最初の I2C の書き込みが断られる。
	if err := c.SetGPIO(DemodT, 0, 0); err != nil {
		return fmt.Errorf("復調のリセット（GPIO 0 ← 0）: %w", err)
	}
	saa7164usb.Sleep(80)
	if err := c.SetGPIO(DemodT, 0, 1); err != nil {
		return fmt.Errorf("復調のリセット（GPIO 0 ← 1）: %w", err)
	}
	saa7164usb.Sleep(1000)

	// InitIsdbtDemod（SMT-LJ100/101）
	demodInit := [][2]byte{
		{0x01, 0x00}, {0x02, 0x00}, {0x03, 0x00}, {0x04, 0x02},
		{0x05, 0x00}, {0x06, 0x80}, {0x07, 0x00}, {0x08, 0x00},
		{0x0c, 0x00}, {0x0f, 0x34}, {0x11, 0x27}, {0x12, 0x0c},
		{0x13, 0x33}, {0x14, 0x20}, {0x15, 0x40}, {0x16, 0x00},
		{0x17, 0x70}, {0x18, 0x31}, {0x19, 0x13}, {0x1a, 0x31},
		{0x1b, 0x13}, {0x1c, 0x38}, {0x1d, 0xa8}, {0x1e, 0xaa},
		{0x1f, 0xa8}, {0x20, 0x00}, {0x21, 0xff}, {0x22, 0x80},
		{0x23, 0x4c}, {0x24, 0x4c}, {0x25, 0x00}, {0x26, 0x00},
		{0x27, 0x0c}, {0x28, 0x60}, {0x29, 0x6b}, {0x2a, 0x40},
		{0x2b, 0x40}, {0x2c, 0x5a}, {0x2d, 0x05}, {0x2e, 0xff},
		{0x2f, 0x00}, {0x30, 0x20}, {0x31, 0x0d}, {0x32, 0x21},
		{0x34, 0x0f}, {0x38, 0x00}, {0x39, 0xaa}, {0x3a, 0x2d},
		{0x3b, 0x21}, {0x3c, 0x3f}, {0x3d, 0x10}, {0x3e, 0x08},
		{0x3f, 0x0c}, {0x40, 0x0c}, {0x41, 0x00}, {0x42, 0x00},
		{0x43, 0x4f}, {0x44, 0xff}, {0x46, 0x20}, {0x47, 0x00},
		{0x48, 0x90}, {0x49, 0xe6}, {0x4a, 0x02}, {0x4b, 0x80},
		{0x4c, 0x00}, {0x4f, 0x05}, {0x50, 0x00}, {0x51, 0x68},
		{0x52, 0x20}, {0x54, 0x57}, {0x55, 0xf1}, {0x56, 0x20},
		{0x57, 0x70}, {0x5c, 0x50}, {0x5d, 0x00}, {0x5f, 0x80},
		{0x70, 0x38}, {0x71, 0x00}, {0x72, 0x00}, {0x75, 0x02},
		{0x76, 0x00}, {0x77, 0x01}, {0x7c, 0x00}, {0x7d, 0xd2},
		{0xba, 0x00}, {0xbb, 0x00}, {0xbc, 0x00}, {0xc7, 0x00},
		{0xe4, 0xf4}, {0xec, 0x00}, {0xef, 0x01},
	}
	for _, kv := range demodInit {
		if err := demod(c, kv[0], kv[1]); err != nil {
			return fmt.Errorf("地デジ復調初期化（0x%02x）: %w", kv[0], err)
		}
	}
	return WakeISDBT(c, lna, att)
}

// WakeISDBT は BS 側を眠らせて地デジ側を起こす（PhilMAS64.sys の ChannelDecoder::EnableISDBS(0) と同じ手順）。
// lna=true で LNA 有効（reg 0x1c = 0x28）、lna=false で 0x38、att=true で |0x0c。
func WakeISDBT(c *saa7164usb.Client, lna, att bool) error {
	var r1c byte = 0x28
	if !lna {
		r1c = 0x38
	}
	if att {
		r1c |= 0x0c
	}
	for _, kv := range [][2]byte{{0x0f, 0x34}, {0x1c, r1c}, {0x1e, 0xa2}, {0x1f, 0x08}} {
		if err := demod(c, kv[0], kv[1]); err != nil {
			return fmt.Errorf("地デジ復調の設定（0x%02x）: %w", kv[0], err)
		}
	}
	// BS 側の復調・チューナーをスリープにする
	for _, kv := range [][2]byte{{0x1c, 0xaa}, {0x1e, 0x20}, {0x1f, 0xaa}} {
		if err := demodS(c, kv[0], kv[1]); err != nil {
			return fmt.Errorf("BS 復調のスリープ（0x%02x）: %w", kv[0], err)
		}
	}
	if err := tunerS(c, 0x04, 0x40); err != nil {
		return fmt.Errorf("BS チューナー（0x04）: %w", err)
	}
	if err := demodS(c, 0x17, 0x01); err != nil {
		return fmt.Errorf("BS 復調（0x17）: %w", err)
	}
	if err := tunerS(c, 0x0e, 0x2f); err != nil {
		return err
	}
	if err := tunerS(c, 0x16, 0x00); err != nil {
		return err
	}
	if err := tunerS(c, 0x02, 0x0c, 0xf6); err != nil {
		return err
	}
	if err := tunerS(c, 0x19, 0x40); err != nil {
		return err
	}
	if err := tunerS(c, 0x20, 0x08); err != nil {
		return err
	}
	// InitIsdbtTuner（SMT-LJ100）
	tunerInit := [][2]byte{
		{0xff, 0xff}, {0x2c, 0x44}, {0x4d, 0x40}, {0x7f, 0x02},
		{0x9a, 0x52}, {0x48, 0x5a}, {0x76, 0x1a}, {0x6a, 0x48},
		{0x64, 0x28}, {0x66, 0xe6}, {0x35, 0x11}, {0x7e, 0x01},
		{0x0b, 0x99}, {0x0c, 0x00}, {0x10, 0x00}, {0x12, 0xca},
		{0x16, 0x90}, {0x32, 0x36}, {0xd8, 0x18}, {0x05, 0x01},
	}
	for _, kv := range tunerInit {
		if err := tuner(c, kv[0], kv[1]); err != nil {
			return fmt.Errorf("地デジチューナー初期化（0x%02x）: %w", kv[0], err)
		}
	}
	return demod(c, 0x03, 0x00)
}

// TuneISDBT は地デジの周波数に合わせる（SMT-LJ100/MJ110 の手順）。
func TuneISDBT(c *saa7164usb.Client, hz uint32) error {
	if hz < 90000000 || hz > 773000000 {
		return fmt.Errorf("周波数が範囲の外（%d Hz）", hz)
	}
	n := FrequencyCode(hz)
	steps := []struct {
		tuner    bool
		reg, val byte
	}{
		{false, 0x25, 0x00},
		{false, 0x23, 0x4d},
		{true, 0x11, 0x00},
		{true, 0x13, 0x15},
		{true, 0x14, byte(n)},
		{true, 0x15, byte(n >> 8)},
		{true, 0x11, 0x02},
		{false, 0x01, 0x40},
		{false, 0x23, 0x4c},
	}
	for _, s := range steps {
		var err error
		if s.tuner {
			err = tuner(c, s.reg, s.val)
		} else {
			err = demod(c, s.reg, s.val)
		}
		if err != nil {
			return fmt.Errorf("選局の書き込み（reg 0x%02x）: %w", s.reg, err)
		}
	}
	return nil
}

// LockISDBT は地デジの復調がロックしているかを返す（0x80 と 0xb0 を読む）。
// 0x80 は下位 6 ビットがすべて 0 のときだけロックとみなす（BonDriver_GVMVPVZ_T と同じ）。
// 以前は 0x28 だけを見ていたため、0x47 のような値でもロックと判定し、電波の無い ch でも lock=yes になっていた。
func LockISDBT(c *saa7164usb.Client) (locked bool, r80, rb0 byte, err error) {
	v, err := c.I2CRead(DemodT, 0x80, 1)
	if err != nil {
		return false, 0, 0, err
	}
	r80 = v[0]
	w, err := c.I2CRead(DemodT, 0xb0, 1)
	if err != nil {
		return false, r80, 0, err
	}
	rb0 = w[0]
	return r80&0x3f == 0 && rb0&0x0f >= 8, r80, rb0, nil
}

// CNRawISDBT は地デジ復調の C/N 用の生値（レジスタ 0x8b〜0x8d の 24 ビットビッグエンディアン）を読む。
func CNRawISDBT(c *saa7164usb.Client) (uint32, error) {
	v, err := c.I2CRead(DemodT, 0x8b, 3)
	if err != nil {
		return 0, err
	}
	return uint32(v[0])<<16 | uint32(v[1])<<8 | uint32(v[2]), nil
}

// CNApproxDB は C/N の目安値（dB）を返す（BonDriver_GVMVPVZ_T と同じ換算）。
func CNApproxDB(c *saa7164usb.Client) (float32, error) {
	raw, err := CNRawISDBT(c)
	if err != nil {
		return 0, err
	}
	if raw == 0 {
		return 25.0, nil
	}
	db := 32.0 - float32(raw)/2000.0
	if db < 5.0 {
		db = 5.0
	}
	if db > 35.0 {
		db = 35.0
	}
	return db, nil
}

// InitColdHardware はコールドブート時（demod reg 0x0f != 0x34）の GPIO リセットと初期レジスタ設定を行う。
func InitColdHardware(c *saa7164usb.Client) error {
	if err := c.SetGPIO(2, 0, 0); err != nil {
		return fmt.Errorf("GPIO 0=0: %w", err)
	}
	saa7164usb.Sleep(15)
	_ = c.SetGPIO(2, 0, 1)
	saa7164usb.Sleep(800)
	_ = c.SetGPIO(2, 0x16, 1)
	saa7164usb.Sleep(50)

	if err := InitISDBT(c, false, false); err != nil {
		return err
	}
	// チューナー初期追加レジスタ
	for _, kv := range [][2]byte{{0x00, 0x3f}, {0x02, 0x00}, {0x03, 0x98}, {0x04, 0x00}} {
		if err := tuner(c, kv[0], kv[1]); err != nil {
			return fmt.Errorf("地デジチューナー追加設定（0x%02x）: %w", kv[0], err)
		}
	}
	return nil
}
