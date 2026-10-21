# 20261021 阶梯条件时间函数与嵌套条件组更新

> 本版本**已包含 20261020 的全部修复**（排行榜管理打不开、使用日志 Token 列换行、用户信息弹窗缺 ID），
> 切换后无需再单独部署 20261020 版本。

## 版本身份与源码来源

- 发布标识：`20261021-v1.0.0-rc.36-tier-time-conditions`。
- 官方基准：`QuantumNous/new-api` 的 `v1.0.0-rc.36`。
- 累积基线（当前生产）：`20261020-ranking-sql-token-cell`，提交 `2ddae513b6133420e7274ca57641ba9cc97e277c`。
  - 该提交是本次发布的**直接祖先**（`git merge-base --is-ancestor 2ddae513b HEAD` 成立），本版本在其之上追加 4 个提交。
- 本次功能源码提交：
  - `6a7103910` feat(billing): allow clock fields in tier conditions
  - `8a3ba5272` feat(billing): nest condition groups inside a tier
  - `75fde8536` fix(pricing): base price is the off-peak fallback tier; drop condition dump from tier table
- 分支：`update/20261021-v1.0.0-rc.36-tier-time-conditions`。`main` 不移动。
- 二进制只随 Release 附件发布，不进入 Git 对象树。

## 本次实际变化

### 零、一并包含的 20261020 修复（`2ddae513b`）

20261020 版本当时只推送了源码分支、编译并部署到生产，未打 tag、未建 Release。本版本已完整继承其全部改动，逐项如下：

| 问题 | 根因 | 修法 | 影响文件 |
|---|---|---|---|
| Token 排行榜管理页打不开，列表与历史都报 500 | `rank` 是 MySQL 8 保留字，`ORDER BY created_at desc, rank asc` 触发 `error 1064` 语法错误 | 给列名加反引号：``ORDER BY created_at desc, `rank` asc`` | `controller/weekly_ranking_admin.go` |
| 使用日志 Token 列被挤成三行，读起来错位 | 缓存指标那行用了 `flex-wrap`，列宽只有默认值 | 缓存行改 `flex-nowrap` 并加 `whitespace-nowrap`，Token 单元格同样不换行，列基准宽度设为 210 | `web/src/features/usage-logs/components/columns/common-logs-columns.tsx` |
| 用户信息弹窗看不出是哪个人 | 标题只有「用户信息」 | 标题右侧追加等宽字体的 `(ID: 123)` | `web/src/features/usage-logs/components/dialogs/user-info-dialog.tsx` |

以上三处修复在本版本二进制中已逐项核验存在（反引号 SQL 特征串、`flex flex-nowrap ... whitespace-nowrap` 组合、`inline-flex items-baseline gap-1.5` 标题容器），且本版本未对这三个文件做任何后续改动（`git diff 2ddae513b..HEAD` 对三者输出为空）。

### 一、阶梯条件支持时钟字段（新功能）

此前阶梯条件只能写 `p` / `c` / `len` 三个 token 变量，无法按时间分档，峰谷定价只能手写整段表达式。现在条件变量扩到 8 个：

| 变量 | 含义 | 取值范围 |
|---|---|---|
| `hour` | 小时（24 小时制） | 0–23 |
| `minute` | 分钟 | 0–59 |
| `weekday` | 星期（1 = 周一） | 1–7 |
| `month` | 月份 | 1–12 |
| `day` | 日 | 1–31 |

- 时间条件必须带时区，默认 `Asia/Shanghai`，界面上是一个独立输入框。
- 生成表达式形如 `hour("Asia/Shanghai") >= 9`。
- 后端零改动：`pkg/billingexpr/run.go` 的求值环境、`compile.go` 的编译桩早已提供这五个函数，`expr-lang` 原生支持 `&&`、`||` 与括号。本次只是把前端可视化编辑器打通到已有能力。
- 阶梯条件上限由 2 条提升到 10 条。峰谷定价本身需要 6 条（星期两条 + 两个时段各两条），原来的 2 条上限连一个时段都放不下。

### 二、阶梯条件支持嵌套条件组（新功能）

此前条件只能全部 AND。一个「峰时」档要同时覆盖 09:00–12:00 和 14:00–18:00 时，只能拆成两个价格完全相同的重复档位。

现在条件是一棵树：`all`（全部满足）与 `any`（任一满足）可以任意嵌套，编辑器渲染成带下拉的嵌套块。

- 相邻的 `>=` 与 `<` 在界面上折叠成一行「小时 从 9 至 12」，底层仍存两条比较，往返序列化逐字节一致。
- 新增「合并为「或」」按钮：选中一个范围，点一下就和下一个范围组成「任一满足」组。
- 解析侧改为手写递归下降解析器，认 `&&`、`||`、括号，不再用正则扫描条件片段。原正则无法处理嵌套括号，这是本期必须重写的原因。
- 序列化时 `any` 组强制加括号，否则外层 `&&` 会吞掉它。

**已知坑与规避**：把 `hour >= 9` 与 `hour < 12` 直接放进同一个「任一满足」组会得到 `hour >= 9 || hour < 12`，恒真，等于该档位对所有请求生效。界面的「合并为「或」」按钮会把每一侧先包成「全部满足」子组再合并，因此不会产生该形状；测试里专门有一条用例锁住这个恒真式，防止回退。

### 三、模型广场显示峰谷价格（展示改进）

- 价格单元格与模型卡片改为显示全部档位的价格区间，例如 `输入 ¥0.1 – ¥0.2 / 1M`，并标注档位名 `峰时 / 闲时`。改动前只显示第一档（通常是峰时）的价格，会让用户以为一直按高价收费。
- 区间计算从「仅 task 类计价」扩展到所有分档表达式。
- 模型详情页「基础价格」改为取**无条件兜底档**，即闲时价。改动前取第一档，峰谷模型会把峰时价当成基础价展示。
- 分档价格表里，已经有名字的档位不再打印整段条件表达式（`星期 ≥ 1 (Asia/Shanghai) && 星期 ≤ 5 ...`），只显示档位名。没有名字的自动档位仍显示条件，否则读者无法区分。

### 四、跟随改动的内部调整

- 上游价格同步预览的条件渲染支持树结构（`∧` / `∨`）。
- 使用日志的上下文阈值提示改为先展平条件树再读 `len` 阈值。
- 简体/繁体中文各补 21 条界面文案（时区、周一至周日、任一/全部条件满足、添加条件组、合并为「或」、删除条件组、从/至、两条上限提示、条件值校验），并修掉基线中 `This month`、`This week` 的重复 key（重复导致一条翻译被覆盖失效）。
- 新增前端用例 19 条，覆盖嵌套 OR 组解析、序列化往返一致、`any` 组必带括号、裸 any 组的恒真式防回归、本地求值、基础价取闲时档。
- 新增 Go 用例 1 条，验证峰谷表达式可编译、可命中档位、缓存读取按自身系数计价。

## 未改动范围

- 后端 `pkg/billingexpr/` 与 `setting/billing_setting/` 的应用代码零改动，只新增测试文件。
- 按次计费、按量计费、请求规则倍率、分组倍率、task 插件计价路径未改动。
- 数据库结构、配置格式、凭证格式未改动，无需迁移。
- `main` 分支与历史 tag 未移动。

## 构建与生产状态

- 构建在 M5 本地完成：Go `1.25.1`、Linux amd64、CGO 启用、`-trimpath -ldflags "-s -w"`。
- 生产机不执行任何编译。
- 生产新 release 目录：`/opt/new-api/releases/20261021-tier-time-conditions/`，SHA256 `2d3e839e77df801b9783ec64bcfda7c3b0e5b307e47d520743b080ab67a9f4b1`，130,248,996 字节，stripped。
- 该目录 `manifest.json` 中 `activated: false`，`ExecStart` 仍指向 `20261020-ranking-sql-token-cell`，**切换需另行确认**。
- 测试服：`https://cs.youyangai.top`，内嵌构建标识 `20261021-tier-groups-base`。
- 切换前生产现役：`20261020-ranking-sql-token-cell`，SHA256 `18c9dbb58e14db0fc5cecab1fa9fb46aff61cfdace9344350a3087e9e3562fce`，130,371,876 字节。

## 验证边界与回退

- 前端 `typecheck` 仍有 2 条错误，位于 `web/src/features/usage-logs/components/usage-logs-table.tsx` 第 87、191 行。已在基线提交 `2ddae513b` 的独立工作树上复现同样两条，属既有问题，与本次改动无关。
- 前端 19 条新用例全部通过；`bun run build` 通过；`go test ./pkg/billingexpr/ ./setting/billing_setting/` 通过。
- 测试服浏览器实测：扁平六条条件点「合并为「或」」后得到 `((hour >= 9 && hour < 12) || (hour >= 14 && hour < 18))`，未出现恒真式；模型广场显示 `¥0.1 – ¥0.2` 与 `峰时 / 闲时`；详情页基础价格为闲时价；分档价格表不再显示条件表达式。
- 20261020 修复的生产侧核验：`/api/weekly-user-ranking/admin/` 与 `/admin/history` 返回 401（未授权，非 500）；生产库直接执行带反引号的 `ORDER BY` 正常返回，不带反引号复现 `error 1064`；上线后日志中 `Error 1064` 计数为 0。
- 回退：`ExecStart` 指回 `20261020-ranking-sql-token-cell` 并 `daemon-reload` + `restart`。无数据库变更，回退不涉及数据。回退**不会丢失**排行榜、日志列、用户 ID 三项修复，因为它们已在 20261020 二进制内。
- 峰谷定价本身（给具体模型配置表达式）不在本次发布内，需单独配置并单独验收。
- 本仓库不包含生产数据库、账号、密码、Token、Cookie、日志正文、真实 systemd 凭据或代理配置。
