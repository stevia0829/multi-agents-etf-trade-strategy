package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/multi-agents-etf-trade-strategy/datasource"
)

func main() {
	now := time.Now()
	fmt.Printf("== 运行时间: %s (%s) ==\n", now.Format("2006-01-02 15:04:05"), now.Location())

	// ---------- 1. ETF 场内实时报价（腾讯 qt.gtimg.cn，含 IOPV） ----------
	ds := datasource.NewEastMoneyDataSource()
	codes := []string{"159687", "513100", "159941", "518850", "159529"}
	fmt.Println("\n== ETF 实时报价 (源: Tencent qt.gtimg.cn) ==")
	type row struct {
		Code, Name string
		Price, Prev, IOPV, ChgPct, PremPct float64
		Time string
		Age  string
	}
	var rows []row
	for _, c := range codes {
		q, err := ds.FetchRealtimeQuote(c)
		if err != nil {
			fmt.Printf("%-8s ERROR: %v\n", c, err)
			continue
		}
		age := ""
		if !q.Time.IsZero() {
			age = now.Sub(q.Time).Round(time.Second).String()
		}
		r := row{q.Code, q.Name, q.Price, q.PrevClose, q.IOPV, q.ChangePct, q.PremiumPct() * 100, q.Time.Format("2006-01-02 15:04:05"), age}
		rows = append(rows, r)
		fmt.Printf("%-7s %-12s 现价%.3f 昨收%.3f 涨跌%+.2f%% IOPV %.4f 溢价%+.2f%% 报价时间 %s (距今 %s)\n",
			r.Code, r.Name, r.Price, r.Prev, r.ChgPct, r.IOPV, r.PremPct, r.Time, r.Age)
	}

	// ---------- 2. 均线（前复权日 K，至最新） ----------
	fmt.Println("\n== 均线与区间 (前复权日K) ==")
	for _, c := range codes {
		kl, err := ds.GetKLine(c, 80)
		if err != nil || len(kl) < 60 {
			fmt.Printf("%-7s K线不足/失败: %v\n", c, err)
			continue
		}
		ma := func(p int) float64 {
			s := 0.0
			for _, k := range kl[len(kl)-p:] {
				s += k.Close
			}
			return s / float64(p)
		}
		lo, hi := kl[len(kl)-1].Low, kl[len(kl)-1].High
		for _, k := range kl[len(kl)-30:] {
			if k.Low < lo {
				lo = k.Low
			}
			if k.High > hi {
				hi = k.High
			}
		}
		last := kl[len(kl)-1]
		fmt.Printf("%-7s 末根K %s close=%.3f partial=%v | MA5 %.4f MA10 %.4f MA20 %.4f MA60 %.4f | 30日区间 %.3f~%.3f\n",
			c, last.Date.Format("2006-01-02"), last.Close, last.IsPartial, ma(5), ma(10), ma(20), ma(60), lo, hi)
	}

	// ---------- 3. 海外指数（Tencent / Stooq / Yahoo 多源） ----------
	fmt.Println("\n== 海外指数 ==")
	f := datasource.NewIndexFetcher()
	idx := f.FetchIndex([]string{"N225", "KOSPI", "HSI", "NDX", "SPX", "DJI"})
	for _, k := range []string{"N225", "KOSPI", "HSI", "NDX", "SPX", "DJI"} {
		q, ok := idx[k]
		if !ok {
			fmt.Printf("%-6s 无数据\n", k)
			continue
		}
		ts := "时间戳缺失"
		if !q.Time.IsZero() {
			ts = q.Time.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("%-6s %-10s last=%.2f prev=%.2f chg=%+.2f%% time=%s source=%s\n",
			k, q.Name, q.Last, q.PrevClose, q.ChangePct*100, ts, q.Source)
	}

	// ---------- 4. 机器可读 ----------
	b, _ := json.MarshalIndent(map[string]interface{}{"now": now.Format(time.RFC3339), "etf": rows, "index": idx}, "", "  ")
	os.WriteFile("/tmp/quotecheck.json", b, 0o644)
	fmt.Println("\n(JSON -> /tmp/quotecheck.json)")
}
