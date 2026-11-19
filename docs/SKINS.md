# 皮肤

皮肤是一个目录，包含 `pet.json` 和若干 PNG 帧图（建议透明背景、统一尺寸）。

```
~/.local/share/mnemo-pet/skins/<名字>/
  pet.json
  idle_0.png idle_1.png ...
  walk_0.png ...
```

放进去后，一分钟内它就会出现在右键菜单 **皮肤** 里，点一下即可穿上，不用改配置、不用重启。这个目录同时被扩展（宿主）和核心（容器内 `/data/skins`，只读）使用，compose 已经挂好。随仓库带的皮肤在 `skins/`，同名时这个目录里的优先。

`data/config.json` 的 `"skin"` 现在只是"默认皮肤"：没有任何换装计划匹配、也没有手动固定时穿它。

## 让皮肤按时间/节日自动穿

在 `pet.json` 里加 `when`（可选 `priority`，默认 10），皮肤就带着自己的计划：

```json
{ "name": "sakura", "label": "樱花",
  "when": { "dates": ["03-25..04-10"], "from": "sunrise", "to": "sunset" },
  "priority": 30, "size": [128, 128], "anims": { "...": {} } }
```

`label` 是菜单里显示的名字（可选）。`when` 的完整语法（时间段、日出日落、日期、节日、星期、月份）见 README 的"换装"一节。写错的 `when` 会被忽略并在核心日志里警告，皮肤本身仍可手动选。也可以不碰皮肤文件，集中写在 `data/wardrobe.json`。

## pet.json

```json
{
  "name": "mochi",
  "label": "麻糬",
  "size": [128, 128],
  "bubble_anchor": [0.5, 0.27],
  "anims": {
    "idle":  { "frames": ["idle_0.png", "idle_1.png"], "fps": 3, "loop": true },
    "wave":  { "frames": ["wave_0.png", "wave_1.png"], "fps": 6, "loop": false }
  }
}
```

- `size`：帧像素尺寸；实际显示大小 = `size × config.scale`。
- `bubble_anchor`：气泡尖对准帧内的哪个位置（相对比例 x,y），一般取角色头顶。
- `anims.<名>.loop`：`false` 的动作播完会通知核心，核心随后切回 `idle`。
- 帧图默认**朝右**；向左走时扩展会水平翻转。

## 动作名

核心和默认规则用到这些名字：`idle`（必需）、`blink`、`walk`、`sit`、`sleep`、`wave`、`happy`、`think`、`apologize`、`drag`、`react`、`notify`。缺哪个，核心就跳过哪个（例如没有 `walk` 就不会散步，没有 `sleep` 就不会睡觉）。你也可以加自己的动作名，在规则或插件里引用。

## 默认皮肤

`tools/gen_default_skin.py`（需要 Pillow）程序化地画出 4 套皮肤（`default`、`night`、`christmas`、`halloween`）：同一套动作加不同配色和头饰。想做自己的换装，复制 `THEMES` 里的一项（配色 + 一个画配饰的函数）再 `make skin` 即可，也是一个很好的起点。
