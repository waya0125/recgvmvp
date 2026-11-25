// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package gvmvphz は I-O DATA GV-MVP/HZ（04bb:0522）の開閉・選局・ストリーム受信をまとめる。
package gvmvphz

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/waya0125/recgvmvp/internal/ivdrm"
	"github.com/waya0125/recgvmvp/internal/saa7164usb"
	"github.com/waya0125/recgvmvp/internal/saigawa"
	"github.com/waya0125/recgvmvp/internal/usbfs"
)

// USBID は GV-MVP/HZ の USB ベンダー・プロダクト ID。
var USBID = usbfs.ID{Vendor: 0x04bb, Product: 0x0522}

// ErrNoDevice は使える GV-MVP/HZ が見つからないことを示す。
var ErrNoDevice = errors.New("使える GV-MVP/HZ（04bb:0522）が見つからない")

const (
	streamEndpoint = 0x83
	urbCount       = 32
	urbSize        = 42112 // 0xa480 (224 TS packets): matches SAA7164 USB bulk block size
)

// OpenOptions はデバイスオープン時の設定。
type OpenOptions struct {
	DevicePath string
	Firmware   string
	Reset      bool
	NoAuth     bool
	Debugf     func(string, ...any)
}

// Device は開いた GV-MVP/HZ。
type Device struct {
	USB        *usbfs.Device
	Client     *saa7164usb.Client
	SessionID  uint32
	SessionKey [16]byte
	WatchDog   *ivdrm.WatchDog
	Stream     *usbfs.BulkStream
	authed     bool
	debugf     func(string, ...any)
}

func (d *Device) dbg(format string, a ...any) {
	if d.debugf != nil {
		d.debugf(format, a...)
	}
}

// OpenDevice は空いている GV-MVP/HZ を開いて認証・WatchDog 起動まで完了する。
func OpenDevice(opt OpenOptions) (*Device, error) {
	dbg := opt.Debugf
	if dbg == nil {
		dbg = func(string, ...any) {}
	}

	paths := []string{opt.DevicePath}
	if opt.DevicePath == "" {
		found, err := usbfs.Find(USBID)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, ErrNoDevice
		}
		paths = found
	}

	if opt.Reset {
		if usb, err := usbfs.Open(paths[0]); err == nil {
			dbg("--reset: issuing USB port reset")
			_ = usb.Reset()
			_ = usb.Close()
		}
		time.Sleep(1500 * time.Millisecond)
		if opt.DevicePath == "" {
			paths = nil
			for i := 0; i < 10 && len(paths) == 0; i++ {
				paths, _ = usbfs.Find(USBID)
				if len(paths) == 0 {
					time.Sleep(500 * time.Millisecond)
				}
			}
			if len(paths) == 0 {
				return nil, errors.New("リセット後にデバイスが再認識されない")
			}
		}
	}

	var last error
	for _, p := range paths {
		dev, err := openOne(p, opt, dbg)
		if err == nil {
			return dev, nil
		}
		last = err
		if !errors.Is(err, syscall.EBUSY) {
			break
		}
	}
	return nil, last
}

func openOne(path string, opt OpenOptions, dbg func(string, ...any)) (*Device, error) {
	id, bcd, err := usbfs.ReadDeviceInfo(path)
	if err != nil {
		return nil, err
	}
	if id != USBID {
		return nil, fmt.Errorf("%s は %s（GV-MVP/HZ は %s）", path, id, USBID)
	}
	dbg("found device %s bcdDevice=0x%04X", path, bcd)

	needDFU := bcd != 0x0100
	if !needDFU {
		if probeUSB, oerr := usbfs.Open(path); oerr == nil {
			needDFU = saa7164usb.IsDFUDevice(probeUSB)
			_ = probeUSB.Close()
		} else {
			return nil, oerr
		}
	}

	if needDFU {
		fwPath, err := saa7164usb.ResolveFirmwareFor(opt.Firmware, saa7164usb.FirmwareNameHZ)
		if err != nil {
			return nil, err
		}
		dbg("cold/DFU device: loading firmware %s", fwPath)
		if err := saa7164usb.BootFirmware(USBID, fwPath, dbg); err != nil {
			return nil, err
		}
		found, err := usbfs.Find(USBID)
		if err != nil || len(found) == 0 {
			return nil, errors.New("ファームウェア転送後にデバイスが見つからない")
		}
		path = found[0]
	}

	usb, err := usbfs.Open(path)
	if err != nil {
		return nil, err
	}
	_ = usb.SetConfiguration(1)
	if err := usb.Claim(0); err != nil {
		_ = usb.Close()
		return nil, err
	}
	drain := make([]byte, saa7164usb.MaxMessage)
	for i := 0; i < 16; i++ {
		if _, err := usb.Transfer(0x81, drain, 50); err != nil {
			break
		}
	}
	if err := usb.Claim(2); err != nil {
		_ = usb.Close()
		return nil, err
	}

	c := &saa7164usb.Client{Dev: usb, Out: 0x01, In: 0x81, TimeoutMS: 2000}

	// 前回終了時の残留データを回復する
	if !opt.NoAuth {
		_ = c.SetState(8, saa7164usb.StatePause)
		_ = c.SetState(8, saa7164usb.StateAcquire)
		_ = c.SetState(8, saa7164usb.StateStop)
		epDrain := make([]byte, urbSize)
		for i := 0; i < 8; i++ {
			if _, err := usb.Transfer(streamEndpoint, epDrain, 30); err != nil {
				break
			}
		}
	}

	// unit 5 (DemodUnit) reg 0x80 が読めない場合のみコールド初期化（unit 4 reg 0x00 は未初期化で触らない）
	if _, _, _, err := saigawa.ReadLock(c); err != nil {
		dbg("cold init: saigawa.Init")
		if err := saigawa.Init(c); err != nil {
			_ = usb.Close()
			return nil, err
		}
		dbg("initInternalBcas (cold)")
		c.InitInternalBCAS()
		st, _ := c.GetCardStatus()
		dbg("B-CAS card status = %d", st)
	} else {
		st, _ := c.GetCardStatus()
		dbg("warm reopen: skip GPIO/Init (B-CAS status=%d)", st)
	}

	dbg("prepareStreamUnit")
	c.PrepareStreamUnit()

	if opt.NoAuth {
		return &Device{
			USB:    usb,
			Client: c,
			debugf: dbg,
		}, nil
	}

	var sid uint32
	sessKey, err := ivdrm.RunAuth(c, 0)
	if err != nil {
		dbg("auth session 0 failed (%v), trying session 1", err)
		sid = 1
		sessKey, err = ivdrm.RunAuth(c, 1)
		if err != nil {
			_ = usb.Close()
			return nil, fmt.Errorf("IVDRM 認証失敗: %w", err)
		}
	}
	dbg("IVDRM auth ok (session=%d)", sid)
	wd, err := ivdrm.StartWatchDog(c, sessKey)
	if err != nil {
		ivdrm.CloseSession(c, sid)
		_ = usb.Close()
		return nil, fmt.Errorf("WatchDog 起動失敗: %w", err)
	}
	dbg("WatchDog started")

	return &Device{
		USB:        usb,
		Client:     c,
		SessionID:  sid,
		SessionKey: sessKey,
		WatchDog:   wd,
		authed:     true,
		debugf:     dbg,
	}, nil
}

// TuneISDBT は地上デジタル ch（13〜62）に同調する。
func (d *Device) TuneISDBT(ch int, stopCh <-chan struct{}) (bool, error) {
	if ch < 13 || ch > 62 {
		return false, fmt.Errorf("地デジの範囲外: %dch（13〜62）", ch)
	}
	select {
	case <-stopCh:
		return false, errors.New("中断された")
	default:
	}
	hz := uint32(473_142_857 + (ch-13)*6_000_000)
	locked, err := saigawa.TuneISDBT(d.Client, hz)
	if err != nil {
		return false, err
	}
	r80, rB0, _, _ := saigawa.ReadLock(d.Client)
	d.dbg("tune UHF %dch (%d kHz) locked=%v reg80=0x%02X regB0=0x%02X", ch, hz/1000, locked, r80, rB0)
	return locked, nil
}

// TuneISDBS は GV-MVP/HZ では非対応のためエラーを返す。
func (d *Device) TuneISDBS(freqKHz uint32, tsid uint16, stopCh <-chan struct{}) (bool, error) {
	return false, errors.New("GV-MVP/HZ は BS/110度CS（ISDB-S）非対応")
}

// StartStream は EP83 の非同期受信を開始してから Unit 8 を RUN にする。
func (d *Device) StartStream(onChunk func([]byte)) error {
	if !d.authed {
		return errors.New("NoAuth で開いた機器では TS を流せない")
	}
	stream, err := d.USB.StartBulkStream(streamEndpoint, urbCount, urbSize, onChunk)
	if err != nil {
		return err
	}
	d.Stream = stream
	time.Sleep(40 * time.Millisecond)
	if err := d.Client.SetState(8, saa7164usb.StateRun); err != nil {
		stream.Stop()
		d.Stream = nil
		return fmt.Errorf("unit 8 RUN 失敗: %w", err)
	}
	d.dbg("Unit8 RUN ok")
	return nil
}

// StopStream は EP83 リーダーを回したまま Unit 8 を停止し、その後 URB を回収する。
func (d *Device) StopStream() {
	if d.Stream == nil {
		return
	}
	_ = d.Client.SetState(8, saa7164usb.StatePause)
	_ = d.Client.SetState(8, saa7164usb.StateAcquire)
	_ = d.Client.SetState(8, saa7164usb.StateStop)
	d.Stream.Stop()
	d.Stream = nil
}

// Close はストリーム・WatchDog・セッション・USB デバイスを順に閉じる。
func (d *Device) Close() {
	if d == nil {
		return
	}
	d.StopStream()
	if d.WatchDog != nil {
		d.WatchDog.Stop()
		d.WatchDog = nil
	}
	if d.Client != nil && d.authed {
		ivdrm.CloseSession(d.Client, d.SessionID)
	}
	if d.USB != nil {
		_ = d.USB.Close()
		d.USB = nil
	}
}
