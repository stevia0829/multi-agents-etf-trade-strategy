import json
import tempfile
import unittest
from datetime import date, datetime, timedelta
from pathlib import Path

from etf_intraday_15m import CST, active_minutes, completed_slot, notify_once, scan, tencent_rows

NOW = datetime.fromisoformat("2026-11-16T10:01:00+08:00")
DAY = NOW.date()


def fixture(code, period):
    if period == 101:
        days = [DAY - timedelta(days=i) for i in range(1, 60) if (DAY - timedelta(days=i)).weekday() < 5]
        return [f"{day},{n+1},{n+1},{n+2},{n+1},100" for n, day in enumerate(reversed(days))]
    days = [DAY - timedelta(days=i) for i in range(1, 9) if (DAY - timedelta(days=i)).weekday() < 5]
    ticks = [t for day in reversed(days) for t in active_minutes(day)]
    ticks += list(active_minutes(DAY))[:3]
    def bar(t, c, h, l):
        return f"{t:%Y-%m-%d %H:%M},{c},{c},{h},{l},100"
    return [bar(t, 1, 1, 1) for t in ticks[:-3]] + [
        bar(ticks[-3], 1.02, 1.03, 0.99), bar(ticks[-2], 1.04, 1.05, 1.03),
        bar(ticks[-1], 0.50, 0.51, 0.49)  # incomplete when we scan; must ignore
    ]


class Intraday(unittest.TestCase):
    def test_tencent_payload(self):
        source = {"code": 0, "data": {"sz159687": {"m15": [
            ["202611161500", "1.605", "1.608", "1.610", "1.604", "55055", {}]
        ]}}}
        self.assertEqual(tencent_rows(source, "sz159687", 15),
                         ["2026-11-16 15:00,1.605,1.608,1.610,1.604,55055"])
        with self.assertRaisesRegex(ValueError, "标的代码不匹配"):
            tencent_rows(source, "sh510300", 15)

    def test_windows(self):
        def at(hour, minute, day=DAY):
            return completed_slot(datetime(day.year, day.month, day.day, hour, minute, tzinfo=CST))
        for h, m in ((9, 45), (11, 46), (12, 30), (13, 15), (15, 15)):
            self.assertIsNone(at(h, m))
        for h, m in ((9, 46), (11, 31), (13, 16), (15, 1)):
            self.assertEqual((at(h, m).hour, at(h, m).minute), ((h - 1, 45) if m == 1 and h == 10 else
                              (h, m - 1)))
        self.assertIsNone(at(10, 1, date(2026, 11, 14)))

    def sources(self, root):
        rec = {"final": {"recommendation": "buy", "target_etf": {"etf": {"code": "510300"}},
                         "picks": [{"etf_code": "510300", "recommendation": "buy"}]}}
        path = root / "etf-report-20261116-083000.json"
        path.write_text(json.dumps(rec))
        hold = root / "current_holds.json"
        hold.write_text(json.dumps({"holds": [{"code": "159687", "qty": 100}]}))
        return path, hold, rec

    def test_union_open_bar_and_gate(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            report, hold, rec = self.sources(root)
            result = scan(root, hold, NOW, fixture)
            self.assertEqual(result["status"], "ok")
            self.assertEqual(result["signals"]["510300"]["signal"], "pullback_confirmed")
            self.assertEqual(result["signals"]["510300"]["close"], 1.04)
            self.assertEqual(result["signals"]["159687"]["signal"], "advice_gate")
            rec["final"]["recommendation"] = "wait"
            report.write_text(json.dumps(rec))
            self.assertEqual(scan(root, hold, NOW, fixture)["signals"]["510300"]["signal"], "advice_gate")

    def test_no_old_report_no_stale_minute(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            report, hold, _ = self.sources(root)
            report.rename(root / "etf-report-20261113-083000.json")
            def delayed(code, period):
                data = fixture(code, period)
                return data[:-2] if period == 15 else data
            result = scan(root, hold, NOW, delayed)
            self.assertEqual(result["status"], "degraded")
            self.assertIn("advice", result["problems"])
            self.assertEqual(set(result["signals"]), {"159687"})
            self.assertEqual(result["signals"]["159687"]["signal"], "data_unavailable")

    def test_held_cross_below_and_deduplication(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            _, hold, _ = self.sources(root)
            hold.write_text('{"codes":["159687","510300"]}')
            def split_prices(code, period):
                result = fixture(code, period)
                if code == "159687" and period == 15:
                    result[-2] = result[-2].split(",")[0] + ",0.99,0.99,1.00,0.98,100"
                return result
            state = root / "state.json"
            result = scan(root, hold, NOW, split_prices)
            self.assertEqual(result["signals"]["159687"]["signal"], "ema_lost")
            self.assertTrue(result["signals"]["510300"]["held"])
            self.assertEqual(len(notify_once(result, state)["alerts"]), 2)
            self.assertEqual(notify_once(result, state)["alerts"], [])


if __name__ == "__main__":
    unittest.main()
