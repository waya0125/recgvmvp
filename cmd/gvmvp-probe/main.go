// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// gvmvp-probe は GV-MVP/VZ に「読むだけ」の命令（GET_CUR）を 1 つ送り、返事をそのまま見せる。
// 機器の設定を変える命令（SET_CUR）は送れないようにしてある。
//
//	gvmvp-probe                       # unit 0 の GET_FW_VERSION_CONTROL（版を聞く）
//	gvmvp-probe -sel 8 -size 32       # GET_FW_STATUS_CONTROL
//	gvmvp-probe -if 1 -out 0x02 -in 0x82
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"syscall"

	"github.com/waya0125/recgvmvp/internal/saa7164usb"
	"github.com/waya0125/recgvmvp/internal/usbfs"
)

func main() {
	pid := flag.Uint("pid", 0x052b, "USB Product ID (例: VZ=0x052b, HZ=0x0522)")
	boot := flag.String("boot", "", "指定したファームウェア (.bin) で DFU ブートを実行してから確認する")
	iface := flag.Uint("if", 0, "取るインターフェース")
	out := flag.Uint("out", 0x01, "命令を送るエンドポイント（bulk OUT）")
	in := flag.Uint("in", 0x81, "返事を受けるエンドポイント（interrupt IN）")
	unit := flag.Uint("unit", 0, "unit id")
	sel := flag.Uint("sel", saa7164usb.GetFWVersionControl, "controlselector")
	size := flag.Uint("size", 4, "データの長さ（GET でもこの長さの 0 を付けて送る。Linux の saa7164 と同じ）")
	seq := flag.Uint("seq", 1, "seqno")
	reads := flag.Int("reads", 3, "返事を待つ回数")
	timeout := flag.Uint("timeout", 1000, "1 回の待ち時間（ミリ秒）")
	bcas := flag.Bool("bcas", false, "unit 9 の GetCardStatus（sel 0x1f）で本体 B-CAS カードの挿入状態を調べる")
	i2c := flag.Bool("i2c", false, "unit に対して I2CRead(reg=sel, len=size) を実行する")
	flag.Parse()

	if err := run(uint16(*pid), *boot, uint32(*iface), uint8(*out), uint8(*in), uint8(*unit), uint16(*sel), uint16(*size), uint8(*seq), *reads, uint32(*timeout), *bcas, *i2c); err != nil {
		fmt.Fprintln(os.Stderr, "gvmvp-probe:", err)
		os.Exit(1)
	}
}

func run(pid uint16, boot string, iface uint32, out, in, unit uint8, sel, size uint16, seq uint8, reads int, timeout uint32, bcas, i2c bool) error {
	id := usbfs.ID{Vendor: 0x04bb, Product: pid}
	if boot != "" {
		if err := saa7164usb.BootFirmware(id, boot, func(f string, a ...any) {
			fmt.Printf(f+"\n", a...)
		}); err != nil {
			return fmt.Errorf("BootFirmware: %w", err)
		}
	}
	paths, err := usbfs.Find(id)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("機器（%s）が見つからない", id)
	}
	dev, err := usbfs.Open(paths[0])
	if err != nil {
		return err
	}
	defer dev.Close()
	if err := dev.Claim(iface); err != nil {
		return err
	}
	fmt.Printf("機器: %s、インターフェース %d、OUT 0x%02x、IN 0x%02x\n", dev.Path(), iface, out, in)

	// 前に溜まっている知らせを捨てる
	buf := make([]byte, saa7164usb.MaxMessage)
	for i := 0; i < 4; i++ {
		n, err := dev.Transfer(in, buf, 100)
		if err != nil {
			break
		}
		fmt.Printf("送る前に溜まっていた知らせ: %s\n", hex.EncodeToString(buf[:n]))
	}

	if bcas {
		c := &saa7164usb.Client{Dev: dev, Out: out, In: in, TimeoutMS: timeout}
		st, err := c.GetCardStatus()
		if err != nil {
			return fmt.Errorf("GetCardStatus: %w", err)
		}
		fmt.Printf("B-CAS カード状態（unit 9 sel 0x1f）: %d（1=挿入あり, 0=未挿入）\n", st)
		return nil
	}
	if i2c {
		c := &saa7164usb.Client{Dev: dev, Out: out, In: in, TimeoutMS: timeout}
		v, err := c.I2CRead(unit, byte(sel), int(size))
		if err != nil {
			return fmt.Errorf("I2CRead(unit=%d reg=0x%02x len=%d): %w", unit, byte(sel), size, err)
		}
		fmt.Printf("I2CRead(unit=%d reg=0x%02x len=%d): %s\n", unit, byte(sel), size, hex.EncodeToString(v))
		return nil
	}

	h := saa7164usb.Header{ID: unit, Size: size, Command: saa7164usb.GetCur, ControlSelector: sel, SeqNo: seq}
	msg, err := h.Marshal(make([]byte, size))
	if err != nil {
		return err
	}
	fmt.Printf("送る: %s\n", hex.EncodeToString(msg))
	n, err := dev.Transfer(out, msg, timeout)
	if err != nil {
		return fmt.Errorf("送れない: %w", err)
	}
	fmt.Printf("送った: %d バイト\n", n)

	for i := 0; i < reads; i++ {
		n, err := dev.Transfer(in, buf, timeout)
		if errors.Is(err, syscall.ETIMEDOUT) {
			fmt.Println("返事なし（時間切れ）")
			continue
		}
		if err != nil {
			return fmt.Errorf("受けられない: %w", err)
		}
		fmt.Printf("受けた %d バイト: %s\n", n, hex.EncodeToString(buf[:n]))
		if rh, data, err := saa7164usb.Unmarshal(buf[:n]); err == nil {
			fmt.Printf("  頭: id=%d flags=0x%02x size=%d command=0x%x sel=0x%x seq=%d データ=%s\n",
				rh.ID, rh.Flags, rh.Size, rh.Command, rh.ControlSelector, rh.SeqNo, hex.EncodeToString(data))
		}
	}
	return nil
}
