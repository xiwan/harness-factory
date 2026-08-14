---
name: game-qa
description: '网页游戏 QA 测试 — 自动试玩网页游戏、验证通关流程、采集证据、输出五维评分报告。当用户说"测试这个游戏"、"QA这个网页游戏"、"游戏能不能通关"、"game qa"、"试玩测试"等类似指令时使用。'
---

# Game QA — 网页游戏自动试玩与质量诊断

给定一个网页游戏 URL(可选源码 ZIP),像玩家一样试玩它,验证核心流程,输出评分、问题卡片和修改建议。

## 职责分层(必须遵守)

- **你(LLM)负责**:战略规划、源码建模、失败分析、评分补全、写报告。
- **qa-driver.mjs 负责**:逐帧操作浏览器、状态采集、截图、规则评分。
- **禁止**:你自己用 web_fetch 去"玩"游戏,或逐步指挥键鼠。一局游戏 = 一次 `play` 调用。
- **节奏纪律**:连续 5 次 shell_exec 会触发 harness 循环保护而中止会话。
  每个阶段(check-env / probe / 每轮 play / score)完成后,先用 artifact_write
  记录该阶段小结,再进行下一次 shell 调用。这既是证据留痕,也重置循环计数。
- **只走流程内命令**:shell_exec 只用于调用本 skill 的三个脚本。不要用
  grep/curl/head 之类做探索(引号内的管道符会被安全 parser 误判,白白消耗
  循环配额);文本检索一律用 fs_search,读文件用 fs_read。
- **不做源码考古**:除非用户主动提供了源码 ZIP,否则不要尝试获取或分析游戏
  源码(web_fetch 拉不了本地地址,游戏文件也在工作目录之外)。黑盒游戏的
  全部信息就来自 probe 和 play 的输出,这已足够写报告。

## Step 0: 环境检测(每次会话第一步,不可跳过)

```bash
bash skills/game-qa/scripts/check-env.sh
```

- `ready: true` → 静默继续,**不要**向用户提环境问题。
- `ready: false` → **停下来**,告诉用户缺什么、`setup.sh` 将安装什么
  (playwright npm 包装到 skill 目录 + chromium 浏览器 ~130MB 装到 ~/.cache),
  **得到用户确认后**才运行:
  ```bash
  bash skills/game-qa/scripts/setup.sh
  ```
  用户拒绝 → 降级为"仅源码静态分析"模式并明确告知能力受限。
  缺 node 本体 → 只能请用户自行安装 Node.js ≥ 18,不要尝试替用户装。

长时试玩前建议 bridge 侧设置 `HF_SHELL_TIMEOUT=300s`(shell 单命令默认 60s 会截断长局)。
未设置时,把 `--duration` 控制在 40 秒内、分多次 play 调用。

## Step 1: 探测游戏

```bash
node skills/game-qa/scripts/qa-driver.mjs probe --url <URL>
```

读取 `capability`:
- `full_test` — 游戏实现了完整 GameFlowAgentAPI(契约见 references/api-contract.md),可精确测试。
- `limited` — API 部分可用,列出 `missing` 方法,报告中建议开发者补齐。
- `black_box` — 无 API,降级为键鼠黑盒;告知用户评分可信度会降为 low,并把
  references/api-contract.md 的接入模板作为改进建议附在报告里。

若用户提供了源码 ZIP:解压到临时目录,fs_read 关键文件,建立游戏模型
(通关条件、关键节点、场景关系),用于指导 play 的类型与时长参数。

## Step 2: 多轮试玩(baseline → completion → verification)

```bash
node skills/game-qa/scripts/qa-driver.mjs play --url <URL> --type <TYPE> --duration 45 --out qa-evidence/attempt-1
```

TYPE ∈ survivor_like | arcade_shooter | platformer | puzzle_card | visual_novel(判型标准见 references/game-profiles.md)。

参数换算:driver 每步约 0.3 秒游戏时间,`--max-steps` 至少设为目标秒数 × 4
(如生存 45 秒 → `--duration 55 --max-steps 200`),否则会在达标前耗尽步数,
把"步数不足"误判成"游戏无法通关"。若某轮以 max_steps_or_timeout 结束且
elapsed 接近目标,下一轮先加大这两个参数再下结论。

编排状态机(你来执行,每轮之间用 artifact_write 记录小结):

1. **第 1 轮 baseline**:默认参数自然探索,了解游戏真实行为。
2. **第 2~N 轮 completion**:读上一轮的 `trace_file`(fs_read),分析失败原因
   (卡在哪个状态、哪些动作无效、stagnation 出现在哪),调整参数重试。最多 6 轮。
3. **成功后 verification**:同参数再跑 1 次确认可复现。
4. **停止条件**:连续 3 轮同样的失败签名、或累计超 15 分钟 → 停止,如实报告。

每轮结束必须 `artifact_write` 一份 `attempt-<n>-summary.md`(动作数、结局、失败签名、下轮计划)。

## Step 3: 评分与报告

```bash
node skills/game-qa/scripts/qa-driver.mjs score --trace qa-evidence/attempt-<best>/trace.json
```

driver 给出规则分,但 `ui_quality` 恒为空 —— 你必须 fs_read 几张关键截图路径确认存在,
结合 trace 中的状态流转给出 ui_quality 判断(截图本身你看不到,基于状态与 console 证据推断,
并在报告中注明该维度证据形式)。评分口径与 caps 规则见 references/scoring.md。

最终用 artifact_write 输出 `qa-report.md`。报告若写测试日期,先用 `date +%F` 取
真实日期(允许的 shell 命令),禁止凭训练记忆编造。结构:

1. **结论一句话**(能否通关 + 总分 + 可信度)
2. **测试概况**(capability 等级、轮次、每轮结局)
3. **五维评分表**(缺证据的维度写 N/A,不猜分)
4. **问题卡片**(每个问题:现象 → 证据[trace 步骤号+截图文件] → 建议修改 → 如何验证)
5. **接入建议**(black_box/limited 时附 API 接入模板指引)

## 原则

- 评分只来自证据,没测到 ≠ 没问题;证据覆盖不足时总分留空。
- Agent 自身操作失误不算游戏问题,不进问题卡片。
- 同一问题只报一次,绑定最佳一次证据。
- 报告面向 vibe coding 创作者:说人话,少术语。
