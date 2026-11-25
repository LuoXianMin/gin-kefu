# SSO 免密进入 · PHP 加密对接文档

商户页面跳转客服 h5 时，将访客信息 AES 加密后拼入链接，访客**无需注册登录**，系统自动建档（写入本商户访客表）并直接进入会话。

## 协议规范

| 项 | 约定 |
|---|---|
| 算法 | `AES-256-GCM`（自带完整性校验，篡改/错钥一律解密失败） |
| 密钥 | `SHA256(共享密钥)` 取 **32 字节原始二进制**（不是 hex 字符串） |
| IV | 12 字节随机数，每次加密都新生成 |
| 密文拼装 | `IV(12B) ‖ 密文 ‖ GCM标签(16B)`，整串 **Base64**（标准编码） |
| 明文 | JSON：`{"username":"...","externalId":"...","phone":"...","nickName":"...","ts":1700000000}` |
| 账号标识 | 三选一即可，优先级 `username > externalId > phone`；建议用商户侧用户 ID 作 `externalId` |
| ts | 可选，签发时刻（Unix 秒）；平台配置 `max-age-sec > 0` 时超时拒绝 |
| 共享密钥 | 开发环境 `gva-cs-sso-dev-key-2026`（生产由部署方另行下发，切勿写死在前端） |

最终链接：

```
https://客服域名/h5.html?t=<商户渠道编码>&user=<urlencode(密文Base64)>
```

> `user` 拼入 URL 前必须 `urlencode`（Base64 含 `+ / =`，不转义会被查询串吃掉导致解密失败）。

## 加密函数（PHP 7.1+，需 openssl 扩展）

```php
<?php
/**
 * 生成客服 SSO 链接的 user 参数
 *
 * @param array  $userInfo ['username'=>..,'externalId'=>..,'phone'=>..,'nickName'=>..]
 * @param string $aesKey   与平台约定的共享密钥（开发: gva-cs-sso-dev-key-2026）
 * @return string          Base64 密文，拼 URL 前请 urlencode
 */
function csSsoEncrypt(array $userInfo, string $aesKey): string
{
    $userInfo['ts'] = time();                        // 可选; 平台未开时效校验时可去掉
    $plain = json_encode($userInfo, JSON_UNESCAPED_UNICODE);

    $key = hash('sha256', $aesKey, true);            // 32字节原始密钥(务必 true)
    $iv  = random_bytes(12);                         // GCM 标准 12 字节 IV
    $tag = '';
    $cipher = openssl_encrypt($plain, 'aes-256-gcm', $key, OPENSSL_RAW_DATA, $iv, $tag);
    if ($cipher === false) {
        throw new RuntimeException('openssl 加密失败: ' . openssl_error_string());
    }
    return base64_encode($iv . $cipher . $tag);      // 拼装顺序不可变: iv‖密文‖tag
}

/** 拼完整客服入口链接 */
function csSsoUrl(string $baseUrl, string $tenantCode, array $userInfo, string $aesKey): string
{
    $user = csSsoEncrypt($userInfo, $aesKey);
    return $baseUrl . '/h5.html?t=' . rawurlencode($tenantCode)
                        . '&user=' . rawurlencode($user);
}

// ---- 用例 ----
echo csSsoUrl(
    'http://192.168.1.12:8090',
    'shopa',
    ['externalId' => 'U10001', 'nickName' => '张三', 'phone' => '13800001234'],
    'gva-cs-sso-dev-key-2026'
), PHP_EOL;
```

## 解密函数（商户侧自测联调用）

```php
<?php
function csSsoDecrypt(string $b64, string $aesKey): array
{
    $raw = base64_decode($b64, true);
    if ($raw === false || strlen($raw) < 28) {
        throw new RuntimeException('非合法Base64或密文过短');
    }
    $key  = hash('sha256', $aesKey, true);
    $iv   = substr($raw, 0, 12);
    $tag  = substr($raw, -16);                       // 末尾 16 字节是 GCM 标签
    $body = substr($raw, 12, -16);
    $plain = openssl_decrypt($body, 'aes-256-gcm', $key, OPENSSL_RAW_DATA, $iv, $tag);
    if ($plain === false) {
        throw new RuntimeException('解密失败(密钥不符或密文被篡改)');
    }
    return json_decode($plain, true);
}
```

## 联调自检

1. **本地闭环**：用上面的加密函数产出密文，再用解密函数还原，能还原出原 JSON 即算法组合正确；
2. **接口连通**（可直接 curl 服务端到端验证）：

```bash
curl -s -X POST 'http://127.0.0.1:8888/member/sso-login' \
  -H 'Content-Type: application/json' \
  -d '{"tenantCode":"shopa","user":"<PHP生成的密文Base64>"}'
# 期望: {"code":0,"data":{"token":"...","member":{...,"source":"SSO"},"tenant":{...}}}
```

3. **浏览器体验**：打开第 2 步拼出的完整链接，应直接看到聊天界面并收到商户欢迎语（首次进入建会话时机器人自动发送）。

## 账号映射规则（服务端行为，商户侧知悉即可）

| 场景 | 结果 |
|---|---|
| 同租户、同标识再次进入 | **复用**原账号，刷新昵称/手机号，直接登录 |
| 标识与**其他商户**账号重名 | 自动落位为 `<商户码>-<标识>`（如 `shopa-member001`），不会误连他人账号 |
| 账号被商户后台停用 | 拒绝登录 |
| 新访客 | 自动建档：`source=SSO`、随机密码（不能也不会走密码登录，后台可重置） |

## 常见错误对照

| 现象 | 原因 |
|---|---|
| `解密失败, 密文无效或密钥不匹配` | 密钥不一致；`hash('sha256',...,true)` 漏了 `true`；拼装顺序不是 iv‖密文‖tag |
| `密文格式非法` | Base64 被 URL 截断/转义（未 urlencode，`+` 变空格）或非标准 Base64 |
| `SSO 登录未启用` | 服务端未注入 `GVA_CS_SSO_KEY` 环境变量 |
| `SSO 链接已过期` | 明文 `ts` 超时效，商户侧重新生成链接 |
| `缺少商户渠道标识` / 渠道不存在 | URL 未带 `t=`，或商户码错误/租户已停用 |
