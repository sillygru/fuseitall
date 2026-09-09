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
#cgo LDFLAGS: -framework MediaPlayer -framework Cocoa
#import <Cocoa/Cocoa.h>
#import <MediaPlayer/MPNowPlayingInfoCenter.h>
#import <MediaPlayer/MPMediaItem.h>

static void playbackSystemSetImpl(const char *title, const char *artist, const char *album, int playing, double position, double duration) {
    @autoreleasepool {
        NSMutableDictionary *info = [NSMutableDictionary dictionary];
        if (title && title[0]) info[MPMediaItemPropertyTitle] = [NSString stringWithUTF8String:title];
        if (artist && artist[0]) info[MPMediaItemPropertyArtist] = [NSString stringWithUTF8String:artist];
        if (album && album[0]) info[MPMediaItemPropertyAlbumTitle] = [NSString stringWithUTF8String:album];
        info[MPMediaItemPropertyPlaybackDuration] = @(duration);
        info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = @(position);
        info[MPNowPlayingInfoPropertyPlaybackRate] = @(playing ? 1.0 : 0.0);
        [[MPNowPlayingInfoCenter defaultCenter] setNowPlayingInfo:info];
    }
}

static void playbackSystemClearImpl(void) {
    @autoreleasepool {
        [[MPNowPlayingInfoCenter defaultCenter] setNowPlayingInfo:nil];
    }
}
*/
import "C"
import "unsafe"

// darwinSystemPlayback mirrors into Control Center Now Playing.
// Fail-soft: panics are recovered by the caller.
type darwinSystemPlayback struct{}

func (darwinSystemPlayback) SetState(v PlaybackView) {
	title := C.CString(v.Title)
	defer C.free(unsafe.Pointer(title))
	artist := C.CString(v.Artist)
	defer C.free(unsafe.Pointer(artist))
	album := C.CString(v.Album)
	defer C.free(unsafe.Pointer(album))
	playing := 0
	if v.State == "playing" {
		playing = 1
	}
	C.playbackSystemSetImpl(title, artist, album, C.int(playing), C.double(float64(v.PositionMs)/1000.0), C.double(float64(v.DurationMs)/1000.0))
}

func (darwinSystemPlayback) Clear() {
	C.playbackSystemClearImpl()
}

func init() {
	systemPlayback = darwinSystemPlayback{}
}
