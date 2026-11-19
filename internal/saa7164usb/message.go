// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package saa7164usb は GV-MVP/VZ の中の NXP SAA7164（USB 版）に命令を送る。
//
// 命令の頭は Linux の drivers/media/pci/saa7164 の struct tmComResInfo と同じ 11 バイト
// （id・flags・size・command・controlselector・seqno、リトルエンディアン）。
// USB ではこの頭のあとにデータを付けて MSG BULK OUT に送り、返事は MSG INT IN に同じ形で来る
// （メーカーのドライバーの UsbMessage::SendMessage・GetMessage を読んで確かめた。1 回は最大 0x10b バイト）。
package saa7164usb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// 命令の種類（tmComResCmd）
const (
	SetCur = 0x01
	GetCur = 0x81
	GetLen = 0x85
)

// 機器全体（unit 0）への controlselector（saa7164-reg.h）
const (
	GetFWStatusControl  = 0x08
	GetFWVersionControl = 0x09
)

// HeaderSize は命令の頭の長さ。
const HeaderSize = 11

// MaxMessage は 1 回に送れる・受けられる長さ（頭を含む）。
const MaxMessage = 0x10b

// Header は tmComResInfo。
type Header struct {
	ID              uint8
	Flags           uint8
	Size            uint16
	Command         uint32
	ControlSelector uint16
	SeqNo           uint8
}

// Marshal は頭とデータを 1 つの送る塊にする。
func (h Header) Marshal(data []byte) ([]byte, error) {
	if int(h.Size) != len(data) {
		return nil, fmt.Errorf("頭の size（%d）とデータの長さ（%d）が合わない", h.Size, len(data))
	}
	if HeaderSize+len(data) > MaxMessage {
		return nil, fmt.Errorf("命令が長すぎる（%d バイト）", HeaderSize+len(data))
	}
	b := make([]byte, HeaderSize+len(data))
	b[0] = h.ID
	b[1] = h.Flags
	binary.LittleEndian.PutUint16(b[2:], h.Size)
	binary.LittleEndian.PutUint32(b[4:], h.Command)
	binary.LittleEndian.PutUint16(b[8:], h.ControlSelector)
	b[10] = h.SeqNo
	copy(b[HeaderSize:], data)
	return b, nil
}

// Unmarshal は受け取った塊を頭とデータに分ける。
func Unmarshal(b []byte) (Header, []byte, error) {
	if len(b) < HeaderSize {
		return Header{}, nil, errors.New("返事が頭より短い")
	}
	h := Header{
		ID:              b[0],
		Flags:           b[1],
		Size:            binary.LittleEndian.Uint16(b[2:]),
		Command:         binary.LittleEndian.Uint32(b[4:]),
		ControlSelector: binary.LittleEndian.Uint16(b[8:]),
		SeqNo:           b[10],
	}
	data := b[HeaderSize:]
	if int(h.Size) < len(data) {
		data = data[:h.Size]
	}
	return h, data, nil
}
