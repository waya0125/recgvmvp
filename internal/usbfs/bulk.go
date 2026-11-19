// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package usbfs

import (
	"errors"
	"runtime"
	"unsafe"
)

// usbdevfs_bulktransfer
type bulkTransfer struct {
	endpoint uint32
	length   uint32
	timeout  uint32
	data     uintptr
}

var ioctlBulk = ioc(iocRead|iocWrite, 2, unsafe.Sizeof(bulkTransfer{}))

// Transfer はバルクか割り込みのエンドポイントで 1 回だけ受け渡しをして、受け渡したバイト数を返す。
// endpoint の bit 7 が立っていれば受け取る（IN）。割り込みのエンドポイントにも使える。
func (d *Device) Transfer(endpoint uint8, data []byte, timeoutMS uint32) (int, error) {
	if len(data) == 0 {
		return 0, errors.New("受け渡しの長さが 0")
	}
	xfer := bulkTransfer{
		endpoint: uint32(endpoint),
		length:   uint32(len(data)),
		timeout:  timeoutMS,
		data:     uintptr(unsafe.Pointer(&data[0])),
	}
	n, err := ioctl(d.fd, ioctlBulk, uintptr(unsafe.Pointer(&xfer)))
	runtime.KeepAlive(data)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}
