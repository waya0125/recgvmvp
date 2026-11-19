// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto
//
// Package ivdrm は GV-MVP/VZ（SAA7164）の unit 9（IVDRM）認証・WatchDog・TS パケット復号を実装する。
// 手順は BonDriver_GVMVPVZ_T（MIT）および recgvmvpvz 移植版に基づく。
package ivdrm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/waya0125/recgvmvp/internal/saa7164usb"
)

var ivdrmDHP = new(big.Int).SetBytes([]byte{
	0xff, 0x63, 0xab, 0x76, 0x95, 0x02, 0xc0, 0xf0, 0x02, 0xf6, 0x58, 0x4b, 0xdb, 0x5f, 0x17, 0x7e,
	0x11, 0xa5, 0xe6, 0x52, 0x85, 0x2f, 0x1e, 0xc1, 0x34, 0x8d, 0x05, 0x47, 0xd5, 0xba, 0x7d, 0x79,
	0x2d, 0xd7, 0xb4, 0x56, 0x7e, 0x75, 0x8d, 0x9d, 0xda, 0xbd, 0x19, 0x5c, 0x86, 0x74, 0xd7, 0xe4,
	0xf2, 0x96, 0xa2, 0x4d, 0x29, 0x6e, 0x50, 0x2e, 0xe3, 0x92, 0x5b, 0x20, 0x87, 0xf0, 0x8a, 0x65,
	0xf8, 0x4e, 0x0d, 0xa2, 0x0d, 0x73, 0x48, 0xed, 0x86, 0xf4, 0x0c, 0x22, 0x1f, 0x4d, 0x1e, 0x0c,
	0x20, 0x8e, 0x53, 0x06, 0x39, 0xc6, 0xa2, 0xb6, 0x83, 0x69, 0x90, 0xe1, 0x06, 0x58, 0x2a, 0x70,
	0x15, 0x0c, 0xe7, 0x7c, 0x60, 0x9e, 0xed, 0xfb, 0x68, 0x54, 0xca, 0x55, 0x8f, 0x87, 0xbc, 0xb1,
	0x50, 0x8a, 0xd9, 0x88, 0x1d, 0x17, 0x06, 0x09, 0x9d, 0x16, 0x71, 0x11, 0x86, 0x87, 0xd5, 0xc7,
})

var authPlain = [16]byte{'A', 'u', 't', 'h', 'o', 'r', 'i', 'z', 'a', 't', 'i', 'o', 'n', ' ', 'O', 'K'}

func modPow1024(base, exp []byte) [128]byte {
	b := new(big.Int).SetBytes(base)
	e := new(big.Int).SetBytes(exp)
	res := new(big.Int).Exp(b, e, ivdrmDHP)
	var out [128]byte
	res.FillBytes(out[:])
	return out
}

func aesECB(key, in []byte, encrypt bool) ([16]byte, error) {
	var out [16]byte
	blk, err := aes.NewCipher(key)
	if err != nil {
		return out, err
	}
	if encrypt {
		blk.Encrypt(out[:], in[:16])
	} else {
		blk.Decrypt(out[:], in[:16])
	}
	return out, nil
}

// CloseSession は unit 9 の指定セッションを閉じる。
func CloseSession(c *saa7164usb.Client, sid uint32) {
	var buf [9]byte
	binary.LittleEndian.PutUint32(buf[1:5], sid)
	_, _ = c.Command(saa7164usb.UnitCAS, saa7164usb.SetCur, 5, buf[:])
}

// RunAuth は unit 9 で Diffie-Hellman 鍵交換と相互認証を行い、16 バイトのセッション鍵を返す。
func RunAuth(c *saa7164usb.Client, sessionID uint32) ([16]byte, error) {
	var key [16]byte
	CloseSession(c, 0)
	CloseSession(c, 1)

	_, _ = c.Command(saa7164usb.UnitCAS, saa7164usb.SetCur, 0, []byte{0})
	_, _ = c.Command(saa7164usb.UnitCAS, saa7164usb.GetCur, 1, make([]byte, 4))

	var openBuf [9]byte
	binary.LittleEndian.PutUint32(openBuf[1:5], sessionID)
	resp, err := c.Command(saa7164usb.UnitCAS, saa7164usb.SetCur, 2, openBuf[:])
	if err != nil || len(resp) < 9 || binary.LittleEndian.Uint32(resp[5:9]) != 0 {
		return key, fmt.Errorf("IVDRM open session %d failed: %w", sessionID, err)
	}

	var expA [16]byte
	if _, err := rand.Read(expA[:]); err != nil {
		return key, err
	}
	expA[0] |= 0x01
	pubA := modPow1024([]byte{2}, expA[:])

	var msg [0x10c]byte
	binary.LittleEndian.PutUint32(msg[0:4], 0x80)
	copy(msg[4:132], pubA[:])
	binary.LittleEndian.PutUint32(msg[0x104:0x108], sessionID)

	resp, err = c.Command(saa7164usb.UnitCAS, saa7164usb.SetCur, 3, msg[:])
	if err != nil || len(resp) < 0x10c || binary.LittleEndian.Uint32(resp[0x108:0x10c]) != 0 {
		return key, fmt.Errorf("IVDRM DH send failed: %w", err)
	}
	resp, err = c.Command(saa7164usb.UnitCAS, saa7164usb.GetCur, 3, msg[:])
	if err != nil || len(resp) < 0x10c || binary.LittleEndian.Uint32(resp[0x108:0x10c]) != 0 {
		return key, fmt.Errorf("IVDRM DH recv failed: %w", err)
	}
	var pubB [128]byte
	copy(pubB[:], resp[4:132])

	sharedS := modPow1024(pubB[:], expA[:])
	copy(key[:], sharedS[:16])

	authEnc, err := aesECB(pubB[:16], authPlain[:], true)
	if err != nil {
		return key, err
	}

	clear(msg[:])
	binary.LittleEndian.PutUint32(msg[0:4], 0x10)
	copy(msg[4:20], authEnc[:])
	binary.LittleEndian.PutUint32(msg[0x104:0x108], sessionID)

	resp, err = c.Command(saa7164usb.UnitCAS, saa7164usb.SetCur, 4, msg[:])
	if err != nil || len(resp) < 0x10c || binary.LittleEndian.Uint32(resp[0x108:0x10c]) != 0 {
		return key, fmt.Errorf("IVDRM auth send failed: %w", err)
	}
	resp, err = c.Command(saa7164usb.UnitCAS, saa7164usb.GetCur, 4, msg[:])
	if err != nil || len(resp) < 0x10c || binary.LittleEndian.Uint32(resp[0x108:0x10c]) != 0 {
		return key, fmt.Errorf("IVDRM auth recv failed: %w", err)
	}

	authDec, err := aesECB(pubA[:16], resp[4:20], false)
	if err != nil {
		return key, err
	}
	if authDec != authPlain {
		return key, errors.New("IVDRM 相互認証が一致しない")
	}
	return key, nil
}

// WatchDog は unit 9 sel 6/7/8 を使ってセッション維持の生存確認を行う。
type WatchDog struct {
	client     *saa7164usb.Client
	sessionKey [16]byte
	blk        cipher.Block
	counter    uint64
	stopCh     chan struct{}
	doneCh     chan struct{}
	once       sync.Once
}

// StartWatchDog は WatchDogInit と初回キックを行い、2 秒周期の更新ゴルーチンを開始する。
func StartWatchDog(c *saa7164usb.Client, sessionKey [16]byte) (*WatchDog, error) {
	blk, err := aes.NewCipher(sessionKey[:])
	if err != nil {
		return nil, err
	}
	w := &WatchDog{
		client:     c,
		sessionKey: sessionKey,
		blk:        blk,
		counter:    1,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
	var wdInit [12]byte
	binary.LittleEndian.PutUint32(wdInit[0:4], 7)
	binary.LittleEndian.PutUint64(wdInit[4:12], w.counter)
	_, _ = c.Command(saa7164usb.UnitCAS, saa7164usb.SetCur, 6, wdInit[:])
	w.kickOnce()

	go w.loop()
	return w, nil
}

func (w *WatchDog) kickOnce() {
	w.counter++
	clients := [3]struct{ id, magic uint32 }{
		{1, 0x4a73f812},
		{2, 0xf94c36dc},
		{4, 0x71c0e12d},
	}
	for _, cl := range clients {
		var plain [16]byte
		var pkt [20]byte
		binary.LittleEndian.PutUint64(plain[0:8], w.counter)
		binary.LittleEndian.PutUint32(plain[8:12], cl.id)
		binary.LittleEndian.PutUint32(plain[12:16], cl.magic)
		w.blk.Encrypt(pkt[4:20], plain[:])
		_, _ = w.client.Command(saa7164usb.UnitCAS, saa7164usb.SetCur, 7, pkt[:])
	}
}

func (w *WatchDog) loop() {
	defer close(w.doneCh)
	tk := time.NewTicker(2 * time.Second)
	defer tk.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-tk.C:
			w.kickOnce()
		}
	}
}

// Stop は WatchDog ゴルーチンを停止して終了を待つ。
func (w *WatchDog) Stop() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		close(w.stopCh)
		<-w.doneCh
	})
}
