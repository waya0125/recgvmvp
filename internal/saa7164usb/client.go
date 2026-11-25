// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package saa7164usb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/waya0125/recgvmvp/internal/usbfs"
)

// 命令の頭の flags（saa7164-reg.h）
const (
	CmdFlagInterrupt  = 0x02
	CmdFlagContinue   = 0x10
	RespFlagError     = 0x01
	RespFlagContinued = 0x10
)

// unit の中の selector（saa7164-reg.h および PhilMAS64.sys の ISDBCASProp）
const (
	ExuRegisterAccessControl = 0x00 // I2C の読み書き
	ExuGPIOControl           = 0x01
	StateControl             = 0x03 // ポートの状態（SAA_STATE_CONTROL）

	// UnitCAS（unit 9）の非 DRM セレクター（ISDBCASProp）
	UnitCAS          = 9
	CasSetPID        = 0x00 // ISDBTSetPID（4 バイト）
	CasClearPID      = 0x01 // ISDBTClearPID（4 バイト）
	CasGetCardInfo   = 0x1e // GetCardInfo（262 バイト）
	CasGetCardStatus = 0x1f // GetCardStatus（4 バイト、SET_CUR の返事に 1=挿入 / 0=未挿入）
	CasGetEvent      = 0x20 // GetEvent（GET_CUR、540 バイト）
	CasCompleteEvent = 0x21 // CompleteEvent（12 バイト）
	CasSetRequest    = 0x22 // CardAccessSetRequest（272 バイト）
)

// ポートの状態
const (
	StateStop    = 0x00
	StateAcquire = 0x01
	StatePause   = 0x02
	StateRun     = 0x03
)

// ErrResponse は機器がエラーの印（PVC_RESPONSEFLAG_ERROR）を付けて返したこと。
type ErrResponse struct {
	Header Header
	Data   []byte
}

func (e *ErrResponse) Error() string {
	return fmt.Sprintf("機器がエラーを返した: unit=%d command=0x%x sel=0x%x データ=%x",
		e.Header.ID, e.Header.Command, e.Header.ControlSelector, e.Data)
}

// Client は MSG BULK OUT・MSG INT IN で命令をやりとりする。
type Client struct {
	Dev       *usbfs.Device
	Out, In   uint8
	TimeoutMS uint32

	mu  sync.Mutex
	seq uint8
}

// SendRaw は組み上がった塊（頭＋中身）を 1 つ送る。
func (c *Client) SendRaw(frame []byte) error {
	if _, err := c.Dev.Transfer(c.Out, frame, c.TimeoutMS); err != nil {
		return fmt.Errorf("命令を送れない: %w", err)
	}
	return nil
}

// ReadResponse は返事を、続きの印が消えるまで読み、中身をつなげて返す。
func (c *Client) ReadResponse() (Header, []byte, error) {
	var data []byte
	var last Header
	buf := make([]byte, MaxMessage)
	for i := 0; i < 64; i++ {
		n, err := c.Dev.Transfer(c.In, buf, c.TimeoutMS)
		if err != nil {
			return last, data, fmt.Errorf("返事を受けられない: %w", err)
		}
		h, d, err := Unmarshal(buf[:n])
		if err != nil {
			return last, data, err
		}
		last = h
		data = append(data, d...)
		if h.Flags&RespFlagError != 0 {
			return h, data, &ErrResponse{Header: h, Data: append([]byte(nil), d...)}
		}
		if h.Flags&RespFlagContinued == 0 {
			return h, data, nil
		}
	}
	return last, data, errors.New("続きの返事が多すぎる")
}

// Command は 1 つの命令を（長ければ 256 バイトずつに分けて）送り、返事を受ける。
func (c *Client) Command(unit uint8, command uint32, sel uint16, data []byte) ([]byte, error) {
	const chunk = MaxMessage - HeaderSize
	if len(data) == 0 {
		return nil, errors.New("中身の無い命令は送れない")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	tag := c.seq
	c.seq++
	for off := 0; off < len(data); off += chunk {
		end := off + chunk
		flags := uint8(CmdFlagInterrupt)
		if end < len(data) {
			flags |= CmdFlagContinue
		} else {
			end = len(data)
		}
		part := data[off:end]
		frame, err := Header{ID: unit, Flags: flags, Size: uint16(len(part)), Command: command, ControlSelector: sel, SeqNo: tag}.Marshal(part)
		if err != nil {
			return nil, err
		}
		if err := c.SendRaw(frame); err != nil {
			return nil, err
		}
	}

	var respData []byte
	buf := make([]byte, MaxMessage)
	skips := 0
	const maxSkips = 32
	for i := 0; i < 64; i++ {
		n, err := c.Dev.Transfer(c.In, buf, c.TimeoutMS)
		if err != nil {
			return respData, fmt.Errorf("返事を受けられない: %w", err)
		}
		h, d, err := Unmarshal(buf[:n])
		if err != nil {
			return respData, err
		}
		if h.ID != unit || h.ControlSelector != sel || h.SeqNo != tag {
			skips++
			if skips > maxSkips {
				return respData, fmt.Errorf("期待しない返事が多すぎる: got unit=%d sel=0x%x seq=%d, want unit=%d sel=0x%x seq=%d",
					h.ID, h.ControlSelector, h.SeqNo, unit, sel, tag)
			}
			continue
		}
		respData = append(respData, d...)
		if h.Flags&RespFlagError != 0 {
			return respData, &ErrResponse{Header: h, Data: append([]byte(nil), d...)}
		}
		if h.Flags&RespFlagContinued == 0 {
			return respData, nil
		}
	}
	return respData, errors.New("続きの返事が多すぎる")
}

// I2CWrite は unit の先の I2C の機器のレジスタ reg に data を書く（saa7164_api_i2c_write と同じ形）。
func (c *Client) I2CWrite(unit uint8, reg byte, data []byte) error {
	if _, err := c.Command(unit, GetLen, ExuRegisterAccessControl, make([]byte, 2)); err != nil {
		return fmt.Errorf("I2C の長さを聞けない: %w", err)
	}
	buf := make([]byte, 256)
	binary.LittleEndian.PutUint32(buf[0:], 1)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(data)))
	buf[8] = reg
	copy(buf[9:], data)
	_, err := c.Command(unit, SetCur, ExuRegisterAccessControl, buf)
	return err
}

// I2CRead は unit の先の I2C の機器のレジスタ reg から n バイト読む（saa7164_api_i2c_read と同じ形）。
func (c *Client) I2CRead(unit uint8, reg byte, n int) ([]byte, error) {
	lenResp, err := c.Command(unit, GetLen, ExuRegisterAccessControl, make([]byte, 2))
	if err != nil {
		return nil, fmt.Errorf("I2C の長さを聞けない: %w", err)
	}
	size := 256
	if len(lenResp) >= 2 {
		if v := int(binary.LittleEndian.Uint16(lenResp)); v > 0 && v <= 256 {
			size = v
		}
	}
	buf := make([]byte, size)
	binary.LittleEndian.PutUint32(buf[0:], 1)
	binary.LittleEndian.PutUint32(buf[4:], uint32(n))
	buf[8] = reg
	resp, err := c.Command(unit, GetCur, ExuRegisterAccessControl, buf)
	if err != nil {
		return nil, err
	}
	if len(resp) < 9+n {
		return nil, fmt.Errorf("I2C の返事が短い（%d バイト）", len(resp))
	}
	return resp[9 : 9+n], nil
}

// SetGPIO は unit の GPIO のピンを state（0 か 1）にする（saa7164_api_modify_gpio と同じ形、{u32 ピン, u8 状態}）。
func (c *Client) SetGPIO(unit uint8, pin uint32, state byte) error {
	buf := make([]byte, 5)
	binary.LittleEndian.PutUint32(buf, pin)
	buf[4] = state
	_, err := c.Command(unit, SetCur, ExuGPIOControl, buf)
	return err
}

// SetState はポート（unit）の状態を変える。
func (c *Client) SetState(unit uint8, state byte) error {
	_, err := c.Command(unit, SetCur, StateControl, []byte{state})
	return err
}

// GetCardStatus は本体スロットの B-CAS カード挿入状態を返す（ISDBCASProp::GetCardStatus、1=挿入 / 0=未挿入）。
func (c *Client) GetCardStatus() (uint32, error) {
	resp, err := c.Command(UnitCAS, SetCur, CasGetCardStatus, make([]byte, 4))
	if err != nil {
		return 0, err
	}
	if len(resp) < 4 {
		return 0, fmt.Errorf("GetCardStatus の返事が短い（%d バイト）", len(resp))
	}
	return binary.LittleEndian.Uint32(resp[:4]), nil
}

// InitInternalBCAS はコールド起動時に本体内蔵 B-CAS カードの初期イベントとリクエストを行う。
func (c *Client) InitInternalBCAS() {
	st, err := c.GetCardStatus()
	if err != nil {
		return
	}
	var info [262]byte
	info[8] = 1
	info[16] = 8
	info[17] = 1
	info[24] = 1
	_, _ = c.Command(UnitCAS, SetCur, CasGetCardInfo, info[:])
	if st == 2 {
		return
	}

	var seq uint32
	for d := 0; d < 4; d++ {
		ev, err := c.Command(UnitCAS, GetCur, CasGetEvent, make([]byte, 540))
		if err != nil || len(ev) < 8 || binary.LittleEndian.Uint32(ev[4:8]) != 0 {
			break
		}
		seq++
		var comp [12]byte
		binary.LittleEndian.PutUint32(comp[4:8], seq)
		_, _ = c.Command(UnitCAS, SetCur, CasCompleteEvent, comp[:])
	}

	var req [272]byte
	binary.LittleEndian.PutUint32(req[4:8], 0x30)
	req[8] = 0x30
	if _, err := c.Command(UnitCAS, SetCur, CasSetRequest, req[:]); err != nil {
		return
	}

	Sleep(60)
	for tries := 0; tries < 20; tries++ {
		ev, err := c.Command(UnitCAS, GetCur, CasGetEvent, make([]byte, 540))
		if err == nil && len(ev) >= 8 && binary.LittleEndian.Uint32(ev[4:8]) == 0 {
			seq++
			var comp [12]byte
			binary.LittleEndian.PutUint32(comp[4:8], seq)
			_, _ = c.Command(UnitCAS, SetCur, CasCompleteEvent, comp[:])
			_, _ = c.Command(UnitCAS, GetCur, CasGetEvent, make([]byte, 540))
			break
		}
		Sleep(30)
	}
}

// PrepareStreamUnit は unit 8（TS 出力ポート）を Acquire→Stop→Probe→Acquire→Pause へ遷移させる。
func (c *Client) PrepareStreamUnit() {
	const port = 8
	_ = c.SetState(port, StateAcquire)
	_ = c.SetState(port, StateStop)
	probe := []byte{0x00, 0x00, 0x01, 0x01}
	_, _ = c.Command(port, SetCur, 1, probe)
	_, _ = c.Command(port, SetCur, 2, probe)
	_ = c.SetState(port, StateAcquire)
	_ = c.SetState(port, StatePause)
	_, _ = c.Command(UnitCAS, SetCur, 22, make([]byte, 8))
}

// Sleep は短く待つ。
func Sleep(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
