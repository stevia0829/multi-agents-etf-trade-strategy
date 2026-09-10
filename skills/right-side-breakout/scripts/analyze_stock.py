#!/usr/bin/env python3
"""A-share right-side breakout analyzer using Tencent quote/K-line endpoints."""

from __future__ import annotations

import argparse
import json
import re
import statistics
import sys
import urllib.parse
import urllib.request
from datetime import datetime
from html import escape
from pathlib import Path

UA = "Mozilla/5.0 right-side-breakout-skill/1.0"


def symbol_of(code: str) -> tuple[str, str]:
    s = code.lower().strip()
    if re.fullmatch(r"(?:sh|sz)\d{6}", s):
        return s, s[2:]
    if not re.fullmatch(r"\d{6}", s):
        raise ValueError("股票代码必须为6位数字，或 sh/sz + 6位数字")
    if s.startswith(("6", "68")):
        return "sh" + s, s
    if s.startswith(("0", "2", "3")):
        return "sz" + s, s
    raise ValueError(f"暂无法自动识别交易所: {s}")


def get_text(url: str) -> str:
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Referer": "https://stockapp.finance.qq.com/"})
    with urllib.request.urlopen(req, timeout=12) as r:
        raw = r.read()
    for enc in ("utf-8", "gbk", "gb18030"):
        try:
            return raw.decode(enc)
        except UnicodeDecodeError:
            pass
    return raw.decode("utf-8", errors="replace")


def fetch_quote(symbol: str) -> dict:
    text = get_text("https://qt.gtimg.cn/q=" + urllib.parse.quote(symbol))
    m = re.search(r'="(.*)"', text)
    if not m:
        raise RuntimeError("腾讯实时行情返回格式异常")
    f = m.group(1).split("~")
    if len(f) < 40:
        raise RuntimeError("腾讯实时行情字段不足")

    def num(i, default=0.0):
        try:
            return float(f[i])
        except (ValueError, IndexError):
            return default

    stamp = f[30] if len(f) > 30 else ""
    trade_date = stamp[:8] if re.fullmatch(r"\d{14}", stamp or "") else ""
    return {
        "name": f[1], "code": f[2], "price": num(3), "prev_close": num(4), "open": num(5),
        "volume_lot": num(6), "high": num(33), "low": num(34), "change": num(31), "pct": num(32),
        "turnover_amount_wan": num(37), "turnover_rate": num(38), "timestamp": stamp,
        "trade_date": trade_date,
    }


def fetch_kline(symbol: str, n: int = 120) -> list[dict]:
    param = f"{symbol},day,,,{n},qfq"
    url = "https://web.ifzq.gtimg.cn/appstock/app/fqkline/get?param=" + urllib.parse.quote(param, safe=",")
    obj = json.loads(get_text(url))
    node = obj.get("data", {}).get(symbol, {})
    rows = node.get("qfqday") or node.get("day")
    if not rows:
        raise RuntimeError("腾讯日K数据为空")
    out = []
    for r in rows:
        if len(r) < 6:
            continue
        out.append({"date": r[0].replace("-", ""), "open": float(r[1]), "close": float(r[2]),
                    "high": float(r[3]), "low": float(r[4]), "volume": float(r[5])})
    if len(out) < 35:
        raise RuntimeError("日K数量不足，至少需要35根")
    return out


def mean(vals):
    return statistics.fmean(vals) if vals else 0.0


def analyze(quote: dict, bars: list[dict]) -> dict:
    qdate = quote.get("trade_date")
    last = len(bars) - 1
    # 腾讯K线可能已经含有今日未收盘bar；若日期与实时行情一致，则前一根作为完整T0。
    t0i = last - 1 if qdate and bars[last]["date"] == qdate else last
    if t0i < 30:
        raise RuntimeError("完整历史K线不足")
    t0 = bars[t0i]

    def ma(n): return mean([x["close"] for x in bars[t0i-n+1:t0i+1]])
    ma5, ma10, ma20, ma30 = ma(5), ma(10), ma(20), ma(30)
    prev5 = bars[t0i-5:t0i]
    prev20 = bars[t0i-20:t0i]
    vma5 = mean([x["volume"] for x in prev5])
    r20_close = max(x["close"] for x in prev20)
    r20_high = max(x["high"] for x in prev20)
    vr = t0["volume"] / vma5 if vma5 else 0
    clv = (t0["close"] - t0["low"]) / (t0["high"] - t0["low"]) if t0["high"] > t0["low"] else None

    checks = [
        ("平台有效突破", t0["close"] >= r20_close * 1.005, f"收盘 {t0['close']:.2f} / 突破线 {r20_close:.2f}"),
        ("站上MA20", t0["close"] > ma20, f"MA20 {ma20:.2f}"),
        ("站上MA30", t0["close"] > ma30, f"MA30 {ma30:.2f}"),
        ("成交量≥前5日均量1.5倍", vr >= 1.5, f"{vr:.2f}x"),
        ("阳线实体", t0["close"] > t0["open"], f"O {t0['open']:.2f} / C {t0['close']:.2f}"),
        ("强势收盘CLV≥0.70", clv is not None and clv >= .70, "N/A" if clv is None else f"{clv:.2f}"),
    ]
    score = sum(1 for _, ok, _ in checks if ok)
    base_state = "高质量突破候选" if score == 6 else "突破候选" if score == 5 else "观察" if score == 4 else "未形成突破"

    price = quote["price"]
    qlow = quote["low"]
    dev = price / r20_close - 1 if r20_close else 0
    t1_state, action, row = "未进入T+1确认", "继续观察，等待新的完整突破结构。", 0
    if score >= 5 and qdate:
        if price < r20_close and qlow < r20_close * .98:
            t1_state, action, row = "回踩过深/盘中失效", "不抢反弹；等待重新收复突破位并形成新的确认。", 6
        elif dev >= .03:
            t1_state, action, row = "趋势延续但偏离突破位较大", "等待回踩，不直接追高；观察突破位或前高附近的承接。", 4
        elif qlow >= r20_close * .98 and price >= r20_close:
            if abs(dev) <= .015:
                t1_state, action, row = "T+1盘中承接成立，位于回踩执行区", "可进入右侧分批观察/执行区；以突破位及其-2%区域作为结构风险参考。", 3
            else:
                t1_state, action, row = "T+1盘中承接成立", "保持右侧观察，优先等待回踩确认或二次突破后的稳定承接。", 2
        elif price < r20_close:
            t1_state, action, row = "跌回突破位下方", "降级观察；若收盘仍在突破位下方，按突破失败处理。", 7

    return {
        "t0": t0, "score": score, "base_state": base_state, "checks": checks,
        "ma5": ma5, "ma10": ma10, "ma20": ma20, "ma30": ma30, "vma5": vma5,
        "volume_ratio": vr, "clv": clv, "r20_close": r20_close, "r20_high": r20_high,
        "deviation": dev, "t1_state": t1_state, "action": action, "decision_row": row,
        "buy_zone_low": r20_close * .995, "buy_zone_high": r20_close * 1.015,
        "hard_risk": r20_close * .98,
    }


def html_report(symbol, quote, a):
    def yes(ok): return '<span class="ok">PASS</span>' if ok else '<span class="bad">FAIL</span>'
    check_rows = "".join(f"<tr><td>{escape(n)}</td><td>{yes(ok)}</td><td>{escape(v)}</td></tr>" for n, ok, v in a["checks"])
    decisions = [
        ("T0得分<4", "任意", "未形成突破", "不参与，继续观察"),
        ("T0得分5~6", "守住突破位", "T+1承接成立", "进入可执行观察区"),
        ("T0得分5~6", "回踩突破位±1%后站回", "优质确认", "可考虑分批右侧执行"),
        ("T0得分5~6", "高于突破位≥3%", "追高风险", "等待回踩，不直接追"),
        ("T0得分5~6", "突破R20_high并稳定", "二次突破", "分批，定义失效位"),
        ("T0得分5~6", "低点<突破位×0.98", "回踩过深", "降级观察"),
        ("T0得分5~6", "收盘跌回突破位下", "突破失败", "放弃本次信号"),
    ]
    drows = "".join(f'<tr class="{"current" if i+1==a["decision_row"] else ""}"><td>{x[0]}</td><td>{x[1]}</td><td>{x[2]}</td><td>{x[3]}</td></tr>' for i,x in enumerate(decisions))
    stamp = quote.get("timestamp") or "未知"
    return f'''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{escape(quote['name'])} {quote['code']} 右侧突破报告</title><style>
body{{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",sans-serif;background:#f5f6f8;color:#20242a;margin:0}}main{{max-width:1050px;margin:30px auto;padding:0 18px}}.card{{background:#fff;border-radius:14px;padding:22px;margin:14px 0;box-shadow:0 2px 12px #0000000b}}h1{{margin:0 0 6px}}h2{{font-size:19px}}.price{{font-size:36px;font-weight:750}}.muted{{color:#69717d}}.grid{{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px}}.metric{{background:#f7f8fa;padding:13px;border-radius:10px}}.metric b{{display:block;font-size:20px;margin-top:4px}}table{{width:100%;border-collapse:collapse}}th,td{{padding:11px 8px;border-bottom:1px solid #eceff2;text-align:left}}.ok{{color:#0a8f48;font-weight:700}}.bad{{color:#c43b3b;font-weight:700}}.current{{background:#fff4d6}}.verdict{{font-size:22px;font-weight:750}}.action{{border-left:4px solid #333;padding:10px 14px;background:#f8f8f8}}code{{background:#f1f2f4;padding:2px 5px;border-radius:5px}}footer{{font-size:12px;color:#777;padding:16px 0 30px}}</style></head><body><main>
<div class="card"><h1>{escape(quote['name'])} <span class="muted">{quote['code']} · {symbol}</span></h1><div class="price">{quote['price']:.2f} <span style="font-size:18px">{quote['pct']:+.2f}%</span></div><div class="muted">腾讯行情时间：{escape(stamp)} · 当前结果若处于交易时段，仅代表盘中状态</div></div>
<div class="card"><div class="grid"><div class="metric">T0突破评分<b>{a['score']} / 6</b></div><div class="metric">前一日结论<b>{a['base_state']}</b></div><div class="metric">T+1状态<b>{a['t1_state']}</b></div><div class="metric">量能 / VMA5<b>{a['volume_ratio']:.2f}x</b></div></div></div>
<div class="card"><h2>当前执行建议</h2><div class="verdict">{a['t1_state']}</div><p class="action">{a['action']}</p><div class="grid"><div class="metric">突破位 R20 Close<b>{a['r20_close']:.2f}</b></div><div class="metric">前高 R20 High<b>{a['r20_high']:.2f}</b></div><div class="metric">回踩参考区<b>{a['buy_zone_low']:.2f} ~ {a['buy_zone_high']:.2f}</b></div><div class="metric">-2%结构风险参考<b>{a['hard_risk']:.2f}</b></div><div class="metric">MA20<b>{a['ma20']:.2f}</b></div><div class="metric">MA30<b>{a['ma30']:.2f}</b></div></div></div>
<div class="card"><h2>T0 六项突破判断 · {a['t0']['date']}</h2><table><thead><tr><th>规则</th><th>结果</th><th>数值</th></tr></thead><tbody>{check_rows}</tbody></table></div>
<div class="card"><h2>右侧交易决策表</h2><p class="muted">黄色行为脚本当前最接近的规则状态（盘中结果可能随价格变化）。</p><table><thead><tr><th>前一日</th><th>当前日</th><th>判断</th><th>动作</th></tr></thead><tbody>{drows}</tbody></table></div>
<div class="card"><h2>指标明细</h2><div class="grid"><div class="metric">MA5<b>{a['ma5']:.2f}</b></div><div class="metric">MA10<b>{a['ma10']:.2f}</b></div><div class="metric">CLV<b>{'N/A' if a['clv'] is None else f'{a["clv"]:.2f}'}</b></div><div class="metric">现价偏离突破位<b>{a['deviation']*100:+.2f}%</b></div><div class="metric">T0 Low<b>{a['t0']['low']:.2f}</b></div><div class="metric">T0 Close<b>{a['t0']['close']:.2f}</b></div></div></div>
<footer>数据源：腾讯财经公开行情与前复权日K接口。策略基于价格、成交量、平台与均线的规则化技术分析，不构成收益保证或个性化投资承诺。盘中信号必须与收盘结果区分。</footer></main></body></html>'''


def main():
    p = argparse.ArgumentParser(description="A股右侧放量突破分析")
    p.add_argument("code")
    p.add_argument("--output")
    p.add_argument("--json", action="store_true")
    args = p.parse_args()
    try:
        symbol, code = symbol_of(args.code)
        quote = fetch_quote(symbol)
        bars = fetch_kline(symbol)
        a = analyze(quote, bars)
        output = Path(args.output) if args.output else Path("reports") / f"{code}-right-side-breakout.html"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(html_report(symbol, quote, a), encoding="utf-8")
        if args.json:
            print(json.dumps({"symbol": symbol, "quote": quote, "analysis": a}, ensure_ascii=False, indent=2))
        print(f"HTML report: {output}")
        print(f"T0: {a['base_state']} ({a['score']}/6); T+1: {a['t1_state']}")
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
