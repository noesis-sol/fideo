package fideo

import "testing"

// TestProbeArgsRequestOutputPixFmt pins that the --hw probe-encode asks for the
// same pixel format as a real encode, so an encoder that would refuse it (a
// build where h264_qsv takes nv12 and no conversion happens) fails the probe
// and --hw falls back to software, rather than every file failing.
func TestProbeArgsRequestOutputPixFmt(t *testing.T) {
	vs := newVideoService(testConfig())
	encode := vs.ffmpegArgs("in.mov", "out.mp4", videoMetadata{}, 1)
	if !containsSeq(encode, "-pix_fmt", outputPixFmt) {
		t.Fatalf("ffmpegArgs = %v, want -pix_fmt %s", encode, outputPixFmt)
	}

	for _, codec := range []string{"h264_nvenc", "h264_qsv", "h264_amf", "h264_videotoolbox", "rawvideo"} {
		t.Run(codec, func(t *testing.T) {
			got := probeArgs(codec)
			if !containsSeq(got, "-c:v", codec, "-pix_fmt", outputPixFmt) {
				t.Errorf("probeArgs(%q) = %v, want -c:v %s -pix_fmt %s", codec, got, codec, outputPixFmt)
			}
		})
	}
}
