// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package main

import (
	"bytes"
	"testing"
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
