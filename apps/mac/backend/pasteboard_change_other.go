//go:build !darwin

// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

// getPasteboardChangeCount stub for non-darwin (CI/tests).
func getPasteboardChangeCount() int { return 0 }
