package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
)

// Optional content-free performance probe, overlaid unchanged on the packet
// baseline by tools/run-render-performance.py. Every invocation is one fresh Go
// test process, with warm filesystem/compiler caches and 100 warmup events per
// scenario. This measures synthetic input -> Update and separate View, plus real
// render command work/result wait/install; never terminal paint or physical input.
func TestRefinementSteadyForegroundSamples(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("T3OXIDE_PERF_SAMPLES"))
	if n == 0 {
		t.Skip("opt-in synthetic performance evidence")
	}
	if n < 1000 || n > 10000 {
		t.Fatal("samples must be bounded to 1000..10000")
	}
	for _, scenario := range []string{"short-100x32", "mixed8k-search-220x70", "plain24k-100x32", "history80-220x70", "rapid-resize", "same-effective-width", "badge-control-search-220x70", "burst-control-search-220x70", "table-cap-search-220x70"} {
		t.Run(scenario, func(t *testing.T) {
			m := testModel(t)
			width, height, rows := 100, 32, 24
			if strings.Contains(scenario, "220x70") {
				width, height = 220, 70
			}
			if scenario == "same-effective-width" {
				width = 112
			}
			history := strings.Contains(scenario, "history80")
			if history {
				rows = 80
			}
			m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			m.status = client.Status{ThreadID: "synthetic-thread", EnvironmentID: "synthetic-source", Writable: !history, State: "live"}
			if strings.Contains(scenario, "search") {
				m.searchTerm = "synthetic"
			}
			base := "Synthetic streaming text."
			if strings.Contains(scenario, "mixed8k") || strings.Contains(scenario, "control") {
				base = strings.Repeat("Synthetic **bold** paragraph with `inline` code and 界.\n\n", 135)
			}
			if strings.Contains(scenario, "plain24k") {
				base = strings.Repeat("Synthetic plain text. ", 1100)
			}
			if strings.Contains(scenario, "table-cap") {
				base = strings.Repeat("A **bold** sample with `inline` text and Unicode 界.\n\n", 430)
			}
			makePage := func(sequence int) *wire.Snapshot {
				s := &wire.Snapshot{Revision: int64(sequence + 1), Metadata: wire.Metadata{ThreadID: "synthetic-thread", State: "live", Live: true}}
				if history {
					cursor := "synthetic-history-cursor"
					s.Metadata.State, s.Metadata.Live, s.Metadata.Historical, s.Metadata.HistoryCursor = "historical", false, true, &cursor
				}
				for i := range rows {
					s.Items = append(s.Items, wire.Row{Key: fmt.Sprintf("synthetic-thread/%02d", i), Kind: "assistant_message", Text: "Synthetic completed **message** with Unicode 界 café."})
				}
				textVersion := sequence
				if scenario == "same-effective-width" || strings.Contains(scenario, "control") {
					textVersion = 0
				}
				s.Items[rows-1].Text, s.Items[rows-1].Streaming = base+fmt.Sprint(textVersion), !history
				if strings.Contains(scenario, "table-cap") {
					s.Items[rows-2].Text = "|" + strings.Repeat("x", 12000) + "|\n|---|\n" + strings.Repeat("|x|\n", 3000)
					s.Items[rows-1].Text, s.Items[rows-1].Streaming = base, false
					s.Items[0].Text = "Synthetic prefix replacement " + fmt.Sprint(sequence)
				}
				if strings.Contains(scenario, "control") {
					s.Metadata.Status = []string{"running", "waiting"}[sequence%2]
					s.Metadata.Sequence = new(int64(sequence))
					s.Metadata.Requests = []map[string]any{{"id": "synthetic-request", "revision": fmt.Sprint(sequence), "kind": "approval_request", "supported": false}}
				}
				totalText := 0
				for _, row := range s.Items {
					if len(row.Text) > wire.MaxTextBytes {
						t.Fatal("synthetic row exceeds native text bound")
					}
					totalText += len(row.Text)
				}
				if totalText > wire.MaxRowsBytes {
					t.Fatal("synthetic page exceeds native text bound")
				}
				return s
			}
			m.snapshot = makePage(0)
			finishCommand(t, m, m.requestRender(false, false))
			if strings.Contains(scenario, "table-cap") && !strings.Contains(m.transcript, "Display limit") {
				t.Fatal("table fixture did not reach the rendered cap")
			}
			for range 100 {
				m.Update(keyPress('x'))
				_ = m.View()
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			countersBefore := performanceCounters(m)
			rssBefore, rssPeak := performanceRSS(), uint64(0)
			heapPeak := before.HeapAlloc
			updateTimes, viewTimes := make([]int64, 0, n), make([]int64, 0, n)
			workTimes, waitTimes, installTimes, admissionTimes := []int64{}, []int64{}, []int64{}, []int64{}
			type completion struct {
				msg      tea.Msg
				work     int64
				finished time.Time
			}
			results := make(chan completion, 1)
			active := false
			frames, retired, peakPending, renders, requests, cancellations := 0, 0, 0, 0, 0, 0
			bytesViewed, maxFrameBytes := int64(0), 0
			launch := func(cmd tea.Cmd) {
				if cmd == nil {
					return
				}
				if active {
					t.Fatal("concurrent render command admission")
				}
				active = true
				renders++
				go func() {
					start := time.Now()
					msg := cmd()
					end := time.Now()
					results <- completion{msg, end.Sub(start).Nanoseconds(), end}
				}()
			}
			consume := func(result completion) {
				active = false
				workTimes = append(workTimes, result.work)
				waitTimes = append(waitTimes, time.Since(result.finished).Nanoseconds())
				oldSerial := m.presentedSerial
				if frame, ok := result.msg.(renderedMsg); ok && frame.err != nil {
					cancellations++
				}
				start := time.Now()
				_, cmd := m.Update(result.msg)
				installTimes = append(installTimes, time.Since(start).Nanoseconds())
				if m.presentedSerial > oldSerial {
					frames++
				} else {
					retired++
				}
				launch(cmd)
			}
			for i := range n {
				select {
				case msg := <-results:
					consume(msg)
				default:
				}
				if i%8 == 0 {
					burstCount := 1
					if strings.Contains(scenario, "burst-control") {
						burstCount = 16
					}
					for burst := range burstCount {
						m.snapshot = makePage(i*16 + burst + 1)
						if scenario == "rapid-resize" {
							if (i/8)%2 == 0 {
								m.width, m.height = 60, 16
							} else {
								m.width, m.height = 220, 70
							}
							m.resize()
						} else if scenario == "same-effective-width" {
							m.width = []int{112, 160}[(i/8)%2]
							m.resize()
						}
						start := time.Now()
						cmd := m.requestRender(false, false)
						admissionTimes = append(admissionTimes, time.Since(start).Nanoseconds())
						requests++
						launch(cmd)
					}
				}
				if m.pendingRender != nil {
					peakPending = 1
				}
				start := time.Now()
				m.Update(keyPress('x'))
				updated := time.Now()
				view := m.View()
				viewed := time.Now()
				updateTimes = append(updateTimes, updated.Sub(start).Nanoseconds())
				viewTimes = append(viewTimes, viewed.Sub(updated).Nanoseconds())
				bytesViewed += int64(len(view.Content))
				maxFrameBytes = max(maxFrameBytes, len(view.Content))
				if i%64 == 0 {
					var memory runtime.MemStats
					runtime.ReadMemStats(&memory)
					heapPeak = max(heapPeak, memory.HeapAlloc)
					rssPeak = max(rssPeak, performanceRSS())
				}
			}
			for active {
				select {
				case msg := <-results:
					consume(msg)
				case <-time.After(15 * time.Second):
					t.Fatal("bounded render lane did not finish")
				}
			}
			runtime.ReadMemStats(&after)
			if got := m.currentDraft(); got != strings.Repeat("x", n+100) {
				t.Fatal("foreground work lost exact editor input")
			}
			if m.rendering || m.pendingRender != nil {
				t.Fatal("render lane failed to drain")
			}
			counts := performanceCounters(m)
			for key := range counts {
				counts[key] -= countersBefore[key]
			}
			record := map[string]any{
				"fixture_version": 3, "scenario": scenario, "samples": n, "warmup": 100, "rows": rows, "historical": history,
				"boundary": "synthetic key -> Model.Update; separate View", "platform": runtime.GOOS + "/" + runtime.GOARCH, "go": runtime.Version(),
				"update_ns": updateTimes, "view_ns": viewTimes, "update_summary_ns": performanceSummary(updateTimes), "view_summary_ns": performanceSummary(viewTimes),
				"render_work_ns": workTimes, "render_result_wait_ns": waitTimes, "frame_install_update_ns": installTimes, "render_admission_ns": admissionTimes,
				"allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "heap_after_bytes": after.HeapAlloc, "heap_peak_sampled_bytes": max(heapPeak, after.HeapAlloc), "gc_cycles": after.NumGC - before.NumGC, "gc_pause_ns": after.PauseTotalNs - before.PauseTotalNs,
				"rss_before_bytes": rssBefore, "rss_peak_sampled_bytes": max(rssPeak, performanceRSS()), "rss_source": "Linux /proc/self/status VmRSS every64events; 0 means unavailable",
				"useful_frames": frames, "retired_frames": retired, "cancellations": cancellations, "pending_high_water": peakPending, "worker_high_water": min(1, renders), "result_queue_capacity": cap(results),
				"render_requests": requests, "render_commands": renders, "counts": counts, "prepared_view_total_bytes": bytesViewed, "prepared_view_max_bytes": maxFrameBytes, "failures": 0,
			}
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			t.Log(string(data))
		})
	}
}

func performanceSummary(input []int64) map[string]int64 {
	if len(input) == 0 {
		return map[string]int64{}
	}
	values := append([]int64(nil), input...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return map[string]int64{"median": values[len(values)/2], "p95": values[(len(values)*95+99)/100-1], "p99": values[(len(values)*99+99)/100-1], "worst": values[len(values)-1]}
}

// Baseline instrumentation is an overlay of counter increments only. Reflective
// reads keep this identical workload compilable before and after implementation.
// Called only after render completion, so no worker fields are raced.
func performanceCounters(m *Model) map[string]uint64 {
	field := func(value reflect.Value, name string) uint64 {
		f := value.FieldByName(name)
		if f.IsValid() && f.Kind() == reflect.Uint64 {
			return f.Uint()
		}
		return 0
	}
	renderer, model := reflect.ValueOf(&m.renderer).Elem(), reflect.ValueOf(m).Elem()
	counts := map[string]uint64{"body": field(renderer, "perfBodies"), "assembly": field(renderer, "perfAssemblies"), "install": field(model, "perfInstalls"), "search": field(model, "perfSearchScans")}
	if current := renderer.FieldByName("counters"); current.IsValid() {
		counts["body"], counts["assembly"] = field(current, "body"), field(current, "assembly")
		counts["rich"], counts["plain"] = field(current, "rich"), field(current, "plain")
		counts["install"], counts["search"] = field(model, "contentInstalls"), field(model, "searchScans")
	}
	return counts
}

func performanceRSS() uint64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "VmRSS:" {
			n, _ := strconv.ParseUint(fields[1], 10, 64)
			return n * 1024
		}
	}
	return 0
}
