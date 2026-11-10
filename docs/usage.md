# 使用说明

## 1. 安装

需要 Go 1.25.x。构建不依赖 CGO、Node、Python、Rust 或数据库：

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 go build -mod=readonly -trimpath -o rtk-go ./cmd/rtk-go
```

Windows：

```powershell
$env:GOTOOLCHAIN="local"; $env:CGO_ENABLED="0"
go build -mod=readonly -trimpath -o rtk-go.exe ./cmd/rtk-go
```

把二进制放到 `PATH`。检查：

```sh
rtk-go version --format json
```

## 2. 两种调用形式

### 简写

除管理命令外，第一个词作为子程序：

```sh
rtk-go git status
rtk-go git log --oneline -n 5
rtk-go rg -n "panic" cmd internal
rtk-go go test ./...
```

### `exec --`（推荐）

`--` 之后逐项透传，适合参数里带 `--`、空格或以 `-` 开头的值：

```sh
rtk-go exec -- git diff -- src/
rtk-go exec -- go test -run TestParser ./...
rtk-go exec -- pytest -q tests
```

两者都不改变 argv、cwd、环境变量、stdin 或退出码。

## 3. Profile

```sh
rtk-go exec --profile safe -- git diff
rtk-go exec --profile balanced -- go test ./...
rtk-go exec --profile aggressive -- rg TODO src
```

| profile | 行为 | 适用 |
|---|---|---|
| `safe` | 不截断查询正文；测试成功仍折叠 RUN/PASS，有损候选可 recall | 低风险审计；逐字用 `--raw` |
| `balanced` | 保留查询正文，折叠重复格式/测试进度 | 日常查询，偏保守 |
| `aggressive` | 有界省略；所有省略可 recall | Codex token 优化（默认） |

通过配置固定默认值：

```json
{
  "schema_version": 1,
  "profile": "balanced"
}
```

## 4. 回读原文

有损输出末尾会出现：

```text
[rtk-go recall 0123456789abcdef0123456789abcdef]
```

```sh
rtk-go recall <id>
rtk-go recall <id> --offset 201 --limit 200
rtk-go recall <id> --stream stderr
rtk-go recall <id> --stream stdout --raw
```

- `offset` 从 1 开始，默认 200 行，单次上限 5000 行；
- `--raw` 逐字输出单流，适合二进制/非 UTF-8；
- `--raw` 不能和 `--stream both` 同时使用；
- 记录过期、缺失或损坏时返回非零，不会重跑原命令。

### 无存储模式

```sh
rtk-go exec --no-store -- git status
```

禁止写入原文存储，同时禁止有损过滤；只输出无损候选或原文。如果任务要求“输出中绝不
丢信息”，使用 `--no-store` 或 `--raw`。

## 5. 纯原文

```sh
rtk-go exec --raw -- go test ./...
rtk-go bypass go test ./...
```

`--raw` 不保存、不压缩；适合管道、机器可读输出和排障。

## 6. 支持矩阵

### git

| 形态 | 行为 |
|---|---|
| `git status` / `git status --long` | aggressive 输出 `XY path`；保留 branch/upstream、rename、冲突、未跟踪路径 |
| `git status --porcelain*`、`-z`、`-s` | 原文 |
| `git diff` / `git diff --cached` / `-U<n>` | aggressive 省略 context，限制改动行，输出 `+A -D truncated`；机器格式原文 |
| `git diff --stat/--name-only/--numstat/--raw/--word-diff` | 原文 |
| `git log` 默认格式 | aggressive 短 hash + subject，默认 10 条；`-n` 可调 |
| `git log --oneline/--format/--pretty/--graph` / patch | 原文 |

### rg

只处理人类可读的 `path:line[:col]:text`：

- aggressive：按路径分组，最多 10 个文件/200 条匹配，单行正文最多 40 字节；输出 hidden 计数；
- safe/balanced：保留全部匹配；
- `--json`、`-0`、`--files`、`-A/-B/-C`、`--context`、`--replace`、`--pre` 等机器/上下文形态：原文。

### 测试/构建

| 命令 | 成功 | 失败 |
|---|---|---|
| `go test` / `-v` | aggressive 输出包级计数 | 只保留失败/跳过块、断言和包汇总；panic/race 原文 |
| `go test -json` | 解析已知事件流 | 未知事件/失败信号 → 原文 |
| `pytest` | 计数摘要 | 失败名、位置、断言；warnings 有界 |
| Node `--test` + TAP | 聚合通过/失败 | 非 TAP reporter、单流 TAP → 原文 |

构建命令（`go build`、`go vet` 等）目前不做自动改写；显式通过 `rtk-go exec --` 调用时，
只有匹配安全矩阵的形态才会过滤。

### 透传

未知命令、非 UTF-8、NUL、控制字符、ANSI 扩展序列、TTY、交互命令、机器可读格式统一输出
原文。任何过滤器异常、超时、原文保存失败或字节收益不足也回退原文。

## 7. 配置与诊断

```sh
rtk-go config show
rtk-go config show --format json
rtk-go doctor
rtk-go doctor --format json
rtk-go doctor --probe
rtk-go gain --since 7d
rtk-go clean --expired --dry-run
```

- `doctor` 只读检查配置、数据目录、权限和探测目录；`--probe` 只写本地 marker；
- `gain` 是基于本地统计的估算，不是真实账单；
- `clean --expired` 只删除受管过期对象，先 `--dry-run`。

完整字段见 [`configuration.md`](configuration.md)。

## 8. Codex CLI

### 推荐：手动前缀 + AGENTS 指令

```sh
rtk-go init codex --mode instructions --scope project --dry-run
rtk-go init codex --mode instructions --scope project
```

写入受管区块，卸载：

```sh
rtk-go uninstall codex --scope project
```

模型是否遵从指令取决于 Codex 版本、项目信任与模型行为，不能保证。需要确定性覆盖时，
在受信任环境里自行把命令接到 `rtk-go exec --`，并验证权限/沙箱行为。

### hook

当前版本没有默认启用 hook 安装。`rtk-go init codex --mode hooks` 会明确拒绝，避免在
未验证 Codex/平台权限回归的情况下改写用户配置。

### 低层重写 API

`rtk-go rewrite --shell posix|powershell --command <text> --format json` 只做纯文本分析，
返回 `rewrite` 或 `passthrough`，不会执行命令。它用于自定义集成；默认配置
`codex.auto_rewrite=false`，不会自动改写。

## 9. 退出码

| 码 | 含义 |
|---:|---|
| 子进程退出码 | `exec` 成功启动并结束时原样返回 |
| 0 | 管理命令成功 |
| 1 | 管理/内部/配置错误 |
| 2 | 用法错误 |
| 125 | wrapper 生命周期失败 |
| 126 | 不可执行 |
| 127 | 找不到程序 |

## 10. Windows 注意

- 原生 `.exe` 直接执行；`.cmd`/`.bat`/`.ps1` 不通过 shell 隐式执行；
- 参数逐项传递，空格路径按原生规则处理；
- PowerShell 下推荐 `rtk-go exec -- git status`，不要把整个 pipeline 塞进 wrapper；
- 不支持把交互式命令、需要 TTY 的命令或依赖 shell 展开的表达式交给过滤器。

## 11. 常见问题

### 为什么命令输出没有被压缩？

按优先级检查：

1. 命令不在支持矩阵，或是机器可读格式；
2. 输出太小，候选不满足“必须比原文更短”的字节守卫；
3. 输出是 TTY/交互式，或包含非 UTF-8/控制字符；
4. 原文保存失败或 `--no-store` 生效；
5. 当前 profile 是 `safe`/`balanced`，该形态本来就不做有界省略。

### 输出里有 `hidden` / `truncated`，是不是丢数据了？

是**上下文中有界省略**，不是存储丢失。原文在过滤前已完整落盘，用提示里的
`rtk-go recall <id>` 回读。不要把它当成完整日志做逐字审计。

### `recall` 提示记录过期/不存在？

默认保留 24 小时，受 `storage.retention_hours` 和配额限制；超过后不可恢复。需要长期
审计时使用 `--raw` 或自行保存原始输出。

### Codex 没有按 AGENTS 指令加前缀？

AGENTS 指令是尽力而为，模型可能忽略。需要确定性覆盖时，在受信任环境里手动包一层
`rtk-go exec --`，或实现并验证自己的 hook；不要为自动覆盖绕过权限检查。

### 可以直接包 `cmd`、管道或 PowerShell 脚本吗？

不行。`rtk-go` 只执行原生 executable 的单次 argv，不通过 shell 解析管道、重定向、
变量展开或 `.cmd/.bat/.ps1`。请让 shell 执行管道，只把单个命令交给 wrapper；或者
使用 `--raw` 避免过滤。

### 会不会把日志上传？

不会。产品不访问网络、不调用模型。日志只可能被本地 recovery 保存，或在 Codex 读取
到输出后按 Codex 自身策略处理。
