// Mnemo Pet - GNOME Shell extension entry point.
// All logic lives in petd; this extension only renders and reports.
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import {App} from './app.js';

export default class MnemoPetExtension extends Extension {
    enable() {
        try {
            this._app = new App(this.path);
            this._app.start();
        } catch (e) {
            console.error(`mnemo-pet: failed to start: ${e}\n${e.stack}`);
            this._app?.stop();
            this._app = null;
        }
    }

    disable() {
        this._app?.stop();
        this._app = null;
    }
}
