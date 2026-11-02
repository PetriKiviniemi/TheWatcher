//go:build windows

package main

import (
	"bytes"
	"unsafe"

	"golang.org/x/sys/windows"
)

// protect encrypts data with DPAPI so only the current Windows user can decrypt it.
func protect(plain []byte) ([]byte, error) {
	return dpapi(plain, func(in, out *windows.DataBlob) error {
		return windows.CryptProtectData(in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, out)
	})
}

func unprotect(sealed []byte) ([]byte, error) {
	return dpapi(sealed, func(in, out *windows.DataBlob) error {
		return windows.CryptUnprotectData(in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, out)
	})
}

func dpapi(data []byte, call func(in, out *windows.DataBlob) error) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := call(&in, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return bytes.Clone(unsafe.Slice(out.Data, out.Size)), nil
}
