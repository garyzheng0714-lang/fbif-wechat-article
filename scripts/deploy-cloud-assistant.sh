#!/usr/bin/env bash
# 通过阿里云云助手把 wechat-sync 部署到资讯机（FBIF-内部工具服务器）。
# 不需要 SSH、公网 IP 白名单或 VPN；只依赖本机已登录的 `aliyun` CLI。
#
# 用法：scripts/deploy-cloud-assistant.sh [--profile new-account]
# 前提：aliyun CLI 已用 OAuth 登录（过期时执行 `aliyun configure --profile new-account --mode OAuth`）。
#
# 流程：本地交叉编译 → gzip → 按 24000 字节分片 SendFile → 服务器合并校验 SHA-256
#       → 备份当前二进制 → 替换重启 → /health 与监控端点冒烟 → 失败自动回滚。
set -euo pipefail

PROFILE="new-account"
REGION="cn-shanghai"
INSTANCE_ID="i-uf6gm1aylcmxcu8luc62"   # FBIF-内部工具服务器 101.133.154.40
APP_DIR="/opt/fbif-wechat-article-dashboard"
CHUNK_BYTES=24000

while [ $# -gt 0 ]; do
  case "$1" in
    --profile) PROFILE="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

cd "$(dirname "$0")/.."
COMMIT=$(git rev-parse --short HEAD)
RELEASE="$(date -u +%Y%m%dT%H%M%SZ)-${COMMIT}"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

echo "==> build ${COMMIT}"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$WORK/wechat-sync" .
gzip -9 -c "$WORK/wechat-sync" > "$WORK/wechat-sync.gz"
SHA_BIN=$(shasum -a 256 "$WORK/wechat-sync" | cut -d' ' -f1)
SHA_GZ=$(shasum -a 256 "$WORK/wechat-sync.gz" | cut -d' ' -f1)
SIZE_GZ=$(stat -f%z "$WORK/wechat-sync.gz" 2>/dev/null || stat -c%s "$WORK/wechat-sync.gz")
echo "    binary sha256 ${SHA_BIN}"
echo "    gz ${SIZE_GZ} bytes, $(( (SIZE_GZ + CHUNK_BYTES - 1) / CHUNK_BYTES )) chunks"

REMOTE_DIR="${APP_DIR}/releases/${RELEASE}"
mkdir -p "$WORK/chunks"
split -b "$CHUNK_BYTES" -a 4 -d "$WORK/wechat-sync.gz" "$WORK/chunks/c."

send_file() { # name path
  local name path token b64 out
  name=$1; path=$2; token="deploy-${RELEASE}-${name}"
  b64=$(base64 < "$path" | tr -d '\n')
  out=$(aliyun ecs SendFile --profile "$PROFILE" --RegionId "$REGION" --InstanceId.1 "$INSTANCE_ID" \
      --Name "$name" --TargetDir "$REMOTE_DIR" --Content "$b64" --ContentType Base64 \
      --Overwrite true --FileMode 0644 --ClientToken "$token" 2>&1) || { echo "SendFile ${name} failed: ${out}" >&2; return 1; }
}

echo "==> upload to ${REMOTE_DIR}"
i=0
for f in "$WORK"/chunks/c.*; do
  send_file "$(basename "$f")" "$f"
  i=$((i+1)); [ $((i % 25)) -eq 0 ] && echo "    ${i} chunks sent"
done
printf '%s  wechat-sync.gz\n' "$SHA_GZ" > "$WORK/SHA256SUMS"
send_file "SHA256SUMS" "$WORK/SHA256SUMS"
echo "    ${i} chunks + SHA256SUMS sent; waiting for delivery"

# SendFile 是异步的，先等待所有文件落地
for _ in $(seq 1 40); do
  pending=$(aliyun ecs DescribeSendFileResults --profile "$PROFILE" --RegionId "$REGION" --InstanceId "$INSTANCE_ID" --PageSize 50 2>/dev/null \
    | python3 -c "import json,sys; d=json.load(sys.stdin); r=[x for x in d.get('Invocations',{}).get('Invocation',[]) if x.get('TargetDir')=='${REMOTE_DIR}']; print(sum(1 for x in r if x.get('InvocationStatus') in ('Pending','Running')))" 2>/dev/null || echo 1)
  [ "$pending" = "0" ] && break
  sleep 3
done

echo "==> install on server"
REMOTE_SCRIPT=$(cat <<EOS
set -euo pipefail
cd "${REMOTE_DIR}"
expected_chunks=${i}
actual=\$(ls c.* | wc -l)
[ "\$actual" -eq "\$expected_chunks" ] || { echo "chunk count \$actual != \$expected_chunks"; exit 3; }
cat \$(ls c.* | sort) > wechat-sync.gz
sha256sum -c SHA256SUMS
gzip -dc wechat-sync.gz > wechat-sync && chmod 755 wechat-sync
echo "\$(sha256sum wechat-sync | cut -d' ' -f1)  wechat-sync" > BUILD_SHA256
grep -q "${SHA_BIN}" BUILD_SHA256 || { echo "binary sha mismatch"; exit 4; }
rm -f c.* wechat-sync.gz
cp -p ${APP_DIR}/bin/wechat-sync ${APP_DIR}/bin/wechat-sync.prev
systemctl stop wechat-sync
cp wechat-sync ${APP_DIR}/bin/wechat-sync
systemctl start wechat-sync
sleep 12
ok=0
for n in 1 2 3 4 5; do curl -fsS --max-time 5 http://127.0.0.1:3002/health >/dev/null && ok=1 && break; sleep 4; done
if [ "\$ok" -ne 1 ]; then
  echo "health check failed, rolling back"
  systemctl stop wechat-sync; cp ${APP_DIR}/bin/wechat-sync.prev ${APP_DIR}/bin/wechat-sync; systemctl start wechat-sync; exit 5
fi
KEY=\$(grep -E '^API_KEY=' ${APP_DIR}/.env | head -1 | cut -d= -f2- | tr -d '"'"'"' ')
code=\$(curl -s -o /dev/null -w '%{http_code}' --max-time 15 -H "X-API-Key: \$KEY" http://127.0.0.1:3002/api/wechat/official/monitoring)
echo "monitoring endpoint HTTP \$code"
printf '{"release":"%s","commit":"%s","sha256":"%s","deployed_at":"%s"}\n' "${RELEASE}" "${COMMIT}" "${SHA_BIN}" "\$(date -u +%FT%TZ)" > ${APP_DIR}/release.json
ls -dt ${APP_DIR}/releases/* | tail -n +6 | xargs -r rm -rf
echo "deployed ${RELEASE}"
EOS
)
CMD_B64=$(printf '%s' "$REMOTE_SCRIPT" | base64 | tr -d '\n')
INV=$(aliyun ecs RunCommand --profile "$PROFILE" --RegionId "$REGION" --InstanceId.1 "$INSTANCE_ID" \
  --Type RunShellScript --CommandContent "$CMD_B64" --ContentEncoding Base64 --Timeout 300 --KeepCommand false \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['InvokeId'])")
for _ in $(seq 1 100); do
  sleep 3
  RES=$(aliyun ecs DescribeInvocationResults --profile "$PROFILE" --RegionId "$REGION" --InvokeId "$INV")
  STATUS=$(printf '%s' "$RES" | python3 -c "import json,sys; print(json.load(sys.stdin)['Invocation']['InvocationResults']['InvocationResult'][0]['InvocationStatus'])")
  case "$STATUS" in Running|Pending|Scheduled) continue;; esac
  printf '%s' "$RES" | python3 -c "import json,sys,base64; d=json.load(sys.stdin)['Invocation']['InvocationResults']['InvocationResult'][0]; print(base64.b64decode(d.get('Output','')).decode('utf-8','replace')); sys.exit(0 if d.get('ExitCode')==0 and d['InvocationStatus']=='Success' else 1)"
  exit $?
done
echo "timed out waiting for install; invokeId=${INV}" >&2
exit 1
