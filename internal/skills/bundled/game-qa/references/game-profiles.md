# 五类游戏类型档案

判型、标准动作集、关键场景与成败信号。源自 wai-play game_profiles 的方法论沉淀。

## 判型标准

| 类型 | 判断依据 | 典型游戏 |
|------|----------|----------|
| `survivor_like` | 生存循环 + 升级选择 + 波次敌人 | 吸血鬼幸存者 |
| `arcade_shooter` | 移动攻击 + 得分循环 + 失败重试 | 太空侵略者 |
| `platformer` | 重力 + 跳跃 + 落点 + 终点 | Mario |
| `puzzle_card` | 网格/回合 + 规则解题 + 关卡目标 | 推箱子、消消乐 |
| `visual_novel` | 对话推进 + 分支选择 + 多结局 | 文字冒险 |

别名归一:"肉鸽/roguelike/幸存者" → survivor_like;"射击/STG" → arcade_shooter;
"跳跃/横版" → platformer;"解谜/卡牌/消除" → puzzle_card;"剧情/AVG/文字" → visual_novel。

## 各类型验证重点

### survivor_like
- 动作:UP/DOWN/LEFT/RIGHT/ATTACK/CHOOSE_1..3(升级选择)
- 关键场景:early_core_loop(P0) → first_upgrade(P0) → enemy_pressure(P1) → low_hp_danger(P1) → boss_phase(P0) → ending_result(P0)
- 成功信号:elapsed ≥ target_duration 且 status.success;失败信号:hp ≤ 0
- 特有检查:升级弹窗出现时是否暂停战斗、选项是否可选、boss 是否真的出现

### arcade_shooter
- 动作:LEFT/RIGHT/ATTACK/UP/DOWN
- 关键场景:首波敌人、得分增长、被击中反馈、失败后重试入口
- 成功信号:score 增长 + 波次推进;失败信号:lives 耗尽且无重试入口(这是问题)
- 特有检查:攻击是否有可见弹道/命中反馈、受击是否有无敌帧或反馈

### platformer
- 动作:RIGHT/JUMP/LEFT/UP
- 关键场景:基础移动跳跃、第一个障碍、检查点、终点旗
- 成功信号:到达终点;失败信号:坠落/碰刺后未在检查点重生(问题)
- 特有检查:跳跃高度能否越过必经障碍(不可达 = 严重流程阻断,评分 cap 45)

### puzzle_card
- 动作:CHOOSE_1..3/CONFIRM(或点击坐标)
- 关键场景:规则展示、首次有效操作、错误操作反馈、关卡完成判定
- 成功信号:关卡目标达成;失败信号:无解状态且不能重置(问题)
- 特有检查:非法操作是否被拒绝且有提示、完成判定是否及时触发

### visual_novel
- 动作:CONFIRM(推进)/CHOOSE_1..2(分支)
- 关键场景:对话推进、第一个分支、至少一个结局、重玩入口
- 成功信号:到达任一结局;失败信号:对话卡死无法推进(严重问题)
- 特有检查:快速连点是否跳过关键选项、分支是否真的导向不同内容

## 通用风险信号(trace 分析时关注)

- HP/生命值单步骤降 > 50% —— 数值失衡或碰撞判定错误
- 同一动作连续 ≥ 5 次且状态零变化 —— 操作失效或卡死
- console error 出现在动作执行后 —— 该动作触发了运行时错误(高价值问题)
- observe() 返回值恒定不变 —— API 是假桥接,capability 应降级并在报告指出
