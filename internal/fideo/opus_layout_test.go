package fideo

import (
	"slices"
	"testing"
)

// TestOpusAudioRemapsChannelLayout pins that only the Opus (webm) encode carries
// the aformat that maps the source's channel layout onto one libopus accepts: a
// 5.1(side) AC3/DTS track fails to open the encoder without it, while AAC takes
// every layout as it is and needs no filter.
func TestOpusAudioRemapsChannelLayout(t *testing.T) {
	const aformat = "aformat=channel_layouts=mono|stereo|3.0|quad|5.0|5.1|6.1|7.1"
	tests := []struct {
		out      string
		wantAF   bool
		audioArg string
	}{
		{"out_video.webm", true, "libopus"},
		{"out_video.mp4", false, "aac"},
		{"out_video.mov", false, "aac"},
		{"out_video.mkv", false, "aac"},
	}
	for _, tt := range tests {
		t.Run(containerOf(tt.out), func(t *testing.T) {
			vs := newVideoService(testConfig())
			args := vs.ffmpegArgs("input.mkv", tt.out, videoMetadata{codec: "h264", height: "1080"}, 1)

			af := slices.Index(args, "-af")
			if !tt.wantAF {
				if af >= 0 {
					t.Fatalf("unexpected -af in %v", args)
				}
				return
			}
			if af < 0 || af+1 >= len(args) || args[af+1] != aformat {
				t.Fatalf("want -af %q; got %v", aformat, args)
			}
			// The filter must precede the encoder it feeds, and stay a
			// separate -af from the video's -vf.
			if ca := slices.Index(args, "-c:a"); ca < af || args[ca+1] != tt.audioArg {
				t.Errorf("-af must come before -c:a %s; got %v", tt.audioArg, args)
			}
		})
	}
}
