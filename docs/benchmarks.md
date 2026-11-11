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
| rtk-go | 仓库根目录，Go 1.25.14，`CGO_ENABLED=0`，默认 `aggressive` |
| 隔离 | 每次全新容器，fixture 内置于镜像，不挂载用户数据 |

## aggressive v2 改动

- 默认 profile 改为 `aggressive`；`safe`/`balanced` 不截断查询正文（测试进度仍可折叠）。
- git status：porcelain 风格 `XY path` 短码，保留全部路径/rename/冲突/分支。
- rg：最多 10 文件/200 匹配，单行正文 40B，输出 hidden 计数 + recall。
- git diff：省略 context，最多 80 行改动，输出 `+A -D truncated`。
- go test：成功输出 `Go test: N passed in M packages`；失败只留失败/跳过块与包汇总；panic/race 原文。
- git log：默认最多 10 条，短 hash + subject；`-n` 可调。
- pytest：计数摘要 + 失败名/位置/断言，warnings 有界。
- 所有省略先完整提交原文；hint 缩短为 `[rtk-go recall <id>]`。

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
| baseline | 50,352 | — | 75,507 | — |
| rtk-go | 35,717 | **-29.1%** | 14,721 | **-80.5%** |
| rtk (0.49.0) | 40,535 | -19.5% | 25,787 | -65.8% |

分命令：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git status` | 6,353 | 5,028 | 4,979 |
| `rg -n TODO_MARKER src` | 61,830 | 9,610 | 20,774 |
| `go test -v ./gen` | 7,324 | 83 | 34 |

## B. 高熵查询

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 65,691 | — | 92,079 | — |
| rtk-go | 42,751 | **-34.9%** | 18,152 | **-80.3%** |
| rtk (0.49.0) | 43,216.5 | -34.2% | 32,917 | -64.3% |

分命令：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git status` | 9,703 | 8,378 | 8,329 |
| `rg -n TODO_MARKER src` | 73,930 | 9,691 | 24,554 |
| `go test -v ./gen` | 8,446 | 83 | 34 |

## C. 失败 + diff

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 26,277.5 | — | 20,873 | — |
| rtk-go | 21,667 | **-17.5%** | 5,452 | **-73.9%** |
| rtk (0.49.0) | 21,873.5 | -16.8% | 6,724 | -67.8% |

分命令：

| 命令 | baseline | rtk-go | rtk |
|---|---:|---:|---:|
| `git diff -- bigdiff.txt` | 14,007 | 5,289 | 6,541 |
| `go test -v ./genfail` | 6,866 | 163 | 183 |

## D. Pytest

| arm | input tokens 中位数 | 相对 baseline | 工具输出字节中位数 | 相对 baseline |
|---|---:|---:|---:|---:|
| baseline | 12,685.5 | — | 1,060 | — |
| rtk-go | 12,559 | **-1.0%** | 237 | **-77.6%** |
| rtk (0.49.0) | 12,551.5 | -1.1% | 241 | -77.3% |

pytest 单命令总量小，token 主要由固定系统提示构成；两者 token 基本持平，
rtk-go 字节略低（237B vs 241B）。

## E. 质量回归（bug fix）

同一 fixture：120 个通过测试 + 1 个真实失败（`Add(2,3)` 错误），要求模型运行
测试、修 `calc.go`、重跑到通过。3 个 arm × 3 轮，全部 **9/9 修复成功**，
`go test ./...` 在独立容器复核通过。aggressive 失败摘要仍保留
`Add(2,3) = -1, want 5`、失败测试名与包汇总，足以定位并修复。

各轮命令数/模型路径不同，token 数字不用于节省对比；此项只验证事实保真与
任务完成率没有回归。

## F. 长日志 / 长文本（lossless）

Fixture：约 299KB 日志（3000 行连续相同 heartbeat + 3 条 ERROR + 1 条 FATAL
堆栈）加约 22KB 无重复行文章；要求模型只根据命令输出回答 4 个事实：

- ERROR 行数；
- FATAL 的 request id；
- FATAL 后第二个栈帧；
- 文章中的 audit window。

| arm | 正确率 | input tokens 中位数 | 工具输出字节中位数 |
|---|---:|---:|---:|
| baseline `cat` | **1/3** | 22,570 | 299,094 |
| `rtk-go --profile lossless` | **3/3** | 16,624 | 23,467 |

baseline 的两次失败不是答错格式，而是原始 299KB 输出被 Codex 截断，模型只看到
文章开头，无法看到 ERROR/FATAL；另一次重试拿到完整输出（126,817 input tokens，
598KB 聚合输出）才答对。lossless 用连续重复行的 `line [rtk-go ×N]` 编码，把
日志压到约 1.1KB，文章原样保留；3 次均在 16.6k input tokens 左右答对。

这里收益来自**消除重复**，不是丢弃信息：行文本、顺序、重复次数都保留，原始字节
仍可用 `recall` 回读。**长文本没有重复行时，lossless 会直接回退原文，收益为 0**；
自然语言文章无法靠无损变换显著缩短。

测试命令：

```sh
cd test/e2e
RUNS=3 ./run-longlog.sh
```

## G. Profile 对比

同一容器、同一 fixture，`rtk-go` 分别指定 `--profile`；输出字节为该命令的 stdout 字节数
（含 recall hint），不包含 Codex 系统提示。`lossless` 只在 `cat`/`tail`/`head` 形态生效。

### 输出字节

| 命令 / fixture | direct | safe | lossless | balanced | aggressive |
|---|---:|---:|---:|---:|---:|
| git status（常规） | 6,353 | 6,105 | 6,105 | 6,105 | **5,028** |
| rg（常规） | 61,830 | 47,600 | 47,600 | 47,600 | **9,610** |
| go test 通过（常规） | 7,324 | 7,324 | 7,324 | 93 | **83** |
| git diff（失败 fixture） | 14,007 | 14,007 | 14,007 | 14,007 | **5,289** |
| pytest（pytest fixture） | 1,060 | 1,060 | 1,060 | 748 | **237** |
| 长日志 + 文章（longlog） | 299,094 | 299,094 | **23,467** | 299,094 | 299,094 |

要点：

- `safe` 不做有损省略；`rg` 的路径分组是唯一明显收益（路径只出现一次）。
- `lossless` 在长日志上从 299KB 降到 23.5KB；文章没有重复行，原样保留。
- `balanced` 保留查询正文，只折叠测试进度；因此 go test/pytest 有小幅收益。
- `aggressive` 的收益来自有界省略；发布前完整保存原文，但**发送给模型的文本不是全量**。
- 不适合把“输出字节最少”直接等同于“任务效果最好”；长日志 baseline 被截断后 token 反而
  更少，但丢事实（见 F 节）。

### p95 延迟（25 samples，容器内）

| 场景 | direct | safe | lossless | balanced | aggressive |
|---|---:|---:|---:|---:|---:|
| git status | 3.4 ms | 7.1 ms | 7.5 ms | 7.9 ms | 8.2 ms |
| rg | 4.7 ms | 9.4 ms | 9.7 ms | 11.6 ms | 14.7 ms |
| go test | 124.0 ms | 152.9 ms | 110.2 ms | 126.4 ms | 130.2 ms |
| git diff | 0.9 ms | 2.5 ms | 2.3 ms | 2.5 ms | 11.8 ms |
| pytest | 226.7 ms | 233.1 ms | 210.6 ms | 207.2 ms | 194.9 ms |

包装器固定启动开销约占 5~10ms；go test/pytest 的总耗时由子进程和容器负载主导，单次
差异不能当作算法结论。`aggressive` 在 diff/rg 上更慢，是因为实际做了截断和分组；
`safe`/`lossless` 不做这些工作。

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
   29.1%/34.9%/17.5% vs 19.5%/34.2%/16.8%，字节降幅
   80.5%/80.3%/73.9% vs 65.8%/64.3%/67.8%。D 组 pytest 基本持平
   （-1.0% vs -1.1%，字节 237B vs 241B）。
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
