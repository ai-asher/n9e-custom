# 数据模型与配置 Schema 设计

> Task #3 产出。本文档定义降噪模块所需的全部数据库表、字段、索引、关系，以及配套的 API/缓存策略。

## 一、整体设计原则

| 原则 | 落地方式 |
|---|---|
| **不动原生表** | 关联通过 hash 字符串引用 `alert_cur_event`，不加外键 |
| **遵循 N9e 风格** | 沿用 GORM tag、`ormx.JSONArr`、`group_id/disabled/create_by` 命名 |
| **独立迁移入口** | `internal/custom/models/MigrateCustomTables(db)` 单次调用即可 |
| **多 DB 兼容** | 字段类型统一用 GORM 自动方言映射（MySQL / PostgreSQL / SQLite） |
| **可降级关闭** | 每张配置表都有 `disabled` 字段；功能整体禁用通过停用所有规则实现 |

## 二、6 张表总览

| 表名 | 类别 | 行数级别 | 说明 |
|---|---|---|---|
| `custom_mute_cron` | 配置 | 数十～数百 | cron 表达式屏蔽规则 |
| `custom_emergency_mute` | 配置（单例） | 1 | 全局应急屏蔽开关 |
| `custom_inhibit_rule` | 配置 | 数十～数百 | 跨规则抑制规则 |
| `custom_aggregate_rule` | 配置 | 数十～数百 | 聚合规则 |
| `custom_incident` | 数据 | 千～百万 | 聚合产生的 Incident |
| `custom_incident_event` | 数据 | 万～千万 | Incident 与原生事件的关联 |

## 三、表详细设计

### 3.1 `custom_mute_cron` — Cron 屏蔽

| 字段 | 类型 | 索引 | 说明 |
|---|---|---|---|
| `id` | int64 PK | — | 主键 |
| `group_id` | int64 | idx | 业务组，0 = 全局 |
| `note` | varchar(1024) | — | 备注 |
| `cron_expr` | varchar(128) | — | 标准 5 段 cron 表达式 |
| `duration_sec` | int64 | — | 每次触发后屏蔽多久（秒），默认 3600 |
| `timezone` | varchar(64) | — | IANA 时区，空 = 服务器本地 |
| `datasource_ids` | varchar(1024) | — | 空 = 全部数据源 |
| `severities` | varchar(64) | — | 空 = 全部严重度 |
| `tags` | text (JSON) | — | `[]TagFilter`，复用原生类型 |
| `disabled` | tinyint | — | 0 启用 / 1 禁用 |
| `create_by`/`update_by` | varchar(64) | — | 审计 |
| `create_at`/`update_at` | int64 | — | 审计时间戳 |

**典型用法**：
- `cron="0 22 * * 1-5"` + `duration=3600` → 工作日 22:00–23:00 屏蔽
- `cron="*/30 9-18 * * *"` → 每 30 分钟触发，常见于业务高峰期"减噪"

### 3.2 `custom_emergency_mute` — 全局应急屏蔽

**单例表**：约定只有 `id=1` 一行，所有写操作 upsert 该行。

| 字段 | 类型 | 索引 | 说明 |
|---|---|---|---|
| `id` | int64 PK | — | 固定 1 |
| `enabled` | tinyint | — | 0 关闭 / 1 开启（开启即屏蔽全部告警） |
| `reason` | varchar(512) | — | 开启原因（必填，审计用） |
| `expire_at` | int64 | — | 自动失效时间戳，0 = 永不（防止误开） |
| `datasource_ids` | varchar(1024) | — | 可选范围限定，空 = 全部 |
| `group_ids` | varchar(1024) | — | 可选业务组限定，空 = 全部 |
| `create_by`/`update_by` | varchar(64) | — | 审计 |
| `create_at`/`update_at` | int64 | — | 审计 |

**安全设计**：UI 开启时强制弹窗输入 reason + 必须勾选 expire_at；`expire_at=0` 需要二次确认。

### 3.3 `custom_inhibit_rule` — 跨规则抑制

| 字段 | 类型 | 索引 | 说明 |
|---|---|---|---|
| `id` | int64 PK | — | 主键 |
| `group_id` | int64 | idx | 业务组 |
| `name` | varchar(255) | — | 规则名 |
| `note` | varchar(1024) | — | 备注 |
| `source_match` | text (JSON) | — | `[]TagFilter`，命中即视为根因 |
| `target_match` | text (JSON) | — | `[]TagFilter`，命中即被抑制候选 |
| `equal_labels` | varchar(1024) | — | `[]string`，必须等值的标签键 |
| `datasource_ids` | varchar(1024) | — | 数据源范围 |
| `disabled` | tinyint | — | 0/1 |
| `create_*`/`update_*` | — | — | 审计 |

**判定算法**（伪代码）：
```
对每条新事件 event：
  for rule in 启用的 inhibit rules:
    if 不匹配 rule.target_match: continue
    for src_event in 当前活跃的根因告警缓存:
      if 不匹配 rule.source_match: continue
      if all(event.tags[k] == src_event.tags[k] for k in rule.equal_labels):
        return 抑制
  return 不抑制
```

**典型用法**：
- 主机宕机 → 抑制该主机所有服务告警：
  - `source_match: [{key:"alertname", op:"==", value:"HostDown"}]`
  - `target_match: [{key:"category", op:"==", value:"service"}]`
  - `equal_labels: ["host"]`
- 高级别压低级别（同对象）：
  - `source_match: [{key:"severity", op:"==", value:1}]`
  - `target_match: [{key:"severity", op:"==", value:2}]`
  - `equal_labels: ["service","instance"]`

### 3.4 `custom_aggregate_rule` — 聚合规则

| 字段 | 类型 | 索引 | 说明 |
|---|---|---|---|
| `id` | int64 PK | — | 主键 |
| `group_id` | int64 | idx | 业务组 |
| `name` | varchar(255) | — | 规则名 |
| `note` | varchar(1024) | — | 备注 |
| `dimensions` | text (JSON) | — | `[]string`，聚合维度（标签键列表） |
| `window_sec` | int64 | — | 窗口长度，默认 300 |
| `filters` | text (JSON) | — | `[]TagFilter`，事件准入过滤 |
| `datasource_ids` | varchar(1024) | — | 范围 |
| `severities` | varchar(64) | — | 范围 |
| `storm_threshold` | int | — | 风暴阈值，0 = 关闭 |
| `storm_window_sec` | int64 | — | 风暴检测窗口 |
| `disabled` | tinyint | — | 0/1 |
| `priority` | int | — | 多规则命中时优先级高的胜出 |
| `create_*`/`update_*` | — | — | 审计 |

**关键决策**：当一个事件命中多个聚合规则时，按 `priority desc, id asc` 取第一个生效。

### 3.5 `custom_incident` — Incident 实体

| 字段 | 类型 | 索引 | 说明 |
|---|---|---|---|
| `id` | int64 PK | — | 主键 |
| `rule_id` | int64 | idx | FK → custom_aggregate_rule.id |
| `incident_key` | varchar(512) | idx | 聚合键，例 `service=order\|cluster=prod` |
| `group_id` | int64 | idx | 业务组（用于查询） |
| `datasource_id` | int64 | idx | 数据源（用于查询） |
| `group_name` | varchar(128) | — | 业务组名快照 |
| `severity` | int | — | 严重度（首条事件继承 / 可升级） |
| `title` | varchar(512) | — | 标题 |
| `summary` | text | — | 摘要 |
| `dimension_values` | text (JSON) | — | 各维度的值，UI 展示用 |
| `status` | tinyint | idx | 0 open / 1 resolved / 2 closed |
| `event_count` | int64 | — | 关联事件数 |
| `first_event_at` | int64 | idx | 首事件时间 |
| `last_event_at` | int64 | idx | 末事件时间 |
| `resolved_at` | int64 | — | 解决时间 |
| `closed_at` | int64 | — | 手动关闭时间 |
| `storm_fired` | tinyint | — | 是否触发过风暴通知 |

**聚合键查询**（最热路径）：
```sql
SELECT id FROM custom_incident
WHERE rule_id=? AND incident_key=? AND status=0
  AND last_event_at >= ? -- now - window_sec
LIMIT 1
```
对应复合索引建议：`(rule_id, incident_key, status, last_event_at)` —— 作为后续优化项，AutoMigrate 先建单列索引。

### 3.6 `custom_incident_event` — 关联表

| 字段 | 类型 | 索引 | 说明 |
|---|---|---|---|
| `id` | int64 PK | — | 主键 |
| `incident_id` | int64 | idx | FK → custom_incident.id |
| `event_hash` | varchar(64) | idx | AlertCurEvent.Hash（不存 id，因为生命周期会迁移） |
| `event_id` | int64 | — | 合并时刻的事件 id 快照 |
| `is_recovered` | tinyint | — | 该事件是否已恢复（用于触发 incident 自动 resolve） |
| `merged_at` | int64 | idx | 合并时间 |

**为什么用 hash 而不是 event_id 做主关联**：原生事件会从 `alert_cur_event` 迁到 `alert_his_event`，id 不一致，但 hash 稳定。

## 四、缓存策略

参考 N9e 原生 `memsto` 的做法，每张配置表对应一个内存缓存：

| 缓存对象 | 刷新频率 | 用途 |
|---|---|---|
| `CustomMuteCronCache` | 5s 全量 | EventMuteHook 命中判断 |
| `CustomEmergencyMuteCache` | 1s 全量（单行） | 应急屏蔽开关需快速生效 |
| `CustomInhibitRuleCache` | 5s 全量 | 抑制判定 |
| `CustomAggregateRuleCache` | 5s 全量 | 聚合 processor 决策 |
| `ActiveIncidentCache` | 实时（写穿） | 聚合时按 incident_key 查活跃 incident |

**应急屏蔽**刷新最快（1s），因为开启时希望"立即生效"，避免污染告警通道。

## 五、API 设计（HTTP Schema 草案）

REST API 独立挂载在 `/api/v1/custom/` 命名空间下，避免与原生 API 冲突。

### Mute Cron

```
GET    /api/v1/custom/mute-cron               list
GET    /api/v1/custom/mute-cron/:id           get
POST   /api/v1/custom/mute-cron               create
PUT    /api/v1/custom/mute-cron/:id           update
DELETE /api/v1/custom/mute-cron/:id           delete
PUT    /api/v1/custom/mute-cron/:id/toggle    enable/disable
```

### Emergency Mute

```
GET    /api/v1/custom/emergency-mute          status
PUT    /api/v1/custom/emergency-mute          set { enabled, reason, expire_at, ... }
```

### Inhibit Rules / Aggregate Rules

```
统一沿用 mute-cron 的 5 端点 CRUD 形态。
```

### Incidents（只读 + 操作）

```
GET    /api/v1/custom/incidents               list (with filters)
GET    /api/v1/custom/incidents/:id           detail (含关联事件列表)
PUT    /api/v1/custom/incidents/:id/close     manual close
PUT    /api/v1/custom/incidents/:id/resolve   manual resolve
```

## 六、风险点与待决策项

| # | 议题 | 倾向方案 | 待你决策 |
|---|---|---|---|
| 1 | 复合索引何时引入 | AutoMigrate 不支持复合索引精细控制；先用单列索引上线，性能问题再补 SQL 迁移文件 | 同意？ |
| 2 | 严重度升级策略 | 默认 incident.severity 跟随首事件；高严重度事件加入时**升级**到更高 severity | 同意？ |
| 3 | Incident 自动恢复 | 所有关联事件 `is_recovered=1` 时自动 resolve | 同意？ |
| 4 | 历史 Incident 清理 | 按 `last_event_at` 保留 90 天，cron 任务异步清理 | 同意？ |
| 5 | 多租户隔离 | 通过 `group_id` 隔离（沿用 N9e 业务组语义） | 同意？ |

## 七、Task #3 验收清单

- ✅ 6 张表 GORM model 全部完成
- ✅ 字段类型与 N9e 原生约定一致（ormx.JSONArr / 标准审计字段）
- ✅ 独立 `MigrateCustomTables` 入口，不动 `models/migrate/migrate.go`
- ✅ 命名一致（`custom_` 前缀、`disabled` 0/1、`create_at`/`update_at` int64）
- ✅ 编译通过（见后续 commit）
- ✅ schema 文档（本文件）覆盖表/索引/缓存/API/风险

下一步：Task #4 — 实现告警聚合 Processor。
