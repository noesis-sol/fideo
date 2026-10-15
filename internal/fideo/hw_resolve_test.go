package fideo

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// allHWEncoderListing is an `ffmpeg -encoders` excerpt from a build with every
// Linux hardware H.264 encoder compiled in.
const allHWEncoderListing = ` V....D libx264              libx264 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10 (codec h264)
 V....D h264_amf             AMD AMF H.264 Encoder (codec h264)
 V....D h264_nvenc           NVIDIA NVENC H.264 encoder (codec h264)
 V....D h264_qsv             H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10 (Intel Quick Sync Video acceleration) (codec h264)
`

// amdOnlyDevices lays out a synthetic Linux device tree with a single AMD render
// node, so the device-presence order is h264_amf, h264_nvenc, h264_qsv.
func amdOnlyDevices(t *testing.T) deviceProbe {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "dev/dri/renderD128"), "")
	writeTestFile(t, filepath.Join(root, "sys/class/drm/renderD128/device/vendor"), "0x1002")
	return deviceProbe{goos: "linux", root: root}
}

// TestHWResolverChoice drives resolveHWEncoder's choice with a stand-in encoder
// listing, probe-encode and lavfi check, so every branch runs without ffmpeg or
// a GPU: which encoder is picked, which probes run, and when it gives up.
func TestHWResolverChoice(t *testing.T) {
	tests := []struct {
		name        string
		listing     string
		listErr     error
		probes      map[string]probeResult // missing codecs fail
		lavfiUsable bool
		want        string
		wantErr     []string // substrings of the error; nil means no error
		wantProbed  []string
		wantLavfi   bool // whether the lavfi check ran
	}{
		{
			name:       "no candidate compiled in",
			listing:    " V....D libx264              libx264 H.264 (codec h264)\n",
			wantErr:    []string{"no hardware H.264 encoder", "h264_nvenc", "h264_qsv", "h264_amf"},
			wantProbed: nil,
		},
		{
			name:       "encoder query fails",
			listErr:    errors.New("exit status 1"),
			wantErr:    []string{"could not query ffmpeg encoders", "exit status 1"},
			wantProbed: nil,
		},
		{
			name:       "first probe OK",
			listing:    allHWEncoderListing,
			probes:     map[string]probeResult{"h264_amf": probeOK, "h264_nvenc": probeOK},
			want:       "h264_amf",
			wantProbed: []string{"h264_amf"},
		},
		{
			name:       "first fails, second OK",
			listing:    allHWEncoderListing,
			probes:     map[string]probeResult{"h264_nvenc": probeOK, "h264_qsv": probeOK},
			want:       "h264_nvenc",
			wantProbed: []string{"h264_amf", "h264_nvenc"},
		},
		{
			name:       "timed-out probe is used without probing further",
			listing:    allHWEncoderListing,
			probes:     map[string]probeResult{"h264_amf": probeTimedOut, "h264_nvenc": probeOK},
			want:       "h264_amf",
			wantProbed: []string{"h264_amf"},
		},
		{
			name:        "all fail with lavfi usable",
			listing:     allHWEncoderListing,
			lavfiUsable: true,
			wantErr:     []string{"none could be initialized", "h264_nvenc, h264_qsv, h264_amf"},
			wantProbed:  []string{"h264_amf", "h264_nvenc", "h264_qsv"},
			wantLavfi:   true,
		},
		{
			name:        "all fail with lavfi unusable -> device-ordered first",
			listing:     allHWEncoderListing,
			lavfiUsable: false,
			want:        "h264_amf",
			wantProbed:  []string{"h264_amf", "h264_nvenc", "h264_qsv"},
			wantLavfi:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var probed []string
			lavfiChecked := false
			r := hwResolver{
				goos:     "linux",
				encoders: func() (string, error) { return tt.listing, tt.listErr },
				probe: func(codec string) probeResult {
					probed = append(probed, codec)
					if res, ok := tt.probes[codec]; ok {
						return res
					}
					return probeFailed
				},
				lavfiUsable: func() bool {
					lavfiChecked = true
					return tt.lavfiUsable
				},
				devices: amdOnlyDevices(t),
			}

			got, err := r.resolve()

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("resolve() error = %v, want %q", err, tt.want)
				}
				if got != tt.want {
					t.Errorf("resolve() = %q, want %q", got, tt.want)
				}
			} else {
				if err == nil {
					t.Fatalf("resolve() = %q, want an error", got)
				}
				for _, sub := range tt.wantErr {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("resolve() error = %q, want it to contain %q", err, sub)
					}
				}
			}
			if diff := cmp.Diff(tt.wantProbed, probed); diff != "" {
				t.Errorf("probed encoders mismatch (-want +got):\n%s", diff)
			}
			if lavfiChecked != tt.wantLavfi {
				t.Errorf("lavfi check ran = %v, want %v", lavfiChecked, tt.wantLavfi)
			}
		})
	}
}

// TestHWResolverDarwinNamesVideoToolbox pins that the error for a build without
// any candidate names the platform's own candidates: VideoToolbox on macOS.
func TestHWResolverDarwinNamesVideoToolbox(t *testing.T) {
	r := hwResolver{
		goos:     "darwin",
		encoders: func() (string, error) { return allHWEncoderListing, nil },
		probe: func(codec string) probeResult {
			t.Errorf("probed %s, want no probe without a compiled-in candidate", codec)
			return probeFailed
		},
		lavfiUsable: func() bool { return true },
		devices:     deviceProbe{goos: "darwin"},
	}
	_, err := r.resolve()
	if err == nil || !strings.Contains(err.Error(), "h264_videotoolbox") {
		t.Fatalf("resolve() error = %v, want one naming h264_videotoolbox", err)
	}
}

// TestHostHWResolverUsesRealProbes pins that production resolution goes through
// the real ffmpeg listing, probe-encode, lavfi check and host device probe.
func TestHostHWResolverUsesRealProbes(t *testing.T) {
	r := hostHWResolver()
	if r.encoders == nil || r.probe == nil || r.lavfiUsable == nil {
		t.Fatal("hostHWResolver left a function field nil")
	}
	if r.devices != hostDeviceProbe() {
		t.Errorf("devices = %+v, want hostDeviceProbe() %+v", r.devices, hostDeviceProbe())
	}
	if r.goos != hostDeviceProbe().goos {
		t.Errorf("goos = %q, want the running OS %q", r.goos, hostDeviceProbe().goos)
	}
}
