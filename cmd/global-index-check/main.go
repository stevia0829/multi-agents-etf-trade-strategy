package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multi-agents-etf-trade-strategy/datasource"
)

// 独立只读诊断，不调用模型、不生成交易计划。
func main() {
	anchor := flag.String("as-of", "", "查询锚点，RFC3339，例如2026-11-02T08:50:00+08:00")
	symbols := flag.String("symbols", "SPX,NDX,DJI,N225,KOSPI,HSI", "内部指数代码，逗号分隔")
	source := flag.String("source", "auto", "数据源：auto / eastmoney / tencent / yahoo；指定来源时不使用备用源")
	jsonOutput := flag.Bool("json", false, "输出JSON")
	timeout := flag.Duration("timeout", 8*time.Second, "单次HTTP超时")
	flag.Parse()
	if *source != "auto" && *source != "eastmoney" && *source != "tencent" && *source != "yahoo" {
		fmt.Fprintln(os.Stderr, "invalid --source, expect auto / eastmoney / tencent / yahoo")
		os.Exit(2)
	}
	f := datasource.NewIndexFetcher()
	f.HTTP = &http.Client{Timeout: *timeout}
	if *anchor != "" {
		t, err := time.Parse(time.RFC3339, *anchor)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		f.WithAsOf(t)
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(*symbols, ",") {
		key := strings.ToUpper(strings.TrimSpace(raw))
		if key != "" && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	if len(keys) == 0 {
		fmt.Fprintln(os.Stderr, "--symbols must contain at least one index")
		os.Exit(2)
	}
	start := time.Now()
	quotes := f.FetchIndexFromSource(keys, *source)
	failed := false
	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(quotes); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else {
		fmt.Println("指数\t来源\t交易日\t状态\t最新\t昨收\t涨跌幅\t报价时间")
		for _, key := range keys {
			q := quotes[strings.ToUpper(strings.TrimSpace(key))]
			if q.Last <= 0 {
				fmt.Printf("%s\t缺失\t%s\n", key, q.Error)
				continue
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%.2f\t%.2f\t%+.2f%%\t%s\n", q.Symbol, q.Source, q.TradingDate, q.Status, q.Last, q.PrevClose, q.ChangePct*100, q.Time.Format(time.RFC3339))
		}
	}
	for _, q := range quotes {
		if q.Last <= 0 {
			failed = true
		}
	}
	available := 0
	for _, q := range quotes {
		if q.Last > 0 {
			available++
		}
	}
	fmt.Fprintf(os.Stderr, "source: %s; available: %d/%d; elapsed: %s\n", *source, available, len(keys), time.Since(start).Round(time.Millisecond))
	if failed {
		os.Exit(1)
	}
}
