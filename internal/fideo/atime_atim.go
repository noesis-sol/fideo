//go:build linux || openbsd

package fideo

import (
	"os"
	"syscall"
	"time"
)

// accessTime returns fi's last-access time, or the zero time when the
// platform's stat data isn't available (os.Chtimes then leaves atime alone).
func accessTime(fi os.FileInfo) time.Time {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}
	}
	return time.Unix(st.Atim.Unix())
}
