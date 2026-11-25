// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package saa7164usb

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/waya0125/recgvmvp/internal/usbfs"
)

const (
	// FirmwareName は GV-MVP/VZ 用 NXP SAA7164 ファームウェアのファイル名。
	FirmwareName = "NXP7164F_052B04BB.bin"
	// FirmwareNameHZ は GV-MVP/HZ 用 NXP SAA7164 ファームウェアのファイル名。
	FirmwareNameHZ = "NXP7164F_052204BB.bin"

	// KnownFirmwareSHA256 は GV-MVP/VZ 付属ドライバ（v3.21 等、版 0x00011d07）に含まれる既知の NXP7164F_052B04BB.bin の SHA-256。
	KnownFirmwareSHA256 = "586e71bc0247ffe036553a6227bf03e6479ba8ce7938017eb4497e8f23c143ea"
	// KnownFirmwareSHA256V419 は GV-MVP/VZ ドライバ（v4.19 等、版 0x00021d14）に含まれる既知の NXP7164F_052B04BB.bin の SHA-256。
	KnownFirmwareSHA256V419 = "075310d9f51ee62e637bbf0f7232018a60447fcdf3baca5d4b0e7884cbeafdd6"
	// KnownFirmwareSHA256HZ は GV-MVP/HZ ドライバ（v1.44 等、版 0x00021d0f）に含まれる既知の NXP7164F_052204BB.bin の SHA-256。
	KnownFirmwareSHA256HZ = "162323cdb0fef9a5b0814a08a23dd8d437c2e9c0bc8803d88a9d2a4c0b91341f"
)

// FirmwareInfo はファームウェアファイルの検証結果。
type FirmwareInfo struct {
	Path      string
	Size      int
	Stage1Len int
	Stage2Len int
	Version   uint32
	SHA256    string
	KnownHash bool
}

// InspectFirmware はファイルまたはディレクトリ内から NXP7164F_052B04BB.bin を探して検証する。
func InspectFirmware(target string) (FirmwareInfo, error) {
	st, err := os.Stat(target)
	if err != nil {
		return FirmwareInfo{}, err
	}
	fwPath := target
	if st.IsDir() {
		fwPath = ""
		_ = filepath.WalkDir(target, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			if strings.EqualFold(d.Name(), FirmwareName) || strings.EqualFold(d.Name(), FirmwareNameHZ) {
				fwPath = p
				return fs.SkipAll
			}
			return nil
		})
		if fwPath == "" {
			return FirmwareInfo{}, fmt.Errorf("%s の中に %s / %s が見つからない", target, FirmwareName, FirmwareNameHZ)
		}
	}
	fw, err := os.ReadFile(fwPath)
	if err != nil {
		return FirmwareInfo{}, err
	}
	if len(fw) < 0x20000 {
		return FirmwareInfo{}, fmt.Errorf("%s が小さすぎる (%d B)", fwPath, len(fw))
	}
	len1 := int((binary.LittleEndian.Uint32(fw[0x10:0x14]) + binary.LittleEndian.Uint32(fw[0x14:0x18]) + 1) << 4)
	off2 := 0x10 + len1
	if off2+0x20 > len(fw) {
		return FirmwareInfo{}, fmt.Errorf("%s の stage1 長が不正 (len1=%d size=%d)", fwPath, len1, len(fw))
	}
	len2 := int((binary.LittleEndian.Uint32(fw[off2:off2+4]) + binary.LittleEndian.Uint32(fw[off2+4:off2+8]) + 1) << 4)
	if off2+len2 > len(fw) {
		return FirmwareInfo{}, fmt.Errorf("%s のヘッダ長が不正 (len1=%d len2=%d size=%d)", fwPath, len1, len2, len(fw))
	}
	sum := sha256.Sum256(fw)
	digest := hex.EncodeToString(sum[:])
	return FirmwareInfo{
		Path:      fwPath,
		Size:      len(fw),
		Stage1Len: len1,
		Stage2Len: len2,
		Version:   binary.LittleEndian.Uint32(fw[0x1c:0x20]),
		SHA256:    digest,
		KnownHash: digest == KnownFirmwareSHA256 || digest == KnownFirmwareSHA256V419 || digest == KnownFirmwareSHA256HZ,
	}, nil
}

// ResolveFirmware は GV-MVP/VZ 用ファームウェアファイルを探索する。
func ResolveFirmware(opt string) (string, error) {
	return ResolveFirmwareFor(opt, FirmwareName)
}

// ResolveFirmwareFor は指定されたファイル名のファームウェアを探索する。
func ResolveFirmwareFor(opt, fwName string) (string, error) {
	var cands []string
	if opt != "" {
		cands = append(cands, opt)
	}
	for _, envName := range []string{"GVMVP_FW_PATH", "GVMVPVZ_FW_PATH", "GVMVPHZ_FW_PATH"} {
		if env := os.Getenv(envName); env != "" {
			cands = append(cands, env)
		}
	}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), fwName))
	}
	cands = append(cands,
		filepath.Join("/usr/local/share/gvmvpvz", fwName),
		filepath.Join("/lib/firmware", fwName),
	)
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s が見つからない（探索先: %s）", fwName, strings.Join(cands, ", "))
}

// BootFirmware はコールド状態（bcdDevice != 0x0100）の GV-MVP/VZ に DFU 2 ステージでファームを転送する。
func BootFirmware(id usbfs.ID, fwPath string, debugf func(string, ...any)) error {
	fw, err := os.ReadFile(fwPath)
	if err != nil {
		return fmt.Errorf("ファームウェアを読めない: %w", err)
	}
	if len(fw) < 0x20000 {
		return fmt.Errorf("ファームウェアが小さすぎる (%d B)", len(fw))
	}
	len1 := int((binary.LittleEndian.Uint32(fw[0x10:0x14]) + binary.LittleEndian.Uint32(fw[0x14:0x18]) + 1) << 4)
	off2 := 0x10 + len1
	if off2+0x20 > len(fw) {
		return fmt.Errorf("ファームウェアの stage1 長が不正 (len1=%d size=%d)", len1, len(fw))
	}
	len2 := int((binary.LittleEndian.Uint32(fw[off2:off2+4]) + binary.LittleEndian.Uint32(fw[off2+4:off2+8]) + 1) << 4)
	if off2+len2 > len(fw) {
		return fmt.Errorf("ファームウェアのヘッダ長が不正 (len1=%d len2=%d size=%d)", len1, len2, len(fw))
	}

	for attempt := 0; attempt < 4; attempt++ {
		paths, err := usbfs.Find(id)
		if err != nil || len(paths) == 0 {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		_, bcd, bcdErr := usbfs.ReadDeviceInfo(paths[0])
		dev, err := usbfs.Open(paths[0])
		if err != nil {
			if debugf != nil {
				debugf("DFU: open failed: %v", err)
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if bcdErr == nil && bcd == 0x0100 && !IsDFUDevice(dev) {
			_ = dev.Close()
			return nil
		}
		iprod := readStringDescASCII(dev, 0x20)
		prod2 := readStringDescASCII(dev, 2)
		prod3 := readStringDescASCII(dev, 3)
		if strings.HasPrefix(iprod, "01") || strings.Contains(prod2, "Boot2") || strings.Contains(prod3, "Boot2") {
			if debugf != nil {
				debugf("DFU: stage 2 (%d B) [iprod=%q prod3=%q]", len2, iprod, prod3)
			}
			_ = uploadDFUStage(dev, fw[off2:off2+len2])
			_ = dev.Close()
			time.Sleep(4000 * time.Millisecond)
		} else {
			if debugf != nil {
				debugf("DFU: stage 1 (%d B) [iprod=%q prod3=%q]", len1, iprod, prod3)
			}
			_ = uploadDFUStage(dev, fw[0x10:0x10+len1])
			_ = dev.Close()
			time.Sleep(2000 * time.Millisecond)
		}
	}

	for i := 0; i < 10; i++ {
		if paths, err := usbfs.Find(id); err == nil && len(paths) > 0 {
			if _, bcd, err := usbfs.ReadDeviceInfo(paths[0]); err == nil && bcd == 0x0100 {
				if dev, oerr := usbfs.Open(paths[0]); oerr == nil {
					dfu := IsDFUDevice(dev)
					_ = dev.Close()
					if !dfu {
						return nil
					}
				} else {
					return fmt.Errorf("再認識されたデバイス (%s) を開けない: %w", paths[0], oerr)
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("ファームウェア転送後に通常モードとして再認識されない")
}

// IsDFUDevice は USB デバイスが DFU / Bootloader 状態かどうかを文字列デスクリプタから判定する。
func IsDFUDevice(dev *usbfs.Device) bool {
	p2 := readStringDescASCII(dev, 2)
	p3 := readStringDescASCII(dev, 3)
	ip := readStringDescASCII(dev, 0x20)
	return strings.Contains(p2, "DFU") || strings.Contains(p2, "Boot") ||
		strings.Contains(p3, "DFU") || strings.Contains(p3, "Boot") ||
		strings.HasPrefix(ip, "01")
}

func readStringDescASCII(dev *usbfs.Device, index uint8) string {
	var buf [128]byte
	for _, lang := range []uint16{0x0409, 0x0000} {
		n, err := dev.Control(0x80, 0x06, uint16(0x0300)|uint16(index), lang, buf[:], 2000)
		if err != nil || n < 2 {
			continue
		}
		var out strings.Builder
		for i := 2; i+1 < n; i += 2 {
			if buf[i+1] == 0 && buf[i] >= 0x20 && buf[i] < 0x7f {
				out.WriteByte(buf[i])
			}
		}
		if out.Len() > 0 {
			return out.String()
		}
	}
	return ""
}

func uploadDFUStage(dev *usbfs.Device, img []byte) error {
	_ = dev.SetConfiguration(1)
	if err := dev.Claim(0); err != nil {
		return err
	}
	defer func() { _ = dev.Release(0) }()
	_ = dev.ClearHalt(0x03)

	var st0 [6]byte
	r, err := dev.Control(0xA1, 3, 0, 0, st0[:], 2000) // DFU_GETSTATUS
	if err == nil && r == 6 && st0[4] == 10 {          // dfuERROR
		_, _ = dev.Control(0x21, 4, 0, 0, nil, 2000) // DFU_CLRSTATUS
		r, err = dev.Control(0xA1, 3, 0, 0, st0[:], 2000)
	}
	if err == nil && r == 6 && st0[4] != 2 {
		_, _ = dev.Control(0x21, 6, 0, 0, nil, 2000) // DFU_ABORT
	}
	_, _ = dev.Control(0x21, 6, 0, 0, nil, 2000)
	_, _ = dev.Control(0xA1, 3, 0, 0, st0[:], 2000)

	var block uint16 = 1
	var pad [4096]byte
	var status [6]byte
	for pos := 0; pos < len(img); block++ {
		chunk := len(img) - pos
		if chunk > 4096 {
			chunk = 4096
		}
		ptr := img[pos : pos+chunk]
		if chunk < 4096 {
			clear(pad[:])
			copy(pad[:], ptr)
			ptr = pad[:]
		}
		var hsLen [2]byte
		binary.LittleEndian.PutUint16(hsLen[:], 4096)
		if _, err := dev.Control(0x21, 1, block, 0, hsLen[:], 2000); err != nil {
			return fmt.Errorf("DFU DNLOAD header block %d: %w", block, err)
		}
		if _, err := dev.Transfer(0x03, ptr, 5000); err != nil {
			return fmt.Errorf("DFU bulk write block %d: %w", block, err)
		}
		r, err = dev.Control(0xA1, 3, 0, 0, status[:], 10000)
		if err != nil {
			return err
		}
		for r == 6 && status[4] == 4 { // dfuDNBUSY
			waitMS := int(status[1]) | int(status[2])<<8 | int(status[3])<<16
			if waitMS <= 0 {
				waitMS = 5
			}
			time.Sleep(time.Duration(waitMS) * time.Millisecond)
			r, err = dev.Control(0xA1, 3, 0, 0, status[:], 10000)
			if err != nil {
				return err
			}
		}
		if r == 6 && status[4] != 5 { // != dfuDNLOAD-IDLE
			return fmt.Errorf("DFU unexpected state %d at block %d", status[4], block)
		}
		pos += chunk
	}
	_, _ = dev.Control(0x21, 1, block, 0, nil, 2000)
	_, _ = dev.Transfer(0x03, []byte{0}, 2000)
	_, _ = dev.Control(0xA1, 3, 0, 0, status[:], 2000)
	_ = dev.Reset()
	return nil
}
