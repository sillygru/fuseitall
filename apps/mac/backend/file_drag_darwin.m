// SPDX-License-Identifier: AGPL-3.0-only

#import <Cocoa/Cocoa.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import "file_drag_darwin.h"

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

extern char* goDownloadRemoteFile(uintptr_t svcPtr, char* remotePath, char* destPath);

@interface FuseItAllPromiseDelegate : NSObject <NSFilePromiseProviderDelegate, NSDraggingSource>
@property (nonatomic, assign) uintptr_t servicePtr;
@property (nonatomic, copy) NSString *remotePath;
@property (nonatomic, copy) NSString *filename;
@property (nonatomic, assign) int64_t fileSize;
@end

@implementation FuseItAllPromiseDelegate

- (NSString *)filePromiseProvider:(NSFilePromiseProvider *)filePromiseProvider fileNameForType:(NSString *)fileType {
    return self.filename ?: @"download";
}

- (void)filePromiseProvider:(NSFilePromiseProvider *)filePromiseProvider writePromiseToURL:(NSURL *)url completionHandler:(void (^)(NSError * _Nullable))completionHandler {
    char *remote = (char *)[self.remotePath UTF8String];
    char *dest = (char *)[[url path] UTF8String];
    char *errStr = goDownloadRemoteFile(self.servicePtr, remote, dest);
    if (errStr != NULL) {
        NSString *errNS = [NSString stringWithUTF8String:errStr];
        free(errStr);
        NSError *error = [NSError errorWithDomain:@"com.fuseitall.error" code:1 userInfo:@{NSLocalizedDescriptionKey: errNS}];
        completionHandler(error);
    } else {
        completionHandler(nil);
    }
}

- (NSOperationQueue *)operationQueueForFilePromiseProvider:(NSFilePromiseProvider *)filePromiseProvider {
    static NSOperationQueue *queue = nil;
    static dispatch_once_t onceToken;
    dispatch_once(&onceToken, ^{
        queue = [[NSOperationQueue alloc] init];
        queue.qualityOfService = NSQualityOfServiceUserInitiated;
    });
    return queue;
}

- (NSDragOperation)draggingSession:(NSDraggingSession *)session sourceOperationMaskForDraggingContext:(NSDraggingContext)context {
    return NSDragOperationCopy;
}

@end

int nativeStartFileDrag(uintptr_t svcPtr, const char *remotePathC, const char *filenameC, int64_t fileSize) {
    if (!remotePathC || !filenameC) {
        return 0;
    }
    NSString *remotePath = [NSString stringWithUTF8String:remotePathC];
    NSString *filename = [NSString stringWithUTF8String:filenameC];
    if (remotePath.length == 0 || filename.length == 0) {
        return 0;
    }

    __block int success = 0;
    void (^block)(void) = ^{
        NSEvent *event = [NSApp currentEvent];
        if (!event) {
            return;
        }
        if (event.type != NSEventTypeLeftMouseDown &&
            event.type != NSEventTypeLeftMouseDragged &&
            event.type != NSEventTypeOtherMouseDown &&
            event.type != NSEventTypeOtherMouseDragged) {
            return;
        }
        NSWindow *window = [event window] ?: [NSApp keyWindow] ?: [[NSApp windows] firstObject];
        if (!window) {
            return;
        }
        NSView *view = [window contentView];
        if (!view) {
            return;
        }

        FuseItAllPromiseDelegate *delegate = [[FuseItAllPromiseDelegate alloc] init];
        delegate.servicePtr = svcPtr;
        delegate.remotePath = remotePath;
        delegate.filename = filename;
        delegate.fileSize = fileSize;

        NSString *ext = [filename pathExtension];
        NSString *fileType = @"public.data";
        if (@available(macOS 11.0, *)) {
            UTType *ut = [UTType typeWithFilenameExtension:ext];
            if (ut && ut.identifier) {
                fileType = ut.identifier;
            }
        }

        NSFilePromiseProvider *provider = [[NSFilePromiseProvider alloc] initWithFileType:fileType delegate:delegate];
        NSDraggingItem *item = [[NSDraggingItem alloc] initWithPasteboardWriter:provider];

        NSPoint mouseLoc = [view convertPoint:[event locationInWindow] fromView:nil];
        NSImage *icon = nil;
        if (@available(macOS 12.0, *)) {
            UTType *ut = [UTType typeWithFilenameExtension:ext];
            if (ut) {
                icon = [[NSWorkspace sharedWorkspace] iconForContentType:ut];
            }
        }
        if (!icon) {
            icon = [[NSWorkspace sharedWorkspace] iconForFileType:ext];
        }
        [icon setSize:NSMakeSize(32, 32)];
        [item setDraggingFrame:NSMakeRect(mouseLoc.x - 16, mouseLoc.y - 16, 32, 32) contents:icon];

        [view beginDraggingSessionWithItems:@[item] event:event source:delegate];
        success = 1;
    };

    if ([NSThread isMainThread]) {
        block();
    } else {
        dispatch_sync(dispatch_get_main_queue(), block);
    }
    return success;
}

#pragma clang diagnostic pop
