// SPDX-License-Identifier: AGPL-3.0-only

//go:build darwin

package backend

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework UniformTypeIdentifiers
#include <stdlib.h>
#include <stdint.h>
#include "file_drag_darwin.h"
*/
import "C"
import (
	"errors"
	"runtime/cgo"
	"unsafe"
)

//export goDownloadRemoteFile
func goDownloadRemoteFile(svcPtr C.uintptr_t, remotePathC, destPathC *C.char) *C.char {
	if svcPtr == 0 || remotePathC == nil || destPathC == nil {
		return C.CString("invalid parameters")
	}
	h := cgo.Handle(svcPtr)
	svc, ok := h.Value().(*Service)
	if !ok || svc == nil {
		return C.CString("invalid service handle")
	}
	remotePath := C.GoString(remotePathC)
	destPath := C.GoString(destPathC)
	if err := svc.DownloadFileToExactPath(remotePath, destPath); err != nil {
		return C.CString(err.Error())
	}
	return nil
}

// StartFileDrag initiates a native macOS dragging session using NSFilePromiseProvider.
// Finder requests and downloads the file directly to the dropped folder URL on demand.
func (s *Service) StartFileDrag(remotePath, filename string, size int64) (bool, error) {
	if !s.IsPaired() {
		return false, errors.New("phone is offline — reconnect first")
	}
	cRemote := C.CString(remotePath)
	defer C.free(unsafe.Pointer(cRemote))
	cName := C.CString(filename)
	defer C.free(unsafe.Pointer(cName))

	h := cgo.NewHandle(s)
	res := C.nativeStartFileDrag(C.uintptr_t(uintptr(h)), cRemote, cName, C.int64_t(size))
	return res != 0, nil
}
