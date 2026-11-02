// rotation-backtest 对当前 Go RotationAgent 做纯量化历史回测。
// 它使用当前 Strategy3Pool、21日聚宽原版动量公式和激进过热过滤，
// 不调用 LLM / News / FinalAgent / PreOpen，适合与聚宽机械 Top1 轮动做对照。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/multi-agents-etf-trade-strategy/agent"
	"github.com/multi-agents-etf-trade-strategy/backtest"
	"github.com/multi-agents-etf-trade-strategy/datasource"
	"github.com/multi-agents-etf-trade-strategy/types"
)

func main() {
	var (
		startRaw  = flag.String("start", "2024-01-01", "回测开始日期 YYYY-MM-DD")
		endRaw    = flag.String("end", time.Now().Format("2006-01-02"), "回测结束日期 YYYY-MM-DD")
		minBars   = flag.Int("min-history", 0, "候选ETF最少历史日K；0=对齐实时advice，建议另跑60做可信性对照")
		cost      = flag.Float64("cost-per-side", 0.0008, "单边综合成本率（手续费+滑点）")
		nextOpen  = flag.Bool("next-open", true, "true=T日收盘信号、T+1开盘成交；false=同日收盘，仅复现旧口径")
		maxDays   = flag.Int("max-days", 0, "最多保留末尾N个交易日；0=完整区间")
		reportDir = flag.String("report-dir", "report", "报告输出目录")
		timeout   = flag.Duration("timeout", 2*time.Hour, "回测超时")
		workers   = flag.Int("prefetch-workers", 8, "历史K线并发预拉 worker 数")
	)
	flag.Parse()

	start, err := parseDate(*startRaw)
	if err != nil {
		fatalf("无效 --start: %v", err)
	}
	end, err := parseDate(*endRaw)
	if err != nil {
		fatalf("无效 --end: %v", err)
	}
	if end.Before(start) {
		fatalf("结束日期不能早于开始日期")
	}
	if *minBars < 0 {
		fatalf("--min-history 不能小于0")
	}
	if *cost < 0 {
		fatalf("--cost-per-side 不能小于0")
	}
	if *workers < 1 {
		fatalf("--prefetch-workers 必须大于0")
	}

	poolSize := len(agent.Strategy3Pool)
	fmt.Printf("=== Go %d只ETF · 聚宽原版21日轮动回测 ===\n", poolSize)
	fmt.Printf("区间: %s ~ %s\n", start.Format("2006-01-02"), end.Format("2006-01-02"))
	fmt.Printf("ETF池: %d只 · 窗口: 21日 · 过滤: 聚宽激进过热 · 成熟期: %d根K\n", poolSize, *minBars)
	entryMode := "T+1开盘"
	if !*nextOpen {
		entryMode = "信号日收盘（旧口径）"
	}
	fmt.Printf("成交: %s · 单边成本: %.4f\n", entryMode, *cost)

	// 每只ETF只访问一次远端接口；逐日回测从完整历史中按AsOf切片。
	baseDS := datasource.NewEastMoneyDataSource()
	codes := make([]string, 0, len(agent.Strategy3Pool)+1)
	seenCodes := map[string]struct{}{"510300": {}}
	codes = append(codes, "510300")
	for _, item := range agent.Strategy3Pool {
		if _, exists := seenCodes[item.Code]; exists {
			continue
		}
		seenCodes[item.Code] = struct{}{}
		codes = append(codes, item.Code)
	}
	fmt.Printf("预拉历史K线: %d个代码，并发=%d ...\n", len(codes), *workers)
	ds, loaded, failed := preloadHistory(baseDS, codes, 800, end, *workers)
	fmt.Printf("预拉完成: 成功=%d，失败=%d\n", loaded, failed)
	if loaded <= 1 {
		fatalf("没有获得可用ETF历史K线，停止回测")
	}
	engine := backtest.NewEngine(ds)
	engine.Variant = "joinquant"
	engine.MinHistoryBars = *minBars
	engine.CostPerSide = *cost
	engine.EntryNextOpen = *nextOpen
	engine.MaxSamples = *maxDays

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := engine.Run(ctx, start, end, 1)
	if err != nil {
		fatalf("回测失败: %v", err)
	}

	printSummary(result)
	if err := os.MkdirAll(*reportDir, 0o755); err != nil {
		fatalf("创建报告目录失败: %v", err)
	}
	stamp := time.Now().Format("20060102-150405")
	base := filepath.Join(*reportDir, fmt.Sprintf("rotation-jq%d-%s", poolSize, stamp))
	mdPath := base + ".md"
	jsonPath := base + ".json"
	if err := os.WriteFile(mdPath, []byte(backtest.BuildMarkdown(result)), 0o644); err != nil {
		fatalf("写Markdown失败: %v", err)
	}
	buf, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatalf("编码JSON失败: %v", err)
	}
	if err := os.WriteFile(jsonPath, buf, 0o644); err != nil {
		fatalf("写JSON失败: %v", err)
	}
	mdAbs, _ := filepath.Abs(mdPath)
	jsonAbs, _ := filepath.Abs(jsonPath)
	fmt.Println("Markdown:", mdAbs)
	fmt.Println("JSON:", jsonAbs)
}

// historicalDataSource 保存每只ETF截至回测末日的完整历史，按请求AsOf返回尾部切片。
// 它只优化取数，不改变K线内容、评分或交易逻辑。
type historicalDataSource struct {
	inner datasource.ETFDataSource
	data  map[string][]types.KLine
}

func (h *historicalDataSource) ListAllETFs() ([]types.ETF, error) {
	return h.inner.ListAllETFs()
}

func (h *historicalDataSource) GetKLine(code string, days int) ([]types.KLine, error) {
	return h.GetKLineAsOf(code, days, time.Time{})
}

func (h *historicalDataSource) GetKLineAsOf(code string, days int, asOf time.Time) ([]types.KLine, error) {
	all, ok := h.data[code]
	if !ok || len(all) == 0 {
		return nil, fmt.Errorf("preloaded history unavailable: %s", code)
	}
	endIdx := len(all)
	if !asOf.IsZero() {
		endIdx = 0
		for i := range all {
			if all[i].Date.After(asOf) {
				break
			}
			endIdx = i + 1
		}
	}
	if endIdx == 0 {
		return nil, fmt.Errorf("no history for %s as of %s", code, asOf.Format("2006-01-02"))
	}
	startIdx := 0
	if days > 0 && endIdx > days {
		startIdx = endIdx - days
	}
	out := make([]types.KLine, endIdx-startIdx)
	copy(out, all[startIdx:endIdx])
	return out, nil
}

func preloadHistory(inner datasource.ETFDataSource, codes []string, days int, end time.Time, workers int) (*historicalDataSource, int, int) {
	type result struct {
		code string
		data []types.KLine
		err  error
	}
	jobs := make(chan string)
	results := make(chan result, len(codes))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for code := range jobs {
				klines, err := fetchHistoryChunks(inner, code, days, end)
				if err != nil {
					results <- result{code: code, err: err}
					continue
				}
				results <- result{code: code, data: klines}
			}
		}()
	}
	go func() {
		for _, code := range codes {
			jobs <- code
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	store := make(map[string][]types.KLine, len(codes))
	loaded, failed := 0, 0
	for item := range results {
		if len(item.data) == 0 {
			failed++
			if failed <= 5 {
				fmt.Printf("  预拉失败 %s: %v\n", item.code, item.err)
			}
			continue
		}
		store[item.code] = item.data
		loaded++
	}
	return &historicalDataSource{inner: inner, data: store}, loaded, failed
}

// fetchHistoryChunks 避开行情接口单次约320根上限，按时间向前分段并去重合并。
func fetchHistoryChunks(inner datasource.ETFDataSource, code string, days int, end time.Time) ([]types.KLine, error) {
	const chunkSize = 100
	byDate := make(map[string]types.KLine, days)
	chunkEnd := end
	for len(byDate) < days {
		chunk, err := inner.GetKLineAsOf(code, chunkSize, chunkEnd)
		if err != nil {
			if len(byDate) == 0 {
				return nil, err
			}
			break
		}
		if len(chunk) == 0 {
			break
		}
		for _, k := range chunk {
			byDate[k.Date.Format("2006-01-02")] = k
		}
		oldest := chunk[0].Date
		if len(chunk) < chunkSize {
			break
		}
		chunkEnd = oldest.AddDate(0, 0, -1)
	}
	if len(byDate) == 0 {
		return nil, fmt.Errorf("no chunked history: %s", code)
	}
	out := make([]types.KLine, 0, len(byDate))
	for _, k := range byDate {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	if len(out) > days {
		out = out[len(out)-days:]
	}
	return out, nil
}

func parseDate(raw string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", raw, time.Local)
}

func printSummary(r *backtest.Result) {
	fmt.Println("\n--- 回测结果 ---")
	fmt.Printf("累计收益: %+.2f%% · 年化: %+.2f%% · 基准: %+.2f%% · Alpha: %+.2f%%\n",
		r.TotalReturn*100, r.AnnualReturn*100, r.BenchmarkReturn*100, r.Alpha*100)
	fmt.Printf("最大回撤: %.2f%% · Sharpe: %.2f · Calmar: %.2f · Sortino: %.2f\n",
		r.MaxDrawdown*100, r.Sharpe, r.Calmar, r.Sortino)
	fmt.Printf("交易: %d笔 · 胜率: %.2f%% · Profit Factor: %.2f · 平均持有: %d交易日\n",
		r.Wins+r.Losses, r.WinRate*100, r.ProfitFactor, r.HoldDays)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
