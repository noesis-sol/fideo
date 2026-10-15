package fideo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFormatVideoInfo(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "clip.mov")
	if err := os.WriteFile(existing, make([]byte, 1536), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.mkv")

	tests := []struct {
		name string
		path string
		meta videoMetadata
		want string
	}{
		{
			name: "bitrate in Mbps, extension upper-cased, real size",
			path: existing,
			meta: videoMetadata{codec: "h264", width: "1920", height: "1080", bitrate: "4000000"},
			want: "1920x1080 | MOV (h264) | 4.0 Mbps | 1.5 KB",
		},
		{
			name: "N/A bitrate passed through",
			path: existing,
			meta: videoMetadata{codec: "hevc", width: "3840", height: "2160", bitrate: "N/A"},
			want: "3840x2160 | MOV (hevc) | N/A | 1.5 KB",
		},
		{
			name: "missing file shows unknown size",
			path: missing,
			meta: videoMetadata{codec: "vp9", width: "1280", height: "720", bitrate: "2500000"},
			want: "1280x720 | MKV (vp9) | 2.5 Mbps | unknown size",
		},
	}

	vs := newVideoService(defaultConfig)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vs.formatVideoInfo(tt.path, tt.meta); got != tt.want {
				t.Errorf("formatVideoInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536 * 1024, "1.5 MB"},
		{1 << 30, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatFileSize(tt.bytes); got != tt.want {
				t.Errorf("formatFileSize(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

// emptyPATH points PATH at an empty directory, so any attempt to exec ffprobe
// fails with a "not found" error instead of the empty-path one.
func emptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestProbeMetadataEmptyPath(t *testing.T) {
	emptyPATH(t)
	vs := newVideoService(defaultConfig)

	_, err := vs.probeMetadata(context.Background(), "")
	if err == nil || err.Error() != "path cannot be empty" {
		t.Fatalf("probeMetadata(\"\") error = %v, want %q", err, "path cannot be empty")
	}
}

func TestGetVideoInfoEmptyPath(t *testing.T) {
	emptyPATH(t)
	vs := newVideoService(defaultConfig)

	if got, want := vs.getVideoInfo(context.Background(), ""), "unable to read video info"; got != want {
		t.Errorf("getVideoInfo(\"\") = %q, want %q", got, want)
	}
}
