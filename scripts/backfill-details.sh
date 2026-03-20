#!/bin/bash
# 文章详情回填脚本
# 用法:
#   ./backfill-details.sh              # 全量回填 (freepublish + x-reader)
#   ./backfill-details.sh freepublish  # 仅回填文章元数据
#   ./backfill-details.sh content      # 仅回填文章正文 (x-reader)
#
# 环境变量:
#   SERVER     - 服务器地址 (默认 http://112.124.103.65:3002)
#   BATCH_SIZE - x-reader 每批处理数量 (默认 50)
#   CONCURRENCY - x-reader 并发数 (默认 3)

SERVER="${SERVER:-http://112.124.103.65:3002}"
BATCH_SIZE="${BATCH_SIZE:-50}"
CONCURRENCY="${CONCURRENCY:-3}"
MODE="${1:-all}"

echo "============================================"
echo "  文章详情回填"
echo "  服务器: $SERVER"
echo "  模式: $MODE"
echo "  批量大小: $BATCH_SIZE"
echo "  并发数: $CONCURRENCY"
echo "============================================"
echo ""

case "$MODE" in
  freepublish)
    echo ">>> Phase 1: 通过 freepublish 回填文章元数据..."
    curl -s -X POST "$SERVER/api/feishu/backfill-details" \
      -H "Content-Type: application/json" \
      --max-time 600 | python3 -m json.tool
    ;;
  content)
    echo ">>> Phase 2: 通过 x-reader 回填文章正文..."
    curl -s -X POST "$SERVER/api/feishu/backfill-content" \
      -H "Content-Type: application/json" \
      -d "{\"batch_size\":$BATCH_SIZE,\"concurrency\":$CONCURRENCY}" \
      --max-time 600 | python3 -m json.tool
    ;;
  all)
    echo ">>> 全量回填: freepublish + x-reader..."
    curl -s -X POST "$SERVER/api/feishu/backfill-all-details" \
      -H "Content-Type: application/json" \
      -d "{\"batch_size\":$BATCH_SIZE,\"concurrency\":$CONCURRENCY}" \
      --max-time 600 | python3 -m json.tool
    ;;
  *)
    echo "未知模式: $MODE"
    echo "用法: $0 [freepublish|content|all]"
    exit 1
    ;;
esac

echo ""
echo "============================================"
echo "  请求已发送，查看服务器日志获取详细进度"
echo "============================================"
