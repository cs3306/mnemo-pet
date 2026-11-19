# mnemo-pet

Linux（GNOME Shell）桌面宠物。**一个独立可用的宠物本体 + 插件系统**，大模型只是众多插件之一：不接任何模型，它照样会待机、眨眼、散步、睡觉、被拖动、响应点击、定时提醒、对通知做出反应；想要对话时再接上 [mnemo](https://github.com/cs3306/mnemo) 或任何 OpenAI 兼容后端即可。

- 只有**动作 + 文字气泡 + 提示音**，没有语音。
- 核心 `petd`（Go，仅标准库）跑在 Docker 里；GNOME Shell 扩展是只管渲染的“薄客户端”，扩展出问题不会拖垮桌面逻辑。
- 插件是独立进程，任何语言都能写（协议是按行 JSON）；也可以不写代码，只改 `rules.json`。

```
 GNOME Shell 扩展 ──render 协议(TCP/NDJSON)── petd（Docker）──plugin 协议── 插件
 渲染 · 拖动 · 气泡 · 菜单               状态机 · 事件总线 · 规则引擎       reminder · sysmon
 提示音 · 传感器(空闲/窗口/通知)          动作仲裁 · 插件托管 · HTTP API     brain-mnemo(可选) · 你的插件
```

> **验证状态（请先读）** 核心、规则引擎、插件和协议有单元测试（含 `-race`）并做过端到端测试；Shell 扩展只做了语法检查，**没有在真实 GNOME Shell 里运行过**（写这个项目的环境里没有 GNOME）；Docker 镜像没有实际构建过（`docker compose config` 校验通过）。第一次运行如遇问题，见文末[故障排查](#故障排查)。

## 快速开始

需要：Docker + Docker Compose、GNOME Shell 46–50（Ubuntu 24.04+/26.04 默认即是）。

```bash
git clone https://github.com/cs3306/mnemo-pet && cd mnemo-pet

# 1) 安装扩展（先做这一步：它会创建皮肤目录，compose 要挂载它）
./install.sh                    # 如核心在别的机器：--host 192.168.x.x --token 密钥

# 2) 启动核心
mkdir -p data
cp .env.example .env            # 改 PET_UID/PET_GID（id -u / id -g）、TZ
docker compose up -d --build    # 本地构建；镜像发布到 Docker Hub 后也可 docker compose pull && docker compose up -d

# 3) 启用扩展
#    Wayland（Ubuntu 默认）：注销再登录一次，让 GNOME 发现新扩展，然后
gnome-extensions enable mnemo-pet@cs3306
#    X11：Alt+F2 输入 r 回车，再执行上面的 enable
```

桌面底部出现一只小猫即成功。右键它打开菜单（番茄钟、隐藏 10 分钟）；拖起来再放下会落回底部；点它会有反应。

检查核心状态：

```bash
curl -s localhost:$PET_HTTP_PORT/api/state | python3 -m json.tool   # 端口见 .env
docker compose logs -f
```

### 不装 GNOME 也能开发/调试

```bash
make run     # 本机直接跑 petd（数据在 ./data，插件和皮肤取自仓库）
make fake    # 另一个终端：模拟渲染器，打印 petd 的指令，可输入 click / idle 700 / text 你好 …
```

## 接上大模型（可选）

不接也完全可用。接 mnemo 或任何 OpenAI 兼容服务，在 `.env` 里写：

```env
BRAIN_URL=http://mnemo:8080/v1      # 含 /v1；留空 = 不启用大脑
BRAIN_KEY=
BRAIN_MODEL=default
BRAIN_SEND_SYSTEM=0                 # mnemo 自带人设时设 0；通用模型保持 1
```

右键菜单会多出“聊天…”，输入文字，回复显示在气泡里。回复开头可带动作标签（如 `[happy] 你好呀`），标签决定动作并从气泡中去掉；`BRAIN_SEND_SYSTEM=1` 时会自动告诉模型可用的标签。如果 mnemo 在另一个 compose 项目里，取消 `docker-compose.yml` 末尾 `networks` 的注释并填上它所在的网络名，`BRAIN_URL` 就可以用容器名。

## 配置

### `data/config.json`（首次启动自动生成，改完重启核心）

| 字段 | 默认 | 说明 |
|---|---|---|
| `listen` / `http_listen` | `:23817` / `:23818`（仅兜底） | 渲染器+插件协议端口 / HTTP API 端口。**实际端口以 `.env` 的 `PET_PORT` / `PET_HTTP_PORT` 为准**，它们优先于这里（见下） |
| `token` | `""` | 共享密钥；也可用环境变量 `PET_TOKEN`（优先） |
| `skin` / `scale` | `default` / `1.4` | 默认皮肤名（没有任何换装计划匹配时穿它）/ 缩放 |
| `location` | `null` | `{"lat":53.9,"lon":-122.7}`；换装用到 `sunrise`/`sunset` 时必须填。时钟用系统时区（Docker 里设 `TZ`） |
| `gravity` / `floor_margin` | `true` / `0` | 放下后落回桌面底部 / 离底部的距离(px) |
| `bubble_ttl` | `6000` | 气泡默认时长(ms)，长文本会自动延长 |
| `idle_marks` | `[120, 600]` | 用户空闲多少秒时上报 `idle` 事件 |
| `sensors` | 见文件 | `focus`(聚焦应用) `focus_title`(含窗口标题) `notify`(通知) `notify_body`(含通知标题/正文) |
| `sounds` | 见文件 | 逻辑名 → 系统主题音名，或以 `/` 开头的音频文件绝对路径 |
| `behavior` | 见文件 | `wander` 是否散步、`min/max_gap_sec` 自主动作间隔、`walk_speed`、`sleep_after_sec`、`weights`(blink/wander/sit 权重) |
| `no_brain_replies` | 两句 | 没有大脑插件时，用户输入文字得到的回应 |
| `plugins.disabled` | `[]` | 不启动的插件名 |
| `plugins.env` | `{}` | 给某个插件额外的环境变量，如 `{"brain-mnemo": {"BRAIN_MODEL": "x"}}` |

隐私：`focus_title`、`notify_body` 默认关闭，默认只上报应用名。事件只发往你自己的 petd 和你启用的插件。

### 端口：不写死，全在 `.env`

`.env` 里的 `PET_PORT`（渲染器/插件协议）和 `PET_HTTP_PORT`（HTTP API）是唯一的真相来源。第一次执行 `make run`、`make env` 或 `./install.sh` 时会自动生成一对 20000–32000 之间的随机空闲端口（低于 Linux 临时端口范围，不会和对外连接撞车）。想换就直接改 `.env`，然后：

- 本机运行：重启 `make run`（Makefile 自动读 `.env`）；
- Docker：`docker compose up -d`（容器内外用同一个端口号）；
- 扩展：重新执行 `./install.sh`（会把新端口写进 `extension.json`，**改端口后扩展要重新连接，通常只需重新启用扩展**）；
- 插件、`fake_render` 等工具读同名环境变量，托管插件由核心自动注入。

优先级：`PET_LISTEN`/`PET_HTTP_LISTEN`（完整地址）＞ `PET_PORT`/`PET_HTTP_PORT` ＞ `config.json`。所以旧的 `data/config.json` 里留着老端口也不会生效。

### `~/.config/mnemo-pet/extension.json`（扩展侧，`install.sh` 生成）

`{"host": "127.0.0.1", "port": <.env 里的 PET_PORT>, "token": "", "render": "auto"}`。`render` 取 `auto`/`content`/`style`，决定帧图的显示方式，画面异常时可试 `style`。

## 规则引擎：不写代码就能扩展

`data/rules.json` 是一个规则列表，“事件 → 动作”。首次启动会写入默认文件（含两条默认关闭的示例规则），直接改它：

```json
[
  { "name": "click-react", "on": "click", "cooldown": 1,
    "do": { "anim": "react", "prio": 40 } },

  { "name": "welcome-back", "on": "active", "match": { "idle_sec>=": 600 },
    "do": { "anim": "wave", "say": "欢迎回来~" } },

  { "name": "lunch", "on": "time", "match": { "hm": "12:00" },
    "do": { "anim": "wave", "say": "中午啦，该吃饭了", "sound": "alarm", "prio": 70 } },

  { "name": "firefox", "on": "focus", "match": { "app~": "firefox" },
    "cooldown": 300, "prob": 0.3,
    "do": [ { "anim": "think", "hold": 4000 }, { "say_any": ["在查资料吗？", "又是网页~"] } ] }
]
```

- `on`：事件名，支持 `time.*` / `*` 这样的前缀通配。
- `match`：对事件数据的条件，键名后缀即运算符：无后缀=相等，`~` 包含(忽略大小写)，`!=`，`>=` `<=` `>` `<`。
- `cooldown`（秒）、`prob`（0–1）、`enabled: false` 可选。
- `channel`（可选，另有 `label`、`group`）：给规则加一个名字，它就会出现在右键菜单"说话内容"里，用户可以随时开关（`group` 默认"互动"；`enabled:false` 表示开关初始为关）。
- `do`：一个动作或动作数组；`say` 里可用 `{字段}` 引用事件数据（如 `{app}`），`say_any` 随机选一句。

**动作字段**：`anim`（动作名）、`loop`、`hold`(ms，锁定该动作多久)、`say`/`say_any`、`ttl`(气泡 ms)、`sound`、`move`（`{"to":"left|right|center|random"}` 或 `{"x":600}`，可加 `speed`）、`prio`。

**事件**

| 事件 | 来源 | 数据 |
|---|---|---|
| `click` `double_click` | 扩展 | `x,y` |
| `drag_start` `drag_end` | 扩展 | `x,y` |
| `idle` / `active` | 扩展 | `sec` / `idle_sec` |
| `focus` | 扩展 | `app,id[,title]` |
| `notify` | 扩展 | `app[,title,body]` |
| `user_text` | 扩展 | `text` |
| `menu.click` | 扩展 | `id`（`skin:*` 和 `channel:*` 由核心自己处理，不会转发给规则和插件） |
| `time` | 核心，每分钟 | `hour,minute,hm,weekday` |
| `plugin.<名>.<事件>` | 插件 | 自定义 |

**优先级仲裁**：多个来源同时想动时，高优先级的赢；低优先级的在锁定期间被丢弃。默认优先级：自主行为 10 < 睡觉 20 < 规则 40 < 插件 50 < HTTP 60 < 回复用户 65 < 拖动 100。气泡只在刚出现的 1.5 秒内受保护，之后新消息一律替换，不会卡住。

## 换装：手动 + 自动

右键宠物 → **皮肤**：列出所有已安装皮肤，点一个就固定穿它（手动，重启后保留）；选 **自动（按计划）** 回到按计划换。当前穿的那套前面有 `●`。API 见下文 `POST /api/skin`。

自动换装有两个来源，合并后按 `priority` 高的优先（同优先级按先后顺序）：

1. **皮肤自带计划**：在皮肤的 `pet.json` 里写 `when`（可选 `priority`，默认 10）。把皮肤文件夹丢进去就会在对应时间自己穿上——一套皮肤就是"一个文件夹，自带关联"。随仓库带了 `night`（22:00–07:00）、`christmas`、`halloween`。
2. **`data/wardrobe.json`**：集中管理，可以给任何皮肤加计划（默认优先级 50，所以能盖过皮肤自带的）。文件改动后一分钟内自动生效，不用重启。首次启动生成的示例都是关闭的。

```json
{
  "enabled": true,
  "entries": [
    { "name": "夜晚", "skin": "night", "priority": 60,
      "when": { "from": "sunset", "to": "sunrise" } },
    { "name": "周末上午", "skin": "default",
      "when": { "weekdays": [6, 7], "from": "09:00", "to": "12:00" } },
    { "name": "我的生日", "skin": "christmas", "priority": 90,
      "when": { "dates": ["06-15"] } }
  ]
}
```

`enabled:false` 彻底关闭自动换装；`"use_skin_schedules": false` 只忽略皮肤自带的计划。

**`when` 的条件**（不同类型之间是"且"，同一个列表里是"或"，至少写一种）：

| 字段 | 写法 |
|---|---|
| `from` / `to` | 必须成对。`"HH:MM"`、`"sunrise"`、`"sunset"`，可加偏移如 `"sunset+30m"`、`"sunrise-1h30m"`。`from` 比 `to` 晚表示跨午夜（`22:00`→`07:00`）。`to` 为开区间 |
| `dates` | `"MM-DD"`（每年）、`"YYYY-MM-DD"`（仅那天）、区间 `"12-20..01-05"`（可跨年） |
| `holidays` | 内置节日名，见下 |
| `weekdays` | `1`=周一 … `7`=周日 |
| `months` | `1`–`12` |

日出日落用标准公式按 `location` 和当天日期计算，误差几分钟；极昼/极夜时 `sunrise`→`sunset` 视为全天/全无。**没填 `location` 时，用到 sun 的条目会被跳过**（日志只提示一次），不会误触发。

**内置节日**：`new_year`(12-31..01-02) `valentine`(02-13..14) `labour_day`(05-01) `children_day`(06-01) `canada_day`(07-01) `halloween`(10-25..31) `christmas`(12-24..26) `national_day_cn`(10-01..07) `easter`(耶稣受难日到复活节星期一) `thanksgiving_ca`(10 月第 2 个周一) `thanksgiving_us`(11 月第 4 个周四) `lunar_new_year`(除夕到初六) `mid_autumn` `dragon_boat`。

> 农历节日（春节、中秋、端午）靠内置对照表：**春节、中秋覆盖 2026–2035，端午覆盖 2026–2032**，表外的年份不会匹配。2032 年端午的日期只有单一来源，可信度较低。表外年份（或你想核对时）请直接在 `dates` 里写明确日期，例如 `"2036-01-28..2036-02-03"`。

## 说话内容：想听什么自己选

右键宠物 → **说话内容**：按组列出所有"会主动开口"的来源，每项一个开关。关掉的来源整条动作（动画、气泡、提示音）都不会触发。选择保存在 `data/state.json`，重启、插件重连后仍然有效。

- **系统播报**（`sysmon`，**默认全关**，按需打开）：资源概况（综合）、CPU 使用率、内存、磁盘、温度、系统负载、开机时长、电池电量。各自有独立的间隔和台词，在 `data/plugins/sysmon/sysmon.json` 的 `reports` 里改（热加载）。开启后第一次播报在一个间隔之后。机器上没有的指标（比如没有电池、没有温度传感器）自动跳过。菜单里还有一个 **汇报资源概况**，随时立即说一次，不受开关影响。
- **系统告警**（默认开）：负载/内存/温度超过阈值才说。
- **提醒**（`reminder`）：每条提醒、番茄钟各一个开关；`reminders.json` 里的 `enabled` 现在只是开关的初始值，之后以菜单为准。
- **互动**：默认规则里的"久别回来打招呼""有新通知探头"。自己的规则加上 `channel`（可选 `label`、`group`）就会出现在菜单里，见规则一节。

`sysmon.json` 示例（想改成每 5 分钟报一次 CPU）：

```json
{ "reports": { "cpu": { "label": "CPU 使用率", "every_min": 5,
                        "say": "CPU 使用率 {cpu:.0f}%", "anim": "think" } } }
```

台词模板可用字段：`{cpu}` `{mem}` `{mem_used_gb}` `{mem_total_gb}` `{disk}` `{disk_free_gb}` `{temp}` `{load}` `{uptime}` `{battery}`；用到的字段本机读不到时这条播报会被跳过。`disk_paths`（默认 `["/data"]`）决定磁盘看哪个路径——在 Docker 里只能看到容器挂载的路径。若你是从旧版本升级，旧的 `sysmon.json` 没有 `reports`，会自动补上默认的。

## 内置插件

放在 `plugins/<名>/`，核心自动拉起并监督（崩溃后退避重启（1s、2s、4s…最长约 1 分钟）；正常退出码 0 不重启）。

| 插件 | 作用 | 配置文件（`data/plugins/<名>/`） |
|---|---|---|
| `reminder` | 每日定点提醒、间隔提醒、右键菜单里的番茄钟 | `reminders.json`（每项有 `id`/`label`，对应一个说话开关） |
| `sysmon` | 周期性汇报 CPU/内存/磁盘/温度等（可选，默认关）+ 过高时告警 | `sysmon.json` |
| `brain-mnemo` | 可选大脑，见上文；`BRAIN_URL` 为空时安静退出 | 环境变量 |

写自己的插件见 [docs/PLUGINS.md](docs/PLUGINS.md)；线路协议见 [docs/PROTOCOL.md](docs/PROTOCOL.md)。最小例子：

```python
import sys; sys.path.insert(0, "plugins/_sdk")
from petplugin import Plugin
p = Plugin("hello", subscribe=["click"])

@p.on("click")
def _(data):
    p.act(say="摸摸~", anim="happy", prio=60)

p.run()
```

## HTTP API（端口 `PET_HTTP_PORT`，设了 token 则需 `Authorization: Bearer …`）

| 接口 | 说明 |
|---|---|
| `GET /healthz` | 健康检查（免认证） |
| `GET /api/state` | 宠物状态、已连接插件、托管插件进程状态 |
| `POST /api/act` | 让宠物做动作：`curl -d '{"say":"hi","anim":"wave"}' localhost:$PET_HTTP_PORT/api/act` |
| `POST /api/event` | 注入事件（会触发规则和插件）：`{"name":"time","data":{"hm":"12:00"}}` |
| `GET /api/skins` | 当前皮肤、模式（`auto`/`manual`）、已安装皮肤列表 |
| `POST /api/skin` | `{"skin":"night"}` 固定某套皮肤；`{"auto":true}` 回到自动。皮肤不存在返回 404 |
| `GET /api/channels` | 所有说话开关及当前状态 |
| `POST /api/channel` | `{"id":"report.cpu","enabled":true}`；id 不存在返回 404 |
| `GET /api/stream` | SSE，实时查看发给渲染器的所有指令（调试用） |

shell 脚本、cron、CI 完成通知都可以直接 `curl /api/act`，不需要写插件。

## 皮肤

随仓库带 4 套皮肤（`tools/gen_default_skin.py` 程序生成，每套 12 组动作 58 帧）：`default`（麻糬）、`night`（睡帽）、`christmas`（圣诞帽）、`halloween`（女巫帽）。自定义皮肤放到 `~/.local/share/mnemo-pet/skins/<名>/`（扩展和容器共用这个目录），同名时这里的优先。出现在右键菜单的"皮肤"里，不需要改配置；要它按时间/节日自动穿，在它的 `pet.json` 加 `when`，或写进 `wardrobe.json`。格式见 [docs/SKINS.md](docs/SKINS.md)。

## 安全

- 端口只绑定在宿主的 `127.0.0.1`，协议没有加密。要让别的机器上的扩展连接，请用 SSH 隧道/WireGuard，并设置 `PET_TOKEN`。
- 托管插件以容器内的用户权限运行任意代码，只放你信任的插件。
- 扩展只上报应用名、空闲时长等；窗口标题和通知正文需要显式打开。

## 故障排查

- **看扩展日志**：`journalctl -f -o cat /usr/bin/gnome-shell | grep -i mnemo-pet`；Alt+F2 输入 `lg` 打开 Looking Glass 看错误。
- **`gnome-extensions enable` 提示找不到**：Wayland 下装完必须注销重登一次。
- **宠物出现但不动/没反应**：`curl localhost:$PET_HTTP_PORT/api/state` 看 `render_connected`；`docker compose logs` 看有没有 `bad token`；确认 `~/.config/mnemo-pet/extension.json` 的端口、token 与核心一致。
- **画面是空白或花的**：把 `extension.json` 的 `render` 改成 `style`（或 `content`）后重新启用扩展。
- **Docker 里 `data` 无法写入**：`.env` 里的 `PET_UID/PET_GID` 要与宿主用户一致，并先 `mkdir -p data`。
- **提醒时间不对**：设置 `.env` 的 `TZ`。
- **透明区域也会挡住鼠标**：宠物窗口是整个帧大小（128×缩放）的方框，目前没有做像素级穿透。
- **全屏时看不到宠物**：这是设计行为（全屏应用上不显示）。
- **Wayland 限制**：GNOME 不允许普通程序置顶/定位窗口，所以本项目做成 Shell 扩展，宠物画在合成器里；这也意味着它只适用于 GNOME Shell。

## 开发

```bash
make test        # go vet + go test -race + python/js 语法检查
make pack        # 打包扩展 zip
make skin        # 重新生成内置的 4 套皮肤
```

```
mnemo-pet/
├─ petd/               核心（Go，仅标准库）
│  └─ internal/        proto hub core rules skin wardrobe state config pluginhost api
├─ extension/          GNOME Shell 扩展（ESM，GNOME 46+）
├─ plugins/            _sdk reminder sysmon brain-mnemo（Python，仅标准库）
├─ skins/              default night christmas halloween（pet.json + PNG）
├─ tools/              fake_render.py  gen_default_skin.py
├─ docs/               PROTOCOL.md PLUGINS.md SKINS.md
├─ Dockerfile  docker-compose.yml  .env.example  install.sh  Makefile
└─ .github/workflows/  ci.yml  docker.yml
```

## 路线图

- 扩展：像素级点击穿透、多显示器、记住上次位置
- 皮肤：精灵表、骨骼动画（Live2D/Spine）作为可选渲染后端
- 更多插件：日历/任务提醒、git/CI 状态、天气

## License

MIT
