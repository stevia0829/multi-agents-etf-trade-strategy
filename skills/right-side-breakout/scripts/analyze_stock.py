#!/usr/bin/env python3
"""A-share right-side breakout analyzer v2.

Tencent quote/K-line -> local platform detection -> T0 classification -> T1 confirmation -> HTML.
"""

from __future__ import annotations

import argparse
import json
import re
import statistics
import sys
import urllib.parse
import urllib.request
from html import escape
from pathlib import Path

UA = "Mozilla/5.0 right-side-breakout-skill/2.0"
PLATFORM_WINDOWS = (7, 10, 15, 20)


def symbol_of(code: str) -> tuple[str, str]:
    s = code.lower().strip()
    if re.fullmatch(r"(?:sh|sz)\d{6}", s):
        return s, s[2:]
    if not re.fullmatch(r"\d{6}", s):
        raise ValueError("股票代码必须为6位数字，或 sh/sz + 6位数字")
    if s.startswith("6"):
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
        out.append({
            "date": r[0].replace("-", ""), "open": float(r[1]), "close": float(r[2]),
            "high": float(r[3]), "low": float(r[4]), "volume": float(r[5])
        })
    if len(out) < 65:
        raise RuntimeError("日K数量不足，v2至少需要65根")
    return out


def mean(vals):
    return statistics.fmean(vals) if vals else 0.0


def detect_local_platform(bars: list[dict], t0i: int) -> dict:
    """Find the most recent compact consolidation before T0.

    A platform is not the maximum price in a fixed 20-day window. We compare
    7/10/15/20-day compact ranges and prefer recent/tight windows whose upper
    edge has been tested repeatedly.
    """
    candidates = []
    for w in PLATFORM_WINDOWS:
        xs = bars[t0i-w:t0i]
        closes = [x["close"] for x in xs]
        med = statistics.median(closes)
        top = max(closes)
        bottom = min(closes)
        local_high = max(x["high"] for x in xs)
        range_pct = (top - bottom) / med if med else 99
        top_tests = sum(1 for c in closes if c >= top * .97)
        tight = range_pct <= .08
        # More top tests and tighter/recent windows are better.
        quality = top_tests * 1.4 - range_pct * 25 - w * .025
        candidates.append({
            "window": w, "top": top, "bottom": bottom, "high": local_high,
            "median": med, "range_pct": range_pct, "top_tests": top_tests,
            "tight": tight, "quality": quality,
        })
    valid = [x for x in candidates if x["tight"] and x["top_tests"] >= 2]
    if valid:
        chosen = max(valid, key=lambda x: x["quality"])
    else:
        chosen = next(x for x in candidates if x["window"] == 10)
        chosen = {**chosen, "fallback": True}
    chosen["candidates"] = candidates
    return chosen


def analyze(quote: dict, bars: list[dict]) -> dict:
    qdate = quote.get("trade_date")
    last = len(bars) - 1
    t0i = last - 1 if qdate and bars[last]["date"] == qdate else last
    if t0i < 60:
        raise RuntimeError("完整历史K线不足")
    t0 = bars[t0i]

    def ma(n):
        return mean([x["close"] for x in bars[t0i-n+1:t0i+1]])

    ma5, ma10, ma20, ma30 = ma(5), ma(10), ma(20), ma(30)
    prev5 = bars[t0i-5:t0i]
    vma5 = mean([x["volume"] for x in prev5])
    vr = t0["volume"] / vma5 if vma5 else 0
    clv = (t0["close"] - t0["low"]) / (t0["high"] - t0["low"]) if t0["high"] > t0["low"] else None

    platform = detect_local_platform(bars, t0i)
    local_breakout = platform["top"]
    local_high = platform["high"]

    prev20 = bars[t0i-20:t0i]
    prev60 = bars[t0i-60:t0i]
    mid_pressure = max(max(x["close"] for x in prev20), max(x["high"] for x in prev20))
    long_pressure = max(x["high"] for x in prev60)

    local_break = t0["close"] >= local_breakout * 1.003
    near_ma20 = t0["close"] >= ma20 * .995
    near_ma30 = t0["close"] >= ma30 * .995
    volume_ok = vr >= 1.5
    bullish = t0["close"] > t0["open"]
    strong_close = clv is not None and clv >= .70

    checks = [
        ("局部平台有效突破", local_break, f"C {t0['close']:.2f} / Local {local_breakout:.2f}"),
        ("接近/站上MA20", near_ma20, f"MA20 {ma20:.2f}"),
        ("接近/站上MA30", near_ma30, f"MA30 {ma30:.2f}"),
        ("成交量≥前5日均量1.5倍", volume_ok, f"{vr:.2f}x"),
        ("阳线实体", bullish, f"O {t0['open']:.2f} / C {t0['close']:.2f}"),
        ("强势收盘CLV≥0.70", strong_close, "N/A" if clv is None else f"{clv:.2f}"),
    ]
    score = sum(1 for _, ok, _ in checks if ok)

    distance_to_local = t0["close"] / local_breakout - 1 if local_breakout else -1
    if local_break and vr >= 1.3 and bullish:
        strong_extra = ((t0["close"] > ma20 and t0["close"] > ma30) or t0["close"] > local_high or t0["close"] > mid_pressure)
        t0_state = "强突破" if strong_extra else "局部平台突破"
    elif volume_ok and bullish and strong_close and distance_to_local >= -.03:
        t0_state = "放量启动"
    elif score >= 3 and distance_to_local >= -.05:
        t0_state = "观察"
    else:
        t0_state = "未启动"

    price = quote["price"]
    qlow = quote["low"]
    dev = price / local_breakout - 1 if local_breakout else 0
    active = t0_state in {"放量启动", "局部平台突破", "强突破"}
    t1_state = "未进入T+1确认"
    action = "继续观察，等待局部平台附近出现量价启动。"
    decision = "无右侧结构"

    if active and qdate:
        if qlow < local_breakout * .98 and price < local_breakout:
            t1_state = "回踩过深/盘中失效风险"
            decision = "降级观察"
            action = "当前已明显跌回局部平台下方；等待重新收复，收盘确认前不把盘中状态当最终结论。"
        elif price > mid_pressure and dev < .03:
            t1_state = "二次突破"
            decision = "二次突破"
            action = "已突破中期压力，观察突破后的稳定承接；仍应定义局部平台失效位。"
        elif dev >= .03:
            t1_state = "趋势延续但追高风险"
            decision = "等待回踩"
            action = "当前偏离局部突破位已较大，右侧不等于追高，优先等待回踩或新平台。"
        elif qlow >= local_breakout * .98 and price >= local_breakout:
            if abs(dev) <= .015:
                t1_state = "优质确认/回踩执行区"
                decision = "优质确认"
                action = "价格位于局部突破位附近且承接尚可，可进入分批右侧观察区；关注收盘是否继续站稳。"
            else:
                t1_state = "T+1盘中承接成立"
                decision = "承接成立"
                action = "局部平台突破后尚未明显跌回，可继续观察回踩或二次突破，不机械追价。"
        elif price >= local_breakout:
            t1_state = "首次突破局部平台"
            decision = "首次右侧确认"
            action = "T0属于放量启动，T1正在首次站上局部平台；重点观察是否能站稳而不是只看瞬时突破。"
        else:
            t1_state = "盘中跌回局部突破位下"
            decision = "失效风险"
            action = "若收盘仍不能重新站上局部突破位，本次右侧结构应降级或失效。"

    return {
        "t0": t0, "score": score, "t0_state": t0_state, "checks": checks,
        "ma5": ma5, "ma10": ma10, "ma20": ma20, "ma30": ma30,
        "vma5": vma5, "volume_ratio": vr, "clv": clv,
        "platform": platform, "local_breakout": local_breakout, "local_high": local_high,
        "mid_pressure": mid_pressure, "long_pressure": long_pressure,
        "deviation": dev, "t1_state": t1_state, "decision": decision, "action": action,
        "buy_zone_low": local_breakout * .99, "buy_zone_high": local_breakout * 1.015,
        "hard_risk": local_breakout * .98,
    }


def html_report(symbol: str, quote: dict, a: dict) -> str:
    def mark(ok):
        return '<span class="ok">PASS</span>' if ok else '<span class="bad">FAIL</span>'

    check_rows = "".join(
        f"<tr><td>{escape(n)}</td><td>{mark(ok)}</td><td>{escape(v)}</td></tr>"
        for n, ok, v in a["checks"]
    )
    candidate_rows = "".join(
        f"<tr><td>{x['window']}日</td><td>{x['bottom']:.2f}~{x['top']:.2f}</td><td>{x['range_pct']*100:.1f}%</td><td>{x['top_tests']}</td><td>{'是' if x['tight'] else '否'}</td></tr>"
        for x in a["platform"]["candidates"]
    )
    decisions = [
        ("未启动", "任意", "无右侧结构", "不参与，等待平台附近量价转强"),
        ("放量启动", "次日首次站上局部平台", "首次右侧确认", "看承接，不追瞬间拉升"),
        ("局部/强突破", "回踩±1%后站回", "优质确认", "进入分批右侧观察区"),
        ("任一启动态", "偏离突破位≥3%", "追高风险", "等待回踩"),
        ("任一启动态", "突破中期压力", "二次突破", "观察稳定承接"),
        ("任一启动态", "低点跌破突破位-2%", "回踩过深", "降级观察"),
        ("任一突破态", "收盘跌回局部平台", "突破失效", "放弃本次信号"),
    ]
    drows = "".join(f"<tr><td>{x[0]}</td><td>{x[1]}</td><td>{x[2]}</td><td>{x[3]}</td></tr>" for x in decisions)
    stamp = quote.get("timestamp") or "未知"
    fallback = "（无紧凑平台，使用10日退化窗口）" if a["platform"].get("fallback") else ""
    return f'''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{escape(quote['name'])} {quote['code']} 右侧突破报告 v2</title><style>
body{{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",sans-serif;background:#f5f6f8;color:#20242a;margin:0}}main{{max-width:1080px;margin:28px auto;padding:0 18px}}.card{{background:white;border-radius:14px;padding:22px;margin:14px 0;box-shadow:0 2px 12px #0000000b}}h1{{margin:0 0 6px}}h2{{font-size:19px}}.price{{font-size:36px;font-weight:760}}.muted{{color:#69717d}}.grid{{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:10px}}.metric{{background:#f7f8fa;padding:13px;border-radius:10px}}.metric b{{display:block;font-size:19px;margin-top:4px}}table{{width:100%;border-collapse:collapse}}th,td{{padding:10px 8px;border-bottom:1px solid #eceff2;text-align:left}}.ok{{color:#0a8f48;font-weight:700}}.bad{{color:#c43b3b;font-weight:700}}.action{{border-left:4px solid #333;padding:12px 14px;background:#f8f8f8}}.tag{{display:inline-block;padding:5px 10px;border-radius:999px;background:#fff1c9;font-weight:700}}.important{{background:#eef5ff;padding:14px;border-radius:10px}}footer{{font-size:12px;color:#777;padding:16px 0 30px}}</style></head><body><main>
<div class="card"><h1>{escape(quote['name'])} <span class="muted">{quote['code']} · {symbol}</span></h1><div class="price">{quote['price']:.2f} <span style="font-size:18px">{quote['pct']:+.2f}%</span></div><div class="muted">腾讯行情时间：{escape(stamp)} · 盘中结论不替代收盘确认</div></div>
<div class="card"><div class="grid"><div class="metric">T0状态<b>{a['t0_state']}</b></div><div class="metric">六项评分<b>{a['score']} / 6</b></div><div class="metric">T1状态<b>{a['t1_state']}</b></div><div class="metric">当前决策<b>{a['decision']}</b></div></div><p class="action">{a['action']}</p></div>
<div class="card"><h2>三层价格结构</h2><div class="important"><b>短线突破位 ≠ 中期压力 ≠ 长期压力。</b> 旧高未突破，不再否定局部平台已经启动或突破。</div><br><div class="grid"><div class="metric">局部平台窗口<b>{a['platform']['window']}日 {fallback}</b></div><div class="metric">局部平台区间<b>{a['platform']['bottom']:.2f} ~ {a['platform']['top']:.2f}</b></div><div class="metric">Local Breakout<b>{a['local_breakout']:.2f}</b></div><div class="metric">Local High<b>{a['local_high']:.2f}</b></div><div class="metric">中期压力<b>{a['mid_pressure']:.2f}</b></div><div class="metric">长期压力<b>{a['long_pressure']:.2f}</b></div></div></div>
<div class="card"><h2>T0 六项判断 · {a['t0']['date']}</h2><table><thead><tr><th>规则</th><th>结果</th><th>数值</th></tr></thead><tbody>{check_rows}</tbody></table></div>
<div class="card"><h2>局部平台候选</h2><table><thead><tr><th>窗口</th><th>收盘区间</th><th>区间宽度</th><th>上沿测试次数</th><th>紧凑</th></tr></thead><tbody>{candidate_rows}</tbody></table></div>
<div class="card"><h2>关键指标</h2><div class="grid"><div class="metric">量能/VMA5<b>{a['volume_ratio']:.2f}x</b></div><div class="metric">CLV<b>{'N/A' if a['clv'] is None else f'{a["clv"]:.2f}'}</b></div><div class="metric">MA5<b>{a['ma5']:.2f}</b></div><div class="metric">MA10<b>{a['ma10']:.2f}</b></div><div class="metric">MA20<b>{a['ma20']:.2f}</b></div><div class="metric">MA30<b>{a['ma30']:.2f}</b></div><div class="metric">回踩参考区<b>{a['buy_zone_low']:.2f} ~ {a['buy_zone_high']:.2f}</b></div><div class="metric">-2%结构风险<b>{a['hard_risk']:.2f}</b></div><div class="metric">现价偏离突破位<b>{a['deviation']*100:+.2f}%</b></div></div></div>
<div class="card"><h2>右侧交易决策表</h2><table><thead><tr><th>T0</th><th>T1</th><th>判断</th><th>动作</th></tr></thead><tbody>{drows}</tbody></table></div>
<footer>数据源：腾讯财经公开实时行情和前复权日K。规则用于技术结构分析，不构成收益保证。T1若为盘中数据，必须与最终收盘确认区分。</footer></main></body></html>'''


def main() -> int:
    p = argparse.ArgumentParser(description="A股右侧放量突破分析 v2")
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
        print(f"T0: {a['t0_state']} ({a['score']}/6); T1: {a['t1_state']}; local breakout: {a['local_breakout']:.2f}")
        return 0
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
