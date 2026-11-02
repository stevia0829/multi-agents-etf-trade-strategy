package report

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadLatestAgentStatePrefersToday 今天的报告优先于历史报告，且同一天取最新时间戳。
func TestLoadLatestAgentStatePrefersToday(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().Format("20060102")

	// 一份旧报告（去年）
	writeAgentState(t, dir, "etf-report-20240101-090000.json", "510300")
	// 今天两份，第二份更新
	writeAgentState(t, dir, "etf-report-"+today+"-080000.json", "159915")
	writeAgentState(t, dir, "etf-report-"+today+"-180000.json", "512660")

	got, path, err := LoadLatestAgentState(dir)
	if err != nil {
		t.Fatalf("LoadLatestAgentState: %v", err)
	}
	if got == nil || got.Final == nil {
		t.Fatal("expected non-nil state with Final")
	}
	if got.Final.TargetETF.ETF.Code != "512660" {
		t.Errorf("expected latest today code 512660, got %s (path=%s)", got.Final.TargetETF.ETF.Code, path)
	}
	if filepath.Base(path) != "etf-report-"+today+"-180000.json" {
		t.Errorf("unexpected path: %s", path)
	}
}

// TestLoadLatestAgentStateMissing 找不到时返回 nil + 无 error（调用方退化兜底）。
func TestLoadLatestAgentStateMissing(t *testing.T) {
	got, path, err := LoadLatestAgentState(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error on missing, got %v", err)
	}
	if got != nil || path != "" {
		t.Errorf("expected (nil,\"\"), got (%v,%q)", got, path)
	}
}

// TestLoadLatestIntraday 从 intraday/ 子目录读最近一次盯盘快照。
func TestLoadLatestIntraday(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "intraday")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("20060102")
	writeFile(t, sub, "intraday-"+today+"-100000.json", `{"generated_at":"2026-06-26T10:00:00Z","snapshots":[{"etf_code":"159915","price":1.2}]}`)
	writeFile(t, sub, "intraday-"+today+"-110000.json", `{"generated_at":"2026-06-26T11:00:00Z","snapshots":[{"etf_code":"159915","price":1.25}]}`)

	got, _, err := LoadLatestIntraday(dir)
	if err != nil || got == nil {
		t.Fatalf("LoadLatestIntraday: %v got=%v", err, got)
	}
	if len(got.Snapshots) != 1 || got.Snapshots[0].Price != 1.25 {
		t.Errorf("expected latest price 1.25, got %+v", got.Snapshots)
	}
}

// TestLoadRecentIntradayOrderAndLimit 取最近 n 份且按老→新返回。
func TestLoadRecentIntradayOrderAndLimit(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "intraday")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("20060102")
	// 6 份，价格序列 1.10 1.20 1.15 1.30 1.25 1.40（时间戳递增）
	seq := []struct {
		ts    string
		price float64
	}{
		{"100000", 1.10}, {"110000", 1.20}, {"120000", 1.15},
		{"130000", 1.30}, {"140000", 1.25}, {"150000", 1.40},
	}
	for _, p := range seq {
		writeFile(t, sub, fmt.Sprintf("intraday-%s-%s.json", today, p.ts),
			fmt.Sprintf(`{"snapshots":[{"etf_code":"159915","price":%g}]}`, p.price))
	}

	got, err := LoadRecentIntraday(dir, 3)
	if err != nil {
		t.Fatalf("LoadRecentIntraday: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 snapshots (n=3), got %d", len(got))
	}
	// 老→新，最近 3 份价格应为 1.30, 1.25, 1.40
	want := []float64{1.30, 1.25, 1.40}
	for i, w := range want {
		if got[i].Snapshots[0].Price != w {
			t.Errorf("idx %d: want %v, got %v (order should be old→new)", i, w, got[i].Snapshots[0].Price)
		}
	}
}

func writeAgentState(t *testing.T, dir, name, code string) {
	t.Helper()
	// 最小可用 AgentState：只填 Final.TargetETF，足够 LoadLatestAgentState 返回非空。
	buf := `{"final":{"target_etf":{"etf":{"code":"` + code + `"}},"overall_score":80,"recommendation":"buy"}}`
	writeFile(t, dir, name, buf)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
