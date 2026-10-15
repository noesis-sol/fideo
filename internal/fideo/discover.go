package fideo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var videoExtensions = map[string]bool{
	".mp4":  true,
	".mov":  true,
	".avi":  true,
	".mkv":  true,
	".m4v":  true,
	".webm": true,
}

// fileStatus is a file's lifecycle state within a batch. statusPending is the
// zero value, so a freshly discovered videoFile starts unprocessed.
type fileStatus int

const (
	statusPending    fileStatus = iota // not yet started
	statusProcessing                   // ffmpeg running
	statusDone                         // finished successfully
	statusError                        // failed
)

type videoFile struct {
	path     string
	name     string
	selected bool
	explicit bool       // named on the command line, not found by a scan or pattern
	status   fileStatus // lifecycle state (statusPending is the zero value)
	progress float64
	info     string
	outInfo  string
	outPath  string // resolved, collision-free output path (assigned when queued)
	note     string // why a done file has no fresh output (e.g. original kept)
	err      error  // Error for this specific file
}

// safeArgPath guards a relative path against being misread by ffmpeg/ffprobe.
// Both tools take a leading "-" as an option (ffprobe reads its input as a
// positional argument, unlike ffmpeg's -i), and both parse a "NAME:" prefix made
// only of URL-scheme characters ([A-Za-z0-9+.-]) as a protocol, so "12:30.mp4"
// fails with "Protocol not found". A relative path that begins with "-" or whose
// first element contains ":" is prefixed with "./": the "/" ends the would-be
// scheme, so the tool opens it as a plain file. Absolute paths, paths with a
// volume name (Windows), and every other relative path are returned unchanged.
func safeArgPath(p string) string {
	if p == "" || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return p
	}
	first, _, _ := strings.Cut(filepath.ToSlash(p), "/")
	if !strings.HasPrefix(p, "-") && !strings.Contains(first, ":") {
		return p
	}
	return "." + string(filepath.Separator) + p
}

func findVideos(dir string) []videoFile {
	var files []videoFile

	if dir == "" {
		return files
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return files
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}

	sources := videoStems(entries)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !videoExtensions[ext] {
			continue
		}

		// Skip hidden files: macOS AppleDouble "._clip.mov" sidecars (left on
		// exFAT camera cards and SMB shares, never a playable video) and the
		// scratch temps a crashed in-place encode may have left behind.
		if isHidden(name) || strings.Contains(name, tempOutputMarker) {
			continue
		}

		// Skip fideo's own outputs, but only if a corresponding source exists.
		if isOwnOutput(name, sources) {
			continue
		}

		files = append(files, videoFile{
			path: safeArgPath(filepath.Join(dir, name)),
			name: name,
		})
	}
	return files
}

// isHidden reports whether a file name is a dotfile, which a directory scan
// skips the way a shell's "*" does.
func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

// videoStems maps the stem of every video file among entries to the set of its
// lowercased extensions (e.g. "clip" -> {".mov", ".mp4"}).
func videoStems(entries []os.DirEntry) map[string]map[string]bool {
	stems := make(map[string]map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if !videoExtensions[strings.ToLower(ext)] {
			continue
		}
		stem := strings.TrimSuffix(name, ext)
		if stems[stem] == nil {
			stems[stem] = make(map[string]bool)
		}
		stems[stem][strings.ToLower(ext)] = true
	}
	return stems
}

// isOwnOutput reports whether name is an output fideo would write for one of
// the videos in sources (see videoStems): out_<stem>.<ext> (getOutputPath),
// or one resolveOutputPath disambiguated within a batch, out_<stem>_<srcext>.<ext>
// or out_<stem>-<n>.<ext>. An out_ file whose source is gone is not one, so
// it can still be listed and compressed.
func isOwnOutput(name string, sources map[string]map[string]bool) bool {
	rest, ok := strings.CutPrefix(name, outputPrefix)
	if !ok {
		return false
	}
	stem := strings.TrimSuffix(rest, filepath.Ext(rest))
	if len(sources[stem]) > 0 {
		return true
	}
	if i := strings.LastIndex(stem, "_"); i >= 0 {
		if srcExt := "." + stem[i+1:]; videoExtensions[srcExt] && sources[stem[:i]][srcExt] {
			return true
		}
	}
	if i := strings.LastIndex(stem, "-"); i >= 0 && isCounter(stem[i+1:]) {
		return len(sources[stem[:i]]) > 0
	}
	return false
}

// isCounter reports whether s is a positive decimal counter, as
// resolveOutputPath appends ("1", "2", ...).
func isCounter(s string) bool {
	if s == "" || s[0] == '0' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func collectVideosFromPattern(pattern string) ([]videoFile, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("error expanding pattern: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no files or directories match pattern: %s", pattern)
	}

	var allFiles []videoFile
	seen := make(map[string]bool)
	for _, match := range matches {
		// Like a shell, "*" doesn't reach dotfiles (AppleDouble "._clip.mov"
		// sidecars, .Trashes): only a pattern whose last element starts with "."
		// does.
		if isHidden(filepath.Base(match)) && !isHidden(filepath.Base(pattern)) {
			continue
		}
		info, err := os.Stat(match)
		if err != nil {
			continue
		}

		if info.IsDir() {
			dirFiles := findVideos(match)
			for _, f := range dirFiles {
				if !seen[f.path] {
					seen[f.path] = true
					allFiles = append(allFiles, f)
				}
			}
			continue
		}

		ext := strings.ToLower(filepath.Ext(match))
		if !videoExtensions[ext] {
			continue
		}
		absPath, err := filepath.Abs(match)
		if err != nil {
			continue
		}
		if !seen[absPath] {
			seen[absPath] = true
			allFiles = append(allFiles, videoFile{
				path: absPath,
				name: filepath.Base(match),
			})
		}
	}

	if len(allFiles) == 0 {
		return nil, fmt.Errorf("no video files found matching pattern: %s", pattern)
	}
	return allFiles, nil
}
