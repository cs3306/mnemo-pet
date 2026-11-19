// Wires the network client, the pet and the sensors together.
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';

import {Net} from './net.js';
import {Pet} from './pet.js';
import {Sensors} from './sensors.js';

const DEFAULTS = {host: '127.0.0.1', port: 23817, token: '', render: 'auto'};

function readLocalConfig() {
    // ~/.config/mnemo-pet/extension.json  {"host","port","token","render"}
    const path = GLib.build_filenamev([GLib.get_user_config_dir(), 'mnemo-pet', 'extension.json']);
    try {
        const [ok, bytes] = Gio.File.new_for_path(path).load_contents(null);
        if (ok)
            return {...DEFAULTS, ...JSON.parse(new TextDecoder().decode(bytes))};
    } catch (e) {
        if (!(e instanceof GLib.Error) || !e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND))
            console.warn(`mnemo-pet: ignoring ${path}: ${e}`);
    }
    return {...DEFAULTS};
}

export class App {
    constructor(extPath) {
        this._extPath = extPath;
        this._sounds = {};
    }

    start() {
        const cfg = this._cfg = readLocalConfig();
        const io = {
            event: (name, data) => this._net?.event(name, data),
            renderMode: cfg.render,
        };
        this._pet = new Pet(this._extPath, io);
        this._sensors = new Sensors(io);
        this._net = new Net({
            host: cfg.host,
            port: cfg.port,
            token: cfg.token,
            onMessage: m => this._onMessage(m),
            onUp: () => console.log('mnemo-pet: connected to petd'),
            onDown: () => {
                console.log('mnemo-pet: petd connection lost, retrying');
                this._sensors.stop();
                this._pet.play('idle', true);
            },
        });
        // Show something immediately; petd's config message refines it.
        this._pet.loadSkin('default', 1.4);
        this._pet.placeInitially();
        this._net.start();
    }

    stop() {
        this._net?.stop();
        this._sensors?.stop();
        this._pet?.destroy();
        this._net = this._sensors = this._pet = null;
    }

    _onMessage(m) {
        switch (m.t) {
        case 'config':
            this._sounds = m.sounds ?? {};
            this._pet.configure(m);
            this._sensors.configure(m);
            break;
        case 'skin':
            this._pet.loadSkin(m.skin ?? 'default', m.scale ?? 1.4);
            break;
        case 'anim':
            this._pet.play(m.name, m.loop !== false);
            break;
        case 'move':
            this._pet.moveTo(m);
            break;
        case 'say':
            this._pet.say(m.text ?? '', m.ttl ?? 6000);
            break;
        case 'sound':
            this._playSound(m.name);
            break;
        case 'menu':
            this._pet.setMenu(m.items);
            break;
        case 'error':
            console.error(`mnemo-pet: petd says: ${m.msg}`);
            break;
        default:
            break;
        }
    }

    _playSound(name) {
        const spec = this._sounds[name] ?? name;
        try {
            const player = global.display.get_sound_player();
            if (spec.startsWith('/'))
                player.play_from_file(Gio.File.new_for_path(spec), name, null);
            else
                player.play_from_theme(spec, name, null);
        } catch (e) {
            console.warn(`mnemo-pet: cannot play sound "${name}": ${e}`);
        }
    }
}
