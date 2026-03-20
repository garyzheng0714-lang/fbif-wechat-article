#!/bin/bash
# 智能全量同步脚本
# 特性:
#   1. 从最近日期往回同步（优先保证近期数据）
#   2. 大批量传参（一次传整个季度，服务端自动按天拆分）
#   3. 检测漏同步（quota 超限时的失败日期）和错更（API 错误）
#   4. 自动记录进度，下次继续
#
# 用法:
#   ./smart-sync.sh                     # 同步所有类型
#   ./smart-sync.sh articles            # 只同步文章
#   ./smart-sync.sh reads               # 只同步阅读
#   ./smart-sync.sh verify              # 验证模式：重新同步全量检查漏更

SERVER="${SERVER:-http://112.124.103.65:3002}"
PROGRESS_DIR="$(dirname "$0")/.sync-progress"
EARLIEST_DATE="2016-09-01"
YESTERDAY=$(date -v-1d +%Y-%m-%d 2>/dev/null || date -d "yesterday" +%Y-%m-%d)
MODE="${1:-all}"
QUARTER_DAYS=90  # 每次同步 90 天（一个季度）

mkdir -p "$PROGRESS_DIR"

# ====== 工具函数 ======

get_progress() {
  local type=$1
  local file="$PROGRESS_DIR/${type}.last"
  if [ -f "$file" ]; then
    cat "$file"
  else
    echo ""
  fi
}

save_progress() {
  local type=$1
  local date=$2
  echo "$date" > "$PROGRESS_DIR/${type}.last"
}

# 日期减法：date_sub "2026-02-27" 90 → "2025-11-29"
date_sub() {
  local d=$1 n=$2
  date -j -v-${n}d -f "%Y-%m-%d" "$d" +%Y-%m-%d 2>/dev/null || date -d "$d - $n days" +%Y-%m-%d
}

# 日期加法
date_add() {
  local d=$1 n=$2
  date -j -v+${n}d -f "%Y-%m-%d" "$d" +%Y-%m-%d 2>/dev/null || date -d "$d + $n days" +%Y-%m-%d
}

# 日期比较：date_le "2020-01-01" "2021-01-01" → true
date_le() { [[ "$1" < "$2" || "$1" == "$2" ]]; }
date_lt() { [[ "$1" < "$2" ]]; }

# 取两个日期中较大的
date_max() { if [[ "$1" > "$2" ]]; then echo "$1"; else echo "$2"; fi; }

# ====== 同步函数 ======

sync_type() {
  local type=$1  # articles | users | reads | shares
  local endpoint
  case "$type" in
    articles) endpoint="sync" ;;
    users)    endpoint="sync-users" ;;
    reads)    endpoint="sync-reads" ;;
    shares)   endpoint="sync-shares" ;;
  esac

  # 确定同步的最远已完成位置（往回同步，所以记录的是"已同步到的最早日期"）
  local progress=$(get_progress "$type")
  local sync_end="$YESTERDAY"
  local sync_start

  if [ -n "$progress" ] && date_lt "$progress" "$EARLIEST_DATE"; then
    echo "  [$type] ✅ 全量已完成（最早到 $progress）"
    return 0
  fi

  if [ -n "$progress" ]; then
    # 往回继续：从上次最早日期再往前推
    sync_end=$(date_sub "$progress" 1)
    if date_lt "$sync_end" "$EARLIEST_DATE"; then
      echo "  [$type] ✅ 全量已完成"
      return 0
    fi
  fi

  echo ""
  echo "━━━ 同步: $type ━━━"
  echo "  目标范围: $EARLIEST_DATE ~ $sync_end"
  echo ""

  local total_synced=0
  local total_errors=0
  local current_end="$sync_end"

  while date_le "$EARLIEST_DATE" "$current_end"; do
    # 计算本批次的开始日期（往回推一个季度）
    local batch_start=$(date_sub "$current_end" $((QUARTER_DAYS - 1)))
    batch_start=$(date_max "$batch_start" "$EARLIEST_DATE")

    echo -n "  [$(date +%H:%M:%S)] $batch_start ~ $current_end ... "

    local response=$(curl -s -X POST "$SERVER/api/feishu/$endpoint" \
      -H "Content-Type: application/json" \
      -d "{\"begin_date\":\"$batch_start\",\"end_date\":\"$current_end\"}" \
      --max-time 600 2>&1)

    # 解析响应
    local stats=$(echo "$response" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    if not d.get('success') and 'error' in d:
        # 整个请求失败（比如配额用完导致 token 获取失败）
        if '45009' in d.get('error',''):
            print('QUOTA_EXCEEDED|||0|||0')
        else:
            print(f'ERROR:{d[\"error\"][:100]}|||0|||0')
    else:
        total = d.get('total', 0)
        created = d.get('created', 0)
        updated = d.get('updated', 0)
        api_errors = d.get('apiErrors', 0)
        quota_hit = d.get('quotaExceeded', False)
        if quota_hit:
            print(f'QUOTA_PARTIAL|||{total}|||{api_errors}')
        else:
            print(f'{total}条(+{created}/↑{updated})|||{total}|||{api_errors}')
except Exception as e:
    print(f'PARSE_ERROR|||0|||0')
" 2>/dev/null)

    local display=$(echo "$stats" | cut -d'|' -f1-1)
    local count=$(echo "$stats" | cut -d'|' -f4)
    local errors=$(echo "$stats" | cut -d'|' -f7)

    if [[ "$display" == "QUOTA_EXCEEDED" ]]; then
      echo "⛔ 配额用完，停止"
      # 保存进度：下次从 current_end 的下一天重新开始
      save_progress "$type" "$(date_add "$current_end" 1)"
      echo "  进度已保存，下次从 $(date_add "$current_end" 1) 往回继续"
      return 1
    elif [[ "$display" == QUOTA_PARTIAL* ]]; then
      echo "⚠️  部分成功（${count} 条数据，${errors} 个API错误，配额不足）"
      # 不保存进度，这批需要重做
      echo "  ⚠️  本批次有漏数据，下次需重做 $batch_start ~ $current_end"
      save_progress "$type" "$(date_add "$current_end" 1)"
      return 1
    elif [[ "$display" == ERROR* ]]; then
      echo "❌ ${display#ERROR:}"
      save_progress "$type" "$(date_add "$current_end" 1)"
      return 1
    elif [[ "$display" == "PARSE_ERROR" ]]; then
      echo "❌ 响应解析失败"
      save_progress "$type" "$(date_add "$current_end" 1)"
      return 1
    else
      local err_info=""
      if [ "$errors" -gt 0 ] 2>/dev/null; then
        err_info=" ⚠️ ${errors}个API错误"
      fi
      echo "✓ $display$err_info"
      total_synced=$((total_synced + ${count:-0}))
      total_errors=$((total_errors + ${errors:-0}))
    fi

    # 保存进度
    save_progress "$type" "$batch_start"

    # 往前推
    current_end=$(date_sub "$batch_start" 1)
  done

  echo ""
  echo "  [$type] 全量同步完成！共 $total_synced 条数据"
  if [ "$total_errors" -gt 0 ]; then
    echo "  ⚠️  共 $total_errors 个API错误，建议运行 verify 模式检查"
  fi
  return 0
}

# ====== 验证模式 ======

verify_type() {
  local type=$1
  local endpoint
  case "$type" in
    articles) endpoint="sync" ;;
    users)    endpoint="sync-users" ;;
    reads)    endpoint="sync-reads" ;;
    shares)   endpoint="sync-shares" ;;
  esac

  echo ""
  echo "━━━ 验证: $type ━━━"
  echo "  重新同步全量数据检查漏更/错更..."
  echo ""

  local current_end="$YESTERDAY"
  local total_errors=0
  local empty_quarters=0

  while date_le "$EARLIEST_DATE" "$current_end"; do
    local batch_start=$(date_sub "$current_end" $((QUARTER_DAYS - 1)))
    batch_start=$(date_max "$batch_start" "$EARLIEST_DATE")

    echo -n "  [验证] $batch_start ~ $current_end ... "

    local response=$(curl -s -X POST "$SERVER/api/feishu/$endpoint" \
      -H "Content-Type: application/json" \
      -d "{\"begin_date\":\"$batch_start\",\"end_date\":\"$current_end\"}" \
      --max-time 600 2>&1)

    local info=$(echo "$response" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    total = d.get('total', 0)
    created = d.get('created', 0)
    updated = d.get('updated', 0)
    api_errors = d.get('apiErrors', 0)
    quota = d.get('quotaExceeded', False)
    if quota:
        print(f'QUOTA|||{api_errors}')
    elif created > 0:
        print(f'MISSING|||{created}条漏更已补，{updated}条已更新，{api_errors}个错误')
    elif api_errors > 0:
        print(f'ERRORS|||{api_errors}个API错误')
    else:
        print(f'OK|||{updated}条已验证')
except:
    print('FAIL|||解析失败')
" 2>/dev/null)

    local status=$(echo "$info" | cut -d'|' -f1-1)
    local detail=$(echo "$info" | cut -d'|' -f4-)

    case "$status" in
      OK)      echo "✓ $detail" ;;
      MISSING) echo "🔧 $detail" ;;
      ERRORS)  echo "⚠️  $detail"; total_errors=$((total_errors + 1)) ;;
      QUOTA)   echo "⛔ 配额用完，停止验证"; return 1 ;;
      *)       echo "❌ $detail"; return 1 ;;
    esac

    current_end=$(date_sub "$batch_start" 1)
  done

  if [ "$total_errors" -eq 0 ]; then
    echo "  [$type] ✅ 验证通过，无漏更"
  else
    echo "  [$type] ⚠️  有 $total_errors 批次存在API错误"
  fi
}

# ====== 主逻辑 ======

echo "╔══════════════════════════════════════════╗"
echo "║       智能全量同步 (从近到远)            ║"
echo "║  范围: $EARLIEST_DATE ~ $YESTERDAY    ║"
echo "╚══════════════════════════════════════════╝"

case "$MODE" in
  articles)
    sync_type articles
    ;;
  users)
    sync_type users
    ;;
  reads)
    sync_type reads
    ;;
  shares)
    sync_type shares
    ;;
  all)
    # 按优先级同步：文章最重要也最耗配额，放第一
    sync_type articles
    sync_type reads
    sync_type users
    sync_type shares
    ;;
  verify)
    verify_type articles
    verify_type reads
    verify_type users
    verify_type shares
    ;;
  verify-articles)
    verify_type articles
    ;;
  verify-reads)
    verify_type reads
    ;;
  reset)
    echo "清除所有进度记录..."
    rm -f "$PROGRESS_DIR"/*.last
    echo "✓ 已清除"
    ;;
  status)
    echo ""
    echo "当前进度:"
    for type in articles reads users shares; do
      local_progress=$(get_progress "$type")
      if [ -n "$local_progress" ]; then
        if date_lt "$local_progress" "$EARLIEST_DATE"; then
          echo "  $type: ✅ 全量完成"
        else
          echo "  $type: 已同步到 $local_progress（往回方向）"
        fi
      else
        echo "  $type: 尚未开始"
      fi
    done
    ;;
  *)
    echo "用法: $0 [all|articles|reads|users|shares|verify|verify-articles|verify-reads|reset|status]"
    exit 1
    ;;
esac

echo ""
echo "完成! $(date +%Y-%m-%d\ %H:%M:%S)"
