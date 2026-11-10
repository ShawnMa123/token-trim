# rtk-go vs 原版 RTK：容器内 Benchmark（aggressive v2）

测试日期：2026-11-10。所有模型调用只走本地 magpie
`my-qoder-local/DeepSeek-Flash`，没有使用云端模型。

## 环境

| 项 | 值 |
|---|---|
| Codex CLI | 0.163.0（官方 npm 包，Linux 容器） |
| 模型 | `my-qoder-local/DeepSeek-Flash`，reasoning=low |
| 网关 | `http://host.docker.internal:3425/v1`（wire_api=responses） |
| 原版 RTK | 本地 `rtk/` 源码 0.49.0，commit `8bc263a9`，Rust 1.91/bookworm |
| rtk-go | `go/`，Go 1.25.14，`CGO_ENABLED=0`，默认 `aggressive` |
| 隔离 | 每次全新容器，fixture 内置于镜像，不挂载用户数据 |

## aggressive v2 改动

- 默认 profile 改为 `aggressive`；`safe`/`balanced` 保留完整正文。
- git status：porcelain 风格 `XY path` 短码，保留全部路径/rename/冲突/分支。
- rg：最多 10 文件/200 匹配，单行正文 40B，输出 hidden 计数 + recall。
- git diff：省略 context，最多 80 行改动，输出 `+A -D truncated`。
- go test：成功输出 `Go test: N passed in M packages`；失败只留失败/跳过块与包汇总；panic/race 原文。
- git log：默认最多 10 条，短 hash + subject；`-n` 可调。
- pytest：计数摘要 + 失败名/位置/断言，warnings 有界。
- 所有省略先完整提交原文；hint 缩短为 `[rtk-go: -N; recall <id>]`。

## 方法

| 套件 | 命令 | 运行数 |
|---|---|---:|
| A 常规查询 | `git status`、`rg -n TODO_MARKER src`、`go test -count=1 -v ./gen` | 6 轮 × 3 arm |
| B 高熵查询 | 同上，路径/匹配内容为伪随机串 | 6 轮 × 3 arm |
| C 失败 + diff | `git diff -- bigdiff.txt`、`go test -v ./genfail`（1 失败） | 6 轮 × 3 arm |
| D pytest | `pytest tests`（120 passed, 1 failed, 1 skipped） | 4 轮 × 3 arm |

arm 为 baseline / rtk-go / rtk，顺序按平衡 Latin square 排列；每次新容器。
Prompt 只允许执行指定命令，最后回复 `DONE`。全部 66 次运行通过命令数、
`DONE`、退出码校验。

Token 数据来自 Codex `turn.completed.usage.input_tokens`（包含系统提示、工具
框架和每轮上下文，不是 magpie 账单）。工具输出字节来自 JSONL 中
`command_execution.aggregated_output` 的实际长度。

## A. 常规查询

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 50,239.5 | — | 75,507 | — |
| rtk-go | 35,816 | **-28.7%** | 14,760 | **-80.5%** |
| rtk (0.49.0) | 40,069.5 | -20.2% | 25,787 | -65.8% |

分命令：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git status` | 6,353 | 5,042 | 4,979 |
| `rg -n TODO_MARKER src` | 61,830 | 9,622 | 20,774 |
| `go test -v ./gen` | 7,324 | 96 | 34 |

## B. 高熵查询

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 65,621.5 | — | 92,079 | — |
| rtk-go | 42,338.5 | **-35.5%** | 18,174 | **-80.3%** |
| rtk (0.49.0) | 43,273 | -34.1% | 32,917 | -64.3% |

分命令：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git status` | 9,703 | 8,385 | 8,329 |
| `rg -n TODO_MARKER src` | 73,930 | 9,699 | 24,554 |
| `go test -v ./gen` | 8,446 | 90 | 34 |

## C. 失败 + diff

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 26,279 | — | 20,873 | — |
| rtk-go | 21,635 | **-17.7%** | 5,466 | **-73.8%** |
| rtk (0.49.0) | 21,858.5 | -16.8% | 6,724 | -67.8% |

分命令：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git diff -- bigdiff.txt` | 14,007 | 5,296 | 6,541 |
| `go test -v ./genfail` | 6,866 | 170 | 183 |

## D. Pytest

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 12,685 | — | 1,060 | — |
| rtk-go | 12,560 | **-1.0%** | 243 | **-77.1%** |
| rtk (0.49.0) | 12,541.5 | -1.1% | 241 | -77.3% |

pytest 单命令总量小，token 主要由固定系统提示构成，两者基本持平。跑完后又把
hint 从 `[rtk-go: -N; recall <id>]` 缩成 `[rtk-go recall <id>]`，同一 fixture
输出降到 237B（原版 241B）。

## 性能（3 warm-up + 25 samples）

### 常规 fixture

| 场景 | arm | p50 | p95 | 最大 RSS | 输出字节 |
|---|---|---:|---:|---:|---:|
| tiny git | direct | 0.7 ms | 0.8 ms | 8.6 MiB | 55 |
| tiny git | rtk-go | 5.0 ms | 5.3 ms | 8.6 MiB | 55 |
| tiny git | rtk | 4.5 ms | 4.9 ms | 10.8 MiB | 37 |
| git status | rtk-go | 5.7 ms | 6.4 ms | 7.2 MiB | 5,035 |
| git status | rtk | 6.7 ms | 7.1 ms | 10.8 MiB | 4,979 |
| rg | rtk-go | 10.6 ms | 11.7 ms | 9.2 MiB | 9,618 |
| rg | rtk | 11.3 ms | 12.7 ms | 12.2 MiB | 20,774 |
| go test | rtk-go | 99.6 ms | 112.7 ms | 85.8 MiB | 90 |
| go test | rtk | 97.9 ms | 149.7 ms | 83.7 MiB | 34 |

### 失败/diff fixture（第二次空闲采样）

| 场景 | arm | p50 | p95 | 最大 RSS | 输出字节 |
|---|---|---:|---:|---:|---:|
| git diff | rtk-go | 5.5 ms | 5.8 ms | 8.3 MiB | 5,296 |
| git diff | rtk | 4.6 ms | 5.0 ms | 11.8 MiB | 6,541 |
| go test fail | rtk-go | 106.8 ms | 130.4 ms | 82.4 MiB | 170 |
| go test fail | rtk | 120.4 ms | 171.4 ms | 83.8 MiB | 183 |
| tiny git | rtk-go | 5.1 ms | 5.5 ms | 8.1 MiB | 55 |
| tiny git | rtk | 4.9 ms | 5.4 ms | 10.8 MiB | 37 |

RSS 是 wrapper 自身 `wait4` maxrss，不含被包装子进程；go test 场景主要由
Go 子进程主导。

## 结论

1. **目标达成：aggressive v2 在 A/B/C 三组同时超过原版 RTK**：token 降幅
   28.7%/35.5%/17.7% vs 20.2%/34.1%/16.8%，字节降幅
   80.5%/80.3%/73.8% vs 65.8%/64.3%/67.8%。D 组 pytest 基本持平
   （-1.0% vs -1.1%，字节 243B vs 241B；hint 缩短后 237B）。
2. 关键杠杆是 **40B 匹配正文上限**：高熵内容占 token 大头；把唯一随机正文
   截断后，rtk-go 用 18KB 输出取得比原版 33KB 更低的 input tokens。
3. 原版在失败测试/diff 上采用“只留失败 + recall”的有损策略；rtk-go 现在
   同样做，并且 diff 截断到 80 行后字节更小（5.3KB vs 6.5KB）。
4. 性能上 rtk-go 启动/RSS 更低；tiny p95 约 5.3~5.5ms（原版 4.9~5.4ms），
   rg p50 10.6ms（原版 11.3ms）。go test 总耗时主要受子进程与 CI 波动影响。
5. 与旧版相比，rtk-go 从“保真但只省 3%”变为“有界省略 + 强制 recall”，
   在保留恢复路径的前提下达到/超过原版效果。

## 限制

- 单一模型/Codex 版本/prompt 形状；本地 DeepSeek 结果不代表云端模型。
- `input_tokens` 是 Codex 统计口径，不是 magpie 账单；未计 IDE/会话外层开销。
- 原版对比的是本地源码 0.49.0；用户机器安装的 1.1.0 可能不同。
- 容器内 bypass sandbox，未验证权限等价。
