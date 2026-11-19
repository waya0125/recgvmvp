// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package usbfs は Linux の usbfs（/dev/bus/usb/BBB/DDD）を ioctl で直接使う。
// libusb も cgo も使わない。
//
// 使うのは、機器の記述子を読むこと、インターフェースを取ること、
// 制御転送、URB を何個も投げておくバルクの受信（Stream）だけ。
package usbfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"unsafe"
)

// ioctl の番号は <linux/usbdevice_fs.h> と同じ作り方で、構造体の大きさから出す。
const (
	iocNone  = 0
	iocWrite = 1
	iocRead  = 2
)

func ioc(dir, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | uintptr('U')<<8 | nr
}

// usbdevfs_ctrltransfer
type ctrlTransfer struct {
	requestType uint8
	request     uint8
	value       uint16
	index       uint16
	length      uint16
	timeout     uint32
	data        uintptr
}

// usbdevfs_urb（iso_frame_desc は使わないので持たない）
type urb struct {
	typ             uint8
	endpoint        uint8
	status          int32
	flags           uint32
	buffer          uintptr
	bufferLength    int32
	actualLength    int32
	startFrame      int32
	numberOfPackets int32
	errorCount      int32
	signr           uint32
	userContext     uintptr
}

var (
	ioctlControl          = ioc(iocRead|iocWrite, 0, unsafe.Sizeof(ctrlTransfer{}))
	ioctlSubmitURB        = ioc(iocRead, 10, unsafe.Sizeof(urb{}))
	ioctlDiscardURB       = ioc(iocNone, 11, 0)
	ioctlReapURB          = ioc(iocWrite, 12, unsafe.Sizeof(uintptr(0)))
	ioctlClaimInterface   = ioc(iocRead, 15, unsafe.Sizeof(uint32(0)))
	ioctlReleaseInterface = ioc(iocRead, 16, unsafe.Sizeof(uint32(0)))
	ioctlClearHalt        = ioc(iocRead, 21, unsafe.Sizeof(uint32(0)))
	ioctlSetInterface     = ioc(iocRead, 4, unsafe.Sizeof([2]uint32{}))
	ioctlSetConfiguration = ioc(iocRead, 5, unsafe.Sizeof(uint32(0)))
	ioctlReset            = ioc(iocNone, 20, 0)
)

const urbTypeBulk = 3

func ioctl(fd int, request, arg uintptr) (uintptr, error) {
	for {
		r, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), request, arg)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return 0, errno
		}
		return r, nil
	}
}

// ID は USB のベンダーと製品の番号。
type ID struct{ Vendor, Product uint16 }

func (id ID) String() string { return fmt.Sprintf("%04x:%04x", id.Vendor, id.Product) }

// DeviceRoot は usbfs の機器ファイルの置き場。試験で差し替える。
var DeviceRoot = "/dev/bus/usb"

// Find は id の機器ファイルを、バス・アドレスの順に返す。
func Find(id ID) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(DeviceRoot, "[0-9][0-9][0-9]", "[0-9][0-9][0-9]"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var found []string
	for _, path := range paths {
		got, err := ReadID(path)
		if err == nil && got == id {
			found = append(found, path)
		}
	}
	return found, nil
}

// ReadID は機器ファイルの先頭にある機器記述子から ID を読む（開く権限がなくても読める）。
func ReadID(path string) (ID, error) {
	id, _, err := ReadDeviceInfo(path)
	return id, err
}

// ReadDeviceInfo は機器記述子から ID と bcdDevice を読む。
func ReadDeviceInfo(path string) (ID, uint16, error) {
	f, err := os.Open(path)
	if err != nil {
		return ID{}, 0, err
	}
	defer f.Close()
	var desc [18]byte
	if n, err := f.Read(desc[:]); err != nil || n < len(desc) {
		return ID{}, 0, fmt.Errorf("%s: 機器記述子を読めない", path)
	}
	return ID{
		Vendor:  uint16(desc[8]) | uint16(desc[9])<<8,
		Product: uint16(desc[10]) | uint16(desc[11])<<8,
	}, uint16(desc[12]) | uint16(desc[13])<<8, nil
}

// Device は開いた USB 機器。
type Device struct {
	path    string
	fd      int
	claimed []uint32
}

// Open は機器ファイルを読み書きで開く。
func Open(path string) (*Device, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%s を開けない: %w", path, err)
	}
	return &Device{path: path, fd: fd}, nil
}

// Path は機器ファイルの場所。
func (d *Device) Path() string { return d.path }

// Claim はインターフェースを取る。ほかのプロセスが使っていれば EBUSY で失敗する。
func (d *Device) Claim(number uint32) error {
	n := number
	if _, err := ioctl(d.fd, ioctlClaimInterface, uintptr(unsafe.Pointer(&n))); err != nil {
		return fmt.Errorf("%s のインターフェース %d を取れない: %w", d.path, number, err)
	}
	d.claimed = append(d.claimed, number)
	return nil
}

// SetInterface はインターフェースの代わりの設定を選ぶ。
func (d *Device) SetInterface(number, alternate uint32) error {
	arg := [2]uint32{number, alternate}
	if _, err := ioctl(d.fd, ioctlSetInterface, uintptr(unsafe.Pointer(&arg))); err != nil {
		return fmt.Errorf("%s のインターフェース %d の設定 %d を選べない: %w", d.path, number, alternate, err)
	}
	return nil
}

// ClearHalt はエンドポイントの停止を解く。
func (d *Device) ClearHalt(endpoint uint8) error {
	n := uint32(endpoint)
	_, err := ioctl(d.fd, ioctlClearHalt, uintptr(unsafe.Pointer(&n)))
	return err
}

// SetConfiguration は USB コンフィギュレーションを選択する。
func (d *Device) SetConfiguration(cfg uint32) error {
	c := cfg
	_, err := ioctl(d.fd, ioctlSetConfiguration, uintptr(unsafe.Pointer(&c)))
	return err
}

// Reset は USB ポートリセットを発行する。
func (d *Device) Reset() error {
	_, err := ioctl(d.fd, ioctlReset, 0)
	return err
}

// Release は Claim したインターフェースを解放する。
func (d *Device) Release(number uint32) error {
	n := number
	_, err := ioctl(d.fd, ioctlReleaseInterface, uintptr(unsafe.Pointer(&n)))
	for i, v := range d.claimed {
		if v == number {
			d.claimed = append(d.claimed[:i], d.claimed[i+1:]...)
			break
		}
	}
	return err
}

// Control は制御転送を 1 回行い、受け渡したバイト数を返す。
// requestType の bit 7 が立っていれば、data に受け取る。
func (d *Device) Control(requestType, request uint8, value, index uint16, data []byte, timeoutMS uint32) (int, error) {
	if len(data) > 0xffff {
		return 0, errors.New("制御転送のデータが長すぎる")
	}
	var buffer []byte
	if len(data) > 0 {
		buffer = data
	}
	xfer := ctrlTransfer{
		requestType: requestType, request: request, value: value, index: index,
		length: uint16(len(data)), timeout: timeoutMS,
	}
	if buffer != nil {
		xfer.data = uintptr(unsafe.Pointer(&buffer[0]))
	}
	n, err := ioctl(d.fd, ioctlControl, uintptr(unsafe.Pointer(&xfer)))
	// 渡したポインターは ioctl の間だけ使われる
	runtime.KeepAlive(buffer)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// Close はインターフェースを返して閉じる。
func (d *Device) Close() error {
	for _, number := range d.claimed {
		n := number
		_, _ = ioctl(d.fd, ioctlReleaseInterface, uintptr(unsafe.Pointer(&n)))
	}
	d.claimed = nil
	return syscall.Close(d.fd)
}
