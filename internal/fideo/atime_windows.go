//go:build windows

package fideo

import (
	"os"
	"syscall"
	"time"
)

// accessTime returns fi's last-access time, or the zero time when the
// platform's stat data isn't available (os.Chtimes then leaves atime alone).
func accessTime(fi os.FileInfo) time.Time {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return time.Time{}
	}
	return time.Unix(0, d.LastAccessTime.Nanoseconds())
}
