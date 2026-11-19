#!/usr/bin/env python3
"""reminder: daily/interval reminders and a pomodoro timer.

Config: $PET_DATA_DIR/reminders.json (created on first run, re-read every minute).
"""
import os
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "_sdk"))
from petplugin import Plugin, log  # noqa: E402

GROUP = "提醒"

# Every item may have "id" and "label". Each becomes a switch under "说话内容 → 提醒"
# (channel reminder.<id>), so the user can mute single reminders from the menu.
# "enabled" here is the default state of that switch.
DEFAULT = {
    "daily": [
        {"id": "lunch", "label": "午饭提醒 12:00", "enabled": False, "at": "12:00", "say": "中午啦，该吃饭了", "anim": "wave", "sound": "alarm"},
        {"id": "bedtime", "label": "睡觉提醒 23:30", "enabled": False, "at": "23:30", "say": "该睡觉啦", "anim": "sit", "sound": "alarm"},
    ],
    "interval": [
        {"id": "stretch", "label": "久坐活动提醒（每 50 分钟）", "enabled": False, "every_min": 50, "say": "坐太久了，起来活动一下吧", "anim": "wave", "sound": "notify"},
    ],
    "pomodoro": {"work_min": 25, "break_min": 5},
}

p = Plugin("reminder", subscribe=["time", "menu.click"])
cfg = p.load_json("reminders.json", DEFAULT)
_state = {"mtime": 0.0, "pomo": None, "last_interval": {}}


def item_id(kind, i, item):
    return item.get("id") or (f"{kind}-{item.get('at')}" if kind == "daily" else f"{kind}-{i}")


def declare_channels():
    """Declare one channel per reminder. The JSON "enabled" is only the default;
    petd remembers the user's menu choice."""
    for kind in ("daily", "interval"):
        for i, item in enumerate(cfg.get(kind, [])):
            default_label = item.get("say", item_id(kind, i, item))[:20]
            p.channel("reminder." + item_id(kind, i, item), item.get("label", default_label),
                      GROUP, item.get("enabled", True))
    p.channel("reminder.pomodoro", "番茄钟播报", GROUP, True)


def reload_cfg():
    global cfg
    path = os.path.join(p.data_dir, "reminders.json")
    try:
        m = os.path.getmtime(path)
        if m != _state["mtime"]:
            _state["mtime"] = m
            cfg = p.load_json("reminders.json", DEFAULT)
            log("[reminder] config loaded")
            declare_channels()
    except (OSError, ValueError) as e:
        log(f"[reminder] bad config, keeping previous: {e}")


def fire(item, kind, i, prio=70):
    kw = {k: item[k] for k in ("say", "anim", "sound", "hold", "ttl") if k in item}
    p.act(prio=prio, channel="reminder." + item_id(kind, i, item), **kw)


def pomo_label():
    st = _state["pomo"]
    if not st:
        return "番茄钟：开始"
    left = max(0, int((st["until"] - time.time()) / 60) + 1)
    return f"番茄钟：{'专注' if st['phase'] == 'work' else '休息'}中（{left} 分）· 点击停止"


@p.on_connect
def _connected():
    declare_channels()
    p.menu_add("pomodoro", pomo_label())


def pomo_start():
    pc = cfg.get("pomodoro", {})
    _state["pomo"] = {"phase": "work", "until": time.time() + pc.get("work_min", 25) * 60}
    p.act(say=f"开始专注 {pc.get('work_min', 25)} 分钟！", anim="happy", sound="pop", prio=65, channel="reminder.pomodoro")
    p.menu_add("pomodoro", pomo_label())


def pomo_stop():
    _state["pomo"] = None
    p.act(say="番茄钟已停止", anim="sit", prio=65, hold=3000, channel="reminder.pomodoro")
    p.menu_add("pomodoro", pomo_label())


def pomo_tick():
    st = _state["pomo"]
    if not st:
        return
    pc = cfg.get("pomodoro", {})
    if time.time() >= st["until"]:
        if st["phase"] == "work":
            _state["pomo"] = {"phase": "break", "until": time.time() + pc.get("break_min", 5) * 60}
            p.act(say="专注结束，休息一下吧~", anim="wave", sound="alarm", prio=75, channel="reminder.pomodoro")
        else:
            _state["pomo"] = {"phase": "work", "until": time.time() + pc.get("work_min", 25) * 60}
            p.act(say="休息结束，继续加油！", anim="happy", sound="alarm", prio=75, channel="reminder.pomodoro")
    p.menu_add("pomodoro", pomo_label())


@p.on("menu.click")
def _menu(data):
    if data.get("id") != "pomodoro":
        return
    pomo_stop() if _state["pomo"] else pomo_start()


@p.on("time")
def _minute(data):
    reload_cfg()
    hm = data.get("hm")
    for i, item in enumerate(cfg.get("daily", [])):
        if item.get("at") == hm:
            fire(item, "daily", i)
    now = time.time()
    for i, item in enumerate(cfg.get("interval", [])):
        every = float(item.get("every_min", 60)) * 60
        last = _state["last_interval"].setdefault(i, now)
        if now - last >= every - 1:
            _state["last_interval"][i] = now
            fire(item, "interval", i, prio=60)
    pomo_tick()


if __name__ == "__main__":
    p.run()
