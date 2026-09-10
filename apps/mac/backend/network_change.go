//go:build darwin

// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

/*
#cgo LDFLAGS: -framework Network
#include <Network/Network.h>
#include <dispatch/dispatch.h>

extern void fuse_path_callback(int up);

static void fuse_start_path_monitor(void) {
    nw_path_monitor_t monitor = nw_path_monitor_create();
    nw_path_monitor_set_queue(monitor, dispatch_get_main_queue());
    nw_path_monitor_set_update_handler(monitor, ^(nw_path_t path) {
        fuse_path_callback(nw_path_get_status(path) == nw_path_status_satisfied ? 1 : 0);
    });
    nw_path_monitor_start(monitor);
    // The Network framework retains the monitor after start. It lives for
    // the process lifetime and exits with the Wails application.
}
*/
import "C"

import (
	"log/slog"
	"sync/atomic"
)

var networkPathCallback func(bool)
var networkPathState atomic.Int32

// StartNetworkPathMonitor forwards macOS reachability changes into Go.
// The callback is edge-triggered and does not poll the network.
func StartNetworkPathMonitor(s *Service, logger *slog.Logger) {
	if s == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	networkPathCallback = func(up bool) {
		current := int32(0)
		if up {
			current = 1
		}
		if networkPathState.Swap(current) == current {
			return
		}
		if !up {
			_, _ = s.NotifyLocalNetworkDown()
			return
		}
		logger.Debug("local network path restored")
	}
	C.fuse_start_path_monitor()
}

//export fuse_path_callback
func fuse_path_callback(up C.int) {
	if networkPathCallback != nil {
		networkPathCallback(up != 0)
	}
}
