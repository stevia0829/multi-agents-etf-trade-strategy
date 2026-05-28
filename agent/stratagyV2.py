from jqdata import *
import random
import datetime

# ================================= 初始化部分 =================================

def initialize(context):
    set_benchmark('000300.XSHG')
    set_option("avoid_future_data", True)
    set_option('use_real_price', True)
    
    # 交易费用与滑点
    set_order_cost(OrderCost(close_tax=0.000, open_commission=0.0001, 
                             close_commission=0.0001, min_commission=5), type='fund')
    set_slippage(PriceRelatedSlippage(0.001), type='fund')

    g.target_n = 2
    g.cooldown_days = 5
    g.last_signal = []
    g.cooldown_dict = {}
    
    g.risk = {'tp': 0.20, 'sl': -0.10, 'ban_etf': None, 'in_wait': False}
    g.log_buffers = {'preopen': [], 'trade': []}

    # 标的池
    g.stocks = {
        '510300.XSHG', '510050.XSHG', '588000.XSHG', '159949.XSHE', 
        '159781.XSHE', '159740.XSHE', '563300.XSHG', '513100.XSHG', 
        '513500.XSHG', '513520.XSHG', '513030.XSHG', '159100.XSHE', 
        '159329.XSHE', '513310.XSHG', '513400.XSHG', '513080.XSHG',
        '162411.XSHE', '159985.XSHE', '518880.XSHG', '511010.XSHG'
    }

    run_daily(before_market_open, time='before_open')
    run_daily(my_trade, time='14:50')
    run_daily(after_market_close, time='after_close')

# ================================= 审计工具函数 =================================

def get_name(security):
    """获取标的中文名称"""
    return get_security_info(security).display_name

def log_trade_detail(security, amount, price, side, reason="", pnl=None):
    """打印详细交易流水"""
    name = get_name(security)
    dt = datetime.datetime.now().strftime('%Y-%m-%d %H:%M')
    
    if side == 'BUY':
        # 手数通常为 amount / 100 (基金交易以份为单位，此处按份显示)
        log.info(f"【买入执行】{name}({security}) | 价格:{round(price,3)} | 数量:{amount}份 | 状态:成功")
    else:
        pnl_str = f" | 盈亏:{round(pnl*100, 2)}%" if pnl is not None else ""
        log.info(f"【卖出执行】{name}({security}) | 原因:{reason} | 价格:{round(price,3)}{pnl_str}")

# ================================= 核心逻辑 =================================

def calc_momentum_scores(curr_data):
    scores = []
    for s in g.stocks:
        cd = curr_data[s]
        if cd.paused: continue
        hist = attribute_history(s, 35, '1d', ['close'])['close']
        if len(hist) < 30: continue
        curr_p = cd.last_price
        p_now = (hist[-2] + hist[-1] + curr_p)
        p_ref = sum(hist[-23:-20])
        if p_ref == 0: continue
        r_value = (p_now - p_ref) * 100 / p_ref
        r_value += random.random() / 1000.0
        can_enter = curr_p > hist[-21] * 1.001
        scores.append({'code': s, 'score': r_value, 'price': curr_p, 'ref': hist[-21], 'ok': can_enter})
    scores.sort(key=lambda x: x['score'], reverse=True)
    return scores

def filter_targets(context, curr_data, candidates):
    today = context.current_dt.date()
    holds = [s for s, p in context.portfolio.positions.items() if p.total_amount > 0]
    filtered, reasons = [], {}
    for s in candidates:
        if s in g.cooldown_dict and today <= g.cooldown_dict[s]:
            reasons[s] = "冷却中"
            continue
        if s not in holds:
            h = attribute_history(s, 6, '1d', ['close'])['close']
            if len(h) >= 6 and (h[-1]/h[0]-1) > 0.15:
                reasons[s] = "5日涨幅过热"
                continue
        filtered.append(s)
    return filtered, reasons

# ================================= 交易处理 =================================

def before_market_open(context):
    today = context.current_dt.date()
    g.cooldown_dict = {s: d for s, d in g.cooldown_dict.items() if today <= d}

def my_trade(context):
    curr_data = get_current_data()
    today = context.current_dt.date()
    holds = [s for s, p in context.portfolio.positions.items() if p.total_amount > 0]
    
    # 1. 止盈止损模块 (带细节日志)
    for s in holds:
        pos = context.portfolio.positions[s]
        price = curr_data[s].last_price
        ret = (price / pos.avg_cost - 1.0) if pos.avg_cost > 0 else 0
        
        reason = ""
        if ret >= g.risk['tp']: reason = "触发止盈"
        elif ret <= g.risk['sl']: reason = "触发止损"
        
        if reason:
            if price > curr_data[s].low_limit * 1.001:
                log_trade_detail(s, pos.total_amount, price, 'SELL', reason=reason, pnl=ret)
                order_target_value(s, 0)
                # 设置冷却期
                trade_days = get_trade_days(start_date=today, end_date=today + datetime.timedelta(days=30))
                if len(trade_days) >= g.cooldown_days:
                    g.cooldown_dict[s] = trade_days[g.cooldown_days-1]
                g.risk['ban_etf'], g.risk['in_wait'] = s, True

    # 2. 动量打分
    scores = calc_momentum_scores(curr_data)
    top_raw = [x['code'] for x in scores if x['ok']][:g.target_n]
    
    # 3. 禁买逻辑
    trade_list = list(top_raw)
    if g.risk['in_wait'] and g.risk['ban_etf'] in trade_list:
        trade_list.remove(g.risk['ban_etf'])
    elif g.risk['in_wait'] and g.risk['ban_etf'] not in trade_list:
        g.risk['ban_etf'], g.risk['in_wait'] = None, False

    # 4. 过滤
    final_targets, reasons = filter_targets(context, curr_data, trade_list)
    
    # 5. 信号切换调仓 (带细节日志)
    if set(final_targets) != set(g.last_signal):
        log.info(f"--- 信号切换逻辑启动：{g.last_signal} -> {final_targets} ---")
        
        # 卖出逻辑
        current_holds = [s for s, p in context.portfolio.positions.items() if p.total_amount > 0]
        for s in current_holds:
            if s not in final_targets:
                if curr_data[s].last_price > curr_data[s].low_limit * 1.001:
                    pos = context.portfolio.positions[s]
                    ret = (curr_data[s].last_price / pos.avg_cost - 1.0) if pos.avg_cost > 0 else 0
                    log_trade_detail(s, pos.total_amount, curr_data[s].last_price, 'SELL', reason="信号掉出Top2", pnl=ret)
                    order_target_value(s, 0)

        # 买入逻辑
        cash = context.portfolio.available_cash
        new_to_buy = [s for s in final_targets if s not in current_holds]
        if new_to_buy and cash > 100:
            target_val_per = (context.portfolio.total_value / g.target_n)
            cash_per = cash / len(new_to_buy)
            for s in new_to_buy:
                if curr_data[s].last_price < curr_data[s].high_limit * 0.999:
                    buy_money = min(cash_per, target_val_per)
                    # 执行下单
                    order_id = order_value(s, buy_money)
                    if order_id:
                        # 估算成交手数（回测中 order_value 价格为当前 last_price）
                        approx_amount = int(buy_money / curr_data[s].last_price)
                        log_trade_detail(s, approx_amount, curr_data[s].last_price, 'BUY')
        
        g.last_signal = list(final_targets)

def after_market_close(context):
    log.info(f"每日收盘净值: {round(context.portfolio.total_value, 2)}")