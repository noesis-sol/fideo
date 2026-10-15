# Fideo features and their tests

The map of what a user touches in fideo, which tests cover it, and what has no
test yet. Every test is Go's `go test`, white-box (`package fideo`), beside the
code in `internal/fideo/`.

## Suites

| Suite | Runner | Command | Needs |
|---|---|---|---|
| gofmt check | `scripts/check-fast.sh` | `test -z "$(gofmt -l .)"` | Go |
| go vet | `scripts/check-fast.sh` | `go vet ./...` | Go |
| build | `scripts/check-fast.sh` | `go build ./cmd/fideo` | Go |
| unit tests | `scripts/check-fast.sh` | `go test ./...` | Go |
| unit + integration tests | `scripts/check-full.sh` | `go test -tags=integration ./...` | ffmpeg and ffprobe on PATH (the integration test skips itself without them) |

CI (`.github/workflows/ci.yml`) runs the same commands on `ubuntu-latest` and
`macos-latest` with ffmpeg installed.

Runner times on an Apple Silicon Mac (darwin/arm64, Go 1.27.1, ffmpeg from
Homebrew), measured 2026-10-15, three runs each:

- `scripts/check-fast.sh`: 1.0s with no test cache (`GOFLAGS=-count=1`), 2.3s
  on a cold build cache, 0.5s when cached.
- `scripts/check-full.sh` (check-fast plus integration): 1.7s with no test
  cache, 0.6s when cached. The one integration test, `TestEncodeIntegration`,
  takes about 0.2s.

There is no end-to-end harness yet: nothing runs the built `fideo` binary or
drives the TUI through a pseudo-terminal.

## Command line: flags, validation and exit codes

- **Reached by**: `fideo [--format mp4|mov|mkv|webm] [--size sm|md|lg] [--hw]
  [--overwrite] [--] [path|pattern ...]`, `fideo --help` / `-h`.
  Code: `parseArgs`, `flagValue`, `buildConfig`, `applyHW`, `Run` in
  `app.go`; `validFormats`, `validSizes` in `config.go`.
- **Tests** (unit): `app_test.go` `TestParseArgs` (flags in any order, inline
  and separate values, `--help`/`-h`, unknown options rejected, boolean flag
  with a value rejected, `--` passes dashed names, missing and empty values);
  `inplace_test.go` `TestParseArgsOverwrite`; `cli_config_test.go` (what `Run`
  decides after parsing, via `buildConfig` and `applyHW`: `--format`
  lowercased, unsupported format and size errors with their hints, the
  `sm/small/md/medium/lg/large` presets, the `mp4` default and its absence
  under `--overwrite`, `--hw` refused with `--format webm`, `--hw` capping
  concurrency at 2, the software fallback warning, the WebM-source warning
  count under `--hw --overwrite`; and the single-dash aliases `-format`,
  `-size`, `-hw`).
- **Tests** (integration, the built binary): `cli_e2e_test.go`
  `TestCLIEndToEnd` builds `./cmd/fideo` and runs it with stub
  ffmpeg/ffprobe on PATH: `--help`/`-h` print usage and exit 0, an unknown
  option or a flag missing its value exits 1 with "Run 'fideo --help' for
  usage.", `--format bogus`, `--size bogus` and `--hw --format webm` exit 1
  with their hints, a missing path, a non-video file, an empty directory and
  a pattern with no match exit 1 with their messages; and with no ffmpeg on
  PATH, `--help` and an unknown option still work.

- **Tickets**: fideo-atq.3 (Run's checks, unit), fideo-atq.4 (the binary).

## Finding the videos

- **Reached by**: `fideo` (current directory), `fideo dir/`, `fideo a.mp4
  b.mov`, `fideo '*.mov'`, shell-expanded globs. Code: `findVideos`,
  `safeArgPath`, `collectVideosFromPattern` in `discover.go`;
  `collectVideosFromArg`, `createModelFromPaths` in `app.go`; `newModel`,
  `initialModel`, `Init` in `model.go`.
- **Tests** (unit): `fixes_test.go` `TestCollectVideosFromArgLiteralMetachar`
  (a file named `clip[1].mp4` is reached literally and pre-selected),
  `TestCollectVideosFromArgGlobStillWorks`; `inplace_test.go`
  `TestFindVideosSkipsScratchTemps`; `discover_skip_test.go`
  `TestFindVideosSkipsHiddenAndOwnOutputs` (dotfiles such as AppleDouble `._`
  sidecars, and `out_` outputs beside their source, including the batch's
  disambiguated `out_<stem>_<srcext>` / `out_<stem>-<n>` names, are skipped),
  `TestCollectVideosFromPatternSkipsDotfiles` (`*` doesn't reach dotfiles,
  `._*` does); `autostart_test.go`
  `TestAutoStartOnlyForNamedFile` (a lone file is pre-selected, and `Init`
  auto-starts it only when it was named, not found by a directory scan or a
  pattern; a file reached by name and through its directory is counted once).
- **Untested**:
  - `findVideos`: `out_` files skipped when their source exists (same or other
    extension) and kept when it doesn't, upper-case extensions (`.MP4`),
    non-video files and subdirectories ignored, a missing or non-directory
    path giving no files, dashed names getting a `./` prefix (`safeArgPath`).
  - `collectVideosFromPattern`: a pattern matching nothing, matching only
    non-videos, matching directories, a file reached twice counted once, a
    malformed pattern.
  - `collectVideosFromArg`: a non-video file refused, a missing path's error, a
    directory argument.
  - `createModelFromPaths`: "no video files found" when the paths hold none.

- **Tickets**: fideo-atq.5 (findVideos), fideo-atq.6 (paths, globs, model).

## Output naming

- **Reached by**: every encode; `out_<name>.<format>` next to the source, or
  the source itself under `--overwrite`. Code: `getOutputPath`, `inPlaceDest`,
  `tempOutputPath`, `resolveOutputPath` in `encode.go`.
- **Tests** (unit): `inplace_test.go` `TestInPlaceDest`, `TestTempOutputPath`,
  `TestTempOutputPathUnique`, `TestResolveOutputPathInPlace`.
- **Untested**: `getOutputPath` (prefix, extension swap, no format keeps the
  extension); `resolveOutputPath` outside `--overwrite`: two sources sharing a
  stem (`a.mp4`, `a.mov` -> `out_a.mp4`, `out_a_mov.mp4`), the numeric suffix
  when that is taken too, case-only differences treated as one path.

- **Tickets**: fideo-atq.7.

## The ffmpeg command

- **Reached by**: every encode; `--format`, `--size` and `--hw` change it.
  Code: `ffmpegArgs`, `buildFFmpegCommand`, `decodeArgs`, `profileFor`,
  `h264Video`, `vp9Video`, `hwEncoderArgs` in `encode.go`;
  `concurrentJobs`, `autoThreadsPerJob`, `autoMaxConcurrent` in `config.go`.
- **Tests** (unit): `encode_test.go` `TestFFmpegArgs`, `TestFFmpegArgsGolden`
  (mp4/mov/mkv/webm, VideoToolbox), `TestFFmpegArgsHeavySourceExact`,
  `TestBuildFFmpegCommandString`, `TestFFmpegArgsHardwareDecodePrecedesInput`,
  `TestDecodeArgs`, `TestProfileAllowsHW`, `TestHWEncoderArgs`,
  `TestContainerOf`; `platform_test.go` `TestDecodeArgsPerOS`;
  `fixes_test.go` `TestEvenHeightScaleFilter`; `portrait_scale_test.go`
  `TestScaleFilterCapsShortSide` (sm/md/lg filters); `stream_test.go`
  `TestThreadBudgetScalesWithBatch`, `TestConcurrentJobs`.
  (integration): `integration_test.go` `TestEncodeIntegration` (a real 320x240
  encode to mp4, output probed as 240p H.264);
  `portrait_scale_integration_test.go` `TestScaleShortSideIntegration`
  (portrait, landscape, square and odd-sized encodes, output size probed).
- **Untested**: full argument lists for NVENC, QSV and AMF (only their codec
  arguments are checked); `profileFor` for an unknown container; `autoThreadsPerJob` below
  one job; `autoMaxConcurrent`'s clamp to [2, 4] (it reads
  `runtime.NumCPU()` directly); real encodes to mov, mkv and webm, and with
  `--size`.

- **Tickets**: fideo-atq.8 (arguments, unit), fideo-atq.9 (real encodes).

## Hardware encoder selection

- **Reached by**: `fideo --hw`. Code: `resolveHWEncoder`, `probeEncode`,
  `lavfiProbeUsable`, `hwEncoderCandidates`, `orderByDevicePresence`,
  `deviceProbe` in `encode.go`.
- **Tests** (unit): `platform_test.go` `TestHWEncoderCandidatesPerOS`,
  `TestDeviceProbeAvailable` (synthetic `/dev` and `/sys` trees),
  `TestOrderByDevicePresence`.
- **Untested**: `resolveHWEncoder`'s choice, which execs ffmpeg directly: no
  compiled-in encoder, a probe that succeeds, one that fails so the next is
  tried, one that times out and is used anyway, every probe failing with and
  without a usable lavfi source. Encoders on real GPUs are ticket fideo-q9n.

- **Tickets**: fideo-atq.10.

## Probing a video

- **Reached by**: every encode, and the `In:` / `Out:` lines under each row.
  Code: `probeMetadata`, `parseProbeOutput`, `withStderr`, `lastLine`,
  `formatVideoInfo`, `getVideoInfo`, `formatFileSize` in `probe.go`.
- **Tests** (unit): `probe_test.go` `TestParseProbeOutput`, `TestWithStderr`,
  `TestLastLine`. (integration): `TestEncodeIntegration` probes a real source
  and output.
- **Untested**: `formatVideoInfo` (bitrate as Mbps, an `N/A` bitrate, the
  container upper-cased, "unknown size" for a missing file);
  `formatFileSize` (bytes, KB, MB, GB boundaries); `getVideoInfo`'s "unable to
  read video info"; `probeMetadata` on an empty path and on a file that isn't
  a video.

- **Tickets**: fideo-atq.11 (formatting, unit); probe failures on real
  files are in fideo-atq.13.

## Encoding a file (the worker)

- **Reached by**: starting a batch; each file runs `processFile` in
  `encode.go`, which probes, runs ffmpeg, streams progress and reports done,
  error or cancel; under `--overwrite` it renames a scratch file over the
  source, or keeps the source when the re-encode is no smaller. Code:
  `processFile`, `finishInPlace`, `streamProgress`, `drainStderr`, `tailWriter`,
  `errorDetail`, `waitWorkers`. A failed encode reports stderr's first error
  line (the cause) and, when it differs, its last one.
- **Tests** (unit): `stream_test.go` `TestStreamProgressCoalescesAndDrains`,
  `TestStreamProgressNoDurationDrains`, `TestDrainStderrKeepsTailPastLongLines`,
  `TestTailWriterRetainsLastBytes`, `TestWaitWorkers`; `errordetail_test.go`
  `TestDrainStderrReportsCause`, `TestDrainStderrOverlongSingleLine`,
  `TestFirstLine`;
  `inplace_keep_test.go` `TestFinishInPlace` (replace, keep a larger or equal
  re-encode, format conversion), `TestFinishInPlaceNoteSizes`,
  `TestFinishInPlaceRenameFailure`, `TestHandleDoneRecordsNote`;
  `worker_test.go` `TestProcessFileInvalidIndex`.
  (integration): `worker_integration_test.go`
  `TestProcessFileSuccessIntegration` runs `processFile` on a real clip, with
  `videoService.send` swapped for a collector: the messages of a successful
  encode in order (info, start, progress, output info, done) and the files
  left: the `out_` copy, `--overwrite` replacing the source with no scratch
  file, and `--overwrite --format mp4` turning `clip.mov` into `clip.mp4`.
- **Untested**: a probe failure; an ffmpeg failure reporting its stderr
  detail and removing the partial output; a cancel removing the partial
  output (the scratch file, never the source under `--overwrite`).

- **Tickets**: fideo-atq.12 (success and in-place), fideo-atq.13 (failure
  and cancel).

## The file picker and batch (TUI state)

- **Reached by**: the TUI: arrows/`j`/`k`, PgUp/PgDn, Home/End/`g`/`G`,
  `space` select, `a` all, `enter` start, `c`/`ctrl+c` cancel, `q`/`esc` quit;
  messages from the workers. Code: `handlers.go`, `model.go` (`Update`,
  `dispatch`).
- **Tests** (unit): `fixes_test.go` `TestFillSlotsSkipsLaunchedFile`,
  `TestFillSlotsCapacityCountsInFlight`, `TestFillSlotsPromptsOnDistinctInPlaceDest`,
  `TestMarkInPlaceCollisions`, `TestMarkInPlaceCollisionsPlainOverwrite`,
  `TestStartProcessingResetsBatchState`; `handlers_test.go`
  `TestProcessingStartUnderCancelKeepsSlot`, `TestBatchJobs`,
  `TestNavigationWhileProcessing`; `view_test.go` `TestUpdateTracksScrollOffset`.
- **Untested**:
  - Keys while idle: `space` toggling selection and dropping the output claim,
    `a` selecting every pending file, `enter` starting, `q`/`esc`/`ctrl+c`
    quitting only when idle, `c` cancelling several in-flight encodes (one
    is covered by `TestProcessingStartUnderCancelKeepsSlot`).
  - Worker messages: `handleDone`, `handleError` updating counts, releasing
    the slot, starting the next file and settling the batch as done (with or
    without errors); `handleCancel` with several workers and for a file that
    was processing (one launching worker is covered); `handleProgress`,
    `handleVideoInfo`, `handleOutputInfo`; spinner ticks ignored when idle;
    `autoStartMsg`.
  - Launching work: anything that reaches `startFile` starts a real worker
    (ffprobe, ffmpeg, the global `program`), so tests that start a file need a
    seam for the launch.

- **Tickets**: fideo-atq.14 (worker messages, adds the launch seam),
  fideo-atq.15 (keys).

## The overwrite prompt

- **Reached by**: starting a batch when an output already exists (an
  `out_` file, or an in-place `--format` conversion onto another file).
  Code: `fillSlots`, `handleOverwritePromptKey`, `handleOverwriteConfirm`,
  `handleOverwriteSkip`, `cancelAll` in `handlers.go`;
  `renderOverwriteDialog` in `view.go`.
- **Tests** (unit): `fixes_test.go` `TestFillSlotsPromptsOnDistinctInPlaceDest`
  (the in-place case only); `cancel_all_test.go`
  `TestCancelAllWithNothingInFlight`, `TestCancelAllWaitsForLaunchingWorker`
  (Cancel all dropping the queue, settling on `m.cancels` and not reporting
  "All done!").
- **Untested**: the prompt for an existing `out_` file; the prompt's keys
  (up/down clamped to its three options, `enter` on Overwrite and Skip,
  `ctrl+c` cancelling in-flight encodes and quitting); Overwrite starting the
  file; Skip deselecting it, releasing its claim and settling the batch.

- **Tickets**: fideo-atq.16.

## Rendering

- **Reached by**: every frame of the TUI. Code: `view.go`.
- **Tests** (unit): `view_test.go` `TestViewport`, `TestViewFitsTerminalHeight`,
  `TestRowLinesMatchesRender`, `TestFitTruncates`, `TestUpdateTracksScrollOffset`.
- **Untested**: the "Processing X of Y files (N completed)" header; the four
  footers (idle help, processing, all done, done with errors); the empty-list
  message; the overwrite dialog's text and its highlighted option;
  `fileGlyph` per state; the percentage readout's rounding and clamping;
  `getProgressColor` at and beyond its ends, and the 101-entry percent tables.

- **Tickets**: fideo-atq.17.

## Missing-ffmpeg help dialog

- **Reached by**: running `fideo` (with a valid command line) on a machine
  without ffmpeg or ffprobe. Code: `checkDependencies` in `app.go`;
  `errorui.go`.
- **Tests**: none.
- **Untested**: `errorModel.Update` (`q`/`esc`/`ctrl+c` quit, `c`/`enter`
  start the copy, a clipboard result marking copied or showing its error);
  `errorModel.View` in its three states; `getInstallCommand` per OS (it reads
  `runtime.GOOS` and `exec.LookPath` directly); `copyToClipboard`.

- **Tickets**: fideo-atq.18.

## The whole app in a terminal

- **Reached by**: running `fideo` in a terminal. Code: `Run` in `app.go`.
- **Tests**: none; there is no pseudo-terminal harness.
- **Untested**: compressing a lone file (auto-start) and a selected batch to
  "All done!" with the `out_` files written; `--overwrite` replacing sources;
  cancelling with `c` leaving no partial output; SIGTERM/SIGINT restoring the
  terminal, stopping ffmpeg and removing partial outputs (`waitWorkers`).
- **Tickets**: fideo-atq.19 (pty harness, happy paths), fideo-atq.20 (cancel
  and signals).
