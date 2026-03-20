#!/bin/bash
# 全量数据同步脚本 - 按月分批调用 sync 接口
# 从 2016-09-01 到今天
#
# 用法: ./full-sync.sh
# 环境变量:
#   SERVER - 服务器地址 (默认 http://112.124.103.65:3002)

SERVER="${SERVER:-http://112.124.103.65:3002}"
START_YEAR=2016
START_MONTH=9
END_DATE=$(date -v-1d +%Y-%m-%d 2>/dev/null || date -d "yesterday" +%Y-%m-%d)

echo "============================================"
echo "  全量数据同步: 2016-09-01 ~ $END_DATE"
echo "  服务器: $SERVER"
echo "============================================"

TOTAL_SUCCESS=0
TOTAL_FAILED=0
year=$START_YEAR
month=$START_MONTH

while true; do
  begin_date=$(printf "%04d-%02d-01" $year $month)

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

  [[ "$end_of_month" > "$END_DATE" ]] && end_of_month=$END_DATE
  [[ "$begin_date" > "$END_DATE" ]] && break

  echo -n "[$(date +%H:%M:%S)] 同步 $begin_date ~ $end_of_month ... "

  response=$(curl -s -X POST "$SERVER/api/feishu/sync" \
    -H "Content-Type: application/json" \
    -d "{\"begin_date\":\"$begin_date\",\"end_date\":\"$end_of_month\"}" \
    --max-time 300 2>&1)

  if echo "$response" | python3 -c "import sys,json; d=json.load(sys.stdin); assert d.get('success')" 2>/dev/null; then
    echo "✓"
    TOTAL_SUCCESS=$((TOTAL_SUCCESS + 1))
  else
    echo "✗ $(echo $response | head -c 200)"
    TOTAL_FAILED=$((TOTAL_FAILED + 1))
  fi

  year=$next_year
  month=$next_month
done

echo ""
echo "完成! 成功: $TOTAL_SUCCESS 月, 失败: $TOTAL_FAILED 月"
