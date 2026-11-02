package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multi-agents-etf-trade-strategy/types"
)

// 本文件集中"读已有报告"的逻辑。
// 约束（AGENTS.md §4.1）：agent/ 不允许直接读文件，跨报告复用统一走 report/ 包。
// 盘中 IntradayWatchAgent 通过 main 注入 AgentState / PreOpenAnalysis / IntradayAnalysis，
// 不再每次重跑 LLM pipeline。

// latestJSON 在 dir 下按 glob 模式查找 .json 报告：优先取当天最新，否则取全局最新。
// 文件名均含时间戳 (YYYYMMDD-HHmmss)，字典序排序即时间序。
// 找不到返回 ("", nil)，由调用方决定是否兜底（如退化到跑 pipeline）。
func latestJSON(dir, pattern string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	today := time.Now().Format("20060102")
	var todays, all []string
	for _, m := range matches {
		base := filepath.Base(m)
		all = append(all, m)
		if strings.Contains(base, today) {
			todays = append(todays, m)
		}
	}
	pool := todays
	if len(pool) == 0 {
		pool = all
	}
	sort.Strings(pool)
	return pool[len(pool)-1], nil
}

func readJSONFile(path string, v any) error {
	buf, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(buf, v); err != nil {
		return fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return nil
}

// LoadLatestAgentState 读当天最新的 8:50 主报告 JSON sidecar (etf-report-*.json)。
// 找不到返回 (nil, "", nil)，调用方可退化到跑 advice pipeline。
func LoadLatestAgentState(dir string) (*types.AgentState, string, error) {
	if dir == "" {
		dir = "report"
	}
	path, err := latestJSON(dir, "etf-report-*.json")
	if err != nil || path == "" {
		return nil, "", err
	}
	var s types.AgentState
	if err := readJSONFile(path, &s); err != nil {
		return nil, "", err
	}
	return &s, path, nil
}

// LoadLatestPreOpen 读当天最新的 9:24 集合竞价复核 JSON (preopen-report-*.json)。
// 找不到返回 (nil, "", nil)（盘中不依赖它，缺失即用 8:50 原价）。
func LoadLatestPreOpen(dir string) (*types.PreOpenAnalysis, string, error) {
	if dir == "" {
		dir = "report"
	}
	path, err := latestJSON(dir, "preopen-report-*.json")
	if err != nil || path == "" {
		return nil, "", err
	}
	var a types.PreOpenAnalysis
	if err := readJSONFile(path, &a); err != nil {
		return nil, "", err
	}
	return &a, path, nil
}

// LoadLatestIntraday 读最近一次盘中盯盘 JSON (intraday/intraday-*.json)。
// 找不到返回 (nil, "", nil)。
func LoadLatestIntraday(dir string) (*types.IntradayAnalysis, string, error) {
	if dir == "" {
		dir = "report"
	}
	path, err := latestJSON(filepath.Join(dir, "intraday"), "intraday-*.json")
	if err != nil || path == "" {
		return nil, "", err
	}
	var a types.IntradayAnalysis
	if err := readJSONFile(path, &a); err != nil {
		return nil, "", err
	}
	return &a, path, nil
}

// LoadRecentIntraday 读最近 n 次盘中盯盘快照，按时间老→新返回（用于跨刷新趋势判断）。
// 一天可能刷新很多次，故读一个序列而非单份。单个文件解析失败不阻断其余。
func LoadRecentIntraday(dir string, n int) ([]*types.IntradayAnalysis, error) {
	if dir == "" {
		dir = "report"
	}
	if n <= 0 {
		n = 5
	}
	matches, err := filepath.Glob(filepath.Join(dir, "intraday", "intraday-*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches) // 文件名含时间戳，字典序 = 老→新
	if len(matches) > n {
		matches = matches[len(matches)-n:]
	}
	out := make([]*types.IntradayAnalysis, 0, len(matches))
	for _, p := range matches {
		var a types.IntradayAnalysis
		if err := readJSONFile(p, &a); err == nil {
			out = append(out, &a)
		}
	}
	return out, nil
}
