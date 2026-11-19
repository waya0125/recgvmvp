// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package usbfs

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// BulkStream は usbfs の非同期 URB を複数投入し、取りこぼしなくバルク IN 受信を行う。
type BulkStream struct {
	dev      *Device
	endpoint uint8
	urbs     []urb
	bufs     [][]byte
	pinner   runtime.Pinner
	onData   func([]byte)

	mu     sync.Mutex
	active int
	stop   atomic.Bool
	gone   atomic.Bool
	done   chan struct{}
}

// StartBulkStream は numURBs 本の非同期バルク IN URB を投入して受信スレッドを開始する。
func (d *Device) StartBulkStream(endpoint uint8, numURBs, bufSize int, onData func([]byte)) (*BulkStream, error) {
	if numURBs <= 0 || bufSize <= 0 {
		return nil, errors.New("URB 数またはバッファ長が不正")
	}
	s := &BulkStream{
		dev:      d,
		endpoint: endpoint,
		urbs:     make([]urb, numURBs),
		bufs:     make([][]byte, numURBs),
		onData:   onData,
		done:     make(chan struct{}),
	}

	for i := 0; i < numURBs; i++ {
		buf := make([]byte, bufSize)
		s.bufs[i] = buf
		s.pinner.Pin(&buf[0])
		s.pinner.Pin(&s.urbs[i])

		s.urbs[i] = urb{
			typ:          urbTypeBulk,
			endpoint:     endpoint,
			buffer:       uintptr(unsafe.Pointer(&buf[0])),
			bufferLength: int32(bufSize),
			userContext:  uintptr(i),
		}
		if _, err := ioctl(d.fd, ioctlSubmitURB, uintptr(unsafe.Pointer(&s.urbs[i]))); err != nil {
			s.stop.Store(true)
			s.discardAll()
			s.reapRemaining(i)
			s.pinner.Unpin()
			return nil, fmt.Errorf("URB %d の投入に失敗: %w", i, err)
		}
		s.active++
	}

	go s.loop()
	return s, nil
}

// DeviceGone はデバイス抜去（ENODEV）が検出されたかを返す。
func (s *BulkStream) DeviceGone() bool {
	return s.gone.Load()
}

func (s *BulkStream) loop() {
	defer close(s.done)
	var consecErr int
	for {
		s.mu.Lock()
		rem := s.active
		s.mu.Unlock()
		if rem <= 0 {
			return
		}

		var reapPtr unsafe.Pointer
		_, err := ioctl(s.dev.fd, ioctlReapURB, uintptr(unsafe.Pointer(&reapPtr)))
		if err != nil {
			if errors.Is(err, syscall.ENODEV) {
				s.gone.Store(true)
				return
			}
			if s.stop.Load() {
				return
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if reapPtr == nil {
			continue
		}

		u := (*urb)(reapPtr)
		idx := int(u.userContext)

		if u.status == 0 || u.status == -int32(syscall.ETIMEDOUT) || u.status == -int32(syscall.EILSEQ) {
			if u.actualLength > 0 && s.onData != nil {
				s.onData(s.bufs[idx][:u.actualLength])
			}
			consecErr = 0
		} else if u.status == -int32(syscall.ENODEV) || u.status == -int32(syscall.ESHUTDOWN) {
			s.gone.Store(true)
			s.mu.Lock()
			s.active--
			s.mu.Unlock()
			continue
		} else if u.status != -int32(syscall.ENOENT) && u.status != -int32(syscall.ECONNRESET) {
			consecErr++
			if consecErr >= 100 {
				s.stop.Store(true)
			}
		}

		if !s.stop.Load() && !s.gone.Load() {
			u.status = 0
			u.actualLength = 0
			if _, err := ioctl(s.dev.fd, ioctlSubmitURB, uintptr(unsafe.Pointer(u))); err == nil {
				continue
			}
		}

		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}
}

func (s *BulkStream) discardAll() {
	for i := range s.urbs {
		_, _ = ioctl(s.dev.fd, ioctlDiscardURB, uintptr(unsafe.Pointer(&s.urbs[i])))
	}
}

func (s *BulkStream) reapRemaining(count int) {
	for i := 0; i < count; i++ {
		var reapPtr uintptr
		if _, err := ioctl(s.dev.fd, ioctlReapURB, uintptr(unsafe.Pointer(&reapPtr))); err != nil {
			break
		}
	}
}

// Stop は全 URB をキャンセルして受信スレッドの終了を待つ。
func (s *BulkStream) Stop() {
	if s == nil {
		return
	}
	s.stop.Store(true)
	s.discardAll()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
	s.pinner.Unpin()
}
