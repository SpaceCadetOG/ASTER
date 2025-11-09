def compound(principal, daily_return, num_days):
    """Compounds a given principal at a fixed daily % for num_days."""
    _r = daily_return / 100
    return round(principal * (1 + _r) ** num_days, 2)

def simulate_weeks(start_capital=100, daily_return=20, num_trades=5, days_per_week=7, num_weeks=4):
    """Simulates multi-trade compounding week by week."""
    capital = start_capital

    print(f"{'Week':<6}{'Start($)':<12}{'End($)':<12}{'Gain($)':<12}")
    print("-" * 40)

    for week in range(1, num_weeks + 1):
        trade_amount = capital / num_trades
        trade_result = compound(trade_amount, daily_return, days_per_week)
        week_total = trade_result * num_trades
        week_end = (week_total + capital) / 2  # your half-reinvest logic

        gain = round(week_end - capital, 2)
        print(f"{week:<6}{capital:<12.2f}{week_end:<12.2f}{gain:<12.2f}")

        capital = week_end  # carry forward to next week

    print("-" * 40)
    print(f"Final balance after {num_weeks * days_per_week} days: ${capital:,.2f}")

def project_fixed_util(start=100, per_trade_return_pct=20, util=1.0, days=30):
    # util in [0,1]; util=1.0 means 5/5 trades daily
    r_trade = per_trade_return_pct / 100
    R_daily = r_trade * util  # portfolio daily return
    bal = start * (1 + R_daily) ** days
    return round(bal, 2)

def project_variable_schedule(start=100, per_trade_return_pct=20, trades_per_day=None):
    """
    trades_per_day: list of ints (0..5), length = number of days
    Each trade uses 20% of current balance and returns per_trade_return_pct that day.
    """
    if trades_per_day is None:
        trades_per_day = [5]*30  # default 30 days full utilization
    bal = start
    a = 0.20
    r = per_trade_return_pct / 100.0

    for k in trades_per_day:
        # portfolio daily return = k * a * r
        daily_R = k * a * r
        bal *= (1 + daily_R)
    return round(bal, 2)





# # Run simulation
simulate_weeks(start_capital=100, daily_return=20)


# # Examples:
# for u in [1.0, 0.8, 0.6, 0.4, 0.2]:
#     print(u, project_fixed_util(100, 20, u, 30))


# # Example: 30 days with a pattern (5,4,3,4,5 repeating)
# pattern = [5,4,3,4,5]*6
# print(project_variable_schedule(100, 20, pattern))


trade_acct_week = compound(20, 20, 7)
full_weekly = trade_acct_week * 5
week1_profit = (full_weekly / 2)
print("1_weekly_trade_win", trade_acct_week)
print("1_full_weekly_trade_win", full_weekly)
print("1_Profit", week1_profit)

w2_allocation = week1_profit / 5

trade_acct_week2 = compound(w2_allocation, 20, 7)
full_weekly2 = trade_acct_week2 * 5
week2_profit = (full_weekly2 / 2)
print("2_weekly_trade_win", trade_acct_week2)
print("2_full_weekly_trade_win", full_weekly2)
print("2_Profit", week2_profit)

w3_allocation = week2_profit / 5

trade_acct_week3 = compound(w3_allocation, 20, 7)
full_weekly3 = trade_acct_week3 * 5
week3_profit = (full_weekly3 / 2)
print("1_weekly_trade_win", trade_acct_week3)
print("1_full_weekly_trade_win", full_weekly3)
print("1_Profit", week3_profit)

w4_allocation = week3_profit / 5

trade_acct_week4 = compound(w4_allocation, 20, 7)
full_weekly4 = trade_acct_week4 * 5
week4_profit = (full_weekly4 / 2)
print("4_weekly_trade_win", trade_acct_week4)
print("4_full_weekly_trade_win", full_weekly4)
print("4_Profit", week4_profit)