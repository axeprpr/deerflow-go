# CLI 多文件场景回归（2026-04-17）

## 范围
- 对比对象：`tacli` / `openclaw` / `opencode`
- 场景目标：验证“多文件 + 长对话后回忆关键字段”是否退化
- 评分口径：`turn1_extract` 与 `turn3_recall` 的 10 字段命中率

## 脚本
- 主脚本：`scripts/cli_multifile_case_regression.py`
- 结果文件：
  - `/tmp/cli_case_regression_v1.json`
  - `/tmp/cli_case_regression_latefacts_noise180.json`
  - `/tmp/cli_case_regression_latefacts_noise350.json`
  - `/tmp/cli_case_regression_nearmiss.json`
  - `/tmp/cli_case_regression_irrelevant.json`

## Case 回归结果（noise=70）

| Tool | Case | turn1 | turn3 | degrade | total_elapsed |
|---|---|---:|---:|---:|---:|
| tacli | late_facts | 10/10 | 10/10 | 0 | 75.78s |
| openclaw | late_facts | 10/10 | 10/10 | 0 | 182.69s |
| opencode | late_facts | 10/10 | 10/10 | 0 | 50.10s |
| tacli | conflict_override | 10/10 | 10/10 | 0 | 97.09s |
| openclaw | conflict_override | 10/10 | 10/10 | 0 | 91.47s |
| opencode | conflict_override | 10/10 | 10/10 | 0 | 49.82s |
| tacli | many_files | 10/10 | 10/10 | 0 | 102.71s |
| openclaw | many_files | 10/10 | 10/10 | 0 | 93.43s |
| opencode | many_files | 10/10 | 10/10 | 0 | 85.97s |

## 近似值干扰补充（near_miss_numbers, noise=80）

| Tool | turn1 | turn3 | degrade | total_elapsed |
|---|---:|---:|---:|---:|
| tacli | 10/10 | 10/10 | 0 | 109.62s |
| openclaw | 10/10 | 10/10 | 0 | 114.72s |
| opencode | 10/10 | 10/10 | 0 | 69.77s |

## 上下文污染补充（irrelevant_interleave, noise=90）

| Tool | turn1 | turn3 | degrade | total_elapsed |
|---|---:|---:|---:|---:|
| tacli | 10/10 | 10/10 | 0 | 93.99s |
| openclaw | 10/10 | 10/10 | 0 | 81.46s |
| opencode | 10/10 | 10/10 | 0 | 106.70s |

## 临界点补充（late_facts）

### noise=180
| Tool | turn1 | turn3 | degrade | total_elapsed |
|---|---:|---:|---:|---:|
| tacli | 10/10 | 10/10 | 0 | 115.34s |
| openclaw | 10/10 | 10/10 | 0 | 66.54s |
| opencode | 10/10 | 10/10 | 0 | 56.61s |

### noise=350
| Tool | turn1 | turn3 | degrade | total_elapsed |
|---|---:|---:|---:|---:|
| tacli | 10/10 | 10/10 | 0 | 122.35s |
| openclaw | 10/10 | 10/10 | 0 | 206.77s |
| opencode | 10/10 | 10/10 | 0 | 110.11s |

## 关键观察
- 本轮用例下三者均未出现“关键字段回忆退化”（degrade=0）。
- `near_miss_numbers` 场景也未出现近似数值误读（如 `1200000` 误读为 `120000`）。
- `irrelevant_interleave` 场景下（插入与投标无关长输出）也未观察到字段召回下降。
- 差异主要体现在耗时抖动：
  - `openclaw` 在长评审轮次更容易拉长；
  - `opencode` 整体较快且稳定；
  - `tacli` 在高噪声下保持稳定但中位耗时高于 `opencode`。
- 先前遇到的“参数过长/截断”属于测试构造问题（超长单行/命令行参数塞文件全文），已通过“真实文件读取”方式规避。

## 下一轮建议（更贴近客户反馈）
- 加入“多文件互相引用 + 页码定位 + 表格字段冲突”场景。
- 加入“回合间插入无关任务”场景，测试摘要污染。
- 加入“真假表格并存（附件 A/B 值冲突）且规则只在页脚声明”场景。
