// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package saa7164usb

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectFirmware(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "VISTA64")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	fwPath := filepath.Join(sub, FirmwareName)
	buf := make([]byte, 0x20000+1024)
	binary.LittleEndian.PutUint32(buf[0x10:0x14], 0x10)
	binary.LittleEndian.PutUint32(buf[0x14:0x18], 0x10)
	binary.LittleEndian.PutUint32(buf[0x1c:0x20], 0x00011d07)
	binary.LittleEndian.PutUint32(buf[0x1FD80:0x1FD84], 0x10)
	binary.LittleEndian.PutUint32(buf[0x1FD84:0x1FD88], 0x10)
	if err := os.WriteFile(fwPath, buf, 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := InspectFirmware(dir)
	if err != nil {
		t.Fatalf("InspectFirmware(dir) failed: %v", err)
	}
	if info.Path != fwPath || info.Version != 0x00011d07 || info.KnownHash {
		t.Fatalf("unexpected FirmwareInfo: %+v", info)
	}
}
