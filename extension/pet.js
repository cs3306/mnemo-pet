// The pet itself: actor, animation player, walking, dragging, bubble, menu, chat entry.
import Clutter from 'gi://Clutter';
import GLib from 'gi://GLib';
import Pango from 'gi://Pango';
import Shell from 'gi://Shell';
import St from 'gi://St';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import {loadFrames, loadManifest} from './skin.js';

const DRAG_THRESHOLD = 5;
const TICK_MS = 16;

const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));

export class Pet {
    /**
     * @param {string} extPath extension directory
     * @param {object} io {event(name,data), renderMode}
     */
    constructor(extPath, io) {
        this._extPath = extPath;
        this._io = io;
        this._skin = null;
        this._scale = 1.4;
        this._gravity = true;
        this._floorMargin = 0;
        this._walkSpeed = 70;
        this._w = 128;
        this._h = 128;
        this._anchor = [0.5, 0.28];
        this._anim = null;
        this._animId = 0;
        this._walk = null;
        this._walkId = 0;
        this._press = null;
        this._capId = 0;
        this._bubbleTimer = 0;
        this._hideTimer = 0;
        this._menuItems = [];
        this._chrome = [];
        this._signals = [];
        this._entryOpen = false;
        this._frameWidgets = [];
        this._curWidget = null;
        this._lastPress = null;

        try {
            this._build();
        } catch (e) {
            // A half-built pet must not stay on screen (enable() would otherwise
            // leak its actors every time it is retried).
            try {
                this.destroy();
            } catch (e2) { /* best effort */ }
            throw e;
        }
    }

    // ---------------------------------------------------------------- build
    _addChrome(actor, params) {
        try {
            Main.layoutManager.addChrome(actor, params);
        } catch (e) {
            // Newer GNOME Shell versions dropped some addChrome options
            // (e.g. "affectsInputRegion"); retry with only the ones it still knows.
            const {affectsInputRegion, ...rest} = params ?? {};
            if (affectsInputRegion === false)
                actor.reactive = false;
            console.warn(`mnemo-pet: addChrome retry without affectsInputRegion (${e.message})`);
            Main.layoutManager.addChrome(actor, rest);
        }
        this._chrome.push(actor);
    }

    _connect(obj, name, fn) {
        this._signals.push([obj, obj.connect(name, fn)]);
    }

    _build() {
        this.actor = new St.Widget({
            name: 'mnemo-pet',
            reactive: true,
            can_focus: false,
            track_hover: false,
            visible: true,
        });
        this.actor.set_pivot_point(0.5, 0.5);
        this._addChrome(this.actor, {affectsInputRegion: true, affectsStruts: false, trackFullscreen: true});
        this._connect(this.actor, 'button-press-event', (_a, ev) => this._onPress(ev));
        this._connect(this.actor, 'notify::x', () => this._placeOverlays());
        this._connect(this.actor, 'notify::y', () => this._placeOverlays());

        // Speech bubble (click-through).
        this._bubble = new St.BoxLayout({style_class: 'mnemo-pet-bubble', visible: false, reactive: false});
        this._bubbleLabel = new St.Label({style_class: 'mnemo-pet-bubble-text'});
        const ct = this._bubbleLabel.clutter_text;
        ct.set_line_wrap(true);
        ct.set_line_wrap_mode(Pango.WrapMode.WORD_CHAR);
        ct.set_ellipsize(Pango.EllipsizeMode.NONE);
        this._bubble.add_child(this._bubbleLabel);
        this._addChrome(this._bubble, {affectsInputRegion: false, affectsStruts: false, trackFullscreen: false});
        this._bubble.hide();

        // Chat entry.
        this._entryBox = new St.BoxLayout({style_class: 'mnemo-pet-bubble', visible: false, reactive: true});
        this._entry = new St.Entry({style_class: 'mnemo-pet-entry', hint_text: '说点什么…', can_focus: true, track_hover: true});
        this._entryBox.add_child(this._entry);
        this._addChrome(this._entryBox, {affectsInputRegion: true, affectsStruts: false, trackFullscreen: false});
        this._entryBox.hide();
        this._entry.clutter_text.connect('activate', () => {
            const text = this._entry.get_text().trim();
            this.closeEntry();
            if (text)
                this._io.event('user_text', {text});
        });
        this._entry.clutter_text.connect('key-press-event', (_a, ev) => {
            if (ev.get_key_symbol() === Clutter.KEY_Escape) {
                this.closeEntry();
                return Clutter.EVENT_STOP;
            }
            return Clutter.EVENT_PROPAGATE;
        });

        // Right-click menu.
        this._menuManager = new PopupMenu.PopupMenuManager(this.actor);
        this._menu = new PopupMenu.PopupMenu(this.actor, 0.5, St.Side.BOTTOM);
        this._menuManager.addMenu(this._menu);
        Main.uiGroup.add_child(this._menu.actor);
        this._menu.actor.hide();
        this._rebuildMenu();

        this._connect(Main.layoutManager, 'monitors-changed', () => this.keepOnScreen());
    }

    destroy() {
        this._clearFrameWidgets();
        for (const f of [() => this._stopAnim(), () => this._stopWalk(), () => this._endCapture(), () => this.closeEntry()]) {
            try {
                f();
            } catch (e) { /* may run on a half-built pet */ }
        }
        for (const id of ['_bubbleTimer', '_hideTimer']) {
            if (this[id]) {
                GLib.source_remove(this[id]);
                this[id] = 0;
            }
        }
        for (const [obj, id] of this._signals) {
            try {
                obj.disconnect(id);
            } catch (e) { /* already gone */ }
        }
        this._signals = [];
        try {
            this._menu.destroy();
        } catch (e) { /* ignore */ }
        for (const a of this._chrome) {
            try {
                Main.layoutManager.removeChrome(a);
                a.destroy();
            } catch (e) { /* ignore */ }
        }
        this._chrome = [];
    }

    // ------------------------------------------------------------- geometry
    // primaryMonitor can be null while no monitor exists yet (headless machine,
    // remote-desktop virtual monitor not created yet). Never return null: fall
    // back to the first monitor, then to the stage size; 'monitors-changed'
    // re-places the pet once a real monitor shows up.
    _monitor() {
        const lm = Main.layoutManager;
        const m = lm.primaryMonitor ?? lm.monitors?.[0];
        if (m)
            return m;
        return {
            x: 0, y: 0, index: 0, synthetic: true,
            width: global.stage?.width || 1920,
            height: global.stage?.height || 1080,
        };
    }

    _workArea() {
        const m = this._monitor();
        if (!m.synthetic) {
            try {
                return Main.layoutManager.getWorkAreaForMonitor(m.index);
            } catch (e) { /* fall through to the monitor rectangle */ }
        }
        return {x: m.x, y: m.y, width: m.width, height: m.height};
    }

    _floorY() {
        const wa = this._workArea();
        return wa.y + wa.height - this._h - this._floorMargin;
    }

    /** Put the pet on the floor, near the right edge, on first start. */
    placeInitially() {
        const wa = this._workArea();
        this.actor.set_position(Math.round(wa.x + wa.width * 0.75 - this._w / 2), Math.round(this._floorY()));
    }

    keepOnScreen() {
        const m = this._monitor();
        const x = clamp(this.actor.x, m.x, m.x + m.width - this._w);
        const y = this._gravity ? this._floorY() : clamp(this.actor.y, m.y, m.y + m.height - this._h);
        this.actor.set_position(Math.round(x), Math.round(y));
    }

    // ----------------------------------------------------------------- skin
    configure(cfg) {
        this._gravity = cfg.gravity !== false;
        this._floorMargin = cfg.floor_margin ?? 0;
        this._walkSpeed = cfg.walk_speed ?? 70;
        const name = cfg.skin ?? 'default';
        const scale = cfg.scale ?? 1.4;
        if (!this._skin || this._skinName !== name || this._scale !== scale)
            this.loadSkin(name, scale);
    }

    loadSkin(name, scale) {
        const found = loadManifest(name, this._extPath) ?? (name !== 'default' ? loadManifest('default', this._extPath) : null);
        if (!found) {
            console.error(`mnemo-pet: skin "${name}" not found`);
            return false;
        }
        const {manifest, dir} = found;
        const loaded = loadFrames(manifest, dir, this._io.renderMode ?? 'auto');
        this._stopAnim();
        this._clearFrameWidgets();
        this._skin = loaded;
        this._skinName = name;
        this._scale = scale;
        this._w = Math.round((manifest.size?.[0] ?? 128) * scale);
        this._h = Math.round((manifest.size?.[1] ?? 128) * scale);
        this._anchor = manifest.bubble_anchor ?? [0.5, 0.28];
        this.actor.set_size(this._w, this._h);
        if (this._skin.mode === 'content')
            this.actor.content_gravity = Clutter.ContentGravity.RESIZE_ASPECT;
        else
            this._buildFrameWidgets();
        this.keepOnScreen();
        this.play('idle', true);
        return true;
    }

    // ------------------------------------------------------------ animation
    _setFrame(frame) {
        if (this._skin.mode === 'content') {
            this.actor.set_content(frame.content);
        } else if (frame.widget && frame.widget !== this._curWidget) {
            // Re-styling one widget on every frame is slow; showing/hiding
            // pre-styled widgets is cheap.
            frame.widget.show();
            this._curWidget?.hide();
            this._curWidget = frame.widget;
        }
    }

    _buildFrameWidgets() {
        const style = f => `${f.style} background-size: ${this._w}px ${this._h}px;`;
        for (const anim of Object.values(this._skin.anims)) {
            for (const f of anim.frames) {
                const w = new St.Widget({style: style(f), width: this._w, height: this._h, visible: false, reactive: false});
                this.actor.add_child(w);
                f.widget = w;
                this._frameWidgets.push(w);
            }
        }
    }

    _clearFrameWidgets() {
        for (const w of this._frameWidgets) {
            try {
                w.destroy();
            } catch (e) { /* ignore */ }
        }
        this._frameWidgets = [];
        this._curWidget = null;
    }

    _stopAnim() {
        if (this._animId) {
            GLib.source_remove(this._animId);
            this._animId = 0;
        }
        this._anim = null;
    }

    play(name, loop = true) {
        if (!this._skin)
            return;
        let a = this._skin.anims[name];
        if (!a) {
            name = 'idle';
            a = this._skin.anims.idle;
            loop = true;
            if (!a)
                return;
        }
        this._stopAnim();
        const anim = this._anim = {name, loop, frames: a.frames, i: 0};
        this._setFrame(anim.frames[0]);
        if (anim.frames.length < 2 && loop)
            return;
        const interval = Math.max(16, Math.round(1000 / a.fps));
        this._animId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, interval, () => {
            if (this._anim !== anim)
                return GLib.SOURCE_REMOVE;
            anim.i++;
            if (anim.i >= anim.frames.length) {
                if (anim.loop) {
                    anim.i = 0;
                } else {
                    this._animId = 0;
                    this._io.event('anim_end', {name: anim.name});
                    return GLib.SOURCE_REMOVE;
                }
            }
            this._setFrame(anim.frames[anim.i]);
            return GLib.SOURCE_CONTINUE;
        });
    }

    // -------------------------------------------------------------- walking
    _stopWalk() {
        if (this._walkId) {
            GLib.source_remove(this._walkId);
            this._walkId = 0;
        }
        this._walk = null;
    }

    _face(dir) {
        this.actor.scale_x = dir < 0 ? -1 : 1;
    }

    moveTo(msg) {
        this._stopWalk();
        const wa = this._workArea();
        const minX = wa.x;
        const maxX = wa.x + wa.width - this._w;
        let target;
        if (typeof msg.x === 'number') {
            target = msg.x;
        } else {
            switch (msg.to) {
            case 'left': target = minX; break;
            case 'right': target = maxX; break;
            case 'center': target = (minX + maxX) / 2; break;
            default: target = minX + Math.random() * (maxX - minX);
            }
        }
        target = clamp(target, minX, maxX);
        const speed = msg.speed || this._walkSpeed;
        const walk = this._walk = {target, speed, last: GLib.get_monotonic_time()};
        this._face(target >= this.actor.x ? 1 : -1);
        this.play(msg.anim || 'walk', true);
        this._walkId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, TICK_MS, () => {
            if (this._walk !== walk)
                return GLib.SOURCE_REMOVE;
            const now = GLib.get_monotonic_time();
            const dt = (now - walk.last) / 1e6;
            walk.last = now;
            const x = this.actor.x;
            const dir = walk.target >= x ? 1 : -1;
            const nx = x + dir * walk.speed * dt;
            if ((dir > 0 && nx >= walk.target) || (dir < 0 && nx <= walk.target)) {
                this.actor.set_position(Math.round(walk.target), this.actor.y);
                this._walkId = 0;
                this._walk = null;
                this._io.event('arrived', {x: Math.round(walk.target)});
                return GLib.SOURCE_REMOVE;
            }
            this.actor.set_position(Math.round(nx), this.actor.y);
            return GLib.SOURCE_CONTINUE;
        });
    }

    // ------------------------------------------------------------- dragging
    _onPress(ev) {
        const button = ev.get_button();
        if (button === Clutter.BUTTON_SECONDARY) {
            this._openMenu();
            return Clutter.EVENT_STOP;
        }
        if (button !== Clutter.BUTTON_PRIMARY)
            return Clutter.EVENT_PROPAGATE;
        const [ex, ey] = ev.get_coords();
        // ClutterEvent.get_click_count() is gone in newer Mutter, so count clicks
        // ourselves: a second press within 400 ms and 8 px is a double click.
        const t = GLib.get_monotonic_time() / 1000;
        const last = this._lastPress;
        const count = last && t - last.t < 400 && Math.hypot(ex - last.ex, ey - last.ey) < 8 ? 2 : 1;
        this._lastPress = count === 2 ? null : {t, ex, ey};
        this._press = {ex, ey, ox: this.actor.x, oy: this.actor.y, moved: false, count};
        this._capId = global.stage.connect('captured-event', (_s, e) => this._onCaptured(e));
        return Clutter.EVENT_STOP;
    }

    _endCapture() {
        if (this._capId) {
            global.stage.disconnect(this._capId);
            this._capId = 0;
        }
        this._press = null;
    }

    _onCaptured(ev) {
        const p = this._press;
        if (!p)
            return Clutter.EVENT_PROPAGATE;
        const type = ev.type();
        if (type === Clutter.EventType.MOTION) {
            const [ex, ey] = ev.get_coords();
            if (!p.moved && Math.hypot(ex - p.ex, ey - p.ey) < DRAG_THRESHOLD)
                return Clutter.EVENT_STOP;
            if (!p.moved) {
                p.moved = true;
                this._stopWalk();
                this.closeEntry();
                this._io.event('drag_start');
                this.play('drag', true);
            }
            const m = this._monitor();
            this.actor.set_position(
                Math.round(clamp(p.ox + ex - p.ex, m.x, m.x + m.width - this._w)),
                Math.round(clamp(p.oy + ey - p.ey, m.y, m.y + m.height - this._h)));
            return Clutter.EVENT_STOP;
        }
        if (type === Clutter.EventType.BUTTON_RELEASE) {
            const moved = p.moved;
            const count = p.count;
            this._endCapture();
            if (moved) {
                this._io.event('drag_end', {x: Math.round(this.actor.x), y: Math.round(this.actor.y)});
                this._fall();
            } else {
                this._io.event('click', {x: Math.round(this.actor.x), y: Math.round(this.actor.y)});
                if (count === 2)
                    this._io.event('double_click');
            }
            return Clutter.EVENT_STOP;
        }
        return Clutter.EVENT_PROPAGATE;
    }

    _fall() {
        if (!this._gravity)
            return;
        const y = Math.round(this._floorY());
        if (Math.abs(this.actor.y - y) < 2)
            return;
        this.actor.ease({
            y,
            duration: 380,
            mode: Clutter.AnimationMode.EASE_OUT_BOUNCE,
        });
    }

    // --------------------------------------------------------------- bubble
    say(text, ttl = 6000) {
        if (this._bubbleTimer) {
            GLib.source_remove(this._bubbleTimer);
            this._bubbleTimer = 0;
        }
        if (this._entryOpen)
            return;
        this._bubbleLabel.set_text(text);
        this._bubble.show();
        this._placeOverlays();
        this._bubbleTimer = GLib.timeout_add(GLib.PRIORITY_DEFAULT, ttl, () => {
            this._bubbleTimer = 0;
            this._bubble.hide();
            return GLib.SOURCE_REMOVE;
        });
    }

    _place(box, preferAbove = true) {
        const m = this._monitor();
        const [, bw] = box.get_preferred_width(-1);
        const [, bh] = box.get_preferred_height(bw);
        const ax = this.actor.x + this._w * this._anchor[0];
        const ay = this.actor.y + this._h * this._anchor[1];
        let x = clamp(ax - bw / 2, m.x + 4, m.x + m.width - bw - 4);
        let y = ay - bh - 6;
        if (!preferAbove || y < m.y + 4)
            y = Math.min(this.actor.y + this._h + 4, m.y + m.height - bh - 4);
        box.set_position(Math.round(x), Math.round(y));
    }

    _placeOverlays() {
        if (this._bubble?.visible)
            this._place(this._bubble);
        if (this._entryBox?.visible)
            this._place(this._entryBox);
    }

    // ----------------------------------------------------------- chat entry
    openEntry() {
        if (this._entryOpen)
            return;
        this._entryOpen = true;
        this._bubble.hide();
        this._entry.set_text('');
        this._entryBox.show();
        this._place(this._entryBox);
        try {
            this._grab = Main.pushModal(this._entryBox, {actionMode: Shell.ActionMode.NORMAL});
        } catch (e) {
            this._grab = null;
        }
        this._entry.grab_key_focus();
        this._outsideId = global.stage.connect('captured-event', (_s, ev) => {
            if (ev.type() === Clutter.EventType.BUTTON_PRESS && !this._entryBox.contains(ev.get_source())) {
                this.closeEntry();
                return Clutter.EVENT_STOP;
            }
            return Clutter.EVENT_PROPAGATE;
        });
    }

    closeEntry() {
        if (!this._entryOpen)
            return;
        this._entryOpen = false;
        if (this._outsideId) {
            global.stage.disconnect(this._outsideId);
            this._outsideId = 0;
        }
        if (this._grab) {
            try {
                Main.popModal(this._grab);
            } catch (e) { /* ignore */ }
            this._grab = null;
        }
        this._entryBox.hide();
    }

    // ----------------------------------------------------------------- menu
    setMenu(items) {
        this._menuItems = items ?? [];
        // Rebuilding while the menu is open would destroy the item under the
        // pointer (e.g. a switch that was just toggled), so wait until it closes.
        if (this._menu.isOpen) {
            this._menuDirty = true;
            if (!this._menuWatch) {
                this._menuWatch = this._menu.connect('open-state-changed', (_m, open) => {
                    if (!open && this._menuDirty) {
                        this._menuDirty = false;
                        this._rebuildMenu();
                    }
                });
            }
            return;
        }
        this._rebuildMenu();
    }

    // Item shapes (see docs/PROTOCOL.md): action {id,label} | {local:"chat"} |
    // switch {type:"switch",id,label,checked} | header {type:"header",label} |
    // submenu {label,children:[...]}
    _addMenuItems(menu, items) {
        for (const it of items) {
            if (it.type === 'header') {
                menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem(it.label));
            } else if (it.type === 'switch') {
                const sw = new PopupMenu.PopupSwitchMenuItem(it.label, !!it.checked);
                sw.connect('toggled', (_item, state) =>
                    this._io.event('menu.click', {id: it.id, checked: state}));
                menu.addMenuItem(sw);
            } else if (Array.isArray(it.children)) {
                const sub = new PopupMenu.PopupSubMenuMenuItem(it.label);
                this._addMenuItems(sub.menu, it.children);
                menu.addMenuItem(sub);
            } else {
                menu.addAction(it.label, () => {
                    if (it.local === 'chat')
                        this.openEntry();
                    else
                        this._io.event('menu.click', {id: it.id});
                });
            }
        }
    }

    _rebuildMenu() {
        this._menu.removeAll();
        this._addMenuItems(this._menu, this._menuItems);
        if (this._menuItems.length > 0)
            this._menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        this._menu.addAction('隐藏 10 分钟', () => this._hideFor(600));
    }

    _openMenu() {
        this._menu.toggle();
    }

    _hideFor(seconds) {
        this.closeEntry();
        this._bubble.hide();
        this.actor.hide();
        if (this._hideTimer)
            GLib.source_remove(this._hideTimer);
        this._hideTimer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, seconds, () => {
            this._hideTimer = 0;
            this.actor.show();
            return GLib.SOURCE_REMOVE;
        });
    }
}
