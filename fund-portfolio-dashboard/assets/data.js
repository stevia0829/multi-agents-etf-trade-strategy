// ===== 持仓数据层 =====
// 修改此文件或通过截图上传自动更新
var DEFAULT_DATA = {
  lastUpdated: "2026-07-13",
  marketContext: {
    shIndex: "3934",
    volume: "2.8万亿",
    regime: "neutral → risk_off 过渡期 · 风格极端分化",
    regimeType: "risk"
  },
  holdings: [
    { id: 1, name: "易方达沪深300指数增强C", category: "A股宽基", categoryColor: "blue", amount: 64305, returnRate: -0.84, momentum: 1.2, momentumStatus: "weak", benchmark: "沪深300（周-1.27%）", advice: "持有", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 2, name: "易方达中证1000指数量化增强C", category: "A股宽基", categoryColor: "blue", amount: 55000, returnRate: 0.36, momentum: -0.8, momentumStatus: "down", benchmark: "中证1000（年化-1.22%·10年）", advice: "持有观察", action: "reduced", actionLabel: "✅ 已减仓", actionColor: "green", note: "↘已减半" },
    { id: 3, name: "长城中证500指数增强C", category: "A股宽基", categoryColor: "blue", amount: 13029, returnRate: -1.34, momentum: -1.5, momentumStatus: "down", benchmark: "中证500（周-2.76%）", advice: "持有观察", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 4, name: "浦银安盛中证A50指数增强C", category: "A股宽基", categoryColor: "blue", amount: 20000, returnRate: 0.00, momentum: 0.5, momentumStatus: "neutral", benchmark: "中证A50", advice: "持有", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 5, name: "长信中证科创创业50指数增强C", category: "A股宽基", categoryColor: "blue", amount: 10627, returnRate: 6.27, momentum: 3.8, momentumStatus: "strong-volatile", benchmark: "科创创业50（6月+26%）", advice: "持有不追", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 6, name: "财通集成电路产业股票C", category: "行业主题", categoryColor: "purple", amount: 3071, returnRate: -15.10, momentum: -2.1, momentumStatus: "down", benchmark: "集成电路（7/13 -0.55%）", advice: "止损清仓", action: "sell", actionLabel: "待止损", actionColor: "red", note: "" },
    { id: 7, name: "富国互联科技股票A", category: "行业主题", categoryColor: "purple", amount: 11103, returnRate: 6.97, momentum: 1.8, momentumStatus: "recover", benchmark: "中概互联（7/2 +3.15%）", advice: "持有/可加仓", action: "buy", actionLabel: "加仓候选", actionColor: "green", note: "" },
    { id: 8, name: "德邦半导体产业混合C", category: "行业主题", categoryColor: "purple", amount: 4000, returnRate: 61.89, momentum: 4.2, momentumStatus: "strong-volatile", benchmark: "半导体（7/2 -9%→7/9涨停）", advice: "持有底仓", action: "reduced", actionLabel: "✅ 已止盈", actionColor: "green", note: "↘已止盈半" },
    { id: 9, name: "易方达高端制造混合C", category: "行业主题", categoryColor: "purple", amount: 11517, returnRate: 13.41, momentum: 1.5, momentumStatus: "weak", benchmark: "高端制造（7/8 -3.56%）", advice: "持有", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 10, name: "易方达科鑫量化选股股票C", category: "行业主题", categoryColor: "purple", amount: 1516, returnRate: -2.28, momentum: -1.0, momentumStatus: "down", benchmark: "全市场量化", advice: "清仓", action: "sell", actionLabel: "待清仓", actionColor: "red", note: "" },
    { id: 11, name: "前海开源优势蓝筹股票C", category: "行业主题", categoryColor: "purple", amount: 39865, returnRate: 4.64, momentum: 0.8, momentumStatus: "weak", benchmark: "大盘蓝筹", advice: "持有（防御仓）", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 12, name: "博时纳斯达克100ETF联接C", category: "QDII海外", categoryColor: "green", amount: 8261, returnRate: 29.08, momentum: 2.5, momentumStatus: "weak", benchmark: "纳指100（上半年+18%）", advice: "持有不追", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 13, name: "易方达全球成长精选混合C", category: "QDII海外", categoryColor: "green", amount: 6054, returnRate: 48.75, momentum: 3.2, momentumStatus: "strong", benchmark: "美股科技（alpha强）", advice: "持有/可加仓", action: "buy", actionLabel: "加仓候选", actionColor: "green", note: "" },
    { id: 14, name: "天弘全球高端制造混合A", category: "QDII海外", categoryColor: "green", amount: 5153, returnRate: 3.18, momentum: 1.0, momentumStatus: "weak", benchmark: "全球制造", advice: "持有", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 15, name: "国富亚洲机会股票C", category: "QDII海外", categoryColor: "green", amount: 11701, returnRate: 14.58, momentum: 1.3, momentumStatus: "weak", benchmark: "亚洲市场（120日+9.1%）", advice: "持有", action: "hold", actionLabel: "持有", actionColor: "gray", note: "" },
    { id: 16, name: "建信新兴市场优选混合C", category: "QDII海外", categoryColor: "green", amount: 212, returnRate: 1.55, momentum: -0.5, momentumStatus: "down", benchmark: "新兴市场", advice: "清仓", action: "sell", actionLabel: "待清仓", actionColor: "red", note: "" },
    { id: 17, name: "鹏华弘利灵活配置混合C", category: "固收+", categoryColor: "blue", amount: 130076, returnRate: 7.86, momentum: 0, momentumStatus: "neutral", benchmark: "固收+底仓", advice: "持有不动", action: "hold", actionLabel: "持有", actionColor: "gray", note: "压舱石" }
  ],
  // 板块动量热力图数据
  sectorHeat: {
    sectors: ['半导体/芯片', '集成电路', '科创创业', '中概互联', '高端制造', '纳斯达克', '沪深300', '中证500', '中证1000', '亚洲市场', '大盘蓝筹', '新兴市场'],
    periods: ['5日', '10日', '20日', '60日', '120日'],
    matrix: [
      [ -5.2,  8.4, 15.6, 28.3, 35.1],
      [-12.1, -3.5,  6.2, 10.8,  8.4],
      [ -3.8,  5.1, 12.3, 22.5, 18.7],
      [  1.2,  3.1,  4.5,  6.8,  3.2],
      [ -1.5,  2.3,  5.8, 11.2, 13.5],
      [  0.8, -0.5,  2.1,  5.6,  8.3],
      [ -2.1, -1.3,  0.8,  3.2,  2.7],
      [ -3.5, -2.8, -1.2,  1.5,  0.8],
      [ -4.2, -3.1, -2.8, -1.5, -3.2],
      [  0.5,  1.8,  3.2,  7.5,  9.1],
      [ -0.8,  0.3,  1.5,  4.2,  5.8],
      [ -1.2, -0.8,  0.5,  2.1,  1.5]
    ]
  },
  // 新闻列表
  news: [
    { type: "bull", icon: "📈", title: "长鑫科技7/13开启初步询价，295亿超级IPO落地", desc: "国产DRAM龙头长鑫科技7月13日开启询价、7/16正式申购。募资295亿，利好半导体存储产业链——持仓中德邦半导体、集成电路相关。", impact: "利好：半导体/集成电路板块（但短期可能抽血资金面）", impactColor: "up" },
    { type: "bear", icon: "📉", title: "上证连破4000/3950逼近3900，科创50单日暴跌5.53%", desc: "7/10放量跳水：上证失守4000收3996，科创50跌5.53%。7/13续跌至3934，市场风格从科技暴涨暴跌转向极端分化。", impact: "利空：科技/成长风格短期承压，蓝筹相对抗跌", impactColor: "down" },
    { type: "bear", icon: "📉", title: "上半年1.85万亿流出宽基ETF，中证1000资金持续外流", desc: "2026上半年六大主流宽基ETF合计净流出1.85万亿。7/10中证1000单日-1.20%，资金流出白酒/军工/创新药/小盘。", impact: "利空：中证1000/中证500增强（已减仓对冲）", impactColor: "down" },
    { type: "neutral", icon: "⚖️", title: "纳斯达克100上半年+18%，6月高位震荡(-0.19%)", desc: "纳指100 2024+24.88%、2025+20.17%、2026上半年+5.63%。QDII限购严重，场外买不进。", impact: "中性：持有享受趋势，不宜追高", impactColor: "warn" },
    { type: "bull", icon: "📈", title: "中概互联低位企稳反弹，资金逆势布局中国互联网资产", desc: "7/2中概互联ETF +3.15%，纳斯达克金龙指数+2.93%。阿里巴巴、拼多多等龙头集体反弹。", impact: "利好：富国互联科技A（估值底部+资金回流）", impactColor: "up" },
    { type: "neutral", icon: "⚖️", title: "周末多重利好落地，机构看好下周反弹", desc: "7/12周末：流动性释放、产业政策、外资持仓回升、中报业绩预期、市场监管五大维度利好。但7/13早盘继续下探，市场仍存分歧。", impact: "待观察：利好兑现 vs 恐慌情绪", impactColor: "warn" }
  ]
};
