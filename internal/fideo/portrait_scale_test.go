package fideo

import "testing"

// TestScaleFilterCapsShortSide pins the scale filter for every --size preset:
// the preset caps the SHORT side, so a portrait source caps its width (the
// iw branch) rather than its height, and both sides stay even.
func TestScaleFilterCapsShortSide(t *testing.T) {
	tests := []struct {
		size string
		want string
	}{
		{"sm", "scale=w='if(gte(iw,ih),-2,max(2,2*trunc(min(540,iw)/2)))':h='if(gte(iw,ih),max(2,2*trunc(min(540,ih)/2)),-2)'"},
		{"md", "scale=w='if(gte(iw,ih),-2,max(2,2*trunc(min(1080,iw)/2)))':h='if(gte(iw,ih),max(2,2*trunc(min(1080,ih)/2)),-2)'"},
		{"lg", "scale=w='if(gte(iw,ih),-2,max(2,2*trunc(min(2160,iw)/2)))':h='if(gte(iw,ih),max(2,2*trunc(min(2160,ih)/2)),-2)'"},
	}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			cfg := testConfig()
			cfg.resolution = validSizes[tt.size]
			args := newVideoService(cfg).ffmpegArgs("in.mov", "out.mp4", videoMetadata{codec: "h264", width: "1080", height: "1920"}, 1)
			i := indexOf(args, "-vf")
			if i < 0 || i+1 >= len(args) {
				t.Fatalf("no -vf filter in %v", args)
			}
			if got := args[i+1]; got != tt.want {
				t.Errorf("scale filter = %q, want %q", got, tt.want)
			}
		})
	}
}
