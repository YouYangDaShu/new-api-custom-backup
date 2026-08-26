# New API 邀请充值返利改造笔记

日期：2026-07-14  
状态：**仅改源码，尚未构建镜像、未重启线上**  
源码目录：`~/projects/sources/new-api-rc19`  
线上容器仍是旧镜像：`new-api-local:gpt56-completion-fix`

---

## 目标规则

1. **邀请注册：不返利**（`QuotaForInviter/Invitee` 保持 0）
2. **被邀请人充值成功：给邀请人返 5%**
3. 充值来源：
   - 兑换码兑余额（当前主力）
   - 以后在线支付充值（Stripe / Creem / Waffo / Waffo Pancake / 管理员补单）
4. 返利入账方式（对齐前端「待使用收益 / 划转」）：
   - 先进入邀请人 **`aff_quota`（待使用收益）**
   - 累计 **`aff_history`（总收益）**
   - 用户再点「转到余额」→ `TransferAffQuotaToQuota` 把待使用收益划入 `quota`
   - **只加算，不覆盖**原有余额/待使用收益

---

## 后端实现

### 1. 配置 `AffTopupRate`

- 文件：`common/constants.go`
- 默认：`0.05`（5%）
- `model/option.go` 接入 OptionMap，范围 0~1
- `/api/status` 增加字段：`aff_topup_rate`（前端展示奖励比例）

### 2. `RewardInviterOnTopup(inviteeId, topupQuota)`

- 文件：`model/user.go`
- 逻辑：
  1. 查被邀请人 `inviter_id`
  2. 无邀请人 / 比例≤0 / 额度≤0 → 跳过
  3. `reward = floor(topupQuota * 0.05)`，且 `0 < reward ≤ topupQuota`
  4. SQL **加算**：
     - `aff_quota = aff_quota + reward`
     - `aff_history = aff_history + reward`
  5. 写系统日志：`邀请充值返利 ...（待使用）`

**安全点：**

```go
Updates(map[string]interface{}{
  "aff_quota":   gorm.Expr("aff_quota + ?", reward),
  "aff_history": gorm.Expr("aff_history + ?", reward),
})
```

不是 `aff_quota = reward`，不会覆盖。

### 3. 邀请人数统计

- 原逻辑：只有 `QuotaForInviter > 0` 时才 `aff_count++`
- 现补丁：注册奖励为 0 时仍调用 `BumpInviterAffCount`
- 密码注册 / OAuth 注册两条路径都已补

### 4. 挂接点

| 场景 | 文件 | 函数 |
|---|---|---|
| 兑换码 | `model/redemption.go` | `Redeem` |
| Stripe | `model/topup.go` | `Recharge` |
| 管理员补单 | `model/topup.go` | `ManualCompleteTopUp` |
| Creem | `model/topup.go` | `RechargeCreem` |
| Waffo | `model/topup.go` | `RechargeWaffo` |
| Waffo Pancake | `model/topup.go` | `RechargeWaffoPancake` |

注册流程 **不发充值返利**。

### 5. 金额例子

- 被邀请人兑码 ¥10 → 邀请人待使用收益 **+¥0.5**
- 总收益 **+¥0.5**
- 邀请人再点划转 → 账户余额 **+¥0.5**（原余额只加不覆盖）

---

## 前端实现（按你给的样式）

文件：`web/default/src/features/wallet/components/affiliate-rewards-card.tsx`

展示结构：

```text
邀请奖励
邀请好友获得额外奖励

收益统计
- 奖励比例：5%（读 status.aff_topup_rate，默认 0.05）
- 待使用收益：aff_quota
- 总收益：aff_history_quota
- 邀请人数：aff_count

邀请链接 + 复制 +（有待使用收益时）转到余额

奖励说明
1. 邀请好友注册，好友充值或使用兑换码后您可获得相应奖励
2. 通过划转功能将奖励额度转入到您的账户余额中
3. 邀请的好友越多，获得的奖励越多
```

其它前端改动：

- `wallet/index.tsx`：传入 `rewardRate`
- `i18n/locales/zh.json`：上述中文文案

数值（¥0.00 / ¥1.00 / 邀请 3）来自用户真实数据，不写死。

---

## 改了哪些文件

### 后端
1. `common/constants.go`
2. `model/option.go`
3. `model/user.go`
4. `model/redemption.go`
5. `model/topup.go`
6. `controller/misc.go`

### 前端
1. `web/default/src/features/wallet/components/affiliate-rewards-card.tsx`
2. `web/default/src/features/wallet/index.tsx`
3. `web/default/src/i18n/locales/zh.json`

---

## 还没做（按你要求）

- ❌ 未 `docker build`
- ❌ 未换线上镜像 / 未重启
- ❌ 未实机兑码验收

**线上现在仍是旧逻辑 + 旧前端。**

---

## 上线时再做

1. 构建含前后端的新镜像  
2. 重建 `new-api-test`，数据卷不变  
3. 确认 `AffTopupRate=0.05`  
4. 验收：
   - 有邀请关系用户兑码 → 邀请人待使用收益 +5%
   - 划转后余额只加不覆盖
   - 注册本身不返利，但邀请人数会 +1

---

## 回滚

改造前备份：

- 桌面：`Desktop/NewAPI备份-改邀请返利前-20260714_002108/`
- WSL：`~/projects/services/new-api-test/backups/pre-aff-topup-rebate-20260714_002108/`
- 镜像标签：`new-api-local:pre-aff-topup-rebate-20260714_002108`

详见备份目录 `RESTORE.md`。
