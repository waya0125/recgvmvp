// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package saa7164usb

import (
	"bytes"
	"testing"
)

func TestMarshalRoundTrip(t *testing.T) {
	h := Header{ID: 0, Size: 4, Command: GetCur, ControlSelector: GetFWVersionControl, SeqNo: 1}
	b, err := h.Marshal(make([]byte, 4))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x00, 0x00, 0x04, 0x00, 0x81, 0x00, 0x00, 0x00, 0x09, 0x00, 0x01, 0, 0, 0, 0}
	if !bytes.Equal(b, want) {
		t.Fatalf("got % x, want % x", b, want)
	}
	got, data, err := Unmarshal(b)
	if err != nil || got != h || len(data) != 4 {
		t.Fatalf("Unmarshal = %+v % x %v", got, data, err)
	}
}

func TestMarshalSizeMismatch(t *testing.T) {
	if _, err := (Header{Size: 3}).Marshal(make([]byte, 4)); err == nil {
		t.Fatal("長さの食い違いを通した")
	}
}
