// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// gvmvp-tune は試しのコマンド: 記録から取り出した初期化を再生し、地デジを選局してロックを確かめ、
// TS の出口（unit 8）を RUN にして EP 0x83 から受けたものをファイルに書く。
//
//	gvmvp-tune -replay analysis/init-02d.replay -ch 27 -sec 10 -o /tmp/x.ts
package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/waya0125/recgvmvp/internal/saa7164usb"
	"github.com/waya0125/recgvmvp/internal/sonylj"
	"github.com/waya0125/recgvmvp/internal/usbfs"
)

func main() {
	replay := flag.String("replay", "", "初期化の再生のファイル（1 行に「待ちミリ秒 塊の16進」）。空なら Go 実装の InitISDBT を使う")
	noInit := flag.Bool("noinit", false, "-replay が空のときでも Go 実装の InitISDBT を省く")
	lna := flag.Bool("lna", true, "地デジ LNA を有効にする（reg 0x1c: true=0x28 / false=0x38）")
	att := flag.Bool("att", false, "地デジ ATT を有効にする（reg 0x1c |= 0x0c）")
	ch := flag.Int("ch", 25, "地デジの物理チャンネル（13〜62）")
	sec := flag.Int("sec", 10, "受ける秒数")
	out := flag.String("o", "", "TS を書くファイル（空なら書かずに数えるだけ）")
	noRun := flag.Bool("norun", false, "選局とロックの確かめだけで、RUN にしない")
	pre := flag.String("pre", "", "選局の前に書く I2C（unit:reg=val をカンマで区切る。16進。例 5:03=00）")
	dump := flag.Bool("dump", false, "選局のあと、復調のレジスタ 0x00〜0x3f・0x80〜0xbf を読んで見せる")
	flag.Parse()
	if err := run(*replay, *noInit, *lna, *att, *ch, *sec, *out, *noRun, *dump, *pre); err != nil {
		fmt.Fprintln(os.Stderr, "gvmvp-tune:", err)
		os.Exit(1)
	}
}

func run(replay string, noInit, lna, att bool, ch, sec int, out string, noRun, dump bool, pre string) error {
	paths, err := usbfs.Find(usbfs.ID{Vendor: 0x04bb, Product: 0x052b})
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("GV-MVP/VZ（04bb:052b）が見つからない")
	}
	dev, err := usbfs.Open(paths[0])
	if err != nil {
		return err
	}
	defer dev.Close()
	for _, i := range []uint32{0, 2} {
		if err := dev.Claim(i); err != nil {
			return err
		}
	}
	c := &saa7164usb.Client{Dev: dev, Out: 0x01, In: 0x81, TimeoutMS: 2000}
	fmt.Println("機器:", dev.Path())

	// 溜まっている知らせを捨てる
	junk := make([]byte, saa7164usb.MaxMessage)
	for i := 0; i < 8; i++ {
		if _, err := dev.Transfer(0x81, junk, 50); err != nil {
			break
		}
	}

	if replay != "" {
		if err := doReplay(c, replay); err != nil {
			return err
		}
	} else if !noInit {
		if err := sonylj.InitISDBT(c, lna, att); err != nil {
			return err
		}
		fmt.Printf("地デジ復調・チューナーを初期化（LNA=%v ATT=%v）\n", lna, att)
	}

	if st, err := c.GetCardStatus(); err == nil {
		fmt.Printf("B-CAS カード状態（unit 9 sel 0x1f）: %d（1=挿入あり, 0=未挿入）\n", st)
	}

	for _, item := range strings.Split(pre, ",") {
		if item == "" {
			continue
		}
		var unit, reg, val int
		if _, err := fmt.Sscanf(item, "%x:%x=%x", &unit, &reg, &val); err != nil {
			return fmt.Errorf("-pre の %q が読めない", item)
		}
		if err := c.I2CWrite(uint8(unit), byte(reg), []byte{byte(val)}); err != nil {
			return fmt.Errorf("-pre の %q: %w", item, err)
		}
		fmt.Printf("書いた: unit %d reg 0x%02x ← 0x%02x\n", unit, reg, val)
	}

	hz, err := sonylj.UHFChannelHz(ch)
	if err != nil {
		return err
	}
	fmt.Printf("選局: %dch（%d Hz、値 0x%04x）\n", ch, hz, sonylj.FrequencyCode(hz))
	if err := sonylj.TuneISDBT(c, hz); err != nil {
		return err
	}
	locked := false
	for i := 0; i < 80 && !locked; i++ {
		time.Sleep(100 * time.Millisecond)
		var r80, rb0 byte
		locked, r80, rb0, err = sonylj.LockISDBT(c)
		if err != nil {
			return fmt.Errorf("ロックを読めない: %w", err)
		}
		fmt.Printf("  %.1f 秒: reg80=0x%02x regb0=0x%02x ロック=%v\n", float64(i+1)/10, r80, rb0, locked)
	}
	if locked {
		if cn, err := sonylj.CNRawISDBT(c); err == nil {
			fmt.Printf("  C/N 生値（reg 0x8b..0x8d）: 0x%06x (%d)\n", cn, cn)
		}
	}
	if dump {
		for _, base := range []int{0x00, 0x80} {
			for r := base; r < base+0x40; r += 16 {
				fmt.Printf("  demod %02x:", r)
				for k := 0; k < 16; k++ {
					v, err := c.I2CRead(sonylj.DemodT, byte(r+k), 1)
					if err != nil {
						fmt.Printf(" --")
						continue
					}
					fmt.Printf(" %02x", v[0])
				}
				fmt.Println()
			}
		}
	}
	if noRun {
		return nil
	}

	const port = 8
	for _, st := range []byte{saa7164usb.StateAcquire, saa7164usb.StatePause, saa7164usb.StateRun} {
		err := c.SetState(port, st)
		fmt.Printf("unit %d の状態を %d に: %v\n", port, st, errText(err))
		if err != nil && st == saa7164usb.StateRun {
			stop(c, port)
			return err
		}
	}
	defer stop(c, port)

	var f *os.File
	if out != "" {
		if f, err = os.Create(out); err != nil {
			return err
		}
		defer f.Close()
	}
	buf := make([]byte, 512*128)
	total, reads := 0, 0
	deadline := time.Now().Add(time.Duration(sec) * time.Second)
	for time.Now().Before(deadline) {
		n, err := dev.Transfer(0x83, buf, 1000)
		if errors.Is(err, syscall.ETIMEDOUT) {
			continue
		}
		if err != nil {
			return fmt.Errorf("EP 0x83 を読めない: %w", err)
		}
		if reads == 0 {
			fmt.Printf("最初の受け取り %d バイト: %s\n", n, hex.EncodeToString(buf[:min(n, 64)]))
		}
		reads++
		total += n
		if f != nil {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
		}
	}
	fmt.Printf("受けた: %d バイト（%d 回）、%.2f Mbps\n", total, reads, float64(total)*8/float64(sec)/1e6)
	return nil
}

func stop(c *saa7164usb.Client, port uint8) {
	for _, st := range []byte{saa7164usb.StateAcquire, saa7164usb.StateStop} {
		if err := c.SetState(port, st); err != nil {
			fmt.Printf("unit %d の状態を %d に: %v\n", port, st, err)
		}
	}
}

func errText(err error) string {
	if err == nil {
		return "OK"
	}
	return err.Error()
}

func doReplay(c *saa7164usb.Client, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 4096)
	lines, errs := 0, 0
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) != 2 {
			continue
		}
		wait, _ := strconv.Atoi(parts[0])
		frame, err := hex.DecodeString(parts[1])
		if err != nil || len(frame) < saa7164usb.HeaderSize {
			return fmt.Errorf("再生のファイルの %d 行目が読めない", lines+1)
		}
		if wait > 0 {
			time.Sleep(time.Duration(min(wait, 200)) * time.Millisecond)
		}
		if err := c.SendRaw(frame); err != nil {
			return fmt.Errorf("再生 %d 行目: %w", lines+1, err)
		}
		lines++
		if frame[1]&saa7164usb.CmdFlagContinue != 0 {
			continue
		}
		if _, _, err := c.ReadResponse(); err != nil {
			errs++
			fmt.Printf("再生 %d 行目（unit %d sel 0x%02x）: %v\n", lines, frame[0], frame[8], err)
		}
	}
	fmt.Printf("初期化を再生: %d 塊、エラー %d\n", lines, errs)
	return sc.Err()
}
