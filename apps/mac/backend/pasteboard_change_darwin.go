//go:build darwin

// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
int pasteboardChangeCount(void) {
    return (int)[[NSPasteboard generalPasteboard] changeCount];
}
*/
import "C"

// getPasteboardChangeCount returns NSPasteboard.generalPasteboard.changeCount
// (stable AppKit API since 10.0). Used as cheap guard before shelling out.
// Returns 0 on failure so callers treat it as changed and read once.
func getPasteboardChangeCount() int {
	return int(C.pasteboardChangeCount())
}
