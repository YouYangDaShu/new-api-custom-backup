# New API 每日日志备份

- 时区：Asia/Shanghai。
- 香港机每日 00:00 归档昨天 00:00 至今天 00:00 的使用日志，含表结构和数据。
- M5 本地每小时 15 分检查并拉取（第一次为 00:15）；离线后会补跑。
- 本地目录：/home/youyang/桌面/Hermes备份工具/备份文件/new-api-daily-logs/
- 本地逐文件核对 SHA256、字节数及 gzip 完整性；校验成功才允许清理对应的数据库旧日志。
- 数据库按北京时间自然日保留最近 15 天：删除早于今天零点减15天的记录。例如 10月17日运行，只删除10月2日零点之前的记录，保留10月2日起的数据。
- 拉取失败、校验失败、缺少某个待删日期完整备份，均不执行数据库清理。
- 每次清理最多1000行，批次之间暂停0.3秒；只涉及 one_api.logs，不涉及用户、余额、周榜奖励表或其他表。
- 服务器和 M5 的备份文件均永久保留，不自动按天数删除，也不使用 rsync --delete。
- 首次运行补备份当前 logs 中已有的历史日期；以前已经清掉的日志不会自动恢复。换表前全量归档仍单独保留。
- 如果已归档日期后来仍有请求结算更新，会生成带时间戳的新版，旧版也保留。

## 调度及日志

服务器：logs-daily-backup.timer → /opt/new-api-backups/logs-daily-backup.sh → logs-backup-worker.py prepare。
M5：new-api-logs-pull.timer → new-api-logs-pull.service → /home/youyang/桌面/Hermes备份工具/new-api-logs-pull.py。
服务器清库只由 M5 拉取校验后调用 worker cleanup，不由服务器备份定时器直接触发。
M5 执行日志：/home/youyang/桌面/Hermes备份工具/new-api-logs-pull.log。
本地 latest-result.json 记录最近成功结果；latest-verified.json 记录最近文件校验。
服务器 local-verified/ 记录已确认的本地副本校验回执。

## 安全恢复

不要把 SQL 直接导入生产库。备份包含 CREATE TABLE logs；应先在单独的空恢复库/测试环境导入，检查日期、行数与主键，确认后才由人工制定回填方案。

## 回滚

本次旧文件保存在 /opt/new-api-backups/config-rollback/ 下对应时间目录。
如需停止任务，应先停用本地 new-api-logs-pull.timer，再停服务器 logs-daily-backup.timer；不要直接恢复带7天删除的旧脚本。
本次备份机制部署没有重启 New API，也没有修改周榜代码；应用后续独立切换记录见仓库发行说明。

## 仓库归档边界

本目录是已部署机制的源码归档。`HOST`、目录与 service 路径是当前安装布局示例，换机器时需先核对。只归档源码和调度模板，不包含数据库、原始归档、真实密钥、SSH 凭据、旧含密码的脚本或执行日志。

本地 `verify()` 实际验证大小、SHA256 和 gzip 完整性，没有检查 SQL 末尾 `Dump completed`；此前人工验收做过末尾检查，不能把人工检查写成脚本自动能力。清理回执的日期和每日统计匹配用于阻止缺档误删，不能替代业务记录逐字段校验。生产脚本保持现状，本次仅归档，不扩展行为。

安全测试：在此目录运行 `python3 -m unittest -v test_backup_safety.py`，不连接生产、不执行真实数据库清理。
