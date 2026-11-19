// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// recgvmvp は I-O DATA GV-MVP/VZ（04bb:052b）単体で地デジを受けて TS を書き出す。
// 引数の形は recut100b・recpt1 と同じ（チャンネル 秒 出力先）。
//
//	recgvmvp [オプション] チャンネル 秒 出力先
//	  チャンネル: 13〜62（UHF）
//	  秒:         - なら止められるまで
//	  出力先:     - なら標準出力
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/waya0125/recgvmvp/internal/channel"
	"github.com/waya0125/recgvmvp/internal/gvmvpvz"
	"github.com/waya0125/recgvmvp/internal/ivdrm"
	"github.com/waya0125/recgvmvp/internal/sonylj"
)

const usage = `使い方: recgvmvp [オプション] チャンネル 秒 出力先
  チャンネル  13〜62（UHF）、BS01_0〜BS23_2（BS）、CS2〜CS24（110度CS）
  秒          録る長さ。- なら止められるまで
  出力先      書き出すファイル。- なら標準出力

オプション:
`

type options struct {
	b25      bool
	strip    bool
	sid      string
	device   string
	firmware string
	tsid     uint16
	check    bool
	reset    bool
	debug    bool
	ch       channel.Channel
	seconds  int // 0 なら止められるまで
	output   string
}

func parseArgs(args []string, stderr io.Writer) (*options, error) {
	o := &options{}
	var fwAlias string
	var tsidStr string
	fs := flag.NewFlagSet("recgvmvp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	fs.BoolVar(&o.b25, "b25", false, "recpt1 互換（本体内蔵 B-CAS で復号されるため何もしない）")
	fs.BoolVar(&o.strip, "strip", false, "recpt1 互換（無視）")
	fs.StringVar(&o.sid, "sid", "", "recpt1 互換（無視）")
	fs.StringVar(&o.device, "device", "", "使う GV-MVP/VZ の機器ファイル（/dev/bus/usb/BBB/DDD）。空なら空いているもの")
	fs.StringVar(&o.firmware, "firmware", "", "コールド状態の機器に送るファームウェア（NXP7164F_052B04BB.bin）")
	fs.StringVar(&fwAlias, "fw", "", "--firmware の短縮形（環境変数 GVMVPVZ_FW_PATH も可）")
	fs.StringVar(&tsidStr, "tsid", "", "BS/CS の TSID を 16 進で明示指定（例: 0x40f1）")
	fs.BoolVar(&o.check, "check", false, "選局してロック状態と C/N（目安）を表示するだけ")
	fs.BoolVar(&o.reset, "reset", false, "開く前に USB ポートをリセットする")
	fs.BoolVar(&o.debug, "debug", false, "詳しいログを stderr に出す（GVMVPVZ_DEBUG=1 も可）")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if o.firmware == "" && fwAlias != "" {
		o.firmware = fwAlias
	}
	if tsidStr != "" {
		v, err := strconv.ParseUint(tsidStr, 0, 16)
		if err != nil {
			return nil, fmt.Errorf("--tsid %q が読めない", tsidStr)
		}
		o.tsid = uint16(v)
	}
	if os.Getenv("GVMVPVZ_DEBUG") == "1" {
		o.debug = true
	}
	if fs.NArg() < 1 || (!o.check && fs.NArg() != 3) {
		fs.Usage()
		return nil, errors.New("チャンネル・秒・出力先の 3 つを渡す")
	}
	ch, err := channel.Parse(fs.Arg(0))
	if err != nil {
		return nil, err
	}
	if o.tsid != 0 {
		ch.TSID = o.tsid
	}
	o.ch = ch

	if o.check {
		o.seconds = 5
		if fs.NArg() >= 2 && fs.Arg(1) != "-" {
			if o.seconds, err = strconv.Atoi(fs.Arg(1)); err != nil || o.seconds <= 0 {
				return nil, fmt.Errorf("秒 %q が読めない", fs.Arg(1))
			}
		}
		return o, nil
	}

	if fs.Arg(1) != "-" {
		if o.seconds, err = strconv.Atoi(fs.Arg(1)); err != nil || o.seconds <= 0 {
			return nil, fmt.Errorf("秒 %q が読めない", fs.Arg(1))
		}
	}
	o.output = fs.Arg(2)
	return o, nil
}

func main() {
	o, err := parseArgs(os.Args[1:], os.Stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	if err := run(o, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(o *options, stderr io.Writer) error {
	dbg := func(format string, a ...any) {
		if o.debug {
			fmt.Fprintf(stderr, "[recgvmvp] "+format+"\n", a...)
		}
	}

	dev, err := gvmvpvz.OpenDevice(gvmvpvz.OpenOptions{
		DevicePath: o.device,
		Firmware:   o.firmware,
		Reset:      o.reset,
		NoAuth:     o.check, // --check は選局とロックを見るだけなので、IVDRM の認証をしない
		Debugf:     dbg,
	})
	if err != nil {
		return err
	}
	defer dev.Close()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	stopCh := make(chan struct{})
	go func() {
		<-sigCh
		close(stopCh)
	}()

	if o.ch.System == channel.ISDBT {
		if _, err := dev.TuneISDBT(o.ch.UHF, stopCh); err != nil {
			return err
		}
	} else {
		if _, err := dev.TuneISDBS(o.ch.FreqKHz, o.ch.TSID, stopCh); err != nil {
			return err
		}
	}

	if o.check {
		ticks := o.seconds * 2
		for i := 0; i < ticks; i++ {
			if o.ch.System == channel.ISDBT {
				lk, _, _, _ := sonylj.LockISDBT(dev.Client)
				cn, _ := sonylj.CNApproxDB(dev.Client)
				lkStr := "no"
				if lk {
					lkStr = "yes"
				}
				fmt.Printf("UHF %dch  lock=%s  C/N(approx)=%.1f dB\n", o.ch.UHF, lkStr, cn)
			} else {
				lk, rc3, _ := sonylj.LockISDBS(dev.Client)
				raw, _ := sonylj.CNRawISDBS(dev.Client)
				lkStr := "no"
				if lk {
					lkStr = "yes"
				}
				fmt.Printf("%s  lock=%s  regC3=0x%02X  cn_raw=%d\n", o.ch.Name, lkStr, rc3, raw)
			}
			select {
			case <-stopCh:
				return nil
			case <-time.After(500 * time.Millisecond):
			}
		}
		return nil
	}

	var outWriter io.Writer
	if o.output == "-" {
		outWriter = os.Stdout
	} else {
		f, err := os.Create(o.output)
		if err != nil {
			return err
		}
		defer f.Close()
		outWriter = f
	}

	var firstRead atomic.Bool
	var dropped atomic.Uint64
	tsCh := make(chan []byte, 512)
	dec, err := ivdrm.NewTSDecryptor(dev.SessionKey, func(tsc uint8) {
		dbg("TS key received (tsc=%d)", tsc)
	})
	if err != nil {
		return err
	}

	if err := dev.StartStream(func(chunk []byte) {
		if !firstRead.Swap(true) {
			dbg("EP83 first read %d B", len(chunk))
		}
		plain := dec.Process(chunk)
		if len(plain) == 0 {
			return
		}
		select {
		case tsCh <- plain:
		default:
			dropped.Add(1)
		}
	}); err != nil {
		return err
	}
	defer dev.StopStream()

	var timerCh <-chan time.Time
	if o.seconds > 0 {
		t := time.NewTimer(time.Duration(o.seconds) * time.Second)
		defer t.Stop()
		timerCh = t.C
	}

	var totalBytes uint64
	started := false
	startTime := time.Now()

loop:
	for {
		select {
		case <-stopCh:
			break loop
		case <-timerCh:
			break loop
		case pkt := <-tsCh:
			if !started {
				started = true
				dbg("streaming started (%.1fs after launch)", time.Since(startTime).Seconds())
			}
			if _, err := outWriter.Write(pkt); err != nil {
				return fmt.Errorf("書き込み失敗: %w", err)
			}
			totalBytes += uint64(len(pkt))
		case <-time.After(200 * time.Millisecond):
			if dev.Stream != nil && dev.Stream.DeviceGone() {
				return errors.New("USB デバイスが切断された")
			}
		}
	}

	dbg("total %d bytes, dropped chunks %d", totalBytes, dropped.Load())
	return nil
}
