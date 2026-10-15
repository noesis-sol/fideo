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
  `TestTempOutputPathUnique`, `TestResolveOutputPathInPlace`;
  `output_naming_test.go` `TestGetOutputPath` (prefix, extension swap, no
  format keeps the extension, directory kept),
  `TestResolveOutputPathDisambiguates` (`a.mp4`, `a.mov`, `a.MOV` ->
  `out_a.mp4`, `out_a_mov.mp4`, `out_a-1.mp4`; case-only claims taken),
  `TestResolveOutputPathIgnoresOwnClaim`.

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
  `TestThreadBudgetScalesWithBatch`, `TestConcurrentJobs`;
  `opus_layout_test.go` `TestOpusAudioRemapsChannelLayout` (webm's aformat).
  (integration): `integration_test.go` `TestEncodeIntegration` (a real 320x240
  encode to mp4, output probed as 240p H.264);
  `portrait_scale_integration_test.go` `TestScaleShortSideIntegration`
  (portrait, landscape, square and odd-sized encodes, output size probed);
  `opus_layout_integration_test.go` `TestWebMSideLayoutIntegration` (a
  5.1(side) AC3 source encoded to webm keeps six Opus channels).
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
  `TestOrderByDevicePresence`; `hw_probe_test.go`
  `TestProbeArgsRequestOutputPixFmt` (the probe asks for the encode's
  `-pix_fmt`). (integration, on the host's own encoder, skipped without one):
  `hw_encode_integration_test.go` `TestHWEncodeIntegration` (8-bit, 4:4:4 and
  10-bit sources come out yuv420p H.264), `TestHWQualityIntegration` (the
  quality knob moves the size and isn't held to a bitrate target).
  `FIDEO_HW_ENCODER=h264_nvenc` (or another) makes them fail, not skip, unless
  that encoder is chosen.
- **Untested**: `resolveHWEncoder`'s choice, which execs ffmpeg directly: no
  compiled-in encoder, a probe that succeeds, one that fails so the next is
  tried, one that times out and is used anyway, every probe failing with and
  without a usable lavfi source. The integration tests have run only on
  VideoToolbox: NVENC, QSV and AMF need a host with that GPU, which CI lacks
  (ticket fideo-8ip).

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
  read video info"; `probeMetadata` on an empty path (a file that isn't a
  video: `TestProcessFileProbeFailureIntegration`).

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
  `worker_failure_integration_test.go`
  `TestProcessFileProbeFailureIntegration` (a text file named `.mp4` gets a
  lone "failed to probe video" error ending in ffprobe's last stderr line),
  `TestProcessFileFFmpegFailureIntegration` (a preset x264 rejects gets
  "ffmpeg failed: …" with the cause, and the empty output ffmpeg created, the
  `out_` copy or the `--overwrite` scratch file, is removed),
  `TestProcessFileCancelIntegration` (cancelling after the first progress
  report, with the partial output on disk, ends in a cancelMsg and removes it,
  the `out_` copy or the scratch file, leaving the source intact).
- **Untested**: a failure while renaming the scratch file over the source in
  a real encode (covered only by the unit `TestFinishInPlaceRenameFailure`).

- **Tickets**: fideo-atq.12 (success and in-place), fideo-atq.13 (failure
  and cancel).

## The file picker and batch (TUI state)

- **Reached by**: the TUI: arrows/`j`/`k`, PgUp/PgDn, Home/End/`g`/`G`,
  `space` select, `a` all, `enter` start, `c`/`ctrl+c` cancel, `q`/`esc` quit;
  messages from the workers. Code: `handlers.go`, `model.go` (`Update`,
  `dispatch`).
- **Tests** (unit): `fixes_test.go` `TestFillSlotsSkipsLaunchedFile`,
  `TestFillSlotsCapacityCountsInFlight`, `TestFillSlotsPromptsOnDistinctInPlaceDest`,
  `TestMarkInPlaceCollisions`, `TestMarkInPlaceCollisionsPlainOverwrite`;
  `inplace_collision_case_test.go` `TestMarkInPlaceCollisionsCaseOnly`,
  `TestSameDestination` (case-only destinations, asserted per the host
  filesystem's case sensitivity); `fixes_test.go`
  `TestStartProcessingResetsBatchState`; `handlers_test.go`
  `TestProcessingStartUnderCancelKeepsSlot`, `TestBatchJobs`,
  `TestNavigationWhileProcessing`; `view_test.go` `TestUpdateTracksScrollOffset`;
  `batch_messages_test.go` (worker messages through `dispatch`, with a fake
  launcher in `model.launch`): `TestDoneStartsNextPendingFile`,
  `TestLastDoneSettlesBatch` (done, or not done when cancelled),
  `TestErrorSettlesBatchAsDone`, `TestErrorStartsNextPendingFile`,
  `TestErrorForLaunchingFileKeepsProcessingCount`, `TestCancelResetsFile`
  (processing and launching files), `TestCancelSettlesOnLastWorker`,
  `TestInfoMessagesSetFields`, `TestSpinnerTick`, `TestAutoStartStartsProcessing`.
- **Untested**:
  - Keys while idle: `space` toggling selection and dropping the output claim,
    `a` selecting every pending file, `enter` starting, `q`/`esc`/`ctrl+c`
    quitting only when idle, `c` cancelling several in-flight encodes (one
    is covered by `TestProcessingStartUnderCancelKeepsSlot`).
  - Launching work: `startFile` calls `model.launch` (nil means
    `processFile`), so a test that starts a file sets a fake launcher; the
    real launch is covered by the worker integration tests.

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
  "All done!"); `overwrite_prompt_test.go` (existing `out_` files in a temp
  dir, a fake launcher in `model.launch`): `TestExistingOutputRaisesPrompt`,
  `TestPromptCursorMovesAndClamps`, `TestOverwriteStartsConfirmedFile`,
  `TestOverwriteFillsRemainingSlots`, `TestSkipDeselectsAndStartsNext`,
  `TestSkipLastFileSettlesBatch`, `TestSkipWaitsForInFlightSibling`,
  `TestCancelAllDropsQueue`, `TestCtrlCOnPromptCancelsAndQuits`,
  `TestPromptSwallowsOtherKeys`; `view_text_test.go` `TestOverwriteDialogText`
  (`renderOverwriteDialog`).

- **Tickets**: fideo-atq.16.

## Rendering

- **Reached by**: every frame of the TUI. Code: `view.go`.
- **Tests** (unit): `view_test.go` `TestViewport`, `TestViewFitsTerminalHeight`,
  `TestRowLinesMatchesRender`, `TestFitTruncates`, `TestUpdateTracksScrollOffset`;
  `view_text_test.go` (text compared through `ansi.Strip`):
  `TestStatusHeaderOnlyWhileProcessing`, `TestFooterText` (idle help,
  processing, all done, done with errors), `TestViewEmptyList`,
  `TestOverwriteDialogText` (base name, highlighted option), `TestFileGlyph`,
  `TestFileRowShowsSpinnerWhileEncoding`, `TestPercentReadout` (rounding,
  clamping), `TestGetProgressColor` (ends and clamping), `TestPercentLabels`
  (the 101-entry percent tables).

- **Tickets**: fideo-atq.17.

## Missing-ffmpeg help dialog

- **Reached by**: running `fideo` (with a valid command line) on a machine
  without ffmpeg or ffprobe. Code: `checkDependencies` in `app.go`;
  `errorui.go`.
- **Tests** (unit): `errorui_test.go` `TestGetInstallCommand` (darwin,
  windows, an unknown OS, linux with apt, with dnf, with neither; the OS and
  PATH lookup are parameters), `TestErrorModelQuitKeys` (`q`/`esc`/`ctrl+c`),
  `TestErrorModelCopyKeys` (`c`/`enter` start the copy),
  `TestErrorModelOtherKeysIgnored`, `TestErrorModelClipboardResult` (copied, or
  its error), `TestErrorModelView` (its three states).
- **Untested**: `copyToClipboard` (it reads `runtime.GOOS` and `exec.LookPath`
  directly and runs the real clipboard tool).

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
