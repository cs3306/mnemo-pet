// Desktop sensors: idle time, focused application, incoming notifications.
// Each one is optional and reports through io.event().
import GLib from 'gi://GLib';
import Shell from 'gi://Shell';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';

export class Sensors {
    constructor(io) {
        this._io = io;
        this._signals = [];
        this._idleIds = [];
        this._activeId = 0;
        this._lastMark = 0;
        this._focusTimer = 0;
        this._monitor = null;
    }

    configure(cfg) {
        this.stop();
        const s = cfg.sensors ?? {};
        this._cfg = s;
        this._startIdle(cfg.idle_marks ?? []);
        if (s.focus)
            this._startFocus();
        if (s.notify)
            this._startNotify();
    }

    stop() {
        for (const [obj, id] of this._signals) {
            try {
                obj.disconnect(id);
            } catch (e) { /* gone */ }
        }
        this._signals = [];
        if (this._monitor) {
            for (const id of this._idleIds)
                this._monitor.remove_watch(id);
            if (this._activeId)
                this._monitor.remove_watch(this._activeId);
        }
        this._idleIds = [];
        this._activeId = 0;
        this._lastMark = 0;
        if (this._focusTimer) {
            GLib.source_remove(this._focusTimer);
            this._focusTimer = 0;
        }
    }

    _connect(obj, name, fn) {
        this._signals.push([obj, obj.connect(name, fn)]);
    }

    // ----------------------------------------------------------------- idle
    _startIdle(marks) {
        if (marks.length === 0)
            return;
        try {
            this._monitor = global.backend.get_core_idle_monitor();
        } catch (e) {
            console.warn(`mnemo-pet: no idle monitor: ${e}`);
            return;
        }
        for (const sec of marks) {
            this._idleIds.push(this._monitor.add_idle_watch(sec * 1000, () => {
                this._lastMark = Math.max(this._lastMark, sec);
                this._io.event('idle', {sec});
                this._armActive();
            }));
        }
    }

    _armActive() {
        if (this._activeId || !this._monitor)
            return;
        this._activeId = this._monitor.add_user_active_watch(() => {
            this._activeId = 0;
            const idle = this._lastMark;
            this._lastMark = 0;
            this._io.event('active', {idle_sec: idle});
        });
    }

    // ---------------------------------------------------------------- focus
    _startFocus() {
        const tracker = Shell.WindowTracker.get_default();
        this._connect(tracker, 'notify::focus-app', () => {
            if (this._focusTimer)
                GLib.source_remove(this._focusTimer);
            // Debounce alt-tab storms.
            this._focusTimer = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 400, () => {
                this._focusTimer = 0;
                const app = tracker.focus_app;
                if (!app)
                    return GLib.SOURCE_REMOVE;
                const data = {app: app.get_name() ?? '', id: app.get_id() ?? ''};
                if (this._cfg.focus_title)
                    data.title = global.display.focus_window?.get_title() ?? '';
                this._io.event('focus', data);
                return GLib.SOURCE_REMOVE;
            });
        });
    }

    // --------------------------------------------------------- notifications
    _startNotify() {
        const tray = Main.messageTray;
        const hook = source => {
            try {
                this._connect(source, 'notification-added', (_s, n) => {
                    const data = {app: source.title ?? ''};
                    if (this._cfg.notify_body) {
                        data.title = n.title ?? '';
                        data.body = n.body ?? '';
                    }
                    this._io.event('notify', data);
                });
            } catch (e) {
                console.warn(`mnemo-pet: cannot watch a notification source: ${e}`);
            }
        };
        try {
            for (const s of tray.getSources())
                hook(s);
            this._connect(tray, 'source-added', (_t, s) => hook(s));
        } catch (e) {
            console.warn(`mnemo-pet: notification sensor unavailable on this shell version: ${e}`);
        }
    }
}
