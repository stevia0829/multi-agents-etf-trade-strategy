package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/multi-agents-etf-trade-strategy/agent"
	"github.com/multi-agents-etf-trade-strategy/datasource"
)

func main() {
	ds := datasource.NewEastMoneyDataSource()
	rot := agent.NewRotationAgent(ds)
	rot.Params.MinScore = -999 // 不过滤，看全部
	rot.Params.MaxScore = 999
	rot.Params.TopN = 0
	rot.Params.MDays = 21

	cands, err := rot.Rank(context.Background())
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("ETF 池总数: %d  扫描日期: %s\n\n", len(cands), time.Now().Format("2006-01-02"))

	// 按 score 排序
	sort.Slice(cands, func(i, j int) bool {
		return cands[i].Score > cands[j].Score
	})

	// 分类打印
	fmt.Println("========== 动量分 TOP 15（最强）==========")
	fmt.Printf("%-4s %-8s %-14s %8s %8s %8s %8s\n", "排名", "代码", "名称", "动量分", "年化%", "R²", "近21日%")
	fmt.Println("--------------------------------------------------------------------------------")
	for i := 0; i < 15 && i < len(cands); i++ {
		c := cands[i]
		chg21 := calcChg21(ds, c.ETF.Code)
		fmt.Printf("%-4d %-8s %-14s %8.4f %8.2f %8.3f %7.2f%%\n",
			i+1, c.ETF.Code, c.ETF.Name, c.Score, c.Annualized*100, c.R2, chg21)
	}

	// 找中证1000 / 宽基的位置
	fmt.Println("\n========== 宽基 & 中小盘 动量分排名 ==========")
	fmt.Printf("%-4s %-8s %-16s %8s %8s %8s %8s\n", "总排", "代码", "名称", "动量分", "年化%", "R²", "近21日%")
	fmt.Println("--------------------------------------------------------------------------------")
	wideCodes := map[string]bool{
		"510050": true, "510300": true, "159922": true, "159915": true,
		"588080": true, "588380": true, "159531": true, "160211": true,
		"588000": true, "159949": true, "560610": true, "159338": true,
	}
	for i, c := range cands {
		if wideCodes[c.ETF.Code] {
			chg21 := calcChg21(ds, c.ETF.Code)
			fmt.Printf("%-4d %-8s %-16s %8.4f %8.2f %8.3f %7.2f%%\n",
				i+1, c.ETF.Code, c.ETF.Name, c.Score, c.Annualized*100, c.R2, chg21)
		}
	}

	// 统计分布
	fmt.Println("\n========== 全池动量分分布 ==========")
	var positive, negative, zero int
	var maxScore, minScore float64
	maxScore = -999
	minScore = 999
	for _, c := range cands {
		if math.Abs(c.Score) < 1e-6 {
			zero++
		} else if c.Score > 0 {
			positive++
		} else {
			negative++
		}
		if c.Score > maxScore {
			maxScore = c.Score
		}
		if c.Score < minScore {
			minScore = c.Score
		}
	}
	fmt.Printf("正动量: %d 只   负动量: %d 只   零/无效: %d 只\n", positive, negative, zero)
	fmt.Printf("最高分: %.4f   最低分: %.4f\n", maxScore, minScore)
	fmt.Printf("正动量占比: %.1f%%\n", float64(positive)/float64(len(cands))*100)

	// MinScore=0 过滤后的合格标的
	fmt.Println("\n========== MinScore=0 过滤后的合格标的（策略实际候选）==========")
	fmt.Printf("%-4s %-8s %-14s %8s %8s %8s\n", "排名", "代码", "名称", "动量分", "年化%", "R²")
	fmt.Println("--------------------------------------------------------------------------------")
	qualified := 0
	for _, c := range cands {
		if c.Score >= 0 && c.Score <= 6 {
			qualified++
			if qualified <= 20 {
				fmt.Printf("%-4d %-8s %-14s %8.4f %8.2f %8.3f\n",
					qualified, c.ETF.Code, c.ETF.Name, c.Score, c.Annualized*100, c.R2)
			}
		}
	}
	fmt.Printf("合格标的总数: %d / %d\n", qualified, len(cands))
}

func calcChg21(ds datasource.ETFDataSource, code string) float64 {
	klines, err := ds.GetKLine(code, 22)
	if err != nil || len(klines) < 22 {
		return 0
	}
	return (klines[len(klines)-1].Close - klines[len(klines)-22].Close) / klines[len(klines)-22].Close * 100
}
