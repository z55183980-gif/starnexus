# 用量统计字段回填

该命令把历史 `logs.other` 中参与用量汇总的字段解析到普通列。解析在应用进程中完成，数据库仅执行按主键分批查询和批量更新，不执行 JSON 运算。

## 推荐上线顺序

1. 使用新代码构建回填命令，并在单个节点执行 `plan`。初始化过程会通过现有 AutoMigrate 添加新列。
2. 旧版本仍在运行时完成第一轮历史回填。
3. 升级所有应用节点，使新日志写入时自动填充统计列。
4. 再执行一次回填，处理升级期间由旧节点写入的尾部数据。
5. 执行 `verify`；只有待回填数量为 0 时才算完成。

不要在多台节点上并行运行回填命令。

```bash
go run ./cmd/log_usage_stats_backfill -mode plan

# 先用小批量验证数据库负载
go run ./cmd/log_usage_stats_backfill -mode backfill -batch-size 500 -pause 500ms -max-rows 10000

# 可从上一轮输出的 last_id 继续；已经完成的行也可安全重复扫描
go run ./cmd/log_usage_stats_backfill -mode backfill -batch-size 1000 -pause 200ms

go run ./cmd/log_usage_stats_backfill -mode verify
```

可使用 `-start-id`、`-end-id` 限定主键范围。命令会输出累计更新数、畸形 JSON 数和最后处理的主键。畸形 JSON 保留原始 `other`，归一化统计值记为零。

回填未完成时，新版汇总接口会返回明确错误，不会对未归一化行按零值统计后返回偏小结果。
