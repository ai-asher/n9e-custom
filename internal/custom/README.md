# internal/custom — 二次开发模块

本目录存放所有针对开源 N9e 的**自定义扩展代码**，是公司内部二开的唯一允许目录。

## 设计目标

1. **隔离性**：所有定制代码集中于此，便于跟踪、审计、迁移
2. **可升级性**：跟随上游 N9e 升级时，冲突范围可控，主代码冲突 < 10 行
3. **可观测性**：明确每个子模块的职责边界

## 目录结构

```
internal/custom/
├── common/      公共工具、类型定义、配置加载（被其它子模块依赖）
├── denoise/     P0 - 告警聚合（多维度合并 Incident）
├── suppress/    P0 - 告警抑制（根因抑制衍生告警）
└── mute/        P1 - 增强屏蔽（cron / 时段 / 条件 / 全局）
```

## 二开铁律

### ✅ 允许

- 在 `internal/custom/` 下任意新增、修改、删除文件
- 在 N9e 原生代码中**调用** `internal/custom/` 里的函数（一两行 hook）
- 在 `models/` 下**新增**自定义表的 Model 文件（命名带 `custom_` 前缀，如 `custom_aggregate_rule.go`）

### ❌ 禁止

- 不要修改 N9e 原生 `.go` 文件的业务逻辑（仅允许加 hook 调用）
- 不要修改 N9e 原生表结构（要扩字段就建关联表）
- 不要在原生目录 `alert/` `models/` `router/` 下散落自定义业务代码

## Hook 接入点（待 Task #2 确认后补充）

降噪逻辑应当插入告警事件流水线的以下位置：

| 位置 | 模块 | 用途 |
|---|---|---|
| 告警生成后 → 屏蔽前 | `alert/mute/` | 注入自定义屏蔽规则 |
| 屏蔽后 → 通知前 | `alert/dispatch/` | 注入聚合 + 抑制 |

具体接入点在 Task #2 确认。

## 升级流程（基于此目录结构）

```bash
# 1. 同步上游
git fetch upstream

# 2. 切临时分支做升级
git checkout -b upgrade-vX.X.X master-custom
git merge upstream/vX.X.X

# 3. 冲突预计只在原生文件的 hook 调用行
#    custom/ 目录下的代码可能要适配新接口，但范围可控

# 4. 跑回归测试，通过后合回 master-custom
git checkout master-custom
git merge upgrade-vX.X.X
git tag vendor-vX.X.X
```

## 当前基线

- 上游版本：`v8.5.1`
- 二开分支：`master-custom`
- 上游 remote：`upstream` → https://github.com/ccfos/nightingale.git
