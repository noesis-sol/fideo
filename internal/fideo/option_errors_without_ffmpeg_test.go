//go:build integration

package fideo

import "testing"

// TestOptionErrorsWithoutFFmpeg pins that invalid --format/--size/--hw values
// are reported as usage errors on a machine without ffmpeg, rather than being
// hidden behind the missing-ffmpeg install dialog: buildConfig runs before the
// dependency check.
func TestOptionErrorsWithoutFFmpeg(t *testing.T) {
	bin := buildFideo(t)
	noTools := t.TempDir()

	tests := []struct {
		name       string
		args       []string
		wantStderr []string
	}{
		{"bogus format", []string{"--format", "foo"},
			[]string{`Error: unsupported format "foo"`, "Supported formats: mp4, mov, mkv, webm"}},
		{"bogus size", []string{"--size", "bogus"},
			[]string{`Error: unsupported size "bogus"`, "Supported sizes: sm, small, md, medium, lg, large"}},
		{"hw with webm", []string{"--hw", "--format", "webm"},
			[]string{"Error: --hw is not compatible with --format webm"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runFideo(t, bin, t.TempDir(), noTools, tt.args...)
			checkCLIResult(t, got, 1, nil, tt.wantStderr)
			if got.stdout != "" {
				t.Errorf("stdout = %q, want empty on a usage error", got.stdout)
			}
		})
	}
}
