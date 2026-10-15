package fideo

import (
	"strings"
	"testing"
)

// ffmpeg 9's stderr (-loglevel error) when libopus refuses a 5.1(side) source:
// the cause comes first, a generic consequence last.
const opusLayoutStderr = `[libopus @ 0xbd6ca0700] Invalid channel layout 5.1(side) for specified mapping family -1.
[aost#0:1/libopus @ 0xbd7094300] [enc:libopus @ 0xbd701dc00] Error while opening encoder - maybe incorrect parameters such as bit_rate, rate, width or height.
[af#0:1 @ 0xbd7098500] Error sending frames to consumers: Invalid argument
[aost#0:1/libopus @ 0xbd7094300] Could not open encoder before EOF
[out#0/webm @ 0xbd6c1c300] Nothing was written into output file, because at least one of its streams received no packets.
`

// TestDrainStderrReportsCause pins that a failed encode's detail leads with the
// first error line (the cause), not ffmpeg's generic last line.
func TestDrainStderrReportsCause(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{"empty", "", ""},
		{"blank", "\n  \n", ""},
		{"single line", "Conversion failed!\n", "Conversion failed!"},
		{
			"cause first, consequence last",
			opusLayoutStderr,
			"[libopus] Invalid channel layout 5.1(side) for specified mapping family -1. … " +
				"[out#0/webm] Nothing was written into output file, because at least one of its streams received no packets.",
		},
		{
			"output error",
			"[Parsed_scale_0 @ 0x927008f00] Invalid size 'bogus'\n[AVFilterGraph @ 0x926c08580] Error initializing filters\nError opening output file o.mp4.\nError opening output files: Invalid argument\n",
			"[Parsed_scale_0] Invalid size 'bogus' … Error opening output files: Invalid argument",
		},
		{
			"cause past a long run of repeats",
			"first cause\n" + strings.Repeat("[h264 @ 0x1] error while decoding MB\n", 4<<10) + "No space left on device\n",
			"first cause … No space left on device",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := drainStderr(strings.NewReader(tt.stderr)); got != tt.want {
				t.Errorf("drainStderr = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDrainStderrOverlongSingleLine: a lone line longer than the retained head
// is reported once, whole from the tail, not as its own prefix plus itself.
func TestDrainStderrOverlongSingleLine(t *testing.T) {
	line := strings.Repeat("e", stderrHeadBytes+100)
	if got := drainStderr(strings.NewReader(line + "\n")); got != line {
		t.Errorf("drainStderr returned %d bytes, want the %d-byte line once", len(got), len(line))
	}
}

func TestFirstLine(t *testing.T) {
	tests := map[string]string{
		"":                    "",
		"single":              "single",
		"\n a \nb\n":          "a",
		"\n\n":                "",
		"  first  \nsecond\n": "first",
		"no trailing newline": "no trailing newline",
	}
	for in, want := range tests {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
