// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package ivdrm

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
)

var keyPacketMarker = []byte("FORIVFROMNXP")

// TSDecryptor は EP83 から受け取った 188 バイト TS パケットの IVDRM 暗号を復号する。
type TSDecryptor struct {
	sessBlk  cipher.Block
	tsBlk    cipher.Block
	tsIv     [16]byte
	hasTsKey bool
	carry    []byte
	onKey    func(tsc uint8)
}

// NewTSDecryptor はセッション鍵を使って TSDecryptor を作る。
func NewTSDecryptor(sessionKey [16]byte, onKey func(tsc uint8)) (*TSDecryptor, error) {
	blk, err := aes.NewCipher(sessionKey[:])
	if err != nil {
		return nil, err
	}
	return &TSDecryptor{
		sessBlk: blk,
		onKey:   onKey,
	}, nil
}

// Process は受信バイト列を 188 バイト TS パケット単位で復号し、有効な TS パケット群を返す。
func (d *TSDecryptor) Process(data []byte) []byte {
	d.carry = append(d.carry, data...)
	var out []byte
	var zeroIv [16]byte

	pos := 0
	for pos+188 <= len(d.carry) {
		if d.carry[pos] != 0x47 {
			pos++
			continue
		}
		pkt := d.carry[pos : pos+188]
		tsc := (pkt[3] >> 6) & 3
		pid := uint16(pkt[1]&0x1f)<<8 | uint16(pkt[2])
		afc := pkt[3] & 0x30

		if (tsc == 1 || tsc == 2) && pid == 0x0000 && afc == 0x20 {
			var dec [176]byte
			cipher.NewCBCDecrypter(d.sessBlk, zeroIv[:]).CryptBlocks(dec[:], pkt[4:180])
			if bytes.Equal(dec[0x20:0x2c], keyPacketMarker) {
				copy(d.tsIv[:], dec[0x00:0x10])
				if blk, err := aes.NewCipher(dec[0x10:0x20]); err == nil {
					d.tsBlk = blk
					if !d.hasTsKey && d.onKey != nil {
						d.onKey(tsc)
					}
					d.hasTsKey = true
				}
			}
		} else if tsc == 3 && d.hasTsKey {
			cipher.NewCBCDecrypter(d.tsBlk, d.tsIv[:]).CryptBlocks(pkt[4:180], pkt[4:180])
			pkt[3] &= 0x3f
			if pid != 0x1fff {
				out = append(out, pkt...)
			}
		} else if tsc == 0 && pid != 0x1fff {
			out = append(out, pkt...)
		}
		pos += 188
	}
	if pos > 0 {
		copy(d.carry, d.carry[pos:])
		d.carry = d.carry[:len(d.carry)-pos]
	}
	return out
}
