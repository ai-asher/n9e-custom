# N9e v8.5.1 告警链路与 Hook 接入点分析

> Task #2 产出。本文档分析 N9e 8.5.1 告警事件从产生到通知的完整链路，并标定降噪/抑制/屏蔽三类自定义逻辑的最优接入点。

## 一、告警事件主流程

```
┌─────────────────┐
│  规则评估        │  alert/eval/alert_rule.go
│  (Eval Loop)    │  采集数据点 → 触发告警条件
└────────┬────────┘
         │ AnomalyPoint[]
         ▼
┌─────────────────────────────────────────────────────────┐
│  Processor.Handle()                                     │
│  alert/process/process.go:127                           │
│                                                         │
│  for each anomalyPoint:                                 │
│    event = BuildEvent(...)                              │
│                                                         │
│    ┌──────────────────────────────────────────────┐     │
│    │ ① Pipeline 处理（DAG processor）              │     │  ← Hook 点 A
│    │   line 157: HandleEventPipeline(...)         │     │
│    │   - eventdrop/relabel/eventupdate/...        │     │
│    │   - 可返回 nil → drop                         │     │
│    └──────────────────────────────────────────────┘     │
│                                                         │
│    ┌──────────────────────────────────────────────┐     │
│    │ ② 原生 Mute 判断                              │     │  ← Hook 点 B
│    │   line 164: mute.IsMuted(...)                │     │
│    │   - TimeRange / Periodic 屏蔽                 │     │
│    │   - severity / tags 过滤                      │     │
│    └──────────────────────────────────────────────┘     │
│                                                         │
│    ┌──────────────────────────────────────────────┐     │
│    │ ③ EventMuteHook（已有的 hook 机制！）         │     │  ← Hook 点 C ★
│    │   line 176: dispatch.EventMuteHook(event)    │     │
│    │   - 全局变量函数，启动时可注入                 │     │
│    │   - 返回 true 即丢弃                          │     │
│    └──────────────────────────────────────────────┘     │
│                                                         │
│    eventsMap[tagHash] = append(...)                     │
│                                                         │
│  for each group of events:                              │
│    handleEvent(events)  → inhibitEvent → fireEvent      │
│                                                         │
│    ┌──────────────────────────────────────────────┐     │
│    │ ④ 原生 Inhibit                                │     │  ← 同规则同 tagHash 内
│    │   line 460: inhibitEvent(...)                │     │     高 severity 抑制低
│    │   - 仅按 severity 抑制                        │     │     ⚠️ 不支持跨规则
│    └──────────────────────────────────────────────┘     │
└────────────────────────┬────────────────────────────────┘
                         │ event 写入队列
                         ▼
┌─────────────────────────────────────────────────────────┐
│  Consumer.LoopConsume()                                 │
│  alert/dispatch/consume.go:63                           │
│                                                         │
│   ┌─────────────────────────────────────────────┐       │
│   │ persist(event)  → 落库                      │       │
│   └─────────────────────────────────────────────┘       │
│                                                         │
│   ┌─────────────────────────────────────────────┐       │
│   │ HandleEventNotify(event)                    │       │  ← Hook 点 D
│   │  - 路由匹配 NotifyRule                       │       │
│   │  - 再次走 Pipeline（per-NotifyRule）         │       │
│   │  - 发送到通知渠道                             │       │
│   └─────────────────────────────────────────────┘       │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
                ┌────────────────────┐
                │  Sender (各通道)   │
                │  钉钉/飞书/企微/邮件 │
                └────────────────────┘
```

## 二、关键源码位置速查表

| 关注点 | 文件 | 行号 | 作用 |
|---|---|---|---|
| 告警评估循环 | `alert/eval/alert_rule.go` | 全文 | 周期性产生 AnomalyPoint |
| **告警处理主入口** | `alert/process/process.go` | 127 (Handle) | 事件构造 → mute → inhibit → fire |
| Pipeline 调度 | `alert/dispatch/dispatch.go` | 234 (HandleEventPipeline) | 走 DAG processor |
| Pipeline 引擎 | `alert/pipeline/engine/engine.go` | 21 (Execute) | 执行 DAG，节点可改/丢事件 |
| **现成 Hook 点** | `alert/dispatch/consume.go` | 35-37 | `EventMuteHookFunc` 全局变量 |
| 原生 Mute 判断 | `alert/mute/mute.go` | 17 (IsMuted) | TimeRange/Periodic 屏蔽 |
| 原生 Inhibit | `alert/process/process.go` | 460 (inhibitEvent) | 同规则 severity 抑制 |
| 通知主流程 | `alert/dispatch/dispatch.go` | 全文 | NotifyRule 匹配 + 渠道分发 |

## 三、N9e 8.5.1 已具备 vs 我们要补的

### 屏蔽（Mute）

| 能力 | 8.5.1 原生 | 我们要补 |
|---|---|---|
| 一次性时段屏蔽 (TimeRange) | ✅ | — |
| 周期屏蔽 (Periodic, 周N + HH:MM) | ✅ | — |
| 按数据源/严重度/标签过滤 | ✅ | — |
| **Cron 表达式屏蔽** | ❌ | ✅ 新增 MuteTimeType=Cron |
| **全局一键屏蔽** | ❌ | ✅ 新增 emergency-mute 表 |
| **业务日历驱动**（交易日/非交易日） | ❌ | P2 暂缓 |

### 抑制（Inhibit）

| 能力 | 8.5.1 原生 | 我们要补 |
|---|---|---|
| 同规则内按 severity 抑制 | ✅ | — |
| **跨规则抑制**（A 触发抑制 B） | ❌ | ✅ Alertmanager 风格 |
| **标签等价匹配 (equal)** | ❌ | ✅ 同 host/service 才抑制 |
| **根因抑制衍生** | ❌ | ✅ 主机宕机 → 抑制其上服务告警 |

### 聚合（Denoise / Aggregate）

| 能力 | 8.5.1 原生 | 我们要补 |
|---|---|---|
| 按 tagHash 分组（仅同规则） | ⚠️ 局部 | 需扩展 |
| **多规则告警合并 Incident** | ❌ | ✅ 主能力 |
| **聚合窗口** | ❌ | ✅ N 分钟内合并 |
| **多维度合并** (service/cluster/alertname) | ❌ | ✅ 可配置 |
| **告警风暴检测** | ❌ | ✅ 触发收敛通知 |

## 四、Hook 接入策略 ⭐

**核心结论：3 个能力对应 3 个不同的接入点，都不需要侵入主流程**。

### 能力 1：增强屏蔽（Cron / 全局）

**接入点**：`alert/process/process.go:176` 已有的 `dispatch.EventMuteHook(event)`

**实现方式**：在 `cmd/n9e/main.go` 或 alert 服务启动入口（`alert/alert.go`）注入：

```go
import "github.com/ccfos/nightingale/v6/internal/custom/mute"

func init() {
    dispatch.EventMuteHook = mute.CustomEventMuteHook
}
```

`internal/custom/mute/` 实现 `CustomEventMuteHook(event) bool`：
- 查 cron 屏蔽规则缓存（自定义表 `custom_mute_cron`）
- 查全局应急屏蔽开关（自定义表 `custom_emergency_mute`）
- 任一命中返回 true

**对原生代码的改动**：1 行 `dispatch.EventMuteHook = ...`（main.go），无业务逻辑改动。

### 能力 2：跨规则抑制

**接入点**：复用 `EventMuteHook`，链式包装。

**实现方式**：
- 不直接覆盖 `EventMuteHook`，而是在自定义初始化中包装它
- 自定义抑制服务订阅"已 fire 的告警事件"（通过 webhook 或共享缓存），维护"当前活跃的根因告警"列表
- 新事件进来时，匹配 `inhibit_rules`（source_match + target_match + equal labels），命中则抑制

```go
// internal/custom/suppress/hook.go
func WrapEventMuteHook(prev dispatch.EventMuteHookFunc) dispatch.EventMuteHookFunc {
    return func(event *models.AlertCurEvent) bool {
        if prev(event) {
            return true  // 已被屏蔽规则拦截
        }
        return matchInhibitRules(event)  // 自定义抑制
    }
}
```

启动注入：
```go
dispatch.EventMuteHook = suppress.WrapEventMuteHook(mute.CustomEventMuteHook)
```

**对原生代码的改动**：仍是初始化 1 行，无主流程侵入。

### 能力 3：聚合 / Incident

**接入点**：**通知出口**（在 fire 之后、发送之前）

**实现方式**：**做成一个 Pipeline Processor**！

N9e 的 pipeline 框架已有 `aisummary / callback / eventdrop / relabel` 等 processor，新增一个 `aggregate` processor 即可：

```
internal/custom/denoise/processor/aggregate.go

func init() {
    models.RegisterProcessor("alert_aggregate", &AggregateConfig{})
}

func (c *AggregateConfig) Process(ctx *ctx.Context, wfCtx *models.WorkflowContext) (*models.WorkflowContext, string, error) {
    event := wfCtx.Event
    incidentKey := buildIncidentKey(event, c.Dimensions)  // service/cluster/alertname

    // 在窗口内已有 incident → 追加 event，本次不发送
    if existing := findActiveIncident(incidentKey, c.WindowSec); existing != nil {
        appendEventToIncident(existing, event)
        wfCtx.Event = nil  // drop，不再走后续节点（包括 sender）
        return wfCtx, "merged into incident", nil
    }

    // 否则创建新 incident，事件正常发送
    createIncident(incidentKey, event)
    return wfCtx, "new incident", nil
}
```

用户在 N9e 前端 Pipeline 配置页面就能拖出"聚合"节点，挂到 NotifyRule 上。

**对原生代码的改动**：**0 行**！（只要 import 进 main.go 触发 init() 注册即可）

## 五、最终改动清单

| 文件/位置 | 改动 | 行数 |
|---|---|---|
| `cmd/n9e/main.go` 或 `alert/alert.go` | 加 import + init 注入 hook | ~3 行 |
| `internal/custom/mute/cron_mute.go` | 新增（cron 屏蔽实现） | 新建 |
| `internal/custom/mute/global_mute.go` | 新增（全局应急屏蔽） | 新建 |
| `internal/custom/mute/hook.go` | 新增（CustomEventMuteHook 入口） | 新建 |
| `internal/custom/suppress/inhibit.go` | 新增（跨规则抑制） | 新建 |
| `internal/custom/suppress/hook.go` | 新增（包装 EventMuteHook） | 新建 |
| `internal/custom/denoise/processor/aggregate.go` | 新增（聚合 processor） | 新建 |
| `internal/custom/denoise/incident.go` | 新增（incident 模型 + 服务） | 新建 |
| `models/custom_*.go` | 新增（自定义表 model） | 新建 |
| **N9e 原生文件改动** | **仅 1 处 import + 3 行注入** | **3 行** |

**升级 v8.6.x 时的预期冲突量：3 行以内**。

## 六、风险与决策点

### 风险 1：EventMuteHook 是单一全局变量

如果未来上游用这个 hook 做了别的事（看起来不太可能，但要监控），我们的注入会冲突。
**应对**：用包装而非直接赋值（如 `WrapEventMuteHook`），保留原行为。

### 风险 2：Pipeline processor 注册依赖 init()

processor 通过 `init()` 注册到全局 map。要确保自定义 processor 包被 import 触发。
**应对**：在 `alert/pipeline/pipeline.go` 上游文件里加一行 import `_ "internal/custom/denoise/processor"`，这是上游已经在用的模式（看其原本就有 6 个 `_ "..."` import），跟得很自然，升级冲突小。

### 风险 3：聚合后的 Incident 持久化

需要新表存储 incident 状态、关联事件。
**应对**：建独立表（不动 `alert_cur_event`），通过 `event.Hash` 关联。

## 七、Task #2 结论

✅ **N9e 8.5.1 提供了非常友好的扩展点**——有现成的 `EventMuteHook` 全局变量 + Pipeline processor 框架，几乎不需要修改原生代码。

✅ **三大能力的接入点已确定**：
- 屏蔽 → `EventMuteHook`
- 抑制 → `EventMuteHook` 链式包装
- 聚合 → 新增 Pipeline processor

✅ **对原生代码的总改动量预计 ≤ 3 行**，完美符合二开升级铁律。

下一步：进入 Task #3，设计三类配置的数据库表结构 + API schema。
