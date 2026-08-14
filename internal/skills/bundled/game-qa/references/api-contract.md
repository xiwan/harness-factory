# GameFlowAgentAPI 接口契约

游戏在 `window.GameFlowAgentAPI` 上暴露以下 10 个方法即可获得 full_test 能力等级。
契约源自 wai-play 项目的标准接口。

## 必需方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `getGameInfo()` | `() => {title, game_type, version, task_goal, target_duration?}` | 游戏元信息与目标 |
| `observe()` | `() => state` | 返回结构化状态(见下),**必须桥接真实游戏状态,禁止返回静态假数据** |
| `availableActions()` | `() => string[]` | 当前可执行的语义动作列表 |
| `step(action)` | `(string) => void` | 执行一个语义动作,桥接到真实游戏操作 |
| `evaluate()` | `() => {done, success, failed, progress?}` | 胜负判定 |
| `listTestScenarios()` | `() => scenario[]` | 关键节点场景清单(P0/P1/P2) |
| `checkScenarioPreconditions(id)` | `(string) => {satisfied, missing?}` | 场景前置条件检查 |
| `repairScenario(id)` | `(string) => bool` | 尝试修复前置条件 |
| `jumpToScenario(id)` | `(string) => bool` | 跳转到指定场景状态 |
| `evaluateScenario(id)` | `(string) => {passed, detail?}` | 场景通过判定 |

## observe() 状态结构(以 survivor_like 为例)

```json
{
  "player":   { "hp": 100, "max_hp": 100, "level": 1, "exp": 0 },
  "world":    { "elapsed": 0, "target_duration": 45, "enemy_count": 0, "current_phase": "early" },
  "combat":   { "kills": 0 },
  "upgrade":  { "is_selecting_upgrade": false, "options": [] },
  "boss":     { "exists": false, "hp": 0 },
  "status":   { "done": false, "success": false, "failed": false }
}
```

`status` 块是通用必需项 —— qa-driver 靠它判定单局结束。

## 场景(scenario)结构

```json
{
  "id": "boss_phase",
  "priority": "P0",
  "goal": "Boss 出现并可被击败",
  "required_preconditions": { "required_state": { "world.elapsed": ">=30" } },
  "repairable": true
}
```

## 接入模板骨架(给开发者的建议)

```js
(function () {
  window.GameFlowAgentAPI = {
    getGameInfo() { return { title: "...", game_type: "survivor_like", task_goal: "..." }; },
    observe() { return window.GameFlowIntegration.getGameState(); },   // 桥接真实状态
    availableActions() { return ["UP","DOWN","LEFT","RIGHT","ATTACK"]; },
    step(a) { window.GameFlowIntegration.applyAction(a); },            // 桥接真实操作
    evaluate() { const s = this.observe(); return s.status; },
    listTestScenarios() { return [/* P0/P1/P2 场景 */]; },
    checkScenarioPreconditions(id) { /* ... */ },
    repairScenario(id) { /* ... */ },
    jumpToScenario(id) { /* ... */ },
    evaluateScenario(id) { /* ... */ },
  };
})();
```

黑盒模式下 qa-driver 的键位约定(游戏若支持这些默认键位,黑盒测试质量更高):
方向键移动、`J` 攻击、`Space` 跳跃、`Enter` 确认、`R` 重开、`1/2/3` 选项。
