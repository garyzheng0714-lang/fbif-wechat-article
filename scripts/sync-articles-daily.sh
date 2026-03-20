#!/bin/bash
# 文章数据限量同步脚本
# 每天运行一次，每次同步最多 N 天的数据，控制在API配额内
# 使用进度文件记录上次同步位置
#
# 用法: ./sync-articles-daily.sh [max-days-per-run]
# max-days-per-run: 每次运行最多同步多少天（默认450，对应约900次API调用，留余量）
#
# 配额计算:
# - getarticlesummary: 每天1000次配额，每天数据需1次调用
# - getarticletotal: 每天1000次配额，每天数据需1次调用
# - 每同步1天 = 2次API调用
# - 450天 = 900次调用，留100次给日常使用

SERVER="${SERVER:-http://112.124.103.65:3002}"
PROGRESS_FILE="/Users/simba/local_vibecoding/fbif-wechat-article-dashboard/scripts/.article-sync-progress"
MAX_DAYS_PER_RUN="${1:-450}"
FIRST_DATE="2019-05-01"  # 从这个日期开始（之前的已同步完）
END_DATE=$(date -v-1d +%Y-%m-%d 2>/dev/null || date -d "yesterday" +%Y-%m-%d)

# 读取上次进度
if [ -f "$PROGRESS_FILE" ]; then
  START_DATE=$(cat "$PROGRESS_FILE")
  echo "从上次进度继续: $START_DATE"
else
  START_DATE="$FIRST_DATE"
  echo "首次运行，从 $START_DATE 开始"
fi

# 检查是否已全部完成
if [[ "$START_DATE" > "$END_DATE" ]]; then
  echo "✅ 所有文章数据已同步完成！"
  exit 0
fi

echo "============================================"
echo "  文章数据限量同步"
echo "  范围: $START_DATE ~ $END_DATE"
echo "  本次最多: $MAX_DAYS_PER_RUN 天"
echo "============================================"
echo ""

# 按月分批同步
year=$(echo $START_DATE | cut -d'-' -f1)
month=$(echo $START_DATE | cut -d'-' -f2 | sed 's/^0//')
days_synced=0
success=0
failed=0
last_synced_date=""

while true; do
  begin_date=$(printf "%04d-%02d-01" $year $month)

  # 如果起始日期在月中，使用实际起始日期
  if [[ "$begin_date" < "$START_DATE" ]]; then
    begin_date="$START_DATE"
  fi

  if [ $month -eq 12 ]; then
    next_year=$((year + 1))
    next_month=1
  else
    next_year=$year
    next_month=$((month + 1))
  fi

  end_of_month=$(date -j -v-1d -f "%Y-%m-%d" "$(printf '%04d-%02d-01' $next_year $next_month)" +%Y-%m-%d 2>/dev/null)
  if [ -z "$end_of_month" ]; then
    end_of_month=$(date -d "$(printf '%04d-%02d-01' $next_year $next_month) - 1 day" +%Y-%m-%d)
  fi

  if [[ "$end_of_month" > "$END_DATE" ]]; then
    end_of_month=$END_DATE
  fi

  if [[ "$begin_date" > "$END_DATE" ]]; then
    break
  fi

  # 计算这个月有多少天
  start_epoch=$(date -j -f "%Y-%m-%d" "$begin_date" +%s 2>/dev/null || date -d "$begin_date" +%s)
  end_epoch=$(date -j -f "%Y-%m-%d" "$end_of_month" +%s 2>/dev/null || date -d "$end_of_month" +%s)
  month_days=$(( (end_epoch - start_epoch) / 86400 + 1 ))

  # 检查是否超过限额
  if [ $((days_synced + month_days)) -gt $MAX_DAYS_PER_RUN ]; then
    # 计算剩余可用天数
    remaining=$((MAX_DAYS_PER_RUN - days_synced))
    if [ $remaining -le 0 ]; then
      break
    fi
    # 调整结束日期
    end_of_month=$(date -j -v+${remaining}d -f "%Y-%m-%d" "$begin_date" +%Y-%m-%d 2>/dev/null)
    if [ -z "$end_of_month" ]; then
      end_of_month=$(date -d "$begin_date + $remaining days" +%Y-%m-%d)
    fi
    # 减1天因为 begin_date 算第1天
    end_of_month=$(date -j -v-1d -f "%Y-%m-%d" "$end_of_month" +%Y-%m-%d 2>/dev/null || date -d "$end_of_month - 1 day" +%Y-%m-%d)

    month_days=$remaining
  fi

  echo -n "[$(date +%H:%M:%S)] 同步文章 $begin_date ~ $end_of_month ($month_days 天) ... "

  response=$(curl -s -X POST "$SERVER/api/feishu/sync" \
    -H "Content-Type: application/json" \
    -d "{\"begin_date\":\"$begin_date\",\"end_date\":\"$end_of_month\"}" \
    --max-time 300 2>&1)

  # 检查配额错误
  if echo "$response" | grep -q "45009"; then
    echo "⚠️  配额用完！"
    failed=$((failed + 1))
    break
  fi

  stats=$(echo "$response" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    if d.get('success'):
        print(f\"{d.get('total',0)}条(+{d.get('created',0)}/↑{d.get('updated',0)})\")
    else:
        print(f\"错误: {d.get('error','unknown')[:80]}\")
except:
    print('解析失败')
" 2>/dev/null)
  echo "✓ $stats"
  success=$((success + 1))
  days_synced=$((days_synced + month_days))

  # 记录下次开始日期（end_of_month + 1天）
  last_synced_date=$(date -j -v+1d -f "%Y-%m-%d" "$end_of_month" +%Y-%m-%d 2>/dev/null || date -d "$end_of_month + 1 day" +%Y-%m-%d)

  # 如果已到结束日期
  if [[ "$end_of_month" >= "$END_DATE" ]]; then
    last_synced_date="DONE"
    break
  fi

  year=$next_year
  month=$next_month
done

# 保存进度
if [ "$last_synced_date" = "DONE" ]; then
  rm -f "$PROGRESS_FILE"
  echo ""
  echo "============================================"
  echo "  🎉 全部文章数据同步完成！"
  echo "  已同步: $days_synced 天, 成功: $success 批"
  echo "============================================"
elif [ -n "$last_synced_date" ]; then
  echo "$last_synced_date" > "$PROGRESS_FILE"
  echo ""
  echo "============================================"
  echo "  本次同步: $days_synced 天, 成功: $success 批"
  echo "  下次从: $last_synced_date 继续"
  echo "  预计还需: $(( ( $(date -j -f "%Y-%m-%d" "$END_DATE" +%s 2>/dev/null || date -d "$END_DATE" +%s) - $(date -j -f "%Y-%m-%d" "$last_synced_date" +%s 2>/dev/null || date -d "$last_synced_date" +%s) ) / 86400 / MAX_DAYS_PER_RUN + 1 )) 天完成"
  echo "============================================"
else
  echo "  未同步任何数据"
fi
