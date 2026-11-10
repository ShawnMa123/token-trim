# 配置

`rtk-go` 只读取一个用户级 JSON 配置，不扫描仓库本地配置，也不执行配置中的命令。

## 路径

默认：

- Windows: `%APPDATA%\rtk-go\config.json`
- macOS: `~/Library/Application Support/rtk-go/config.json`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/rtk-go/config.json`

覆盖路径：

```sh
RTK_GO_CONFIG=/absolute/path/config.json rtk-go config show
```

文件不存在时使用内置默认值；文件存在但损坏/非法时，`exec` 回退原文，管理命令返回 1。

## 完整示例

```json
{
  "schema_version": 1,
  "profile": "aggressive",
  "storage": {
    "enabled": true,
    "data_dir": "/home/me/.cache/rtk-go",
    "retention_hours": 24,
    "total_mib": 256
  },
  "capture": {
    "memory_mib": 8,
    "stream_threshold_ms": 1500,
    "test_threshold_ms": 30000
  },
  "stats": {
    "enabled": true,
    "retention_days": 30
  },
  "codex": {
    "auto_rewrite": false,
    "rewrite_tests": false
  }
}
```

Windows 示例：

```json
{
  "schema_version": 1,
  "profile": "balanced",
  "storage": {
    "enabled": true,
    "data_dir": "C:\\Users\\me\\AppData\\Local\\rtk-go"
  }
}
```

`storage.data_dir` 显式设置时必须是绝对路径；省略时使用用户缓存目录。

## 字段

| 字段 | 默认 | 约束 | 说明 |
|---|---:|---|---|
| `schema_version` | `1` | 必须为 `1` | 配置契约版本 |
| `profile` | `aggressive` | `safe`/`balanced`/`aggressive` | 默认过滤级别 |
| `storage.enabled` | `true` | bool | 是否允许原文落盘 |
| `storage.data_dir` | 用户缓存 | 绝对路径 | recovery 根目录 |
| `storage.retention_hours` | `24` | 1..168 | 原文保留时间 |
| `storage.total_mib` | `256` | 64..4096 | 受管对象配额 |
| `capture.memory_mib` | `8` | 1..32 | 单流内存捕获上限 |
| `capture.stream_threshold_ms` | `1500` | 100..5000 | 查询类命令转流式窗口 |
| `capture.test_threshold_ms` | `30000` | 1500..120000 | 显式测试命令窗口 |
| `stats.enabled` | `true` | bool | 本地统计 |
| `stats.retention_days` | `30` | 1..90 | 统计保留天数 |
| `codex.auto_rewrite` | `false` | bool | 低层重写 API 默认关闭 |
| `codex.rewrite_tests` | `false` | bool | 是否允许测试命令参与重写分析 |

未知键不会导致失败，会由 `config show`/`doctor` 给出警告；非法值会导致配置无效并回退原文。

## 环境变量

| 变量 | 作用 |
|---|---|
| `RTK_GO_CONFIG` | 指定配置文件 |
| `RTK_GO_DISABLE=1` | 紧急禁用过滤：全部原文，管理命令也按禁用处理 |
| `RTK_GO_NO_STORE=1` | 禁止原文存储；有损过滤器自动回退原文 |

`RTK_GO_DISABLE` 和 `RTK_GO_NO_STORE` 只能由环境开启，wrapper 参数不能关闭它们。

## 优先级

```text
内置默认值 < 配置文件 < 环境开关 < exec 的 --profile/--raw/--no-store
```

`--raw` 总是原文；`--no-store` 禁止落盘，因此有损过滤不可用；它不会让 `RTK_GO_DISABLE=1`
失效。

## 数据目录

默认 recovery 路径：

- Windows: `%LOCALAPPDATA%\rtk-go`
- macOS: `~/Library/Caches/rtk-go`
- Linux: `${XDG_CACHE_HOME:-~/.cache}/rtk-go`

目录下只保存受管对象。`clean --expired` 不删除目录外文件；`--dry-run` 只报告。

## 配置诊断

```sh
rtk-go config show
rtk-go config show --format json
rtk-go doctor --format json
```

`doctor` 会检查 schema、路径、权限/ACL 和剩余空间；`--probe` 只写一个本地 marker 验证可写性，
不会执行用户命令。
