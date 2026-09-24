# 捕获轨迹室 (Capture Trace Chamber)

一套明确语义的正则执行器：解析 pattern 构建 AST，用项目自己的回溯执行模型
匹配输入，并暴露完整轨迹——匹配分支、捕获组更新、失败回退与被剪枝分支。
核心匹配**不调用**宿主 `regexp` 包。

## 运行

```sh
go build ./...
go test ./... -count=1
go run ./cmd/server --addr 127.0.0.1:5274
# 打开 http://127.0.0.1:5274 ，页面标题为「捕获轨迹室」
```

可选参数：`--db <path>`（默认 `capture-trace.db`，SQLite，存运行、事件与 frontier）。

## 语法版本与 Unicode 数据版本

每次结果都钉住两个版本：

- 语法版本：`gsb.syntax/1.0.0`（`internal/syntax.Version`）
- Unicode 数据版本：Go 标准库 `unicode.Version`（当前 15.0.0），
  字符类 `\p{...}`、`\w`、`\b` 的判定均以此为准

## 支持的语法

- 字面量、`.`、字符类 `[...]`（范围、取反、内嵌 `\d \w \s \p{...}`）
- 量词 `* + ? {n} {n,} {n,m}`，均可加 `?` 变惰性；嵌套量词（如 `a**`）报错
- alternation `|`、分组 `(...)`、`(?:...)`、命名组 `(?P<n>...)` / `(?<n>...)`
- 反向引用 `\1`..`\9`、`\k<name>`
- anchor：`^ $ \A \z \b \B`（`^/$` 无 multiline，等价于 `\A/\z`）
- lookaround：`(?=...) (?!...) (?<=...) (?<!...)`
- 转义：`\d \D \w \W \s \S \p{X} \P{X} \n \t \r \f \v \xHH \uHHHH`

## 执行语义（文档化约定）

- **分支次序**：alternation 与量词的分支偏好严格从左到右、先贪婪后回退；
  惰性量词偏好"少匹配"。优化（`Optimize`，jump threading）只重定向跳转目标，
  绝不重排 SPLIT 的两个分支，优化前后可观测轨迹（分支决策、捕获、失败）
  逐项一致，由 `TestOptimizationTraceEquivalence` 保证。
- **捕获历史**：每次 SAVE 记入轨迹；回退时按状态快照恢复捕获，
  轨迹中产生 `capture-rollback` 事件；被撤销的捕获不会泄漏进最终结果。
- **零宽重复**：每个无界循环带 PROGRESS 守卫，循环体匹配空串时立即终止
  该迭代并回退，保证 `(a*)*` 等模式终止。
- **位置**：轨迹事件同时给出 code point 位置与 UTF-8 字节位置；
  无效 UTF-8 字节解码为 U+FFFD 且占 1 字节。
- **lookaround**：子程序共享预算计数；lookaround 内的捕获按约定丢弃；
  lookbehind 通过枚举起点并要求终点恰为当前位置实现。

## 资源控制

执行设置三个预算（0 表示不限）：

- `maxSteps`：指令执行步数
- `maxStates`：压入回溯栈的状态总数
- `maxSaves`：捕获快照（SAVE）次数

超限或命中断点时返回 **indeterminate** 并携带 frontier（当前状态 + 回溯栈 +
未尝试的搜索起点），绝不冒充 no-match。frontier 持久化在 SQLite，
`/api/step` 可从 frontier 继续（单步 = 追加小额步数预算）。

## API

- `POST /api/run` `{pattern, input, optimize, budget, breaks}` → 运行并返回
  AST、指令清单、轨迹事件、捕获组、frontier、版本
- `POST /api/step` `{runID, steps, breaks}` → 从 frontier 继续
- `GET /api/run?id=N` / `GET /api/runs` → 历史查询
- `POST /api/diff` `{patternA, patternB, maxLen}` → 语义差分

## 语义差分

`internal/diff` 在两个 pattern 的字面字符导出的字母表上枚举长度
`0..maxLen` 的候选串，用项目引擎做全串成员判定，把只属于一侧的串按
**长度再按字典序**稳定列出。预算不足的候选按 indeterminate 跳过，
不计入任何一侧；结果附预算范围说明（长度界、字母表、每候选步数、候选总数）。

## 测试覆盖

`internal/engine/engine_test.go`：嵌套量词终止、空匹配、alternation 捕获
回滚、反向引用、lookaround、组合字符（e + U+0301）、无效 UTF-8、
预算暂停/恢复一致性、indeterminate 不冒充 no-match、优化前后轨迹等价、
惰性量词、有界重复、命名组与 anchor。
`internal/syntax`、`internal/diff`、`internal/web` 另有解析、差分与 HTTP 流程测试。
