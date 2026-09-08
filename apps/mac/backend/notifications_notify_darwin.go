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
#import <objc/runtime.h>

@interface FuseItAllNotifDelegate : NSObject <NSUserNotificationCenterDelegate>
@end

@implementation FuseItAllNotifDelegate
- (BOOL)userNotificationCenter:(NSUserNotificationCenter *)center shouldPresentNotification:(NSUserNotification *)notification {
    return YES;
}
@end

@interface NSBundle (FuseItAllFakeBundle)
- (NSString *)__fuseitallBundleId;
@end

@implementation NSBundle (FuseItAllFakeBundle)
- (NSString *)__fuseitallBundleId {
    if (self == [NSBundle mainBundle]) {
        return @"com.fuseitall.mac";
    }
    return [self __fuseitallBundleId];
}
@end

static void ensureBundleIdentifier(void) {
    static dispatch_once_t onceToken;
    dispatch_once(&onceToken, ^{
        NSString *bundleId = [[NSBundle mainBundle] bundleIdentifier];
        if (!bundleId || bundleId.length == 0) {
            Class cls = [NSBundle class];
            Method original = class_getInstanceMethod(cls, @selector(bundleIdentifier));
            Method swizzled = class_getInstanceMethod(cls, @selector(__fuseitallBundleId));
            if (original && swizzled) {
                method_exchangeImplementations(original, swizzled);
            }
        }
    });
}

static int postNotif(const char *titleC, const char *bodyC, const char *iconB64C) {
    @autoreleasepool {
        @try {
            ensureBundleIdentifier();
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

            if (iconB64C && strlen(iconB64C) > 0) {
                NSString *b64Str = [NSString stringWithUTF8String:iconB64C];
                NSData *data = [[NSData alloc] initWithBase64EncodedString:b64Str options:NSDataBase64DecodingIgnoreUnknownCharacters];
                if (data && data.length > 0) {
                    NSImage *img = [[NSImage alloc] initWithData:data];
                    if (img) {
                        @try { [n setValue:img forKey:@"_identityImage"]; } @catch (NSException *e) {}
                        @try { [n setValue:@NO forKey:@"_identityImageHasBorder"]; } @catch (NSException *e) {}
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
	"fmt"
	"os/exec"
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
	cTitle := C.CString(title)
	cBody := C.CString(body)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cBody))

	trimmedIcon := strings.TrimSpace(iconB64)
	var cIcon *C.char
	if trimmedIcon != "" {
		cIcon = C.CString(trimmedIcon)
		defer C.free(unsafe.Pointer(cIcon))
	}

	if C.postNotif(cTitle, cBody, cIcon) == 0 {
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
