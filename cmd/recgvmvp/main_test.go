// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package main

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/waya0125/recgvmvp/internal/channel"
)

func TestParseArgs(t *testing.T) {
	var errBuf bytes.Buffer
	o, err := parseArgs([]string{"--b25", "--device", "/dev/bus/usb/004/004", "--firmware", "/tmp/fw.bin", "25", "10", "/tmp/out.ts"}, &errBuf)
	if err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}
	if o.ch.UHF != 25 || o.seconds != 10 || o.output != "/tmp/out.ts" || o.device != "/dev/bus/usb/004/004" || o.firmware != "/tmp/fw.bin" || !o.b25 {
		t.Fatalf("unexpected options: %+v", o)
	}

	o2, err := parseArgs([]string{"--check", "--fw", "/tmp/a.bin", "BS15_0", "-"}, &errBuf)
	if err != nil {
		t.Fatalf("parseArgs --check failed: %v", err)
	}
	if !o2.check || o2.ch.Name != "BS15_0" || o2.ch.TSID != 0x40f1 || o2.seconds != 5 || o2.firmware != "/tmp/a.bin" {
		t.Fatalf("unexpected --check options: %+v", o2)
	}

	if _, err := parseArgs([]string{"99", "5", "-"}, &errBuf); err == nil {
		t.Fatal("expected error for invalid channel 99")
	}
}

type fakeTuner struct {
	tunerDevice
	ids   []uint16
	tuned []uint16
}

func (f *fakeTuner) TuneISDBS(freqKHz uint32, tsid uint16, stopCh <-chan struct{}) (bool, error) {
	f.tuned = append(f.tuned, tsid)
	return true, nil
}
func (f *fakeTuner) ReadTSIDs() ([]uint16, error) { return f.ids, nil }

func TestTuneISDBS(t *testing.T) {
	nodbg := func(string, ...any) {}
	parse := func(s string) channel.Channel {
		ch, err := channel.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return ch
	}

	// 表の値で当たればそのまま
	f := &fakeTuner{ids: []uint16{0x4090, 0x4092}}
	if err := tuneISDBS(f, parse("BS09_1"), false, nil, io.Discard, nodbg); err != nil || len(f.tuned) != 1 || f.tuned[0] != 0x4092 {
		t.Fatalf("BS09_1: err=%v tuned=%#04x", err, f.tuned)
	}

	// 表が古くても、一覧の並びで選び直す
	ch := parse("BS09_1")
	ch.TSID = 0x4091
	f = &fakeTuner{ids: []uint16{0x4090, 0x4092}}
	if err := tuneISDBS(f, ch, false, nil, io.Discard, nodbg); err != nil || len(f.tuned) != 2 || f.tuned[1] != 0x4092 {
		t.Fatalf("stale table: err=%v tuned=%#04x", err, f.tuned)
	}

	// 無い番目はエラーで止まる（先頭の TS を出さない）
	f = &fakeTuner{ids: []uint16{0x4090, 0x4092}}
	if err := tuneISDBS(f, parse("BS09_2"), false, nil, io.Discard, nodbg); err == nil {
		t.Fatal("BS09_2: expected error")
	}
	// --check のときは止めない
	if err := tuneISDBS(f, parse("BS09_2"), true, nil, io.Discard, nodbg); err != nil {
		t.Fatalf("BS09_2 --check: %v", err)
	}

	// --tsid で渡した TSID が無ければエラー
	ch = parse("BS09_0")
	ch.TSID, ch.ExplicitTSID = 0x4091, true
	f = &fakeTuner{ids: []uint16{0x4090, 0x4092}}
	if err := tuneISDBS(f, ch, false, nil, io.Discard, nodbg); err == nil {
		t.Fatal("explicit 0x4091: expected error")
	}
}

// 出力先が読まれなくなっても、書き出しのゴルーチンが止まるだけで呼び出し側は止まらない（issue #2）。
func TestWriterDoesNotBlockCaller(t *testing.T) {
	r, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer wr.Close()
	w := startWriter(wr)
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		pkt := make([]byte, 188*1024)
		for {
			select {
			case w.ch <- pkt:
			case <-stop:
				return
			}
		}
	}()
	time.Sleep(200 * time.Millisecond) // パイプとチャンネルが詰まるまで
	close(stop)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("caller blocked on a stalled writer")
	}
}
