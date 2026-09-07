//go:build darwin
// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Darwin bundle-attributed notifier. Uses NSUserNotificationCenter so the
// banner is attributed to com.fuseitall.mac (FuseItAll) instead of
// osascript/Script Editor. Best-effort: any failure is silent. Icon data is
// decoded to a temp PNG and set as contentImage via KVC so system
// notification shows the source app's icon per row (fallback: bundle icon).

package backend

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
#import <Cocoa/Cocoa.h>

@interface FuseItAllNotifDelegate : NSObject <NSUserNotificationCenterDelegate>
@end

@implementation FuseItAllNotifDelegate
- (BOOL)userNotificationCenter:(NSUserNotificationCenter *)center shouldPresentNotification:(NSUserNotification *)notification {
    return YES;
}
@end

static int postNotif(const char *titleC, const char *bodyC, const char *iconPathC) {
    @autoreleasepool {
        @try {
            NSString *bundleId = [[NSBundle mainBundle] bundleIdentifier];
            if (!bundleId || bundleId.length == 0) {
                return 0; // Not running inside an app bundle, trigger fallback
            }
            NSUserNotificationCenter *center = [NSUserNotificationCenter defaultUserNotificationCenter];
            if (!center) {
                return 0;
            }
            static FuseItAllNotifDelegate *delegate = nil;
            static dispatch_once_t onceToken;
            dispatch_once(&onceToken, ^{
                delegate = [[FuseItAllNotifDelegate alloc] init];
            });
            center.delegate = delegate;

            NSString *title = titleC ? [NSString stringWithUTF8String:titleC] : @"FuseItAll";
            NSString *body = bodyC ? [NSString stringWithUTF8String:bodyC] : @"";
            if (title.length == 0) title = @"FuseItAll";
            NSUserNotification *n = [[NSUserNotification alloc] init];
            n.title = title;
            if (body.length > 0) n.informativeText = body;
            n.soundName = NSUserNotificationDefaultSoundName;
            if (iconPathC) {
                NSString *path = [NSString stringWithUTF8String:iconPathC];
                if (path.length > 0) {
                    NSImage *img = [[NSImage alloc] initWithContentsOfFile:path];
                    if (img) {
                        // contentImage is private KVC but widely used for per-notification icons
                        @try { [n setValue:img forKey:@"_identityImage"]; } @catch (NSException *e) {}
                        @try { [n setValue:img forKey:@"contentImage"]; } @catch (NSException *e) {}
                    }
                }
            }
            [center deliverNotification:n];
            return 1;
        } @catch (NSException *e) {
            return 0;
        }
    }
}
#pragma clang diagnostic pop
*/
import "C"

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"
)

func notifyUserInternal(title, body, iconB64 string) {
	if strings.TrimSpace(title) == "" && strings.TrimSpace(body) == "" {
		return
	}
	if strings.TrimSpace(title) == "" {
		title = "FuseItAll"
	}
	iconPath := ""
	if strings.TrimSpace(iconB64) != "" {
		if p, ok := decodeIconToTemp(iconB64); ok {
			iconPath = p
			defer os.Remove(p)
		}
	}
	cTitle := C.CString(title)
	cBody := C.CString(body)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cBody))
	var cPath *C.char
	if iconPath != "" {
		cPath = C.CString(iconPath)
		defer C.free(unsafe.Pointer(cPath))
	}
	if C.postNotif(cTitle, cBody, cPath) == 0 {
		fallbackNotify(title, body)
	}
}

func fallbackNotify(title, body string) {
	safeTitle := strings.ReplaceAll(strings.ReplaceAll(title, "\\", "\\\\"), "\"", "\\\"")
	safeBody := strings.ReplaceAll(strings.ReplaceAll(body, "\\", "\\\\"), "\"", "\\\"")
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, safeBody, safeTitle)
	cmd := exec.Command("osascript", "-e", script)
	_ = cmd.Run()
}

func decodeIconToTemp(b64 string) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", false
	}
	if len(raw) == 0 || len(raw) > 64*1024 {
		return "", false
	}
	// PNG magic check: 89 50 4E 47 0D 0A 1A 0A
	if len(raw) < 8 || raw[0] != 0x89 || raw[1] != 0x50 || raw[2] != 0x4E || raw[3] != 0x47 {
		return "", false
	}
	dir := os.TempDir()
	f, err := os.CreateTemp(dir, "fuseitall-notif-*.png")
	if err != nil {
		return "", false
	}
	p := f.Name()
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(p)
		return "", false
	}
	f.Close()
	// Ensure extension stays .png for NSImage init.
	if filepath.Ext(p) != ".png" {
		np := p + ".png"
		_ = os.Rename(p, np)
		p = np
	}
	return p, true
}
