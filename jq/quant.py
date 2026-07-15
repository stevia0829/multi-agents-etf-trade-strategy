# 克隆自聚宽文章：https://www.joinquant.com/post/65582
# 标题：【讨论帖】三驾马车策略-今年涨177%回撤13%五年21倍
# 作者：烟花三月zy

# 克隆自聚宽文章：https://www.joinquant.com/post/63661
# 标题：三马v10.2 测试框架
# 作者：Cibo

"""
ETF 动量轮动策略 (单策略精简版)

本文件由 Cibo「三驾马车」多策略框架精简而来，仅保留 **策略3：ETF 动量轮动**。
原框架包含 策略1 小市值 / 策略2 ETF反弹 / 策略3 ETF轮动 / 策略4 白马攻防，
现已删除其余三套策略及其选股、择时、止损、资金再平衡相关代码，只留轮动主循环
与必要的下单 / 记录 / 汇总公共函数。轮动选基与调仓逻辑保持不变。

策略3 原帖: https://www.joinquant.com/post/62083
"""

import math

import numpy as np
import pandas as pd
import prettytable
from prettytable import PrettyTable

from jqdata import *
from jqfactor import *

""" ====================== 基础配置 ====================== """


def initialize(context):
    set_backtest()  # 设置回测条件
    set_params(context)  # 设置基础参数
    set_strategy_params(context)  # 设置策略参数

    # 过滤日志
    log.set_level('order', 'error')


# 基础参数设置
def set_params(context):
    # 单策略：资金 100% 分配给 ETF 轮动（保留下标 [2] 语义，与 strategy_3 一致）
    g.portfolio_value_proportion = [0, 0, 1, 0]  # 小市值/ETF反弹/ETF轮动/白马攻防

    g.starting_cash = context.portfolio.total_value  # 策略初始资金, 用于计算收益波动曲线
    g.stock_strategy = {}  # 记录股票对应的策略, 反向映射方便检索
    g.strategy_holdings = {3: []}
    # 记录策略初始的金额, 用于计算收益波动曲线
    g.strategy_starting_cash = {
        3: g.starting_cash * g.portfolio_value_proportion[2],  # ETF轮动 初始资金
    }
    # 记录每日策略收益
    g.strategy_value_data = {}
    g.strategy_value = {
        3: g.starting_cash * g.portfolio_value_proportion[2],  # ETF轮动 初始资金
    }


# 策略参数设置
def set_strategy_params(context):
    """ 策略3 ETF轮动 参数 """
    # 策略3全局变量
    g.etf_pool_3 = [
        '518880.XSHG',  # 黄金ETF
        '161226.XSHE',  # (白银JJ)
        '501018.XSHG',  # 南方原油etf
        '159985.XSHE',  # 豆粕etf
        '513520.XSHG',  # 日经ETF
        '513100.XSHG',  # 纳指100
        '513300.XSHG',  # (纳斯达克ETF)
        '513400.XSHG',  # (道琼斯)
        '159529.XSHE',  # (标普消费ETF)
        '513030.XSHG',  # (德国30)
        '159329.XSHE',  # (沙特ETF)
        '513130.XSHG',  # 恒生科技ETF
        '513090.XSHG',  # (香港证券etf)
        '513120.XSHG',  # (香港创新药)
        '159206.XSHE',  # (卫星ETF).
        '159218.XSHE',  # (卫星产业ETF)
        '159227.XSHE',  # (航天航空ETF)
        '159565.XSHE',  # (汽车零部件ETF)
        '562500.XSHG',  # (机器人)
        '159819.XSHE',  # (人工智能)
        '159363.XSHE',  # (创业板人工智能TFHB)
        '512480.XSHG',  # (半导体)
        '512760.XSHG',  # (存储芯片)
        '515880.XSHG',  # (通信ETF)
        '515230.XSHG',  # 软件ETF
        '515050.XSHG',  # (5GETF)
        '159786.XSHE',  # (VRETF)
        '159890.XSHE',  # (云计算ETF)
        '516160.XSHG',  # (新能源)
        '515790.XSHG',  # (光伏ETF)
        '159755.XSHE',  # (电池ETF)
        '512660.XSHG',  # (军工ETF)
        '159732.XSHE',  # (消费电子)
        '159992.XSHE',  # (创新药XY)
        '159852.XSHE',  # (软件ETF)
        '159851.XSHE',  # (金融科技ETF)
        '159869.XSHE',  # (游戏ETF)
        '516780.XSHG',  # (稀土ETF)
        '159928.XSHE',  # (消费ETF)
        '512690.XSHG',  # (酒ETF)
        '515170.XSHG',  # (食品饮料ETF)
        '512010.XSHG',  # (医药ETF)
        '512980.XSHG',  # (传媒ETF)
        '159378.XSHE',  # (通用航空ETF)
        '159611.XSHE',  # (电力ETF)
        '159766.XSHE',  # (旅游ETF)
        '515220.XSHG',  # (煤炭ETF)
        '159865.XSHE',  # (养殖ETF)
        '562800.XSHG',  # (稀有金属)
        '560860.XSHG',  # 工业有色ETF
        '510050.XSHG',  # 上证50etf
        '510300.XSHG',  # 沪深300etf
        '159922.XSHE',  # 中证500etf
        '159531.XSHE',  # 中证2000ETF
        '159915.XSHE',  # 创业板etf
        '588080.XSHG',  #(科创板50)
        '588380.XSHG',  # (双创50ETF)
        '160211.XSHE',  # 国泰小盘
        '512000.XSHG',  # 券商ETF
        '512800.XSHG',  # 银行ETF
        '510880.XSHG',  # 红利ETF
        '511090.XSHG',  # 30年国债ETF
    ]
    g.m_days = 21  # 动量参考天数
    # 回测资格过滤：至少有 60 根历史日 K 才参与排名，避免新上市 ETF 的短样本高波动放大收益。
    # 只约束候选资格，不改变 21 日动量分数，也不增加换手。
    g.min_history_days = 60
    g.max_score = 6  # 动量过滤分数
    g.min_score = 0
    g.score_threshold_multiplier = 1.1  # 动量分数增长阈值倍数


# 回测设置
def set_backtest():
    set_option('avoid_future_data', True)
    set_benchmark('000300.XSHG')
    set_option('use_real_price', True)

    set_slippage(FixedSlippage(0.002), type="stock")
    set_slippage(FixedSlippage(0.001), type="fund")
    cost_configs = [
        ("stock", 0.0005, 0.85 / 10000, 5),
        ("fund", 0, 0.5 / 10000, 5),
        ("mmf", 0, 0, 0)
    ]
    for asset_type, close_tax, commission, min_comm in cost_configs:
        set_order_cost(OrderCost(
            open_tax=0, close_tax=close_tax,
            open_commission=commission, close_commission=commission,
            close_today_commission=0, min_commission=min_comm
        ), type=asset_type)


""" ====================== 策略3: ETF轮动 ====================== """


def get_etf_rank(context, etf_pool):
    """计算ETF池中各ETF的动量得分并排名"""
    data = pd.DataFrame(index=etf_pool, columns=["annualized_returns", "r2", "score"])
    print_data = {}
    filtered_out = [] # 记录被过滤掉的ETF
    high_score_etfs = [] # 记录超过阈值的ETF

    # 初始化局部变量，用于追踪当前批次中的最高分
    max_observed_score = float('-inf')

    for etf in etf_pool:
        # 先取成熟期所需历史，再仅用末尾 g.m_days 计算动量。
        history = attribute_history(etf, max(g.m_days + 1, g.min_history_days), '1d', ['close'])['close']

        if history.empty or len(history) < g.min_history_days:
            continue
        prices = history.iloc[-g.m_days:]

        # 核心计算逻辑：加权线性回归
        y = np.log(prices.values)
        x = np.arange(len(y))
        weights = np.linspace(1, 2, len(y))

        try:
            # 计算年化收益率
            slope, intercept = np.polyfit(x, y, 1, w=weights)
            annualized_returns = math.exp(slope * 250) - 1
            data.loc[etf, "annualized_returns"] = annualized_returns

            # 计算R2
            ss_res = np.sum(weights * (y - (slope * x + intercept)) ** 2)
            ss_tot = np.sum(weights * (y - np.mean(y)) ** 2)
            r2 = 1 - ss_res / ss_tot if ss_tot else 0
            data.loc[etf, "r2"] = r2

            # 计算得分
            score = annualized_returns * r2
            data.loc[etf, "score"] = score

            # 更新局部变量 max_observed_score
            if score > max_observed_score:
                max_observed_score = score

            etf_name = get_security_info(etf).display_name
            print_data[etf_name] = score
        except Exception as e:
            log.warning(f"计算ETF {etf} 得分时出错: {e}")
            continue

    # --- 以下代码保持不变 ---
    # 过滤不符合条件的ETF，并按得分降序排列
    data.dropna(inplace=True)

    # 分别应用上下限过滤
    above_min = data.query(f"score >= {g.min_score}")
    below_max = data.query(f"score <= {g.max_score}")

    # 检查是否有ETF超过阈值
    above_threshold = data.query(f"score > {g.max_score}")
    if len(above_threshold) > 0:
        # 有ETF超过阈值，执行新逻辑
        high_score_etfs = above_threshold.index.tolist()
        log.info(f"发现 {len(high_score_etfs)} 只ETF超过阈值: {[get_security_info(etf).display_name for etf in high_score_etfs]}")

        # 计算前一天的分数
        yesterday_scores = {}
        for etf in high_score_etfs:
            # 获取过去 g.m_days+1 天的收盘价历史数据，以便计算t-1日分数
            prices = attribute_history(etf, g.m_days + 1, '1d', ['close'])['close']
            if prices.empty or len(prices) < g.m_days + 1:
                continue

            # 计算t-1日的分数（使用前g.m_days天的数据）
            yesterday_prices = prices[:-1] # 去掉最后一天，保留前g.m_days天

            # 核心计算逻辑：加权线性回归
            y = np.log(yesterday_prices.values)
            x = np.arange(len(y))
            weights = np.linspace(1, 2, len(y))

            try:
                # 计算年化收益率
                slope, intercept = np.polyfit(x, y, 1, w=weights)
                annualized_returns = math.exp(slope * 250) - 1

                # 计算R2
                ss_res = np.sum(weights * (y - (slope * x + intercept)) ** 2)
                ss_tot = np.sum(weights * (y - np.mean(y)) ** 2)
                r2 = 1 - ss_res / ss_tot if ss_tot else 0

                # 计算t-1日得分
                yesterday_score = annualized_returns * r2
                yesterday_scores[etf] = yesterday_score
            except Exception as e:
                log.warning(f"计算ETF {etf} t-1日得分时出错: {e}")
                continue

        # 更新g.yesterday_scores
        g.yesterday_scores = yesterday_scores.copy()

        # 根据新逻辑筛选ETF
        qualified_etfs = []
        for etf in high_score_etfs:
            if etf in g.yesterday_scores:
                t_day_score = data.loc[etf, "score"]
                t_minus_1_score = g.yesterday_scores[etf]
                # 检查t日分数是否大于等于t-1日分数的1.5倍
                if t_day_score >= t_minus_1_score * g.score_threshold_multiplier:
                    qualified_etfs.append(etf)
                    log.info(f"{get_security_info(etf).display_name}({etf}): t日分数 {t_day_score:.4f} >= t-1日分数 {t_minus_1_score:.4f} * {g.score_threshold_multiplier} = {t_minus_1_score * g.score_threshold_multiplier:.4f}")
                else:
                    log.info(f"{get_security_info(etf).display_name}({etf}): t日分数 {t_day_score:.4f} < t-1日分数 {t_minus_1_score:.4f} * {g.score_threshold_multiplier} = {t_minus_1_score * g.score_threshold_multiplier:.4f}，不进入排名")
            else:
                # 如果没有前一天的分数，则使用原逻辑
                qualified_etfs.append(etf)

        # 对符合条件的ETF按分数降序排列
        if qualified_etfs:
            qualified_data = data.loc[qualified_etfs]
            valid_data = qualified_data.sort_values(by="score", ascending=False)
        else:
            # 如果没有符合条件的ETF，按照原逻辑处理
            valid_data = data.query(f"{g.min_score} <= score <= {g.max_score}") # 仅保留符合范围的ETF
            valid_data = valid_data.sort_values(by="score", ascending=False)
    else:
        # 没有ETF超过阈值，沿用原逻辑
        valid_data = data.query(f"{g.min_score} <= score <= {g.max_score}") # 仅保留符合范围的ETF
        valid_data = valid_data.sort_values(by="score", ascending=False)

    # 输出过滤信息
    if high_score_etfs:
        log.info(f"超过阈值({g.max_score})的ETF: {', '.join([f'{get_security_info(etf).display_name}({etf})' for etf in high_score_etfs])}")

    # 打印排名靠前的ETF
    top_etfs_info = []
    # 假设 print_info 是基于 print_data 构建的，这里进行修正
    # 注意：原代码中的 print_info 可能是股票部分的变量，这里应使用 print_data
    for etf_code in valid_data.index.tolist()[:5]: # 假设只取前5个
        etf_name = get_security_info(etf_code).display_name
        # 从 print_data 中获取分数，如果不存在则为 'N/A'
        score_val = print_data.get(etf_name, 'N/A')
        top_etfs_info.append(f"{etf_name} ({score_val:.4f})")
    log.info("ETF动量评分排名: {}".format(' > '.join(top_etfs_info)))

    # 返回排序后的ETF列表
    return valid_data.index.tolist()


def strategy_3_sell(context):
    # 获取动量最高的ETF
    rank_df = get_etf_rank(context, g.etf_pool_3)

    # 如果没有符合条件的ETF，清仓当前持仓
    if not rank_df:
        for current_etf in g.strategy_holdings[3]:
            print("👿👿👿👿👿 ETF轮动没有一个能打的, 清仓当前持仓")
            close_position(current_etf)
            g.strategy_holdings[3] = []
        return
    # 初始化买入ETF为None
    g.buy_etf = None
    # 选择排名最高的ETF
    select_etf = rank_df[0]
    current_etf = None

    # 检查当前持仓
    for asset in context.portfolio.positions:
        if asset in g.etf_pool_3:
            current_etf = asset
            break

    # 策略3专用资金
    strategy_cash = context.portfolio.total_value * g.portfolio_value_proportion[2]

    # 需要调仓的情况（当前持仓不为空且与选择的ETF不同）
    if current_etf and current_etf != select_etf:
        print(f"ETF轮动调仓: {current_etf} -> {select_etf}")
        close_position(current_etf)  # 卖掉当前的
        g.buy_etf = select_etf  # 实际买入在 strategy_3_buy 中执行
    # 首次买入或恢复持仓（当前没有持仓且有可用资金）
    elif not current_etf and strategy_cash > 0:
        g.buy_etf = select_etf
        print(f"ETF轮动建仓: {select_etf}")
        # open_position(context, select_etf, strategy_cash, 3)  # 买入新的
    g.strategy_holdings[3] = list(set(g.strategy_holdings[3]))


def strategy_3_buy(context):
    # 如果有指定的买入ETF，则执行买入操作
    if g.buy_etf:
        strategy_cash = context.portfolio.total_value * g.portfolio_value_proportion[2]
        open_position(context, g.buy_etf, strategy_cash, 3)  # 买入新的
    g.strategy_holdings[3] = list(set(g.strategy_holdings[3]))


""" ====================== 公共函数 ====================== """


# 封装实盘下单函数
def my_order_target_value(security, value):
    o = order_target_value(security, value)
    if o:
        if o.is_buy:
            if o.price * o.amount > 0:
                print(f"🚚🚚🚚🚚🚚 {format_stock_code(security)}  "
                      f"买价{o.price:<7.2f}  "
                      f"买量{o.amount:<7}   "
                      f"价值{o.price * o.amount:.2f}")
                return o
        else:
            if o.price * o.amount > 0:
                print(f"🚛🚛🚛🚛🚛 {format_stock_code(security)}  "
                      f"卖价{o.price:<7.2f}  "
                      f"成本{o.avg_cost:<7.2f}   "
                      f"卖量{o.amount:<7}   "
                      f"盈亏{(o.price - o.avg_cost) * o.amount:.2f}"
                      f"( {(o.price - o.avg_cost) / o.avg_cost * 100:.2f}% )")
                return o


# 开仓买入并记录策略持仓
def open_position(context, security, value, strategy_id):
    if value <= 5000:
        # print(f"买入金额{value}较小, 不进行买入")
        return
    if security in context.portfolio.positions:
        security_value = context.portfolio.positions[security].value
        if abs(value - security_value) < 5000:
            # print(f"买入差额{value - security_value}较小, 不进行买入")
            return
    order = my_order_target_value(security, value)
    if order:
        security not in g.strategy_holdings[strategy_id] and g.strategy_holdings[strategy_id].append(security)
        g.stock_strategy[security] = strategy_id
    return order


# 闭仓卖出并清空策略持仓
def close_position(security):
    order = my_order_target_value(security, 0)
    if order:
        strategy_id = g.stock_strategy[security]
        # 持仓列表移除
        security in g.strategy_holdings[strategy_id] and g.strategy_holdings[strategy_id].remove(security)
        # 计算卖出的盈亏
        pnl_value = (order.price - order.avg_cost) * order.amount
        # 每日策略总价值更新盈亏
        g.strategy_value[strategy_id] += pnl_value
    return order


""" ====================== 模块工具函数 ====================== """


# 展示优化
def format_stock_code(stock_code):
    try:
        stock_info = get_security_info(stock_code)
    except Exception:
        return f"{stock_code[:6]}"
    return f"{stock_code[:6]}({stock_info.display_name}) "


# 记录每日策略收益
def make_record(context):
    positions = context.portfolio.positions
    if not positions:
        return
    current_data = get_current_data()
    g.strategy_value_data = {3: 0}
    # 复制一个昨天的记录进行累计
    copy_strategy_value = {3: g.strategy_value[3]}
    for stock, pos in positions.items():
        strategy_id = g.stock_strategy[stock]
        current_value = pos.total_amount * current_data[stock].last_price  # 当前价值
        cost_value = pos.total_amount * pos.avg_cost  # 成本价值
        pnl_value = current_value - cost_value  # 当前盈亏金额
        copy_strategy_value[strategy_id] += pnl_value  # 计算浮盈浮亏
        g.strategy_value_data[strategy_id] += current_value
    record(ETF轮动=round(copy_strategy_value[3] / g.strategy_starting_cash[3] * 100 - 100, 2))


# 制表展示每日收益
def print_summary(context):
    """
    打印当前投资组合的总资产和持仓详情

    参数:
        context: 包含投资组合信息的对象。
        get_current_data: 获取当前市场数据的函数。
    """
    # 获取总资产
    total_value = round(context.portfolio.total_value, 2)

    # 获取当前持仓
    current_stocks = context.portfolio.positions
    if not current_stocks:
        # 如果没有持仓，只显示总资产
        print(f"🚤🚤🚤🚤🚤 当前总资产: {total_value} 休息ing ")
        return

    # 创建表格
    table = PrettyTable([
        " 所属策略 ",
        " 股票代码 ",
        " 股票名称 ",
        " 持仓数量 ",
        " 持仓价格 ",
        " 当前价格 ",
        " 盈亏数额 ",
        " 盈亏比例 ",
        " 股票市值 ",
        " 仓位占比 "])
    table.hrules = prettytable.ALL  # 显示所有水平线

    # 遍历持仓股票
    total_market_value = 0  # 总市值（用于累加每只股票的市值）
    for stock in current_stocks:
        current_shares = current_stocks[stock].total_amount  # 持仓数量
        current_price = round(get_current_data()[stock].last_price, 3)  # 当前价格
        avg_cost = round(current_stocks[stock].avg_cost, 3)  # 持仓平均成本

        # 计算盈亏比例
        profit_ratio = (current_price - avg_cost) / avg_cost if avg_cost != 0 else 0
        profit_ratio_percent = f"{profit_ratio * 100:.2f}%"  # 转为百分比并保留两位小数
        profit_ratio_percent += f" {'↑' if profit_ratio > 0 else '↓'}"
        # 计算盈亏数额
        profit_amount = round((current_price - avg_cost) * current_shares, 2)

        # 计算市值
        market_value = round(current_shares * current_price, 2)
        total_market_value += market_value  # 累加总市值

        # 处理股票代码：移除后缀
        stock_code = stock.split(".")[0]  # 只保留股票代码部分

        # 添加到表格
        table.add_row([
            g.stock_strategy[stock],
            stock_code,
            format_stock_code(stock),
            current_shares,
            avg_cost,
            current_price,
            profit_amount,
            profit_ratio_percent,
            market_value,
            f"{market_value / context.portfolio.total_value * 100:.2f}%"
        ])

    # 账户总资产
    total_value = context.portfolio.total_value
    # 汇总
    if g.strategy_value_data.get(3):
        table.add_row(["ETF轮动", "", "", "", "", "", "", "", f"{g.strategy_value_data[3]:.2f}",
                       f"{g.strategy_value_data[3] / total_value * 100:.2f}%"])
    table.add_row(["总市值", "", "", "", "", "", "", "", f"{total_market_value:.2f}", ""])
    table.add_row(["总资产", "", "", "", "", "", "", "", f"{total_value:.2f}", ""])

    # 打印表格
    print(f'当前总资产\n{table}')


""" ====================== 调度入口 ====================== """


def after_code_changed(context):
    unschedule_all()

    # 策略3 ETF轮动策略
    run_daily(strategy_3_sell, '9:35:00')
    run_daily(strategy_3_buy, '9:35:05')

    # 记录各策略每日收益
    run_daily(make_record, '15:01')
    # 打印每日收益
    run_daily(print_summary, '15:02')
