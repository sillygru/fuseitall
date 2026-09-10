//go:build !darwin

// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import "log/slog"

// StartNetworkPathMonitor is unavailable outside macOS; the frontend online
// event remains the fallback for development and cross-compilation builds.
func StartNetworkPathMonitor(_ *Service, _ *slog.Logger) {}
