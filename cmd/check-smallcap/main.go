package main

import (
	"fmt"
	"math"
	"time"

	"github.com/multi-agents-etf-trade-strategy/datasource"
)

func main() {
	ds := datasource.NewEastMoneyDataSource()

	// 512100 = 中证1000ETF南方（流动性最大的中证1000 ETF）
	codes := []string{"512100", "560010", "159531"}
	names := map[string]string{"512100": "中证1000ETF南方", "560010": "中证1000ETF广发", "159531": "中证2000ETF南方"}

	for _, code := range codes {
		klines, err := ds.GetKLine(code, 30)
		if err != nil || len(klines) < 22 {
			fmt.Printf("%s(%s): 拉取失败或数据不足: %v, len=%d\n", names[code], code, err, len(klines))
			continue
		}

		n := len(klines)
		fmt.Printf("\n=== %s(%s) 最近 %d 日 ===\n", names[code], code, n)

		// 打印最近 5 日行情
		start := n - 5
		if start < 0 {
			start = 0
		}
		for i := start; i < n; i++ {
			k := klines[i]
			chg := 0.0
			if i > 0 {
				chg = (k.Close - klines[i-1].Close) / klines[i-1].Close * 100
			}
			fmt.Printf("  %s  收:%.3f  涨跌:%+.2f%%  量:%.0f\n",
				k.Date.Format("01-02"), k.Close, chg, k.Volume)
		}

		// 计算 21 日动量分（与策略一致）
		mDays := 21
		if n < mDays {
			fmt.Printf("  数据不足 %d 日，跳过动量计算\n", mDays)
			continue
		}
		window := klines[n-mDays : n]

		// 加权对数线性回归
		y := make([]float64, mDays)
		x := make([]float64, mDays)
		w := make([]float64, mDays)
		for i := 0; i < mDays; i++ {
			y[i] = math.Log(window[i].Close)
			x[i] = float64(i)
			w[i] = 1.0 + float64(i)/float64(mDays-1) // linspace(1,2,n)
		}

		var sw, swx, swy, swxx, swxy float64
		for i := 0; i < mDays; i++ {
			sw += w[i]
			swx += w[i] * x[i]
			swy += w[i] * y[i]
			swxx += w[i] * x[i] * x[i]
			swxy += w[i] * x[i] * y[i]
		}
		denom := sw*swxx - swx*swx
		if denom == 0 {
			fmt.Printf("  回归失败\n")
			continue
		}
		slope := (sw*swxy - swx*swy) / denom
		intercept := (swy - slope*swx) / sw
		annualized := math.Exp(slope*250) - 1

		// R²
		yMean := 0.0
		for i := 0; i < mDays; i++ {
			yMean += y[i]
		}
		yMean /= float64(mDays)
		var ssRes, ssTot float64
		for i := 0; i < mDays; i++ {
			fitted := slope*x[i] + intercept
			ssRes += w[i] * (y[i] - fitted) * (y[i] - fitted)
			ssTot += w[i] * (y[i] - yMean) * (y[i] - yMean)
		}
		r2 := 1.0 - ssRes/ssTot

		score := annualized * r2
		fmt.Printf("  → 21日动量分: %.4f  (年化:%.2f%% × R²:%.3f)\n", score, annualized*100, r2)

		// 简单区间涨跌幅
		if n >= 5 {
			chg5 := (klines[n-1].Close - klines[n-5].Close) / klines[n-5].Close * 100
			fmt.Printf("  → 近5日涨跌: %+.2f%%\n", chg5)
		}
		if n >= 21 {
			chg21 := (klines[n-1].Close - klines[n-21].Close) / klines[n-21].Close * 100
			fmt.Printf("  → 近21日涨跌: %+.2f%%\n", chg21)
		}

		_ = time.Now
	}
}
