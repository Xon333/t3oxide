package ui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

// A real one-shot child process speaks the existing preview helper protocol.
// It has no native server, credentials, journal, socket or mutation handlers.
// This isolates the portable optional-helper/Go/UI boundary; adapter-native
// bounded-read contracts are covered by the adapter integration fixtures.
const performancePreviewHelper = `import readline from 'node:readline';
import {appendFileSync} from 'node:fs';
const log=process.argv[process.argv.indexOf('--config')+1];
const environmentId='source-env';
const note=value=>appendFileSync(log,JSON.stringify({...value,rss:process.memoryUsage().rss})+'\n');
note({stage:'process'});
readline.createInterface({input:process.stdin}).on('line', async line=>{
 const a=JSON.parse(line); note({stage:'read',op:a.op});
 let result;
 if(a.op==='hello') result={environmentId,protocol:1,orchestrationProtocol:2,features:{threadPreview:true}};
 else if(a.op==='thread-preview') {
  await new Promise(resolve=>setTimeout(resolve,10));
  const items=Array.from({length:24},(_,i)=>({key:a.threadId+'/'+i,sourceThreadId:a.threadId,itemId:String(i),messageId:'synthetic-'+i,kind:'assistant_message',text:'Synthetic bounded preview paragraph. '.repeat(8),truncated:false,streaming:false,attachmentCount:0,position:i}));
  const observedAt='2026-10-30T00:00:00Z';
  result={threadId:a.threadId,available:true,readOnly:true,observedAt,view:{revision:1,threadId:a.threadId,title:'Synthetic preview',model:'synthetic',status:'idle',activeRunId:null,requests:[],hasMore:false,historyCursor:null,limited:false,sequence:1,observedAt,state:'synchronizing',live:false,historical:false,items}};
 } else { note({stage:'forbidden',op:a.op});process.exit(41); }
 const frame=JSON.stringify({v:1,environmentId,id:a.id,result})+'\n';
 note({stage:'write',bytes:Buffer.byteLength(frame)});process.stdout.write(frame);
});`

func TestRefinementDemandPreviewForegroundSamples(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("T3OXIDE_PREVIEW_PERF_SAMPLES"))
	if n == 0 {
		t.Skip("opt-in actual helper protocol foreground evidence")
	}
	if n < 1000 || n > 10000 {
		t.Fatal("preview samples must be bounded to1000..10000")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	modes := []bool{false, true}
	if mode := os.Getenv("T3OXIDE_PREVIEW_PERF_MODE"); mode == "disabled" {
		modes = []bool{false}
	} else if mode == "enabled" {
		modes = []bool{true}
	} else if mode != "" {
		t.Fatal("preview performance mode must be disabled or enabled")
	}
	for _, enabled := range modes {
		name := "preview-disabled"
		if enabled {
			name = "preview-enabled"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			helper, log := filepath.Join(dir, "preview.mjs"), filepath.Join(dir, "operations.ndjson")
			if err := os.WriteFile(helper, []byte(performancePreviewHelper), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(log, nil, 0600); err != nil {
				t.Fatal(err)
			}
			state, err := config.OpenState(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			manager := client.NewManager([]config.Environment{{ID: "source", EnvironmentID: "source-env", Label: "Synthetic", Enabled: true, Node: node, Adapter: helper, HostConfig: log}}, state)
			t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
			session, err := manager.NewSession("source", "attached-thread")
			if err != nil {
				t.Fatal(err)
			}
			m := refinementNavigation(t, 24)
			m.session, m.sessionCtx = session, m.ctx
			m.composer.SetValue("Synthetic exact draft preserved during preview.")
			draft := m.currentDraft()
			for range 100 {
				m.Update(keyPress(tea.KeyUp))
				_ = m.View()
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			type completion struct {
				msg     tea.Msg
				elapsed int64
			}
			results := make(chan completion, 1)
			active, started, finished := false, 0, 0
			updateTimes, viewTimes, roundtripTimes, completionTimes := []int64{}, []int64{}, []int64{}, []int64{}
			rssBefore, rssPeak := performanceRSS(), uint64(0)
			heapPeak := before.HeapAlloc
			consume := func(result completion) {
				active = false
				finished++
				roundtripTimes = append(roundtripTimes, result.elapsed)
				msg, ok := result.msg.(navigatorPreviewMsg)
				if !ok || msg.err != nil {
					t.Fatalf("actual preview helper failed: %#v", result.msg)
				}
				start := time.Now()
				m.Update(msg)
				completionTimes = append(completionTimes, time.Since(start).Nanoseconds())
				if m.detailTitle != "Preview" || m.navigator.preview == nil || !strings.Contains(m.detail, "Synthetic bounded preview") {
					t.Fatal("exact helper result failed to reach read-only preview")
				}
			}
			for i := range n {
				select {
				case result := <-results:
					consume(result)
				default:
				}
				if i%64 == 0 {
					if m.detail != "" {
						m.Update(keyPress(tea.KeyEscape))
					}
					if enabled && !active {
						cmd := m.previewNavigator()
						if cmd == nil {
							t.Fatal("explicit preview demand did not produce one-shot read")
						}
						active = true
						started++
						go func() { start := time.Now(); msg := cmd(); results <- completion{msg, time.Since(start).Nanoseconds()} }()
					}
				}
				start := time.Now()
				m.Update(keyPress(tea.KeyUp))
				updated := time.Now()
				_ = m.View()
				viewed := time.Now()
				updateTimes = append(updateTimes, updated.Sub(start).Nanoseconds())
				viewTimes = append(viewTimes, viewed.Sub(updated).Nanoseconds())
				if i%64 == 0 {
					var memory runtime.MemStats
					runtime.ReadMemStats(&memory)
					heapPeak = max(heapPeak, memory.HeapAlloc)
					rssPeak = max(rssPeak, performanceRSS())
				}
			}
			if active {
				select {
				case result := <-results:
					consume(result)
				case <-time.After(10 * time.Second):
					t.Fatal("bounded preview did not drain")
				}
			}
			if m.detail != "" {
				m.Update(keyPress(tea.KeyEscape))
			}
			if m.currentDraft() != draft || m.status.ThreadID != "attached-thread" || session.Status().ThreadID != "attached-thread" {
				t.Fatal("optional preview changed exact working context")
			}
			if enabled && (started == 0 || started != finished) {
				t.Fatal("preview fixture performed no complete bounded reads")
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			processes, hellos, reads, writes, bytes := 0, 0, 0, 0, 0
			helperRSS := uint64(0)
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if line == "" {
					continue
				}
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if rss, ok := entry["rss"].(float64); ok {
					helperRSS = max(helperRSS, uint64(rss))
				}
				switch entry["stage"] {
				case "process":
					processes++
				case "forbidden":
					t.Fatalf("preview attempted forbidden operation: %v", entry["op"])
				case "read":
					if entry["op"] == "hello" {
						hellos++
					} else if entry["op"] == "thread-preview" {
						reads++
					} else {
						t.Fatal("unexpected preview operation")
					}
				case "write":
					writes++
					bytes += int(entry["bytes"].(float64))
				}
			}
			if processes != started || hellos != started || reads != started || writes != 2*started {
				t.Fatalf("helper lifecycle/read count differs: processes=%d hello=%d preview=%d writes=%d demands=%d", processes, hellos, reads, writes, started)
			}
			runtime.ReadMemStats(&after)
			record := map[string]any{"fixture_version": 3, "scenario": name, "samples": n, "warmup": 100, "boundary": "synthetic Up -> Model.Update and separate View while explicit one-shot preview helper runs/finishes; no terminal or native server", "platform": runtime.GOOS + "/" + runtime.GOARCH, "go": runtime.Version(), "update_ns": updateTimes, "view_ns": viewTimes, "preview_roundtrip_ns": roundtripTimes, "frame_install_update_ns": completionTimes, "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "heap_after_bytes": after.HeapAlloc, "heap_peak_sampled_bytes": max(heapPeak, after.HeapAlloc), "gc_cycles": after.NumGC - before.NumGC, "gc_pause_ns": after.PauseTotalNs - before.PauseTotalNs, "rss_before_bytes": rssBefore, "rss_peak_sampled_bytes": max(rssPeak, performanceRSS()), "helper_rss_peak_bytes": helperRSS, "combined_rss_peak_upper_bound_bytes": max(rssPeak, performanceRSS()) + helperRSS, "helper_processes": processes, "hello_reads": hellos, "preview_reads": reads, "wire_writes": writes, "wire_bytes": bytes, "worker_high_water": min(1, started), "pending_high_water": 0, "useful_frames": finished, "failures": 0}
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			t.Log(string(encoded))
		})
	}
}
