// SPDX-License-Identifier: AGPL-3.0-only

//go:build !darwin

package backend

import "errors"

// StartFileDrag stub for non-darwin platforms.
func (s *Service) StartFileDrag(remotePath, filename string, size int64) (bool, error) {
	return false, errors.New("native file drag is only supported on macOS")
}
