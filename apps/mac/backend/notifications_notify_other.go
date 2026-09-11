//go:build !darwin
// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Fallback for non-darwin builds (tests, linux CI): osascript path but
// still avoids shell interpolation. Banner attribution is irrelevant off-Mac.
func notifyUserInternal(title, body, _ string) {
	if strings.TrimSpace(title) == "" && strings.TrimSpace(body) == "" {
		return
	}
	if strings.TrimSpace(title) == "" {
		title = "FuseItAll"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e",
		`on run argv
  display notification (item 2 of argv) with title (item 1 of argv)
end run`, title, body)
	_ = cmd.Run()
}
