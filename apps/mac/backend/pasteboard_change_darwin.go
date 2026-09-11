//go:build darwin

// SPDX-License-Identifier: AGPL-3.0-only

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
