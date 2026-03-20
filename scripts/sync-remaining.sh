#!/bin/bash
# 分类型同步剩余数据 - 不调用文章API（已超限）
# 用法: ./sync-remaining.sh [sync-type] [start-date]
# sync-type: users | reads | shares | all-except-articles
# start-date: YYYY-MM-DD (默认 2020-06-01)

SERVER="${SERVER:-http://112.124.103.65:3002}"
SYNC_TYPE="${1:-all-except-articles}"
START_DATE="${2:-2020-06-01}"
END_DATE=$(date -v-1d +%Y-%m-%d 2>/dev/null || date -d "yesterday" +%Y-%m-%d)

START_YEAR=$(echo $START_DATE | cut -d'-' -f1)
START_MONTH=$(echo $START_DATE | cut -d'-' -f2 | sed 's/^0//')

echo "============================================"
echo "  分类同步: $SYNC_TYPE"
echo "  范围: $START_DATE ~ $END_DATE"
echo "============================================"
echo ""

sync_type_endpoint() {
  case "$1" in
    users)  echo "sync-users" ;;
    reads)  echo "sync-reads" ;;
    shares) echo "sync-shares" ;;
  esac
}

sync_one_type() {
  local type=$1
  local begin=$2
  local end=$3
  local endpoint=$(sync_type_endpoint $type)

  local response=$(curl -s -X POST "$SERVER/api/feishu/$endpoint" \
    -H "Content-Type: application/json" \
    -d "{\"begin_date\":\"$begin\",\"end_date\":\"$end\"}" \
    --max-time 120 2>&1)

  # Check for quota error
  if echo "$response" | grep -q "45009"; then
    echo "QUOTA_EXCEEDED"
    return 1
  fi

  local stats=$(echo "$response" | python3 -c "
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
  echo "$stats"
  return 0
}

run_sync() {
  local type=$1
  echo ""
  echo ">>> 开始同步: $type"
  echo "-------------------------------------------"

  local year=$START_YEAR
  local month=$START_MONTH
  local success=0
  local failed=0

  while true; do
    local begin_date=$(printf "%04d-%02d-01" $year $month)

    if [ $month -eq 12 ]; then
      local next_year=$((year + 1))
      local next_month=1
    else
      local next_year=$year
      local next_month=$((month + 1))
    fi

    local end_of_month=$(date -j -v-1d -f "%Y-%m-%d" "$(printf '%04d-%02d-01' $next_year $next_month)" +%Y-%m-%d 2>/dev/null)
    if [ -z "$end_of_month" ]; then
      end_of_month=$(date -d "$(printf '%04d-%02d-01' $next_year $next_month) - 1 day" +%Y-%m-%d)
    fi

    if [[ "$end_of_month" > "$END_DATE" ]]; then
      end_of_month=$END_DATE
    fi

    if [[ "$begin_date" > "$END_DATE" ]]; then
      break
    fi

    echo -n "  [$(date +%H:%M:%S)] $begin_date ~ $end_of_month ... "

    local result=$(sync_one_type $type $begin_date $end_of_month)

    if [ "$result" = "QUOTA_EXCEEDED" ]; then
      echo "⚠️  配额已用完，停止"
      failed=$((failed + 1))
      echo ""
      echo "  $type 同步因配额不足停止，下次从 $begin_date 开始"
      return 1
    else
      echo "✓ $result"
      success=$((success + 1))
    fi

    year=$next_year
    month=$next_month
  done

  echo "  $type 同步完成! 成功: $success 个月"
  return 0
}

case "$SYNC_TYPE" in
  users)
    run_sync users
    ;;
  reads)
    run_sync reads
    ;;
  shares)
    run_sync shares
    ;;
  all-except-articles)
    run_sync users
    run_sync shares
    run_sync reads  # reads 放最后，因为配额最紧张
    ;;
  *)
    echo "未知类型: $SYNC_TYPE"
    echo "用法: $0 [users|reads|shares|all-except-articles] [start-date]"
    exit 1
    ;;
esac

echo ""
echo "============================================"
echo "  同步完成!"
echo "============================================"
