package fideo

import (
	"runtime"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// lightSource is a 1080p H.264 source: cheap to decode, so ffmpegArgs adds no
// -hwaccel prefix and the argument lists below are exact on every host.
var lightSource = videoMetadata{codec: "h264", height: "1080"}

// TestFFmpegArgsSizePresets checks that the --size resolution reaches -vf as the
// short-side cap, spelled out literally so a change to the expression shows here.
func TestFFmpegArgsSizePresets(t *testing.T) {
	tests := []struct {
		resolution string
		filter     string
	}{
		{"540", "scale=w='if(gte(iw,ih),-2,2*trunc(min(540,iw)/2))':h='if(gte(iw,ih),2*trunc(min(540,ih)/2),-2)'"},
		{"1080", "scale=w='if(gte(iw,ih),-2,2*trunc(min(1080,iw)/2))':h='if(gte(iw,ih),2*trunc(min(1080,ih)/2),-2)'"},
		{"2160", "scale=w='if(gte(iw,ih),-2,2*trunc(min(2160,iw)/2))':h='if(gte(iw,ih),2*trunc(min(2160,ih)/2),-2)'"},
	}
	for _, tt := range tests {
		t.Run(tt.resolution, func(t *testing.T) {
			cfg := testConfig()
			cfg.resolution = tt.resolution
			args := newVideoService(cfg).ffmpegArgs("input.mov", "out_video.mp4", lightSource, 1)
			if !containsSeq(args, "-vf", tt.filter) {
				t.Errorf("args lack -vf %s:\n%v", tt.filter, args)
			}
		})
	}
}

// TestFFmpegArgsGPUEncoders checks the full argument list for each Linux/Windows
// hardware encoder: its codec arguments replace libx264's, no -threads cap is
// set, and mp4 output still gets +faststart.
func TestFFmpegArgsGPUEncoders(t *testing.T) {
	tests := []struct {
		encoder string
		codec   []string
	}{
		{"h264_nvenc", []string{"-c:v", "h264_nvenc", "-rc", "vbr", "-cq", "28", "-b:v", "0"}},
		{"h264_qsv", []string{"-c:v", "h264_qsv", "-global_quality", "28"}},
		{"h264_amf", []string{"-c:v", "h264_amf", "-rc", "cqp", "-qp_i", "28", "-qp_p", "28", "-qp_b", "28"}},
	}
	for _, tt := range tests {
		t.Run(tt.encoder, func(t *testing.T) {
			cfg := testConfig()
			cfg.hwAccel = true
			cfg.hwEncoder = tt.encoder
			args := newVideoService(cfg).ffmpegArgs("input.mov", "out_video.mp4", lightSource, 2)

			want := append([]string{"-i", "input.mov"}, tt.codec...)
			want = append(want,
				"-vf", scaleFilter("1080"),
				"-pix_fmt", "yuv420p",
				"-c:a", "aac", "-b:a", "96k",
				"-movflags", "+faststart",
				"-progress", "pipe:1", "-loglevel", "error", "-y", "out_video.mp4",
			)
			if diff := cmp.Diff(want, args); diff != "" {
				t.Errorf("ffmpegArgs mismatch (-want +got):\n%s", diff)
			}
			if contains(args, "-threads") {
				t.Errorf("hardware encode should not cap -threads: %v", args)
			}
		})
	}
}

// TestProfileForUnknownContainer checks that a container fideo has no profile
// for falls back to H.264/AAC, allows hardware encoders, and gets no
// +faststart (an MP4/MOV-only muxer flag).
func TestProfileForUnknownContainer(t *testing.T) {
	p := profileFor("avi")
	if !p.allowsHW {
		t.Error("profileFor(avi).allowsHW = false, want true")
	}
	if len(p.muxFlags) != 0 {
		t.Errorf("profileFor(avi).muxFlags = %v, want none", p.muxFlags)
	}
	cfg := testConfig()
	if diff := cmp.Diff(h264Video(cfg, 3), p.video(cfg, 3)); diff != "" {
		t.Errorf("profileFor(avi).video is not H.264 (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(aacAudio(cfg), p.audio(cfg)); diff != "" {
		t.Errorf("profileFor(avi).audio is not AAC (-want +got):\n%s", diff)
	}

	args := newVideoService(cfg).ffmpegArgs("input.mov", "out_video.avi", lightSource, 1)
	if contains(args, "-movflags") {
		t.Errorf("avi output should not get -movflags: %v", args)
	}
	if !containsSeq(args, "-c:v", "libx264") || !containsSeq(args, "-c:a", "aac") {
		t.Errorf("avi output should encode H.264/AAC: %v", args)
	}
}

// TestAutoThreadsPerJobBelowOneJob checks that zero or negative job counts are
// treated as one job: every core, never a division by zero or a negative count.
func TestAutoThreadsPerJobBelowOneJob(t *testing.T) {
	want := autoThreadsPerJob(1)
	if want != runtime.NumCPU() {
		t.Fatalf("autoThreadsPerJob(1) = %d, want NumCPU %d", want, runtime.NumCPU())
	}
	for _, jobs := range []int{0, -1, -8} {
		t.Run(strconv.Itoa(jobs), func(t *testing.T) {
			if got := autoThreadsPerJob(jobs); got != want {
				t.Errorf("autoThreadsPerJob(%d) = %d, want %d", jobs, got, want)
			}
		})
	}
}
