# rtk-go

独立的 Go 命令行输出压缩器，面向 **Codex CLI** 的 shell 结果降噪。实现位于本仓库 `go/`，不复制上游 Rust RTK 源码，也不依赖 Rust、Node、Python、数据库或网络服务。

## 构建

要求真实 Go **1.25.x**。默认零第三方模块、`CGO_ENABLED=0`：

```sh
cd go
GOTOOLCHAIN=local CGO_ENABLED=0 go build -mod=readonly -trimpath -o rtk-go ./cmd/rtk-go
```

Windows PowerShell：

```powershell
$env:GOTOOLCHAIN="local"; $env:CGO_ENABLED="0"
go build -mod=readonly -trimpath -o rtk-go.exe ./cmd/rtk-go
```

`go.mod` 声明 `go 1.25.0`。不要用 1.26+ 的成功替代 1.25 兼容验收。

## 使用

```text
rtk-go git status                 # 简写包装（管理命令之外的首词即程序）
rtk-go exec -- go test ./...      # 推荐形式；-- 后参数逐项透传
rtk-go exec --raw -- go test ./...  # 完全原文
rtk-go bypass -- <program> ...    # 与 raw 等价
rtk-go recall <id>                # 回读已存原文；绝不重跑命令
rtk-go gain --since 7d            # 本地估算统计
rtk-go clean --expired --dry-run  # 只清理受管过期对象
rtk-go doctor [--probe]           # 只读诊断；probe 仅本地 marker
rtk-go version --format json
```

过滤后的 stdout 末尾会出现：

```text
[rtk-go: hidden=120 lines; recall=rtk-go recall <id>]
```

`recall` 默认分页（1-based offset、默认 200 行、上限 5000）。机器可读二进制/非 UTF-8 原文用单流 `--raw`：

```text
rtk-go recall <id> --stream stdout --raw
```

`--raw` 与 `--stream both` 同时使用会退出 2。

## 支持矩阵

| 命令形态 | 处理 |
|---|---|
| `git status` 默认长格式 | 折叠提示/空行，保留分支、ahead/behind、全部路径、rename 两端、冲突 |
| `git status --porcelain*` / `-z` / `-s` | 原文 |
| `git diff`、`git diff --stat`、patch、任何机器格式 | 原文 |
| `git log` 默认格式（安全 flags） | 折叠 Author 与空行，保留完整 hash、日期、消息、顺序、merge 行 |
| `git log --oneline` / `--format` / `--pretty` / patch / graph | 原文 |
| `rg` 人类可读 `path:line[:col]:text` | 按路径无损分组，保留全部匹配与行号 |
| `rg --json`、`-0`、`--files`、`-A/-B/-C`、`--pre` 等 | 原文 |
| `go test` / `-v` | 成功时折叠 RUN/PASS；失败、panic、race、非零退出整次原文 |
| `go test -json` | 仅成功且事件流完整时输出包摘要；未知事件/失败/非法 JSON 原文 |
| `pytest` / `python -m pytest` | 保留失败/错误/warning/skip/最终 summary；未知 preamble 原文 |
| `node --test --test-reporter=tap` | 折叠成功 TAP 块，保留失败/skip/todo 与计数；嵌套/未知 TAP 原文 |
| `go build` / `go vet` / 其他程序 | 原文；成功静默不伪造输出 |

`safe` profile 只启用 Git status/log 与 rg；`balanced`（默认）增加测试过滤器。所有有损候选都必须在原文完整提交后，且候选（含 hint）字节数严格小于原文才会采用；平局与空输出一律原文。

## 安全模型

- 直接 argv 执行，不经过 shell 拼接；cwd、stdin、env 继承，仅增加 `RTK_GO_ACTIVE=1` 防递归。
- 子进程只执行一次；过滤、统计、存储失败不会改变已取得退出码。
- 退出码：spawn not found 127、不可执行 126、其他启动失败 125；输出管道不完整/下游 broken pipe 且子进程为 0 时 125。
- stdout/stderr 并发 drain。stderr 永不压缩；只有 stdout 可能压缩。
- stdout 捕获默认 8 MiB、单行 1 MiB、查询 1500 ms、显式测试 30000 ms；超限先精确回放已捕获前缀，再切换 raw streaming，每字节恰好一次。
- 过滤只用 UTF-8 文本；NUL、非法 UTF-8、非 SGR ANSI/OSC、覆盖式 `\r` 进度一律原文。v1 不做 GB18030 转码。
- Windows 使用 **kill-on-close Job Object**：子进程以 suspended 状态创建，加入 Job 后才 resume，避免“先执行后关联”的逃逸窗口；无法关联时退出 125 且不执行用户代码。
- Unix 使用进程组回收；单 wrapper 被 SIGKILL 的场景依赖 host 管理整树，产品不作零孤儿承诺。
- 有损输出必须先把完整 stdout、非空 stderr、manifest 原子提交到用户私有目录；commit 失败输出原文。

## 配置

默认路径：`os.UserConfigDir()/rtk-go/config.json`（Windows 为 `%AppData%\rtk-go\config.json`）。`RTK_GO_CONFIG` 可显式指定文件；不会加载项目内同名配置。

```json
{
  "schema_version": 1,
  "profile": "balanced",
  "storage": {
    "enabled": true,
    "data_dir": "",
    "retention_hours": 24,
    "total_mib": 256
  },
  "capture": {
    "memory_mib": 8,
    "stream_threshold_ms": 1500,
    "test_threshold_ms": 30000
  },
  "stats": { "enabled": true, "retention_days": 30 },
  "codex": { "auto_rewrite": false, "rewrite_tests": false }
}
```

- `storage.data_dir` 为空时使用 `os.UserCacheDir()/rtk-go`；显式值必须是绝对路径。
- 配置类型/范围错误时 `exec` 原文运行，`doctor`/`config show` 退出 1 并指出字段。
- 未知键仅告警，不作为可执行配置。
- 紧急开关：`RTK_GO_DISABLE=1` 使包装器与 rewrite 透传；`RTK_GO_NO_STORE=1` 禁止所有有损变换与原文写入。两者不能被 flag 覆盖。

## 数据生命周期

- `runs/<id>/stdout.bin`、`stderr.bin`、`manifest.json`；id 为 128-bit 随机 hex。
- 默认保留 24 h；manifest 含 SHA256、字节数、complete 标记与过期时间。
- 总预算 256 MiB，每次有损候选预留 64 MiB；锁等待上限 200 ms，拿不到锁即 raw。
- Unix 目录 0700/文件 0600；Windows 设置并校验受保护 DACL（当前用户 + SYSTEM + Administrators），其他主体可读则禁用落盘。
- `clean --expired` 只删除过期记录与超过 24 h 的 `.partial`；`--dry-run` 零写入。
- 统计位于 `stats/YYYY-MM-DD/<id>.json`，不含完整 argv、路径、env 或输出正文；默认保留 30 天。

## Codex 接入

**手动优先**（无需 hook、无需改 Codex 配置）：

```text
rtk-go exec -- git status
rtk-go exec -- go test ./...
```

**instructions 模式**（写入受管 AGENTS 块，可逆）：

```text
rtk-go init codex --mode instructions --scope project --dry-run
rtk-go init codex --mode instructions --scope project
rtk-go uninstall codex --scope project
```

用户级安装使用 `--scope user`，尊重 `CODEX_HOME`。

**hook 模式默认关闭。** 没有目标 Codex/平台组合的 allow/ask/deny/managed/sandbox 负向权限回归前，`init codex --mode hooks`（包括 dry-run）退出 1 且零写入；`rtk-go hook codex` 始终返回 `{}`。`rewrite` 只做纯分析，不执行命令：

```text
rtk-go rewrite --shell powershell --command "git status" --format json
rtk-go rewrite --shell posix --command "rg -n 'foo' src" --format json
```

只改写已验证语法中的单条 `git status`、`git log`（安全 flags）、`rg` 查询；管道、重定向、展开、变量、嵌套 shell、`git -c`/`--exec-path`、rg `--pre`、测试/构建命令一律 passthrough。

## 验证记录（2026-11-10）

Go 1.25 容器（`golang:1.25-alpine`，实际 `go1.25.14 linux/amd64`，空模块、`GOTOOLCHAIN=local`、`GOPROXY=off`、`CGO_ENABLED=0`）：

```text
go test -mod=readonly ./...      PASS
go test -race ./...              PASS（容器内临时安装 gcc/musl-dev）
go vet -mod=readonly ./...       PASS
go build -mod=readonly -trimpath ./cmd/rtk-go        PASS (linux/amd64)
GOOS=windows GOARCH=amd64 go build ...               PASS (windows/amd64)
go list -m all                   -> 仅主模块
```

Go 1.25.14 交叉编译出的 `windows/amd64` 二进制已在本机 Windows 10 原生运行：`version` 报告 `go1.25.14 windows/amd64`，并通过真实 `git status` 过滤与 recall。本机 Go 1.26.4 的 `go test ./...`、`go vet ./...`、Windows Job Object 取消/孙进程回收测试仅作辅助验证，不替代 1.25 兼容证明。真实 `go test -json`、`go test -v`、Node 22 TAP 成功/失败输出完成端到端试跑。

过滤基准（Linux/amd64，Go 1.25.14）：

| 输入 | 时间 | 吞吐 | 分配 |
|---|---:|---:|---:|
| Git status 1 MiB | 2.55 ms/op | 411 MB/s | 25 allocs |
| rg 1 MiB | 19.1 ms/op | 55 MB/s | 66k allocs |

**未验证**：Windows 上的 Go 1.25 运行验证（仅交叉编译）、真实 Codex PreToolUse 端到端与权限矩阵、pytest 真实环境（本机未安装，未自动安装）、精确 tokenizer 收益、公司机器上的性能 gate。`gain` 的 token 为 `ceil(bytes/4)` 估算，不是账单。

## 已知限制

- 不支持 `.cmd/.bat/.ps1` 直接包装（明确 126），不注入 npm 参数。
- 不压缩 stderr、Git patch、机器可读格式、交互式/TTY 命令。
- TTY 下自动 raw，避免把延迟输出误当实时进度。
- v1 不转码 GBK/GB18030；非 UTF-8 文本 recall 明确拒绝并提示单流 `--raw`。
- 无 hook 自动改写；无法证明权限等价时保持 manual/instructions。
- 不提供守护进程、遥测、自动更新、模型调用或网络请求。
