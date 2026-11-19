// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package gvmvpvz は I-O DATA GV-MVP/VZ（04bb:052b）の開閉・選局・ストリーム受信をまとめる。
package gvmvpvz

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/waya0125/recgvmvp/internal/ivdrm"
	"github.com/waya0125/recgvmvp/internal/saa7164usb"
	"github.com/waya0125/recgvmvp/internal/sonylj"
	"github.com/waya0125/recgvmvp/internal/usbfs"
)

// USBID は GV-MVP/VZ の USB ベンダー・プロダクト ID。
var USBID = usbfs.ID{Vendor: 0x04bb, Product: 0x052b}

// ErrNoDevice は使える GV-MVP/VZ が見つからないことを示す。
var ErrNoDevice = errors.New("使える GV-MVP/VZ（04bb:052b）が見つからない")

const (
	streamEndpoint = 0x83
	urbCount       = 8
	urbSize        = 32768
)

// OpenOptions はデバイスオープン時の設定。
type OpenOptions struct {
	DevicePath string
	Firmware   string
	Reset      bool
	// NoAuth が true なら IVDRM の認証と WatchDog を始めない。
	// 選局とロック・C/N の確かめ（--check）だけに使う。TS は流せない。
	NoAuth bool
	Debugf func(string, ...any)
}

// Device は開いた GV-MVP/VZ。
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

// OpenDevice は空いている GV-MVP/VZ を開いて認証・WatchDog 起動まで完了する。
// DevicePath が空なら、見つかったもののうち EBUSY でない最初のものを選ぶ。
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
		return nil, fmt.Errorf("%s は %s（GV-MVP/VZ は %s）", path, id, USBID)
	}
	dbg("found device %s bcdDevice=0x%04X", path, bcd)

	if bcd != 0x0100 {
		fwPath, err := saa7164usb.ResolveFirmware(opt.Firmware)
		if err != nil {
			return nil, err
		}
		dbg("cold device: loading firmware %s", fwPath)
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
	// EP81 の残留通知を捨てる
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

	// 前回異常終了時に Unit 8 RUN のまま残った EP83 データを回復する
	junk := make([]byte, urbSize)
	stale := 0
	for k := 0; k < 4; k++ {
		n, err := usb.Transfer(streamEndpoint, junk, 50)
		if err != nil || n <= 0 {
			break
		}
		stale += n
	}
	if stale > 0 {
		dbg("recovering stale EP83 (%d B)", stale)
		_ = c.SetState(8, saa7164usb.StatePause)
		_ = c.SetState(8, saa7164usb.StateAcquire)
		_ = c.SetState(8, saa7164usb.StateStop)
		for k := 0; k < 4; k++ {
			if n, err := usb.Transfer(streamEndpoint, junk, 30); err != nil || n <= 0 {
				break
			}
		}
	}

	// コールド判定（地デジ起床時 0x34、BS/CS 起床時 0x1c のどちらでもないときのみコールド初期化）
	demodCheck, err := c.I2CRead(sonylj.DemodT, 0x0f, 1)
	if err != nil || len(demodCheck) == 0 || (demodCheck[0] != 0x34 && demodCheck[0] != 0x1c) {
		dbg("initHardware (cold)")
		if err := sonylj.InitColdHardware(c); err != nil {
			_ = usb.Close()
			return nil, err
		}
		dbg("initInternalBcas (cold)")
		c.InitInternalBCAS()
	} else {
		st, _ := c.GetCardStatus()
		dbg("warm reopen: skip GPIO/INT (reg0F=0x%02X, B-CAS status=%d)", demodCheck[0], st)
	}

	dbg("prepareStreamUnit")
	c.PrepareStreamUnit()

	if opt.NoAuth {
		dbg("open OK (NoAuth: IVDRM の認証をしない)")
		return &Device{USB: usb, Client: c, debugf: dbg}, nil
	}

	dbg("runIvdrmAuth")
	var sid uint32
	sessKey, err := ivdrm.RunAuth(c, 0)
	if err != nil {
		sid = 1
		sessKey, err = ivdrm.RunAuth(c, 1)
		if err != nil {
			_ = usb.Close()
			return nil, fmt.Errorf("IVDRM 認証に失敗（USB を抜き差しして再試行してください）: %w", err)
		}
	}

	wd, err := ivdrm.StartWatchDog(c, sessKey)
	if err != nil {
		ivdrm.CloseSession(c, sid)
		_ = usb.Close()
		return nil, err
	}
	dbg("open OK (sid=%d)", sid)

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

// TuneISDBT は地デジ側を起こしてから指定した UHF 物理チャンネルに選局し、最大 2.5 秒ロックを待つ。
func (d *Device) TuneISDBT(ch int, stopCh <-chan struct{}) (bool, error) {
	hz, err := sonylj.UHFChannelHz(ch)
	if err != nil {
		return false, err
	}
	if err := sonylj.WakeISDBT(d.Client, true, false); err != nil {
		return false, err
	}
	if err := sonylj.TuneISDBT(d.Client, hz); err != nil {
		return false, err
	}
	locked := false
	var r80, rb0 byte
	for i := 0; i < 25; i++ {
		select {
		case <-stopCh:
			return locked, nil
		case <-time.After(100 * time.Millisecond):
		}
		locked, r80, rb0, err = sonylj.LockISDBT(d.Client)
		if err == nil && locked {
			break
		}
	}
	d.dbg("tune UHF %dch (%d kHz) locked=%v reg80=0x%02X regB0=0x%02X", ch, hz/1000, locked, r80, rb0)
	return locked, nil
}

// TuneISDBS は BS・110度CS へ切り替え、周波数と TSID に合わせて最大 2.5 秒ロックを待つ。
func (d *Device) TuneISDBS(freqKHz uint32, tsid uint16, stopCh <-chan struct{}) (bool, error) {
	if err := sonylj.InitISDBSDemod(d.Client); err != nil {
		return false, err
	}
	if err := sonylj.InitISDBSTuner(d.Client); err != nil {
		return false, err
	}
	if err := sonylj.WakeISDBS(d.Client); err != nil {
		return false, err
	}
	if err := sonylj.TuneISDBS(d.Client, freqKHz, tsid); err != nil {
		return false, err
	}
	locked := false
	var rc3 byte
	var err error
	for i := 0; i < 25; i++ {
		select {
		case <-stopCh:
			return locked, nil
		case <-time.After(100 * time.Millisecond):
		}
		locked, rc3, err = sonylj.LockISDBS(d.Client)
		if err == nil && locked {
			break
		}
	}
	d.dbg("tune ISDB-S %d kHz TSID=0x%04X locked=%v regC3=0x%02X", freqKHz, tsid, locked, rc3)
	if ids, err := sonylj.ReadTSIDs(d.Client); err == nil && len(ids) > 0 {
		d.dbg("available TSIDs: %#x", ids)
	}
	return locked, nil
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
