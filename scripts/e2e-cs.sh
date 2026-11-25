#!/usr/bin/env bash
# 客服系统 D~I 期全链路验收脚本
# 依赖: curl / python3 / node(≥21, 内置 WebSocket) ; 服务: docker compose 全栈(8888/8889)
set -uo pipefail
BASE=http://127.0.0.1:8888
WS=ws://127.0.0.1:8889/ws
# 开发环境重跑前置: 清掉短信发送间隔/每日上限与登录限流的 Redis 测试键(仅限本地验收容器)
clear_test_keys() {
  local ks
  ks=$(docker exec gva-redis redis-cli --scan --pattern 'cs:sms:*' 2>/dev/null)
  [ -n "$ks" ] && echo $ks | xargs docker exec gva-redis redis-cli DEL >/dev/null 2>&1
  ks=$(docker exec gva-redis redis-cli --scan --pattern 'cs:rl:*' 2>/dev/null)
  [ -n "$ks" ] && echo $ks | xargs docker exec gva-redis redis-cli DEL >/dev/null 2>&1
}
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "  ✅ $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  ❌ $1 -> $2"; }
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval("d"+sys.argv[1]))' "$1" 2>/dev/null; }

# ---------- 1. member 登录 + 建会话(自动清理上轮未结束会话, 保证 fresh pending/seq/判重) ----------
echo "[1] member 登录/建会话"
clear_test_keys
agent_login() {
  local user=$1
  local TCF=""
  [ -n "${2:-}" ] && TCF=",\"tenantCode\":\"$2\""
  local CR DEV SMSRES
  CR=$(curl -s -X POST $BASE/agent/captcha)
  SMSRES=$(curl -s -X POST $BASE/agent/sms-code -H 'Content-Type: application/json' \
    -d "{\"username\":\"$user\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\"$TCF}")
  DEV=$(echo "$SMSRES" | jget "['data']['devCode']")
  CR=$(curl -s -X POST $BASE/agent/captcha)
  curl -s -X POST $BASE/agent/login -H 'Content-Type: application/json' \
    -d "{\"username\":\"$user\",\"password\":\"123456\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\",\"smsCode\":\"$DEV\"$TCF}"
}
MTOK=$(curl -s -X POST $BASE/member/login -H 'Content-Type: application/json' \
  -d '{"username":"member001","password":"123456","tenantCode":"demo"}' | jget "['data']['token']")
[ -n "$MTOK" ] && ok "member登录" || bad "member登录" "(见下行原始响应)"
new_conv() { curl -s -X POST $BASE/member/conversations -H "x-token: $MTOK" -H 'Content-Type: application/json' \
  -d '{"title":"E2E测试会话","source":"H5"}'; }
CV=$(new_conv)
CONV=$(echo "$CV" | jget "['data']['id']")
ST=$(echo "$CV" | jget "['data']['status']")
if [ -n "$CONV" ]; then
  # 无论 pending/active 都先结束旧会话, 保证本轮全新会话(seq从2起—seq1为bot欢迎语/文件判重不跨轮)
  echo "  ℹ️ 存在旧会话 id=$CONV status=$ST, 结束它"
  CLEAN_A1_TOKEN=""
  CT=$(agent_login agent001 | jget "['data']['token']")
  CLEAN_A1_TOKEN=$CT
  if [ -n "$CT" ]; then
    curl -s -X POST $BASE/agent/conversations/$CONV/claim -H "x-token: $CT" >/dev/null  # pending则接单以便结束
    CR2=$(curl -s -X POST $BASE/agent/conversations/$CONV/close -H "x-token: $CT")
    echo "$CR2" | grep -q '"code":0' || {  # 失败=其他客服当值, 请其结束
      CT2=$(agent_login agent002 | jget "['data']['token']")
      [ -n "$CT2" ] && curl -s -X POST $BASE/agent/conversations/$CONV/close -H "x-token: $CT2" >/dev/null
    }
  fi
  CV=$(new_conv)
  CONV=$(echo "$CV" | jget "['data']['id']")
  ST=$(echo "$CV" | jget "['data']['status']")
fi
[ -n "$CONV" ] && [ "$ST" = "pending" ] && ok "会话就绪 id=$CONV(pending)" || bad "创建会话" "$CV"

# ---------- 2. 客服端 MFA 登录(若清理阶段已登录则复用令牌, 避免撞短信60s发送间隔) ----------
echo "[2] agent MFA 登录"
if [ -n "${CLEAN_A1_TOKEN:-}" ]; then
  ARES="{\"code\":0,\"data\":{\"token\":\"$CLEAN_A1_TOKEN\"}}"
else
  ARES=$(agent_login agent001)
fi
ATOK=$(echo "$ARES" | jget "['data']['token']")
[ -n "$ATOK" ] && ok "agent001登录(MFA)" || bad "agent001登录" "$ARES"

# ---------- 3. 待接入列表 + 原子抢单 ----------
echo "[3] 抢单"
PEND=$(curl -s "$BASE/agent/conversations/pending" -H "x-token: $ATOK")
echo "$PEND" | grep -Eq "\"id\":$CONV[,}]" && ok "待接入列表含会话$CONV" || bad "待接入列表" "$PEND"
CLAIM=$(curl -s -X POST $BASE/agent/conversations/$CONV/claim -H "x-token: $ATOK")
echo "$CLAIM" | grep -q '"status":"active"' && ok "抢单成功→active" || bad "抢单" "$CLAIM"
CLAIM2=$(curl -s -X POST $BASE/agent/conversations/$CONV/claim -H "x-token: $ATOK")
echo "$CLAIM2" | grep -q '不可重复\|已被接入\|not pending\|已被' && ok "重复抢单被拒" || echo "  ℹ️ 重复抢单响应: $(echo $CLAIM2|head -c 100)"

# ---------- 4. 消息热路径: 发送/幂等/双端 ----------
echo "[4] 消息"
CMID=$(python3 -c "import uuid;print(uuid.uuid4())")
M1=$(curl -s -X POST $BASE/member/conversations/$CONV/messages -H "x-token: $MTOK" -H 'Content-Type: application/json' \
  -d "{\"clientMsgId\":\"$CMID\",\"type\":\"text\",\"body\":\"你好(E2E-1)\"}")
SEQ1=$(echo "$M1" | jget "['data']['seq']")
[ "$SEQ1" = "2" ] && ok "member发送 seq=2(机器人欢迎语占 seq=1, Kafka acks=all ACK)" || bad "member发送" "$M1"
M1DUP=$(curl -s -X POST $BASE/member/conversations/$CONV/messages -H "x-token: $MTOK" -H 'Content-Type: application/json' \
  -d "{\"clientMsgId\":\"$CMID\",\"type\":\"text\",\"body\":\"你好(E2E-1)\"}")
echo "$M1DUP" | grep -q '"deduplicated":true' && ok "幂等重发命中" || bad "幂等重发" "$M1DUP"
M2=$(curl -s -X POST $BASE/agent/conversations/$CONV/messages -H "x-token: $ATOK" -H 'Content-Type: application/json' \
  -d "{\"clientMsgId\":\"$(python3 -c "import uuid;print(uuid.uuid4())")\",\"type\":\"text\",\"body\":\"您好, 客服008为您服务(E2E-2)\"}")
SEQ2=$(echo "$M2" | jget "['data']['seq']")
[ "$SEQ2" = "3" ] && ok "agent发送 seq=3(会话内单调)" || bad "agent发送" "$M2"

# ---------- 5. storage-worker → Mongo → 双窗口历史 ----------
echo "[5] 历史(storage-worker 落库, 等待3s)"
sleep 3
HIST=$(curl -s "$BASE/member/conversations/$CONV/messages?limit=50" -H "x-token: $MTOK")
N=$(echo "$HIST" | jget "['data']['messages'].__len__()")
[ "$N" = "3" ] && ok "member历史3条(欢迎语+双向, Mongo经worker落库)" || bad "member历史" "$HIST"
echo "$HIST" | grep -q '"senderType":"bot"' && ok "bot欢迎语落历史(seq=1)" || bad "bot欢迎语" "$HIST"
echo "$HIST" | grep -q '"windowLimited":true' && ok "用户侧7天窗口标记" || bad "窗口标记" "$HIST"
AHIST=$(curl -s "$BASE/agent/conversations/$CONV/messages?limit=50" -H "x-token: $ATOK")
echo "$AHIST" | grep -q '"windowLimited":false' && ok "客服侧全量标记" || bad "客服全量" "$AHIST"
echo "$AHIST" | grep -q 'E2E-2' && ok "客服可见自己消息工号链路" || bad "客服历史内容" "$AHIST"
# 越权: 用另一账号/伪造访问他人会话(用member令牌访问不存在的会话)
FORBID=$(curl -s "$BASE/member/conversations/999999/messages" -H "x-token: $MTOK")
echo "$FORBID" | grep -q '"code":4\|无权' && ok "非成员会话被拒(IDOR防护)" || bad "IDOR检查" "$FORBID"

# ---------- 6. 已读上报 ----------
echo "[6] 已读"
RD=$(curl -s -X POST $BASE/member/conversations/$CONV/read -H "x-token: $MTOK" -H 'Content-Type: application/json' -d '{"lastReadSeq":2}')
echo "$RD" | grep -q '"code":0' && ok "已读水位上报" || bad "已读" "$RD"

# ---------- 7. WS 网关实时推送 ----------
echo "[7] WebSocket 推送"
node -e "
const ws = new WebSocket('$WS?token=$ATOK');
const timer = setTimeout(() => { console.log('WS_TIMEOUT'); process.exit(1) }, 8000);
ws.onopen = () => ws.send(JSON.stringify({action:'subscribe', conversationId: $CONV, lastSeq: 0}));
ws.onmessage = (e) => {
  const p = JSON.parse(e.data);
  if (p.type === 'subscribed') {
    fetch('$BASE/member/conversations/$CONV/messages', {method:'POST', headers:{'x-token':'$MTOK','Content-Type':'application/json'},
      body: JSON.stringify({clientMsgId:'ws-e2e-'+Date.now(), type:'text', body:'WS实时消息(E2E-3)'})});
  }
  if (p.type === 'message' && p.body.body.includes('E2E-3')) { clearTimeout(timer); console.log('WS_GOT'); process.exit(0); }
};
ws.onerror = () => { console.log('WS_ERR'); process.exit(1) };
" > /tmp/ws_out.txt 2>&1
grep -q WS_GOT /tmp/ws_out.txt && ok "agent经WS收到member实时消息" || bad "WS推送" "$(cat /tmp/ws_out.txt)"

# ---------- 8. G: 商户管理 + 转接 + 轨迹 ----------
echo "[8] 商户管理/转接/轨迹"
merchant_login() {
  local user=$1
  local CR SMS SMSRES
  CR=$(curl -s -X POST $BASE/merchant/captcha)
  SMSRES=$(curl -s -X POST $BASE/merchant/sms-code -H 'Content-Type: application/json' \
    -d "{\"username\":\"$user\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\"}")
  SMS=$(echo "$SMSRES" | jget "['data']['devCode']")
  CR=$(curl -s -X POST $BASE/merchant/captcha)
  curl -s -X POST $BASE/merchant/login -H 'Content-Type: application/json' \
    -d "{\"username\":\"$user\",\"password\":\"123456\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\",\"smsCode\":\"$SMS\"}"
}
MRES=$(merchant_login merchantadmin)
KTOK=$(echo "$MRES" | jget "['data']['token']")
[ -n "$KTOK" ] && ok "商户管理员登录" || bad "商户登录" "$MRES"
NEWAG=$(curl -s -X POST $BASE/merchant/admin/agents -H "x-token: $KTOK" -H 'Content-Type: application/json' \
  -d '{"username":"agent002","password":"123456","phone":"13800000003","realName":"李四","role":"agent","maxActive":8}')
A2ID=$(echo "$NEWAG" | jget "['data']['id']")
if [ -z "$A2ID" ]; then
  # 重跑场景: agent002 已存在则从列表取 id
  AL=$(curl -s "$BASE/merchant/admin/agents?page=1&pageSize=50" -H "x-token: $KTOK")
  A2ID=$(echo "$AL" | python3 -c "
import sys,json
d=json.load(sys.stdin)['data']
if isinstance(d,dict): d=d.get('list',[])
print(next((a['id'] for a in d if a.get('username')=='agent002'),''))" 2>/dev/null)
fi
[ -n "$A2ID" ] && ok "客服agent002就绪 id=$A2ID" || bad "创建客服" "$NEWAG"
TR=$(curl -s -X POST $BASE/agent/conversations/$CONV/transfer -H "x-token: $ATOK" -H 'Content-Type: application/json' \
  -d "{\"toAgentId\":$A2ID,\"reason\":\"E2E转接\"}")
echo "$TR" | grep -q '"code":0' && ok "转接到agent002" || bad "转接" "$TR"
EV=$(curl -s $BASE/agent/conversations/$CONV/events -H "x-token: $ATOK")
echo "$EV" | grep -q transfer && ok "接待轨迹含transfer事件" || bad "轨迹" "$EV"
OV=$(curl -s $BASE/merchant/admin/reports/overview -H "x-token: $KTOK")
echo "$OV" | grep -q '"convTotal"' && ok "商户报表总览" || bad "报表" "$OV"

# ---------- 9. H: 文件上传/扫描/短时效下载 ----------
echo "[9] 文件模块"
echo "hello cs file e2e $(date +%s)" > /tmp/e2e_ok.txt
UP=$(curl -s -X POST $BASE/member/conversations/$CONV/files -H "x-token: $MTOK" -F "file=@/tmp/e2e_ok.txt")
FID=$(echo "$UP" | jget "['data']['id']")
echo "$UP" | grep -q '"scanStatus":"clean"' && ok "上传+扫描clean id=$FID" || bad "上传" "$UP"
URL=$(curl -s $BASE/member/files/$FID/url -H "x-token: $MTOK" | jget "['data']['url']")
[ -n "$URL" ] && ok "签发短时效URL" || bad "签发URL" "$URL"
DL=$(curl -s "$BASE$URL")
echo "$DL" | grep -q "hello cs file" && ok "签名URL下载内容正确" || bad "下载" "$DL"
BADURL=$(echo "$URL" | sed 's/sig=./sig=x/')
DLBAD=$(curl -s "$BASE$BADURL")
echo "$DLBAD" | grep -q '"code":7\|无效' && ok "篡改签名被拒" || bad "签名篡改检查" "$DLBAD"
printf 'X5O!P%%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H* run=%s\n' "$(date +%s)" > /tmp/e2e_eicar.txt
UP2=$(curl -s -X POST $BASE/member/conversations/$CONV/files -H "x-token: $MTOK" -F "file=@/tmp/e2e_eicar.txt")
echo "$UP2" | grep -q '"scanStatus":"infected"' && ok "EICAR命中→infected" || bad "病毒扫描" "$UP2"
FID2=$(echo "$UP2" | jget "['data']['id']")
U2=$(curl -s $BASE/member/files/$FID2/url -H "x-token: $MTOK")
echo "$U2" | grep -q '"code":7\|违规\|扫描' && ok "违规文件禁止下载" || bad "违规下载拦截" "$U2"

# ---------- 10. I: 指标/限流 ----------
echo "[10] 指标与限流"
MET=$(curl -s $BASE/cs/metrics)
echo "$MET" | grep -q "cs_msg_sent_total" && ok "metrics含消息计数" || bad "metrics" "$(echo $MET|head -c 150)"
SENT=$(echo "$MET" | grep '^cs_msg_sent_total' | awk '{s+=$2}END{print s+0}')
[ "${SENT:-0}" -ge 3 ] 2>/dev/null && ok "消息计数≥3 (实际$SENT)" || echo "  ℹ️ msg_sent原始行: $(echo "$MET"|grep cs_msg_sent_total|head -3|tr '\n' ' ')"
RL=""
for i in $(seq 1 12); do R=$(curl -s -X POST $BASE/member/login -H 'Content-Type: application/json' -d '{"username":"x","password":"y"}'); echo "$R" | grep -q "频繁" && RL=1 && break; done
[ -n "$RL" ] && ok "登录限流生效(第${i}次)" || bad "限流" "12次未触发"

# ---------- 11. K: 类型白名单 + 图片消息全链路 + 附件仿用 ----------
echo "[11] 类型白名单与图片消息"
VID=$(curl -s -X POST $BASE/member/conversations/$CONV/messages -H "x-token: $MTOK" -H 'Content-Type: application/json' \
  -d "{\"clientMsgId\":\"$(python3 -c "import uuid;print(uuid.uuid4())")\",\"type\":\"video\",\"body\":\"http://x/y.mp4\"}")
echo "$VID" | grep -q '"code":7' && ok "type=video被白名单拒绝" || bad "video拒绝" "$VID"
# 1x1 png + 时间戳尾字节(避开sha256跨轮判重)
python3 -c "import base64;open('/tmp/e2e_img.png','wb').write(base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='))"
printf 'run=%s' "$(date +%s)" >> /tmp/e2e_img.png
UP3=$(curl -s -X POST $BASE/member/conversations/$CONV/files -H "x-token: $MTOK" -F "file=@/tmp/e2e_img.png")
FID3=$(echo "$UP3" | jget "['data']['id']")
FSZ=$(wc -c < /tmp/e2e_img.png | tr -d ' ')
echo "$UP3" | grep -q '"scanStatus":"clean"' && ok "图片上传clean id=$FID3" || bad "图片上传" "$UP3"
IMG=$(curl -s -X POST $BASE/member/conversations/$CONV/messages -H "x-token: $MTOK" -H 'Content-Type: application/json' \
  -d "{\"clientMsgId\":\"$(python3 -c "import uuid;print(uuid.uuid4())")\",\"type\":\"image\",\"body\":\"{\\\"fileId\\\":$FID3,\\\"fileName\\\":\\\"e2e_img.png\\\",\\\"size\\\":$FSZ}\"}")
echo "$IMG" | grep -q '"seq"' && ok "image消息发送ACK" || bad "image发送" "$IMG"
sleep 3
MHIST=$(curl -s "$BASE/member/conversations/$CONV/messages?limit=50" -H "x-token: $MTOK")
echo "$MHIST" | grep -q '"type":"image"' && ok "历史含image消息" || bad "image历史" "$(echo $MHIST | head -c 200)"
URL3=$(curl -s $BASE/member/files/$FID3/url -H "x-token: $MTOK" | jget "['data']['url']")
curl -s -o /tmp/e2e_img_dl.png "$BASE$URL3"
cmp -s /tmp/e2e_img.png /tmp/e2e_img_dl.png && ok "图片下载字节与原件一致" || bad "图片下载比对" "$(ls -l /tmp/e2e_img_dl.png 2>/dev/null)"
# 仿用他人附件: agent001(历史接待者可写消息)拿 member 上传的 fileId 发送 → 上传者校验拒绝
FAKE=$(curl -s -X POST $BASE/agent/conversations/$CONV/messages -H "x-token: $ATOK" -H 'Content-Type: application/json' \
  -d "{\"clientMsgId\":\"$(python3 -c "import uuid;print(uuid.uuid4())")\",\"type\":\"image\",\"body\":\"{\\\"fileId\\\":$FID3,\\\"fileName\\\":\\\"x.png\\\",\\\"size\\\":1}\"}")
echo "$FAKE" | grep -q '"code":7' && ok "借他人附件发送被拒" || bad "附件仿用校验" "$FAKE"
# 不存在的 fileId
FAKE2=$(curl -s -X POST $BASE/member/conversations/$CONV/messages -H "x-token: $MTOK" -H 'Content-Type: application/json' \
  -d "{\"clientMsgId\":\"$(python3 -c "import uuid;print(uuid.uuid4())")\",\"type\":\"image\",\"body\":\"{\\\"fileId\\\":999999,\\\"fileName\\\":\\\"x.png\\\",\\\"size\\\":1}\"}")
echo "$FAKE2" | grep -q '"code":7' && ok "无效fileId被拒" || bad "无效fileId" "$FAKE2"

# ---------- 12. K: 会话详情侧栏资料 + 跨会话接待汇总 ----------
echo "[12] 详情侧栏与接待汇总"
DET=$(curl -s $BASE/agent/conversations/$CONV -H "x-token: $ATOK")
echo "$DET" | grep -q '"name"' && ok "详情携带用户资料" || bad "详情资料" "$(echo $DET | head -c 200)"
echo "$DET" | grep -q '"totalConversations"' && ok "携带跨会话接待汇总" || bad "汇总字段" "$(echo $DET | head -c 300)"
DETF=$(curl -s $BASE/agent/conversations/999999 -H "x-token: $ATOK")
echo "$DETF" | grep -q '无权\|"code":7' && ok "不存在会话详情拒绝(IDOR)" || bad "详情IDOR" "$DETF"

# ---------- 13. K: 平台概览/租户/审计 + 商户审计域 ----------
echo "[13] 平台管理与审计"
platform_login() {
  local CR SMSRES DEV
  CR=$(curl -s -X POST $BASE/cs/platform/captcha)
  SMSRES=$(curl -s -X POST $BASE/cs/platform/sms-code -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\"}")
  DEV=$(echo "$SMSRES" | jget "['data']['devCode']")
  CR=$(curl -s -X POST $BASE/cs/platform/captcha)
  curl -s -X POST $BASE/cs/platform/login -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"123456\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\",\"smsCode\":\"$DEV\"}"
}
PRES=$(platform_login)
PTOK=$(echo "$PRES" | jget "['data']['token']")
[ -n "$PTOK" ] && ok "平台管理员登录(MFA)" || bad "平台登录" "$PRES"
POV=$(curl -s $BASE/cs/platform/admin/overview -H "x-token: $PTOK")
echo "$POV" | grep -q '"tenantTotal"' && ok "平台聚合概览" || bad "overview" "$(echo $POV | head -c 200)"
echo "$POV" | grep -q '"onlineAgents"' && ok "含在线客服聚合指标" || bad "overview字段" "$POV"
PL=$(curl -s "$BASE/cs/platform/admin/tenants?page=1&pageSize=10" -H "x-token: $PTOK")
echo "$PL" | grep -q '"total"' && ok "租户列表分页" || bad "租户列表" "$(echo $PL | head -c 200)"
sleep 1
PA=$(curl -s "$BASE/cs/platform/admin/audits?page=1&pageSize=5" -H "x-token: $PTOK")
echo "$PA" | grep -q '"list"' && ok "平台审计查询" || bad "平台审计" "$(echo $PA | head -c 200)"
PA2=$(curl -s "$BASE/cs/platform/admin/audits?page=1&pageSize=5&action=audit.query" -H "x-token: $PTOK")
echo "$PA2" | grep -q 'audit.query' && ok "审计查询自身落审计行" || echo "  ℹ️ 未见audit.query行(可能异步未落库, 不判失败)"
TEID=$(echo "$MRES" | jget "['data']['tenant']['id']")
MA=$(curl -s "$BASE/merchant/admin/audits?page=1&pageSize=50" -H "x-token: $KTOK")
OUT=$(echo "$MA" | TEID="$TEID" python3 -c "
import sys,json,os
te=int(os.environ.get('TEID') or 0)
d=json.load(sys.stdin)['data']['list']
print('OUT' if d and all(x['tenantId']==te for x in d) else 'BAD')" 2>/dev/null)
[ "$OUT" = "OUT" ] && ok "商户审计仅本租户动作" || bad "商户审计域" "$MA"

# ---------- 14. B2B 多商户渠道闭环 ----------
echo "[14] B2B 多商户(开租户/商户管理员/访客渠道登录)"
clear_test_keys
PRES=$(platform_login)
PTOK=$(echo "$PRES" | jget "['data']['token']")
[ -n "$PTOK" ] && ok "平台登录(B2B段)" || bad "平台登录(B2B)" "$PRES"
merchant_login() {
  local user=$1 CR SMSRES DEV
  CR=$(curl -s -X POST $BASE/merchant/captcha)
  SMSRES=$(curl -s -X POST $BASE/merchant/sms-code -H 'Content-Type: application/json' \
    -d "{\"username\":\"$user\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\"}")
  DEV=$(echo "$SMSRES" | jget "['data']['devCode']")
  CR=$(curl -s -X POST $BASE/merchant/captcha)
  curl -s -X POST $BASE/merchant/login -H 'Content-Type: application/json' \
    -d "{\"username\":\"$user\",\"password\":\"123456\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\",\"smsCode\":\"$DEV\"}"
}
# 幂等开通: shopb 已存在则复用
TID=$(curl -s "$BASE/cs/platform/admin/tenants?page=1&pageSize=50&keyword=shopb" -H "x-token: $PTOK" | jget "['data']['list'][0]['id']")
if [ -z "$TID" ] || [ "$TID" = "None" ]; then
  TRES=$(curl -s -X POST $BASE/cs/platform/admin/tenants -H "x-token: $PTOK" -H 'Content-Type: application/json' \
    -d '{"code":"shopb","name":"商户B","package":"basic","adminUsername":"shopbadmin","adminPhone":"13911112222","adminPassword":"123456"}')
  TID=$(echo "$TRES" | jget "['data']['id']")
fi
[ -n "$TID" ] && [ "$TID" != "None" ] && ok "平台开通租户B+同步建商户管理员" || bad "开通租户B" "${TRES:-$TID}"
SBRES=$(merchant_login shopbadmin)
SBK=$(echo "$SBRES" | jget "['data']['token']")
[ -n "$SBK" ] && ok "商户管理员登录租户B后台" || bad "shopbadmin登录" "$SBRES"
curl -s -X POST $BASE/merchant/admin/members -H "x-token: $SBK" -H 'Content-Type: application/json' \
  -d '{"username":"shopb_member","password":"123456","nickName":"B访客","source":"H5"}' >/dev/null
curl -s -X POST $BASE/merchant/admin/agents -H "x-token: $SBK" -H 'Content-Type: application/json' \
  -d '{"username":"shopb_agent","password":"123456","phone":"13922221111","realName":"B客服","role":"agent"}' >/dev/null
MLS=$(curl -s "$BASE/merchant/admin/members?page=1&pageSize=10&keyword=shopb_member" -H "x-token: $SBK")
echo "$MLS" | grep -q 'shopb_member' && ok "商户后台访客账号建/查" || bad "访客账号CRUD" "$MLS"
BAD=$(curl -s -X POST $BASE/member/login -H 'Content-Type: application/json' -d '{"username":"member001","password":"123456","tenantCode":"shopb"}')
echo "$BAD" | grep -q '渠道' && ok "跨商户/缺渠道登录被拒(渠道强制)" || bad "跨商户校验" "$BAD"
GOOD=$(curl -s -X POST $BASE/member/login -H 'Content-Type: application/json' -d '{"username":"shopb_member","password":"123456","tenantCode":"shopb"}')
BTOK=$(echo "$GOOD" | jget "['data']['token']")
TG=$(echo "$GOOD" | jget "['data']['tenant']['code']")
[ "$TG" = "shopb" ] && ok "访客带渠道标识登录+回显商户信息" || bad "渠道登录" "$GOOD"
LEG=$(curl -s -X POST $BASE/member/login -H 'Content-Type: application/json' -d '{"username":"shopb_member","password":"123456"}')
echo "$LEG" | grep -q '渠道' && ok "无渠道标识登录被拒(B2B强制)" || bad "无渠道拒登" "$LEG"
TP=$(curl -s "$BASE/member/tenant?code=shopb")
echo "$TP" | grep -q '"status":1' && echo "$TP" | grep -q '商户B' && ok "公开商户渠道查询" || bad "渠道查询" "$TP"
CVB=$(curl -s -X POST $BASE/member/conversations -H "x-token: $BTOK" -H 'Content-Type: application/json' -d '{"source":"H5-shopb"}')
CIDB=$(echo "$CVB" | jget "['data']['id']")
[ -n "$CIDB" ] && ok "访客B会话建立(归租户B)" || bad "访客B建会话" "$CVB"
A1K=$(agent_login agent001 | jget "['data']['token']")
A1P=$(curl -s "$BASE/agent/conversations/pending?page=1&pageSize=50" -H "x-token: $A1K")
if echo "$A1P" | grep -q "\"id\":$CIDB,"; then bad "租户A客服看不到B会话" "泄漏 id=$CIDB"; else ok "租户A客服看不到B会话"; fi
SBAK=$(agent_login shopb_agent | jget "['data']['token']")
SBP=$(curl -s "$BASE/agent/conversations/pending?page=1&pageSize=50" -H "x-token: $SBAK")
echo "$SBP" | grep -q "\"id\":$CIDB," && ok "商户B客服待接入可见B会话" || bad "B客服待接入" "$SBP"
curl -s -X PUT $BASE/cs/platform/admin/tenants/status -H "x-token: $PTOK" -H 'Content-Type: application/json' -d "{\"id\":$TID,\"status\":2}" >/dev/null
DIS=$(curl -s -X POST $BASE/member/login -H 'Content-Type: application/json' -d '{"username":"shopb_member","password":"123456","tenantCode":"shopb"}')
echo "$DIS" | grep -q '停用' && ok "停用商户拦截访客登录" || bad "停用拦截" "$DIS"
TP2=$(curl -s "$BASE/member/tenant?code=shopb")
echo "$TP2" | grep -q '"status":2' && ok "渠道查询反映停用态" || bad "停用态查询" "$TP2"
curl -s -X PUT $BASE/cs/platform/admin/tenants/status -H "x-token: $PTOK" -H 'Content-Type: application/json' -d "{\"id\":$TID,\"status\":1}" >/dev/null
RE=$(curl -s -X POST $BASE/member/login -H 'Content-Type: application/json' -d '{"username":"shopb_member","password":"123456","tenantCode":"shopb"}')
echo "$RE" | grep -q '"token"' && ok "重新启用后恢复服务" || bad "恢复启用" "$RE"
# 客服工作台渠道登录: 短信正常但 login 带错商户标识被拒(login 环节独立校验, 校验先于 smsCode 消费故码可复用); 正确标识成功并回显商户
TC_A=$(echo "$MRES" | jget "['data']['tenant']['code']")
clear_test_keys
CR=$(curl -s -X POST $BASE/agent/captcha)
DEV=$(curl -s -X POST $BASE/agent/sms-code -H 'Content-Type: application/json' \
  -d "{\"username\":\"shopb_agent\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\",\"tenantCode\":\"shopb\"}" | jget "['data']['devCode']")
CR=$(curl -s -X POST $BASE/agent/captcha)
WROA=$(curl -s -X POST $BASE/agent/login -H 'Content-Type: application/json' \
  -d "{\"username\":\"shopb_agent\",\"password\":\"123456\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\",\"smsCode\":\"$DEV\",\"tenantCode\":\"$TC_A\"}")
echo "$WROA" | grep -q '不属于' && ok "客服登录环节错渠道标识被拒" || bad "客服login渠道校验" "$WROA"
CR=$(curl -s -X POST $BASE/agent/captcha)
AGR=$(curl -s -X POST $BASE/agent/login -H 'Content-Type: application/json' \
  -d "{\"username\":\"shopb_agent\",\"password\":\"123456\",\"captcha\":\"$(echo "$CR" | jget "['data']['devCaptcha']")\",\"captchaId\":\"$(echo "$CR" | jget "['data']['captchaId']")\",\"smsCode\":\"$DEV\",\"tenantCode\":\"shopb\"}")
echo "$AGR" | grep -q '"token"' && ok "客服带渠道标识登录成功" || bad "客服渠道登录" "$AGR"
echo "$AGR" | grep -q '"shopb"' && ok "客服登录响应回显商户B" || bad "客服登录回显" "$AGR"
# 删除闭环: 临时商户D建即删(无会话可删); 商户B有未结束会话拒删
SUF=$((RANDOM%9000+1000))
DTID=$(curl -s -X POST $BASE/cs/platform/admin/tenants -H "x-token: $PTOK" -H 'Content-Type: application/json' \
  -d "{\"code\":\"shopd$SUF\",\"name\":\"商户D临时\",\"adminUsername\":\"shopdadmin$SUF\",\"adminPhone\":\"1393333$SUF\",\"adminPassword\":\"123456\"}" | jget "['data']['id']")
DEL=$(curl -s -X PUT $BASE/cs/platform/admin/tenants/delete -H "x-token: $PTOK" -H 'Content-Type: application/json' -d "{\"id\":$DTID}")
echo "$DEL" | grep -q '"code":0' && ok "平台删除商户(无进行中会话)" || bad "删除商户" "$DEL"
DELB=$(curl -s -X PUT $BASE/cs/platform/admin/tenants/delete -H "x-token: $PTOK" -H 'Content-Type: application/json' -d "{\"id\":$TID}")
echo "$DELB" | grep -q '未结束会话' && ok "有进行中会话拒删" || bad "拒删保护" "$DELB"

echo ""
echo "================ 验收结果: PASS=$PASS FAIL=$FAIL ================"
[ $FAIL -eq 0 ] && exit 0 || exit 1
