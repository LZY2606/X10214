# 捕获轨迹室 (Capture Trace Room)

一套**不依赖宿主正则库求结果**的、语义明确的正则执行器。它自己完成：

- pattern 解析为 AST；
- AST 降级为一个显式栈的回溯虚拟机（character class、量词、alternation、
  anchor、命名组、反向引用、lookaround 全部由本项目执行模型处理）；
- 对一次输入完整展示：匹配分支选择、捕获组槽位更新、失败回退、零宽重复终止、
  被剪枝的分支，以及字节/code point 双坐标位置；
- 对步数、状态数、捕获快照数设预算，超限返回**带 frontier 的 `indeterminate`**，
  可从 frontier 继续——绝不把“没算完”冒充成 `no_match`；
- 两个 pattern 的**语义差分**：在文档化预算范围内枚举有限反例，按 code point
  长度 + 字典序稳定列出；
- Go + SQLite 服务与单页界面，运行与事件持久化到 SQLite。

核心匹配代码不 import `regexp`（可用 `grep -R 'regexp' internal/regex` 核验，
仅允许出现在测试对照之外的零处）。

## 钉住的版本

每个 `Result` 与每次差分结果都带两个版本号：

- `syntax_version`：本项目语法契约版本（当前 `CTR-1.0`，见
  `internal/regex/ast.go` 的 `SyntaxVersion`）。
- `unicode_version`：字符属性/类别表所依据的 Unicode 数据库版本（当前
  `16.0.0`，对应构建工具链 Go 1.26 自带的 `unicode` 表；常量
  `UnicodeDataVersion`）。

版本是结果载荷的一部分并随每次运行存入 SQLite，便于复盘时知道结论针对哪份语法
与哪份字符数据。

## 构建与演示

```bash
go build ./...
go test ./... -count=1
go run ./cmd/server --addr 127.0.0.1:5274
# 浏览器打开 http://127.0.0.1:5274 ，页面标题/正文出现“捕获轨迹室”
```

参数：`--addr`（默认 `127.0.0.1:5274`）、`--db`（默认 `capturetraceroom.db`，
传 `:memory:` 用内存库）。

## 执行模型（为什么捕获不会泄漏）

- 每个回溯状态（`Thread`）携带**独立的捕获槽位快照**、独立的零宽守卫数组与
  lookaround 帧栈。`OpSplit` 把“次优先续体”作为独立快照压栈，优先续体先跑。
- 贪婪量词的优先续体是“再重复一次”，惰性量词的优先续体是“退出”。两者在指令里
  都通过 `OpSplit A/B` 表达，**A 永远先执行**；任何优化都不得交换 A/B（见
  `internal/regex/optimize.go` 的契约与轨迹等价测试）。
- 当一个分支失败、回退到备选快照时，该分支里写过的捕获槽位随之丢弃——
  “参与过但最终被撤销”的捕获不可能泄漏到胜出分支。alternation 捕获回滚、
  lookahead 捕获回滚都有专门测试。
- 无界量词编译为带守卫的循环：每次迭代入口记录位置，迭代体跑完回到同位置时
  `guardcheck` 让该续体死亡，从而终止 `(a*)*`、`()*`、`|` 这类零宽重复。
  有界量词在 `{n}`/`{n,m}` 上限不超过 4096 时展开，超过则编译报错。

### lookaround

- lookahead 在断言锚点开一个帧，体内匹配后位置回退到锚点；
- lookbehind 只支持**码点意义上的定宽**模式（每个 char/class/dot 算 1 个 code
  point），变宽（含反向引用、无界量词、分支宽度不一致）编译期报错；
- 负向 lookaround 的“续体”不会作为普通回溯状态常驻栈中：体内执行只在“body
  确实失败”时由失败重写路径恢复位置并进入续体；body 若成功，负向断言失败，
  不会误续。

## 坐标与无效 UTF-8

`MapPositions` 同时给出：

- `byte_to_rune`：每个字节边界对应的 code point 序号（续字节与前导字节同号）；
- `rune_to_byte`：每个 code point 的起始字节；
- 无效字节按 `unicode/utf8` 的 `RuneError, 1` 解码，单独计为一个 code point，
  与真实 U+FFFD 区分；`.` 默认不吞无效字节，`[^...]` 与显式字面量可以命中它。

## 预算与 frontier

`Budget`：`max_steps`（指令步数）、`max_states`（累计压入的回溯状态/快照数）、
`max_snapshots`（捕获快照拷贝预算）、`max_events`（保留的轨迹事件数）。

任一预算触顶：

- 当前状态随**完整 frontier**（状态栈、下一个搜索位置、已用计数、事件序号）
  返回 `status = indeterminate`；
- 轨迹里有 `prune` 事件，说明哪个续体因预算被丢弃；
- 调 `/api/resume`（带同一 pattern、输入和 frontier，或带持久化的 `run_id`）
  用新预算继续。恢复不会从 0 重试，也不会把“暂停”误报成 no-match。

事件超 `max_events` 时只截断轨迹（`events_truncated=true`），不改变判定。

## 语义差分

`Diff(A, B, DiffBudget)` 对字母表做按长度的全枚举（长度 0..`max_length`），
以 fullmatch 语义比较两个 pattern；只收集判定不同的输入：

- 排序稳定：先按 code point 长度，再按字母表 rune 的字典序；
- `enumerated` 是实际枚举的输入数，`exhaustive` 表示预算内是否穷尽；
- 单个输入若触发 `indeterminate`，记入 `indeterminate_inputs` 且
  `exhaustive=false`（差分本身也不拿“没算完”当等价/不等价证据）。

## HTTP API（均为 JSON）

- `POST /api/parse` `{pattern, optimize}` → AST（含节点 id 与可读 label）。
- `POST /api/run` `{pattern, input, optimize, step_limit, breakpoints, budget, persist}`
  → `{result, run_id, program}`；`step_limit>0` 用于单步。
- `POST /api/resume` `{pattern, input, optimize, frontier|run_id, step_limit, breakpoints, budget}`。
- `POST /api/diff` `{pattern_a, pattern_b, budget, persist}`。
- `POST /api/positions` `{input}` → 字节/码点双坐标。
- `GET /api/versions`；`GET /api/runs?limit=`；`GET /api/runs/{id}`（含事件轨迹）。

断点形如 `{node_id, rune_pos}`（任一为 0/-1 表示通配），在对应 AST 指令执行前
暂停。

## 支持的语法（CTR-1.0 摘要）

- 字面量、`.`、字符类 `[...]`/`[^...]`、范围、`\d \D \w \W \s \S`、
  `\p{L}` `\p{Greek}` / `\P{...}`、十六进制 `\x.. \u.... \U........`；
- 量词 `* + ? {n} {n,} {n,m}`，惰性后缀 `?`；possessive `++` 显式不支持并报错；
- alternation `a|b|c`、捕获组 `( )`、非捕获 `(?: )`、命名 `(?<n>)` / `(?P<n>)`；
- 反向引用 `\1` 与 `\k<name>`；
- anchors `^ $ \A \z \b \B`；模式修饰 `(?i) (?m) (?s) (?u)` 与 `(?i:...)`；
- lookaround：`(?=) (?!) (?<=) (?<!)`（后顾要求定宽）。

## 代码地图

- `internal/regex/`：`ast.go`/`parser*.go`（解析、AST）、`vm.go`（编译器）、
  `engine*.go`（虚拟机、预算、轨迹、frontier）、`charmatch.go`/`props.go`
  （Unicode 字符语义）、`optimize.go`（顺序保持优化）、`diff.go`（语义差分）。
- `internal/store/`：SQLite 持久层（runs / events / diffs）。
- `internal/server/` + `web/`：HTTP API 与单页界面。
- `cmd/server/`：服务入口。
