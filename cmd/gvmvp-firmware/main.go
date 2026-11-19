// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// gvmvp-firmware は GV-MVP/VZ のファームウェア（NXP7164F_052B04BB.bin）の検証・配置・手動転送を行う。
//
//	gvmvp-firmware verify <NXP7164F_052B04BB.bin | ドライバフォルダ>
//	gvmvp-firmware install <NXP7164F_052B04BB.bin | ドライバフォルダ> [-o /usr/local/share/gvmvpvz/NXP7164F_052B04BB.bin]
//	gvmvp-firmware load [NXP7164F_052B04BB.bin]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/waya0125/recgvmvp/internal/saa7164usb"
	"github.com/waya0125/recgvmvp/internal/usbfs"
)

var vzID = usbfs.ID{Vendor: 0x04bb, Product: 0x052b}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "install":
		err = cmdInstall(os.Args[2:])
	case "load":
		err = cmdLoad(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gvmvp-firmware:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `使い方:
  gvmvp-firmware verify <NXP7164F_052B04BB.bin | ドライバフォルダ>
      ファームウェアの形式・版・SHA-256 ハッシュを照合する
  gvmvp-firmware install <NXP7164F_052B04BB.bin | ドライバフォルダ> [-o 出力先]
      照合したファームウェアを所定の場所へコピーする（既定: /usr/local/share/gvmvpvz/NXP7164F_052B04BB.bin）
  gvmvp-firmware load [NXP7164F_052B04BB.bin]
      コールド状態の GV-MVP/VZ（04bb:052b）にファームウェアを送る`)
	os.Exit(2)
}

func cmdVerify(args []string) error {
	if len(args) != 1 {
		usage()
	}
	info, err := saa7164usb.InspectFirmware(args[0])
	if err != nil {
		return err
	}
	printInfo(info)
	return nil
}

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	out := fs.String("o", filepath.Join("/usr/local/share/gvmvpvz", saa7164usb.FirmwareName), "配置先のパス")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	info, err := saa7164usb.InspectFirmware(fs.Arg(0))
	if err != nil {
		return err
	}
	data, err := os.ReadFile(info.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return err
	}
	printInfo(info)
	fmt.Printf("  → %s へ配置しました\n", *out)
	return nil
}

func cmdLoad(args []string) error {
	var fwArg string
	if len(args) > 0 {
		fwArg = args[0]
	}
	fwPath, err := saa7164usb.ResolveFirmware(fwArg)
	if err != nil {
		return err
	}
	info, err := saa7164usb.InspectFirmware(fwPath)
	if err != nil {
		return err
	}
	paths, err := usbfs.Find(vzID)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("GV-MVP/VZ (%s) が見つからない", vzID)
	}
	_, bcd, err := usbfs.ReadDeviceInfo(paths[0])
	if err != nil {
		return err
	}
	if bcd == 0x0100 {
		fmt.Printf("%s はすでにウォーム状態 (bcdDevice=0x%04X) のため転送不要です\n", paths[0], bcd)
		return nil
	}
	fmt.Printf("転送開始: %s (ver=0x%08x, SHA-256=%s)\n", info.Path, info.Version, info.SHA256)
	return saa7164usb.BootFirmware(vzID, info.Path, func(f string, a ...any) {
		fmt.Printf("  "+f+"\n", a...)
	})
}

func printInfo(info saa7164usb.FirmwareInfo) {
	match := "不明なハッシュ"
	if info.KnownHash {
		match = "既知の正規ハッシュと一致"
	}
	fmt.Printf("%s\n  サイズ: %d B (stage1=%d B, stage2=%d B)\n  版: 0x%08x\n  SHA-256: %s (%s)\n",
		info.Path, info.Size, info.Stage1Len, info.Stage2Len, info.Version, info.SHA256, match)
}
