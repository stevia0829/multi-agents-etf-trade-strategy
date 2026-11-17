#!/usr/bin/env python3
"""Read-only 15-minute technical overlay for ETF Strategy's daily advice."""
import argparse
import concurrent.futures
import json
import math
import re
import statistics
import sys
from datetime import datetime, time, timedelta, timezone
from pathlib import Path
from urllib.parse import urlencode
from urllib.request import Request, urlopen

CST = timezone(timedelta(hours=8))
SESSIONS = ((time(9, 30), time(11, 30)), (time(13), time(15)))
CODES = re.compile(r"(?:15|16|50|51|52|56|58)\d{4}", flags=re.ASCII)
ACTIONABLE = {"pullback_confirmed", "ema_lost"}
TENCENT = "https://proxy.finance.qq.com/ifzqgtimg/appstock/app"


def completed_slot(now):
    """Allow a 1-minute feed grace and end-of-session read after the closing bar."""
    if now.weekday() >= 5:
        return None
    for opening, closing in SESSIONS:
        start = datetime.combine(now.date(), opening, CST)
        end = datetime.combine(now.date(), closing, CST)
        available = now - timedelta(minutes=1)
        if start + timedelta(minutes=15) <= available and now < end + timedelta(minutes=15):
            return min(end, start + timedelta(minutes=(available - start) // timedelta(minutes=15) * 15))
    return None


def get_json(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def code_of(raw):
    if not isinstance(raw, str) or not CODES.fullmatch(raw):
        raise ValueError("标的代码不是支持的场内基金六位代码")
    return raw


def holdings(path):
    payload = get_json(path)
    found = False
    if isinstance(payload, dict):
        items = []
        for key in ("holds", "codes"):
            if key in payload:
                found = True
                if not isinstance(payload[key], list):
                    raise ValueError("持仓字段须为数组")
                items.extend(payload[key])
        if not found:
            raise ValueError("持仓 JSON 缺少 holds/codes")
        payload = items
    if not isinstance(payload, list):
        raise ValueError("持仓须为数组或含 holds/codes 的对象")
    return {code_of(x.get("code") if isinstance(x, dict) else x) for x in payload}


def advice(directory, now):
    # Never fall back to a previous day's report or a later replay snapshot.
    day = now.strftime("%Y%m%d")
    paths = sorted(directory.glob(f"etf-report-{day}-*.json"), reverse=True)
    for path in paths:
        stamp = path.stem.removeprefix("etf-report-")
        if not re.fullmatch(r"\d{8}-\d{6}", stamp):
            continue
        if datetime.strptime(stamp, "%Y%m%d-%H%M%S").replace(tzinfo=CST) > now:
            continue
        report = get_json(path)
        if report.get("run_mode") not in (None, "", "advice"):
            continue
        final = report.get("final") or {}
        if not final or not final.get("recommendation"):
            raise ValueError(f"早报缺少最终决策：{path.name}")
        picks = final.get("picks") or []
        result = {code_of(p["etf_code"]): p.get("recommendation", "unknown") for p in picks}
        main = (final.get("target_etf") or {}).get("etf") or {}
        if main.get("code"):
            result[code_of(main["code"])] = final["recommendation"]
        # A watch signal must never override a daily risk guard.
        return result, final["recommendation"].lower(), path.name
    raise FileNotFoundError("没有可用的今日早报 JSON")


def fetch(code, interval):
    symbol = ("sz" if code.startswith(("15", "16")) else "sh") + code_of(code)
    if interval == 15:
        endpoint, param = "kline/mkline", f"{symbol},m15,,150"
    else:
        endpoint, param = "fqkline/get", f"{symbol},day,,2050-01-01,150,qfq"
    query = urlencode({"param": param})
    with urlopen(Request(f"{TENCENT}/{endpoint}?{query}", headers={"User-Agent": "Mozilla/5.0"}), timeout=8) as res:
        raw = res.read(524289)
    if len(raw) > 524288:
        raise ValueError("行情响应过大")
    return tencent_rows(json.loads(raw), symbol, interval)


def tencent_rows(message, symbol, interval):
    data = (message.get("data") or {}).get(symbol) or {}
    keys = ("m15",) if interval == 15 else ("qfqday", "day")
    rows = next((data[k] for k in keys if data.get(k)), [])
    if message.get("code") != 0 or not rows:
        raise ValueError("腾讯行情缺失或标的代码不匹配")
    def normalize(row):
        if len(row) < 6:
            raise ValueError("腾讯行情 K 线字段不足")
        stamp = row[0]
        if interval == 15:
            stamp = datetime.strptime(stamp, "%Y%m%d%H%M").strftime("%Y-%m-%d %H:%M")
        return ",".join(map(str, (stamp, *row[1:6])))
    return [normalize(r) for r in rows]


def active_minutes(day):
    for opening, closing in SESSIONS:
        t = datetime.combine(day, opening, CST) + timedelta(minutes=15)
        end = datetime.combine(day, closing, CST)
        while t <= end:
            yield t
            t += timedelta(minutes=15)


def decode(rows, minute):
    result = {}
    for row in rows:
        stamp, o, c, h, l, volume, *_ = row.split(",")
        key = datetime.strptime(stamp, "%Y-%m-%d %H:%M" if minute else "%Y-%m-%d").replace(tzinfo=CST)
        if minute and key not in active_minutes(key.date()):
            raise ValueError("15分钟行情时间不是有效K线结束时间")
        vals = tuple(float(x) for x in (o, c, h, l, volume))
        o, c, h, l, volume = vals
        if not all(map(math.isfinite, vals)) or not 0 < l <= min(o, c) <= max(o, c) <= h or volume < 0:
            raise ValueError("行情含无效 OHLCV")
        if key in result:
            raise ValueError("行情包含重复K线")
        result[key] = vals
    return sorted(result.items())


def ema(series, period=20):
    value = statistics.mean(series[:period])
    out = [value]
    for close in series[period:]:
        value += 2 * (close - value) / (period + 1)
        out.append(value)
    return out


def evaluate(code, slot, provider, daily_action, daily_allowed, held):
    minutes = [(ts, v) for ts, v in decode(provider(code, 15), True) if ts <= slot]
    if not minutes or minutes[-1][0] != slot:
        raise ValueError("该标的没有本时点已完成的15分钟K线")
    if len(minutes) < 60:
        raise ValueError("15分钟预热不足60根")
    recent_days = sorted({ts.date() for ts, _ in minutes[-60:]})
    present = {ts for ts, _ in minutes}
    if any(ts not in present for day in recent_days for ts in active_minutes(day)
           if minutes[-60][0] <= ts <= slot):
        raise ValueError("15分钟行情存在缺口")
    daily = [(ts, bar) for ts, bar in decode(provider(code, 101), False) if ts.date() < slot.date()]
    if len(daily) < 25 or len(recent_days) < 2 or daily[-1][0].date() != recent_days[-2]:
        raise ValueError("已完成日线不足或与分钟行情日期不一致")
    closes = [v[1] for _, v in daily]
    ma = statistics.mean(closes[-20:])
    old_ma = statistics.mean(closes[-25:-5])
    trend = closes[-1] > ma > old_ma
    prices = [v[1] for _, v in minutes]
    prev_ema, ema_now = ema(prices)[-2:]
    prev_stamp, (_, prev_c, prev_h, prev_l, _) = minutes[-2]
    _, (_, close, _, _, _) = minutes[-1]
    same_session = any(opening < prev_stamp.time() and slot.time() <= closing
                       and opening < slot.time() and prev_stamp.time() <= closing for opening, closing in SESSIONS)
    confirmed = (slot - prev_stamp == timedelta(minutes=15) and same_session
                 and prev_l <= prev_ema < prev_c and close > prev_h and close > ema_now)
    if held and prev_c >= prev_ema and close < ema_now:
        signal = "ema_lost"
    elif trend and confirmed and daily_allowed:
        signal = "pullback_confirmed"
    elif confirmed and not daily_allowed:
        signal = "advice_gate"
    else:
        signal = "watch" if trend else "trend_not_confirmed"
    return dict(signal=signal, close=close, ema20=round(ema_now, 6),
                daily_ma20=round(ma, 6), daily_trend=trend, held=held, daily_action=daily_action,
                iopv_checked=False, execution="manual_review_only")


def safe_eval(code, slot, provider, action, allowed, held):
    try:
        return evaluate(code, slot, provider, action, allowed, held)
    except (OSError, ValueError, TypeError) as e:
        return dict(signal="data_unavailable", detail=str(e), held=held, daily_action=action)


def read_source(fn):
    try:
        return fn(), None
    except (OSError, ValueError, TypeError, KeyError, AttributeError) as exc:
        return None, str(exc)


def scan(report_dir, holdings_path, now, provider):
    slot = completed_slot(now)
    if slot is None:
        return {"status": "outside_window", "alerts": []}
    source, advice_error = read_source(lambda: advice(report_dir, now))
    held, holdings_error = read_source(lambda: holdings(holdings_path))
    picks, global_action, report = source or ({}, "unknown", None)
    tracked = sorted(set(picks) | (held or set()))
    def inspect(code):
        action = picks.get(code, "not_in_advice")
        allowed = global_action == "buy" and action.lower() == "buy"
        return code, safe_eval(code, slot, provider, action, allowed, held is not None and code in held)
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        signals = dict(pool.map(inspect, tracked))
    problems = {k: v for k, v in (("advice", advice_error), ("holdings", holdings_error)) if v}
    if signals and all(x["signal"] == "data_unavailable" for x in signals.values()):
        problems["prices"] = "所有标的均缺少本时点行情或可用预热数据"
    return dict(status="degraded" if problems else "ok", bar_end=slot.isoformat(),
                advice_report=report, holdings_known=held is not None, problems=problems, signals=signals)


def notify_once(result, state_path):
    if result["status"] == "outside_window":
        return result
    state = get_json(state_path) if state_path.exists() else {}
    if state.get("bar_end", "") > result["bar_end"]:
        raise ValueError("状态文件比本次行情新；历史回放请使用独立状态文件")
    signals = result["signals"]
    alerts = [{"code": code, **row} for code, row in signals.items()
              if row["signal"] in ACTIONABLE and state.get("bar_end") != result["bar_end"]]
    if state.get("bar_end") == result["bar_end"]:
        acknowledged = set(state.get("sent", ()))
        alerts = [{"code": code, **row} for code, row in signals.items()
                  if row["signal"] in ACTIONABLE and code not in acknowledged]
    else:
        acknowledged = set()
    problem_ids = sorted(result["problems"])
    result["new_problem"] = bool(problem_ids and problem_ids != state.get("problems", []))
    result["recovered"] = bool(state.get("problems") and not problem_ids)
    result["alerts"] = alerts
    state_path.parent.mkdir(parents=True, exist_ok=True)
    tmp = state_path.with_suffix(".tmp")
    tmp.write_text(json.dumps(dict(bar_end=result["bar_end"],
                                  sent=sorted(acknowledged | {r["code"] for r in alerts}),
                                  problems=problem_ids), ensure_ascii=False), encoding="utf-8")
    tmp.replace(state_path)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report-dir", type=Path, required=True)
    parser.add_argument("--hold-file", type=Path, required=True)
    parser.add_argument("--state", type=Path)
    parser.add_argument("--fixture", type=Path, help="offline JSON: {code: {'15': [...], '101': [...]}}")
    parser.add_argument("--as-of", help="ISO timestamp with offset; only for --fixture")
    args = parser.parse_args()
    if args.as_of and not args.fixture:
        parser.error("--as-of requires --fixture")
    if args.fixture and args.state:
        parser.error("offline fixture mode cannot use live notification state")
    now = datetime.fromisoformat(args.as_of) if args.as_of else datetime.now(CST)
    if now.utcoffset() is None:
        parser.error("--as-of must include a timezone offset")
    quote = fetch
    if args.fixture:
        data = get_json(args.fixture)
        def quote(code, kind):
            return data.get(code, {}).get(str(kind), [])
    result = scan(args.report_dir, args.hold_file, now.astimezone(CST), quote)
    if args.state:
        result = notify_once(result, args.state)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, TypeError, KeyError) as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
