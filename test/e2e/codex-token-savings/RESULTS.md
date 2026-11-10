# rtk-go vs 原版 RTK：容器内 Benchmark

测试日期：2026-11-10。所有模型调用只走本地 magpie 的
`my-qoder-local/DeepSeek-Flash`，没有使用云端 Codex/OpenAI 模型。

## 环境

| 项 | 值 |
|---|---|
| Codex CLI | 0.163.0（官方 npm 包，Linux 容器） |
| 模型 | `my-qoder-local/DeepSeek-Flash`，reasoning=low |
| 网关 | `http://host.docker.internal:3425/v1`（wire_api=responses） |
| 原版 RTK | 本地源码 `rtk/` 0.49.0，commit `8bc263a9`，Rust 1.91/bookworm 构建 |
| rtk-go | 本仓库 `go/`，Go 1.25.14，`CGO_ENABLED=0` |
| 运行隔离 | 每次一个全新容器，fixture 内置于镜像，不挂载用户数据 |

## 测试矩阵

| 套件 | 命令 | 容器镜像 |
|---|---|---|
| A 常规查询 | `git status`、`rg -n TODO_MARKER src`、`go test -count=1 -v ./gen` | `rtk-go-codex-e2e` |
| B 高熵查询 | 同上，但路径/匹配内容为伪随机串 | `rtk-go-codex-e2e-entropy` |
| C 失败/diff | `git diff -- bigdiff.txt`、`go test -count=1 -v ./genfail`（1 个失败） | `rtk-go-codex-e2e-fail` |

每个套件 6 个循环 × 3 个 arm（baseline / rtk-go / rtk），共 18 次 Codex
运行；arm 顺序按平衡 Latin square 排列。Prompt 要求只执行指定命令、不使用
其他工具、最后只回复 `DONE`。所有 54 次运行均通过命令数/`DONE`/退出码校验。

Token 数据来自 Codex 的 `turn.completed.usage.input_tokens`（包含系统提示、
工具框架和每一轮上下文，不是按单条命令计费）。`tool-output bytes` 来自
Codex JSONL 中每条 `command_execution.aggregated_output` 的实际长度。

## A. 常规查询：token 与字节

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 50,250.5 | — | 75,507 | — |
| rtk-go | 48,715 | **-3.1%** | 53,862 | **-28.7%** |
| rtk (0.49.0) | 40,118 | **-20.2%** | 25,787 | **-65.8%** |

分命令输出字节（每次运行都完全一致）：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git status` | 6,353 | 6,136 | 4,979 |
| `rg -n TODO_MARKER src` | 61,830 | 47,600 | 20,774 |
| `go test -v ./gen` | 7,324 | 126 | 34 |

## B. 高熵查询：token 与字节

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 65,900.5 | — | 92,079 | — |
| rtk-go | 63,788.5 | **-3.2%** | 64,564 | **-29.9%** |
| rtk (0.49.0) | 51,185.5 | **-22.3%** | 32,917 | **-64.3%** |

分命令输出字节：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git status` | 9,703 | 9,486 | 8,329 |
| `rg -n TODO_MARKER src` | 73,930 | 54,950 | 24,554 |
| `go test -v ./gen` | 8,446 | 128 | 34 |

## C. 失败与 diff

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 26,270 | — | 20,873 | — |
| rtk-go | 26,376.5 | **-0.4%** | 20,873 | **0.0%** |
| rtk (0.49.0) | 21,860 | **-16.8%** | 6,724 | **-67.8%** |

分命令输出字节：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git diff -- bigdiff.txt` | 14,007 | 14,007 | 6,541 |
| `go test -v ./genfail`（1 个失败） | 6,866 | 6,866 | 183 |

rtk-go 对非零退出整次回退原文，因此失败测试与 diff 均为 0 节省。原版
RTK 输出失败摘要与 condensed diff（带 recall 提示），节省显著。

## 工具输出截断验证

单独执行一条 `rg -n TODO_MARKER src`（不做其他命令）：

| arm | 工具输出字节 | input tokens |
|---|---:|---:|
| baseline | 73,930 | 37,214 |
| rtk-go | 54,950 | 38,696 |

字节减少 19KB，但 token 没有下降。原因是 Codex 对单条工具输出应用
truncation policy：55KB 和 74KB 都超过预算，模型侧看到的是同样被截断的
内容。原版 RTK 把同一 rg 输出压到 24.5KB，低于预算，才产生 20% 级的
token 节省。这个现象解释了两组 fixture 中“字节 -29% 但 token 只有 -3%”。

## 失败/diff 性能

| 场景 | arm | p50 | p95 | 最大 RSS | 输出字节 |
|---|---|---:|---:|---:|---:|
| git diff | direct | 0.7 ms | 0.8 ms | 8.3 MiB | 14,007 |
| git diff | rtk-go | 1.9 ms | 2.0 ms | 8.3 MiB | 14,007 |
| git diff | rtk | 4.5 ms | 4.8 ms | 12.1 MiB | 6,541 |
| go test 失败 | direct | 92.3 ms | 105.0 ms | 85.9 MiB | 6,866 |
| go test 失败 | rtk-go | 99.0 ms | 105.7 ms | 86.6 MiB | 6,866 |
| go test 失败 | rtk | 101.6 ms | 163.5 ms | 87.4 MiB | 183 |

## 性能（容器内，3 warm-up + 25 samples）

### 常规 fixture

| 场景 | arm | p50 | p95 | 最大 RSS | 输出字节 |
|---|---|---:|---:|---:|---:|
| tiny git status | direct | 0.7 ms | 0.8 ms | 8.4 MiB | 55 |
| tiny git status | rtk-go | 3.5 ms | 3.8 ms | 8.4 MiB | 55 |
| tiny git status | rtk | 4.6 ms | 5.0 ms | 11.0 MiB | 37 |
| git status | direct | 1.9 ms | 2.4 ms | 6.1 MiB | 6,353 |
| git status | rtk-go | 6.7 ms | 8.2 ms | 7.3 MiB | 6,136 |
| git status | rtk | 7.1 ms | 7.5 ms | 10.9 MiB | 4,979 |
| rg | direct | 4.4 ms | 4.6 ms | 9.2 MiB | 61,830 |
| rg | rtk-go | 7.7 ms | 8.6 ms | 9.2 MiB | 47,600 |
| rg | rtk | 12.1 ms | 13.4 ms | 12.4 MiB | 20,774 |
| go test | direct | 102.3 ms | 127.2 ms | 86.2 MiB | 7,324 |
| go test | rtk-go | 107.9 ms | 115.6 ms | 85.8 MiB | 126 |
| go test | rtk | 107.1 ms | 118.0 ms | 85.6 MiB | 34 |

### 高熵 fixture

| 场景 | arm | p50 | p95 | 最大 RSS | 输出字节 |
|---|---|---:|---:|---:|---:|
| tiny git status | direct | 0.7 ms | 0.8 ms | 8.5 MiB | 55 |
| tiny git status | rtk-go | 3.5 ms | 3.8 ms | 8.5 MiB | 55 |
| tiny git status | rtk | 5.7 ms | 7.4 ms | 10.9 MiB | 37 |
| rg | direct | 3.2 ms | 4.2 ms | 9.3 MiB | 73,930 |
| rg | rtk-go | 8.0 ms | 8.9 ms | 9.2 MiB | 54,950 |
| rg | rtk | 11.3 ms | 12.8 ms | 12.5 MiB | 24,554 |
| go test | direct | 101.1 ms | 168.0 ms | 86.4 MiB | 8,446 |
| go test | rtk-go | 105.8 ms | 125.4 ms | 85.3 MiB | 128 |
| go test | rtk | 142.5 ms | 168.2 ms | 83.4 MiB | 34 |

RSS 是 wrapper 进程自身的 `wait4` maxrss，不包含被包装的子进程内存。

## 结论

1. **原版 RTK 的 token 节省明显更高**：常规/高熵查询分别 20.2%/22.3%，
   rtk-go 只有 3.1%/3.2%。字节层面差距更大（65.8%/64.3% vs
   28.7%/29.9%）。
2. 差距主要来自**有损省略/截断**，不是解析速度：
   - 原版 `rtk rg` 只展示前 10 个文件，明确写 `+20 more files` 和
     `[+400 hidden: rtk recall ...]`；rtk-go 保留全部 600 条匹配（630 行）。
   - 原版 `rtk git status` 用 `M`/`M `/`??` 短码；rtk-go 保留
     `modified:` 标签和缩进。
   - 原版失败测试只输出失败摘要、diff 只保留变化行；rtk-go 对非零退出
     整次原文回退（失败/diff 场景 token 节省 -0.4% vs 16.8%）。
3. Token 节省低于字节节省：Codex 对单条工具输出有截断预算。单条 rg 探针
   中 rtk-go 字节 -26% 但 token +4%（噪声内），说明 55KB 仍未跨过截断阈值；
   原版把同一输出压到 24.5KB 才产生真实节省。另有系统提示/工具定义的固定
   成本，因此会话总 token 不会与命令输出字节等比下降。
4. 性能上 rtk-go 的启动/RSS 更低，原版在 rg 上多做丢弃/截断，耗时更高；
   两者 p95 开销都远低于 20 ms 的 Linux 目标（tiny 命令 +3.0~+4.2 ms）。
   原版在高熵 `go test` 上 p50 明显更高（142.5 ms vs 105.8 ms）。

## 限制

- 单一模型、单一 Codex 版本、单一 prompt 形状；使用本地 DeepSeek，不代表
  其他模型/真实订阅成本。
- `input_tokens` 是 Codex 的统计口径，不是 magpie 账单；不包含 Codex 外层
  tool framing 之外的 IDE/会话开销。
- 原版 RTK 0.49.0 是本仓库源码版本；用户机器安装的 1.1.0 行为可能不同。
- 容器内用 `--dangerously-bypass-approvals-and-sandbox`，未验证权限等价。
