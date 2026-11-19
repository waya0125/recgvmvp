// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package ivdrm

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

func TestAESVectors(t *testing.T) {
	// FIPS-197 Appendix C.1
	k := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	p := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	wantC := [16]byte{0x69, 0xc4, 0xe0, 0xd8, 0x6a, 0x7b, 0x04, 0x30, 0xd8, 0xcd, 0xb7, 0x80, 0x70, 0xb4, 0xc5, 0x5a}

	enc, err := aesECB(k, p, true)
	if err != nil || enc != wantC {
		t.Fatalf("ECB encrypt = %x, err=%v; want %x", enc, err, wantC)
	}
	dec, err := aesECB(k, enc[:], false)
	if err != nil || !bytes.Equal(dec[:], p) {
		t.Fatalf("ECB decrypt = %x, err=%v; want %x", dec, err, p)
	}
}

func TestTSDecryptor(t *testing.T) {
	var sessKey [16]byte
	for i := range sessKey {
		sessKey[i] = byte(i + 1)
	}
	var gotKey bool
	d, err := NewTSDecryptor(sessKey, func(tsc uint8) {
		if tsc == 2 {
			gotKey = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	// 鍵パケット構築: [IV(16) | key(16) | "FORIVFROMNXP"] を sessionKey で CBC 暗号化
	tsIV := [16]byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}
	tsKey := [16]byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f}

	var plainKeyPkt [176]byte
	copy(plainKeyPkt[0x00:0x10], tsIV[:])
	copy(plainKeyPkt[0x10:0x20], tsKey[:])
	copy(plainKeyPkt[0x20:0x2c], keyPacketMarker)

	sessBlk, _ := aes.NewCipher(sessKey[:])
	var zeroIv [16]byte
	var keyTS [188]byte
	keyTS[0] = 0x47
	keyTS[1] = 0x00
	keyTS[2] = 0x00
	keyTS[3] = 0xA0 // tsc=2, afc=0x20
	cipher.NewCBCEncrypter(sessBlk, zeroIv[:]).CryptBlocks(keyTS[4:180], plainKeyPkt[:])

	// 暗号化データパケット (PID=0x0100, tsc=3)
	var dataTS [188]byte
	dataTS[0] = 0x47
	dataTS[1] = 0x01
	dataTS[2] = 0x00
	dataTS[3] = 0xD0 // tsc=3, afc=0x10
	for i := 4; i < 188; i++ {
		dataTS[i] = byte(i)
	}
	wantPlain := append([]byte(nil), dataTS[:]...)
	wantPlain[3] &= 0x3f

	tsBlk, _ := aes.NewCipher(tsKey[:])
	cipher.NewCBCEncrypter(tsBlk, tsIV[:]).CryptBlocks(dataTS[4:180], dataTS[4:180])

	stream := append(keyTS[:], dataTS[:]...)
	out := d.Process(stream)
	if !gotKey {
		t.Fatal("onKey callback not invoked")
	}
	if !bytes.Equal(out, wantPlain) {
		t.Fatalf("decrypted TS mismatch: got %d bytes", len(out))
	}
}
