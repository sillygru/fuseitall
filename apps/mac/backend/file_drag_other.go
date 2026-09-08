// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

//go:build !darwin

package backend

import "errors"

// StartFileDrag stub for non-darwin platforms.
func (s *Service) StartFileDrag(remotePath, filename string, size int64) (bool, error) {
	return false, errors.New("native file drag is only supported on macOS")
}
