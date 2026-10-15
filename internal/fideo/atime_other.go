//go:build !(darwin || freebsd || netbsd || linux || openbsd || windows)

package fideo

import (
	"os"
	"time"
)

// accessTime has no portable source on this platform; the zero time makes
// os.Chtimes leave the access time alone.
func accessTime(os.FileInfo) time.Time { return time.Time{} }
