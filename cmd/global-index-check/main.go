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
	jsonOutput := flag.Bool("json", false, "输出JSON")
	timeout := flag.Duration("timeout", 8*time.Second, "单次HTTP超时")
	flag.Parse()
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
	keys := strings.Split(*symbols, ",")
	start := time.Now()
	quotes := f.FetchIndex(keys)
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
	fmt.Fprintln(os.Stderr, "elapsed:", time.Since(start).Round(time.Millisecond))
	if failed {
		os.Exit(1)
	}
}
