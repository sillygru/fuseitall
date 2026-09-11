//go:build !darwin

// SPDX-License-Identifier: AGPL-3.0-only

package backend

// getPasteboardChangeCount stub for non-darwin (CI/tests).
func getPasteboardChangeCount() int { return 0 }
