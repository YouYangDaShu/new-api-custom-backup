# 20261028 模型广场基础价修正

## 版本身份与源码来源

- 发布标识：`20261028-plaza-clock-prod`。
- 官方基准：`QuantumNous/new-api` 的 `v1.0.0-rc.36`。
- 累积基线（当前生产）：`20261021-tier-time-conditions`，提交 `337f924fe4e24505802d07d989550e4a6bcab082`。
  - 该提交是本次发布的**直接父提交**（`git rev-parse HEAD~2` 与线上 `manifest.json` 的 `commit` 完全一致），本版本在其之上只追加 2 个提交。
- 本次功能源码提交：
  - `da7b835cc` fix(pricing): advertise the tier the clock selects for time-conditional models
  - `82b301d1a` test(pricing): pin the base price of the expressions production stores
- 分支：`fix/20261028-plaza-base-price-on-prod`。`main` 不移动。
- 二进制只随 Release 附件发布，不进入 Git 对象树。

## 为什么改

模型广场的每个模型卡片只显示一个「基础价格」。这个价格取自分档表达式的某一档，取哪一档由前端 `getDynamicPricingSummary` 决定。

线上原来取的是**最后一个没有条件的档**（`fallbackTier`）。分档表达式必然是 `条件1 ? 档A : 条件2 ? 档B : 档C` 这种链式结构，最后一档是 else 分支，天生没有条件——所以这条规则的实际效果就是「**永远显示 else 那一档**」。

对峰谷模型，else 是闲时档，碰巧是想要的，但代价是不管现在几点都显示闲时价：工作日峰时那 4 个小时（9-12、14-18）客户看到 ¥1，实际扣 ¥2。

对长度分档模型，else 是长上下文档，直接就是错的：

```
gpt-6-astra: len < 272000 ? tier("base", ...) : tier("tier_2", ...)
```

结果：**10 个长度分档模型在广场上全部显示成长上下文档的价格，看起来是实际的两倍。**

### 修复前 / 修复后实测对照

用生产库 `option` 表里 `billing_setting.billing_expr` 存的**原始表达式文本**，跑同一份前端代码，逐模型对比：

| 模型 | 修复前显示 | 修复后显示 |
|---|---|---|
| gpt-5.6-sol | long_context ¥10 / ¥45 | **standard ¥5 / ¥30** |
| gpt-5.6-terra | long_context ¥4 / ¥18 | **standard ¥2 / ¥12** |
| gpt-6-astra | tier_2 ¥20 / ¥75 | **base ¥10 / ¥50** |
| gpt-6-sol | tier_2 ¥4 / ¥15 | **base ¥2 / ¥10** |
| gpt-6.1-sol | tier_2 ¥4 / ¥15 | **base ¥2 / ¥10** |
| gpt-6.1-terra | tier_2 ¥2 / ¥7.5 | **base ¥1 / ¥5** |
| grok-4.5 | tier_2 ¥4 / ¥12 | **base ¥2 / ¥6** |
| grok-4.6 | tier_2 ¥4 / ¥12 | **base ¥2 / ¥6** |
| grok-4.7 | tier_2 ¥4 / ¥12 | **base ¥2 / ¥6** |
| minimax-m3 | tier_2 ¥4.2 / ¥16.8 | **base ¥2.1 / ¥8.4** |
| deepseek-v4.1-flash（周三 10:00 峰时） | 闲时 ¥1 / ¥4 | **峰时 ¥2 / ¥8** |

峰谷模型在闲时（工作日 12-14、18 点后、周末全天）仍显示 ¥1 / ¥4，不受影响。

## 改了什么

### 一、基础价改成按时钟选档

`web/src/features/pricing/lib/dynamic-price.ts`

新增 `getCurrentTimePricingTier(tiers, now)`：按表达式顺序逐档判条件，第一个条件成立的档就是此刻实际计费的那一档，与计费引擎对单个请求的判断一致。

关键安全设计：**条件里只要出现 `p` / `c` / `len` 这类请求相关变量，就返回 `null` 放弃按时钟选档**，退回 `tiers[0]`。因为这类条件只有具体请求才能决定真假，任何时钟都判不了；如果强行判，就会把长度分档模型误判成峰谷模型。

所以最终规则是：

| 模型类型 | 取值 |
|---|---|
| 长度分档（`len` 条件） | `tiers[0]`，即短上下文档 |
| 峰谷（`weekday` / `hour` 条件） | 时钟此刻选中的那一档 |
| 无条件单档 | 唯一那一档 |

`DynamicPriceOptions` 新增可选 `now?: Date`（供测试注入固定时刻），`DynamicPricingSummary` 新增 `isTimePricing?: boolean`（标记本次是否由时钟选档）。

### 二、时钟求值工具

`web/src/features/pricing/lib/billing-expr.ts`

新增 4 个导出函数：

- `readClockParts(timezone, now)` — 按指定时区取出 hour / minute / weekday / month / day，带 `Map` 缓存 formatter。
- `evaluateTierConditionAt(node, now)` — 对单个条件节点求值，遇到 `p` / `c` / `len` 返回 `null`。
- `isTimeOnlyCondition(nodes)` — 判断一组条件是否纯时钟条件。
- `TIER_COMPARATORS` — 条件比较器表。

注意 `weekday` 的取值约定与后端一致：**周日 = 0**，周一 = 1。

### 三、回归测试

`web/src/features/pricing/lib/__tests__/tier-time-conditions.test.ts` 追加 28 个用例中的时钟相关部分。

`web/src/features/pricing/lib/__tests__/prod-expression-base-price.test.ts`（新增 72 行）：直接用生产库里存的表达式原文做断言，覆盖

- 峰时窗口内 → 峰时价
- 峰时窗口外 → 闲时价
- 周末中午 → 闲时价（尽管小时数落在峰时窗口）
- 长度分档模型 → 第一档，且 `isTimePricing === false`

这样以后有人改了库里的表达式，测试会立刻发现广场显示变了。

## 影响范围

- **改动全部在 `web/src/features/pricing/`，Go 代码零改动。** `git diff --name-only 337f924fe HEAD | grep -v '^web/'` 输出为空。
- **数据库零改动**，无 migration。
- **36 条计费表达式一行都不用改。** 这是本次设计的关键：不需要像「基础价直接取 `tiers[0]`」那样要求表达式必须把基础档写在最前面，因而不需要重排生产表达式。
- **实收金额不变。** 只改展示层，计费引擎一行未动。客户实际扣的钱和以前完全一样，变的只是广场上标出来的价——从「长上下文档价」变成「基础档价」，也就是从「看起来翻倍」变回「和实收一致」。
- 需要重启进程（前端 embed 在二进制里）。

## 门禁

- `npx vitest run tier-time-conditions.test.ts prod-expression-base-price.test.ts` → **32/32 通过**
- `npx tsgo -b` → 只剩 2 个基线旧错（`usage-logs-table.tsx` 的 `canManageScope` 未使用、`MidjourneyLog` 无 `type`），线上代码同样报这两个，与本次无关
- `npx rsbuild build` → 成功，Total 58679.8 kB / gzip 16931.5 kB

## 部署与回滚

- 二进制已构建并上传至生产 `/opt/new-api/releases/20261028-plaza-clock-prod/`，**未切换**。
  - SHA256 `0c1401370d61150c5259e7aed832d2eb00308610b351bf4175945b42b067cf05`
  - 130248996 bytes
  - 内嵌前端资源 `web/dist/static/js/index.a62427858b.js`，SHA256 `cd3ff0979e3c11b568b0d2110dbe3a492b5eb970feffe9599bb5bd4f37e6b6fa`
- 切换：把 `/etc/systemd/system/new-api.service` 的 `ExecStart` 指向新目录，`systemctl daemon-reload && systemctl restart new-api`。
- 回滚：指回 `/opt/new-api/releases/20261021-tier-time-conditions/new-api`（SHA256 `2d3e839e77df801b9783ec64bcfda7c3b0e5b307e47d520743b080ab67a9f4b1`），同样两步。
