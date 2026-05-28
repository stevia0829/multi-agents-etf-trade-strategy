# Eino Multi-Agent ETF Strategy

A 股 ETF 开盘前多 Agent 分析系统。基于 [eino](https://github.com/cloudwego/eino) multi-agent 编排思想构建：以"策略 3 ETF 轮动"（来自 [agent/strategy.py](agent/strategy.py)）为核心动量打分器，叠加技术面、消息面、宏观、资金面、跨境联动 5 路 Agent，最后由 FinalAgent 加权融合，给出**次日开盘前的交易决策**（标的 / 入场价 / 止损 / 止盈 / 仓位上限）。

LLM 统一使用 **DeepSeek (deepseek-chat)**，通过 OpenAI 兼容协议调用；具备**主备模型 + 静态 JSON 兜底**的三级降级能力，离线 / 网络故障也能跑出规则版决策。

---

## 一、快速开始

### 1. 准备环境

- Go 1.21+
- DeepSeek API Key（项目内置默认值，可直接跑）；如需替换：

```bash
export DEEPSEEK_API_KEY=sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### 2. 一键出今日报告（默认 advice 模式）

```bash
go run .
```

输出：终端打印 + 自动落地 Markdown 报告到 `report/etf-report-YYYYMMDD-HHMMSS.md`。

### 3. 命令行参数

```bash
go run . [flags]
```

| Flag | 默认值 | 说明 |
|---|---|---|
| `--mode` | `advice` | 运行模式：`advice`（单次出报告） / `backtest`（历史回测） |
| `--date` | 当天 | 基准日期 `YYYY-MM-DD`，可用于复盘 / 跑指定交易日 |
| `--current-hold` | 空 | 可选：当前持仓 ETF 代码（如 `159915`），用于在报告"持仓对照"章节给出建议；**留空即跳过，系统不做任何本地持久化** |
| `--report-dir` | `report` | 报告输出目录 |
| `--skip-report` | `false` | 仅打印结果，不落地 Markdown |
| `--bt-start` | 一年前 | 回测起始日（仅 backtest 模式） |
| `--bt-end` | `--date` 或今天 | 回测结束日 |
| `--bt-step` | `5` | 采样间隔（交易日） |
| `--bt-hold` | `5` | 持有期（交易日） |
| `--bt-max` | `60` | 最大样本数 |

### 4. 常用命令示例

```bash
# 出今天的开盘前决策
go run .

# 出明早 (5/26) 开盘前决策（K 线自动 clamp 到最近收盘日）
go run . --date 2026-05-26

# 复盘上周一
go run . --date 2026-05-18

# 报告输出到自定义目录
go run . --date 2026-05-26 --report-dir ./tmp_report

# 给出"我手上是创业板 ETF"的持仓对照建议
go run . --current-hold 159915

# 历史回测：最近 60 个采样点，每个采样点持有 5 个交易日
go run . --mode backtest --bt-start 2025-12-01 --bt-end 2026-05-25 --bt-step 5 --bt-hold 5

# 仅打印不落地报告（CI / 调试场景）
go run . --skip-report
```

### 5. 测试 / 构建

```bash
go build ./...        # 编译全部包
go vet ./...          # 静态检查
go test ./...         # 单元测试（含 momentum_score / rotation / writer / resilient）
```

---

## 二、整体架构

### 2.1 编排流程图

```
                    ┌─────────────────┐
   start ─────▶     │  ScreenerAgent  │  策略3 + 60日 K 线 + 归一化
                    └────────┬────────┘
                             │ Top5 + Best
                             ▼
                    ┌─────────────────┐
                    │   fan-out (5)   │
                    └─┬──┬──┬──┬──┬───┘
              ┌───────┘  │  │  │  └────────┐
              ▼          ▼  ▼  ▼           ▼
        ┌─────────┐ ┌──────┐ ┌──────┐ ┌─────────┐ ┌──────────┐
        │  News   │ │Global│ │ Tech │ │ Regime  │ │MoneyFlow │
        │ (LLM)   │ │(LLM) │ │(LLM) │ │ (规则)  │ │ (规则)   │
        └─────────┘ └──────┘ └──────┘ └─────────┘ └──────────┘
              │          │      │         │           │
              └──────────┴──┬───┴─────────┴───────────┘
                            ▼
                    ┌─────────────────┐
                    │  FinalAgent     │  6 路加权 → 决策
                    │  (LLM + 规则)   │  入场 / 止损 / 止盈
                    └────────┬────────┘
                             ▼
                          end + Markdown 报告
```

入口实现：[orchestrator/pipeline.go](orchestrator/pipeline.go) 的 `Pipeline.Run`，使用 `sync.WaitGroup` 实现 fan-out / fan-in，所有 Agent 共享 `*types.AgentState`。

### 2.2 各 Agent 职责

| Agent | 类型 | 输入 | 输出 | 数据来源 |
|---|---|---|---|---|
| **ScreenerAgent** | 规则 | 全部 ETF | Top5 + Best（含 60 日技术指标） | K 线（腾讯） |
| **RotationAgent** | 规则 | Strategy3Pool (62 只) | 21 日加权动量得分 | K 线（腾讯） |
| **NewsAgent** | LLM | Best 标的板块 | 情绪 / 评分 / 关键要点 | DeepSeek |
| **GlobalMarketAgent** | LLM | — | 美股前夜 + 日韩盘中 | DeepSeek |
| **TechnicalAgent** | LLM | Best 标的 60 日 K 线 + 指标 | 趋势 / 支撑压力 / 持仓区间 | DeepSeek |
| **RegimeAgent** | 规则 | 510300 K 线 | 宏观趋势 / 仓位上限 | K 线（腾讯） |
| **MoneyFlowAgent** | 规则 | Best 标的 K 线 | 北向 / 申赎 / 主力代理估算 | K 线（腾讯） |
| **FinalAgent** | LLM + 规则 | 上述全部 | 综合决策（入场 / 止损 / 止盈） | DeepSeek |

### 2.3 加权融合公式

来自 [agent/final.go](agent/final.go) 的 system prompt：

```
综合评分 = 0.30 × 量化分
        + 0.25 × 技术面
        + 0.15 × 资金面
        + 0.10 × 消息面
        + 0.10 × 海外联动
        + 0.10 × 宏观环境
```

**Recommendation 映射**：
- ≥ 80 → `strong_buy`
- ≥ 65 → `buy`
- ≥ 50 → `hold`
- < 50 → `avoid`

**Regime 反向约束**：
- `regime.trend == "risk_off"` → 强制 `avoid`
- `regime.trend == "bear"` → 降一档（`strong_buy / buy → hold`）
- `position_cap` 必须 ≤ regime 给的上限

---

## 三、核心策略：策略 3 ETF 轮动

### 3.1 来源

完整 Python 实现见 [agent/strategy.py](agent/strategy.py) 的 `get_etf_rank` 函数。Go 端在 [agent/rotation.go](agent/rotation.go) 等价实现。

### 3.2 核心动量公式

```
1. y = log(close[-21:])
2. x = arange(21);  weights = linspace(1, 2, 21)     # 越近权重越高
3. slope, intercept = polyfit(x, y, 1, w=weights)    # 加权线性回归
4. annualized = exp(slope × 250) - 1                 # 年化收益率
5. R² = 1 - Σw·(y-ŷ)² / Σw·(y-ȳ)²                    # 加权 R²
6. score = annualized × R²                           # 最终动量得分
```

实现：[indicator/momentum_score.go](indicator/momentum_score.go)

### 3.3 过滤参数（来自 strategy.py）

| 参数 | 值 | 含义 |
|---|---|---|
| `m_days` | 21 | 动量参考天数 |
| `max_score` | 6 | 过热阈值，超过需要日间 1.1× 加速门槛 |
| `min_score` | -1 | 下限（Go 端主动放宽，原 Python 为 0） |
| `score_threshold_multiplier` | 1.1 | 过热标的的日间增长门槛 |

### 3.4 Action 五态语义

由 [`RotationCandidate.Action()`](agent/rotation.go) 给出，**完全无状态**（不依赖本地持仓）：

| Action | 含义 | 触发条件 |
|---|---|---|
| 🚀 `strong_buy` | 强烈买入（动量加速） | `score_T ≥ score_{T-1} × 1.1` |
| ✅ `buy` | 买入（动量向上 / 反转） | `score_T > score_{T-1}` 或 `prev≤0 且 score>0` |
| ⏸ `hold_only` | 观望（动量减速） | `score_T ≤ score_{T-1}` |
| ❌ `avoid` | 回避（趋势失效） | `score < 0` 或 `R² < 0.3` |

---

## 四、数据源

详细见 [datasource/eastmoney.go](datasource/eastmoney.go)。**三级降级**保证可用性：

```
1. 腾讯财经 web.ifzq.gtimg.cn  ← 主源（前复权 qfq，本机环境最稳定）
       ↓ 失败
2. 东方财富 push2his.eastmoney.com  ← 备源（fqt=1 前复权）
       ↓ 失败
3. mockKLinesAsOf  ← 离线兜底（含 code seed，避免回测退化）
```

主源 URL 范例：

```
https://web.ifzq.gtimg.cn/appstock/app/fqkline/get?param=sh518880,day,,2026-05-25,22,qfq
```

ETF 池查询走东方财富 `push2.eastmoney.com/api/qt/clist/get`；失败时回退到 12 只硬编码池。注意**策略 3 实际用的是写死的 [Strategy3Pool](agent/rotation.go)（62 只）**，与 strategy.py 的 `g.etf_pool_3` 对齐。

---

## 五、目录结构

```
eino-muti-etf-strategy/
├── main.go                         # CLI 入口（advice / backtest 模式）
├── orchestrator/
│   └── pipeline.go                 # 多 Agent 编排（fan-out/fan-in）
├── agent/
│   ├── screener.go                 # 量化筛选（轮动 + 技术指标 + 归一化）
│   ├── rotation.go                 # 策略 3 轮动核心 + Action 语义
│   ├── technical.go                # 技术面 LLM Agent
│   ├── news.go                     # 消息面 LLM Agent
│   ├── global.go                   # 跨境联动 LLM Agent
│   ├── regime.go                   # 宏观环境 (510300 趋势/仓位上限)
│   ├── moneyflow.go                # 资金面代理估算
│   ├── final.go                    # 决策融合 (LLM + 规则降级)
│   ├── common.go                   # 共享：callLLMJSON / weightedScore / Cap
│   └── strategy.py                 # Python 原版策略 3 (源真理)
├── indicator/
│   ├── momentum_score.go           # 21 日加权动量得分
│   └── indicator.go                # MA / RSI / MACD / Volatility 等
├── datasource/
│   └── eastmoney.go                # K 线 + ETF 池 (腾讯/东财/mock)
├── llm/
│   ├── client.go                   # LLM 客户端接口
│   ├── deepseek.go                 # DeepSeek (OpenAI 兼容协议)
│   ├── factory.go                  # 配置 → 客户端
│   └── resilient.go                # 主备 + 静态 fallback
├── backtest/
│   └── engine.go                   # 历史胜率回测 (规则版决策)
├── report/
│   ├── writer.go                   # Markdown 报告生成器
│   └── etf-report-*.md             # 已生成的报告
├── config/
│   └── config.go                   # 配置 (LLM 主备 / API Key / 模型名)
├── types/
│   └── types.go                    # 共享类型 (AgentState / KLine / ETF...)
├── go.mod
└── README.md
```

---

## 六、典型输出示例

### 6.1 Advice 模式（终端）

```
=== A 股 ETF 开盘前多 Agent 分析 ===
基准日期: 2026-05-25 (回测/复盘模式)
[pipeline] step1: screener running…
[pipeline] step1 done. best=卫星产业ETF(159218) score=95.23
[pipeline] step2: news / global / technical / regime / moneyflow fan-out…
[pipeline] step2 done.
[pipeline] step3: final agent aggregating…
[pipeline] step3 done. recommendation=buy score=68.00

--- Top5 候选 ---
1) 卫星产业ETF(159218) sector=军工 score=95.23 action=buy ...
2) 德国30ETF(513030) sector=海外 score=87.89 action=strong_buy ...
...

=== 最终交易决策 ===
综合评分: 68.00
建议: buy
入场: 6.240  止损: 6.100  止盈: 6.500
理由: ① 整体逻辑 ... ② 关键风险 ... ③ 操作要点 ...

📄 Markdown 报告已生成: report/etf-report-20260525-195335.md
```

### 6.2 Backtest 模式

```bash
go run . --mode backtest --bt-start 2025-12-01 --bt-end 2026-05-25
```

```
样本=60 实际建仓=38 胜率=55.26% 平均加权收益=+0.42% Sharpe=0.31
📄 Markdown 报告已生成: report/backtest-20260525-183951.md
```

---

## 七、扩展与注意事项

1. **K 线无缓存**：单次 advice 约 65~70 个 HTTP 请求，回测约几千个。如需密集回测，建议在 `datasource` 层加文件缓存（按 `code+date` 落 JSON）。

2. **News / Global 是 LLM 推断**：跑"未来日期"模式时，News / Global Agent 给出的是 LLM 基于训练数据的合理猜测，**不是真实新闻 / 行情**。Screener / Tech / Regime / MoneyFlow 仍基于真实 K 线，可信度高。

3. **持仓信息无状态**：`--current-hold` 只在本次会话使用，**不写入任何文件 / 不持久化**，符合"用户不存持仓"的设计前提。

4. **想严格复现 Python 策略 3**：把 [agent/rotation.go](agent/rotation.go) 中 `DefaultRotationParams.MinScore` 改回 `0`，并把 `Rank()` 的步骤 1+2 改为"过热触发时只留过热标的，未达标回退 min~max"即可。当前 Go 端 3 处主动偏离（MinScore=-1、过热软门槛、Action 负分反转）是为了配合多 Agent 软评分模式。

5. **eino 框架接入**：当前 `orchestrator/pipeline.go` 用 `sync.WaitGroup` 模拟 `compose.Graph` 的 fan-out/fan-in 行为；如需切换到真正的 eino，按其 graph DSL 重写 `Pipeline.Run` 即可，Agent 接口已经收敛成 `Run(ctx) (*Result, error)` 形式。

---

## 八、致谢与免责声明

- 策略 3 ETF 轮动算法基于聚宽社区开源策略改写（见 [strategy.py](agent/strategy.py)）。
- 本项目仅用于研究 / 学习多 Agent 编排与量化策略组合，**报告内容不构成投资建议**，请勿用于实盘决策。
