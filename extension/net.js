// Newline-delimited JSON client for petd, with automatic reconnect.
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';

const ENC = new TextEncoder();

export class Net {
    constructor({host, port, token, onMessage, onUp, onDown}) {
        this._host = host;
        this._port = port;
        this._token = token;
        this._onMessage = onMessage;
        this._onUp = onUp ?? (() => {});
        this._onDown = onDown ?? (() => {});
        this._gen = 0;
        this._stopped = true;
        this._up = false;
        this._backoff = 1;
        this._timer = 0;
        this._queue = [];
        this._writing = false;
    }

    get connected() {
        return this._up;
    }

    start() {
        this._stopped = false;
        this._connect();
    }

    stop() {
        this._stopped = true;
        if (this._timer) {
            GLib.source_remove(this._timer);
            this._timer = 0;
        }
        this._teardown();
    }

    event(name, data = {}) {
        this.send({t: 'event', name, data});
    }

    send(obj) {
        if (!this._up)
            return;
        this._queue.push(`${JSON.stringify(obj)}\n`);
        this._pump();
    }

    _connect() {
        const gen = ++this._gen;
        const cancel = this._cancel = new Gio.Cancellable();
        const client = new Gio.SocketClient();
        client.set_timeout(5);
        client.connect_to_host_async(`${this._host}:${this._port}`, this._port, cancel, (c, res) => {
            if (gen !== this._gen)
                return;
            let conn;
            try {
                conn = c.connect_to_host_finish(res);
            } catch (e) {
                this._scheduleRetry();
                return;
            }
            // SocketClient.set_timeout() above also becomes the I/O timeout of
            // the connected socket, so an idle connection (petd sends nothing
            // for a few seconds) would be dropped and reconnected over and over.
            // Keep it for connecting only; an established link has no timeout.
            try {
                conn.get_socket().set_timeout(0);
            } catch (e) { /* not a socket connection */ }
            this._conn = conn;
            this._out = conn.get_output_stream();
            this._in = new Gio.DataInputStream({base_stream: conn.get_input_stream()});
            this._queue = [];
            this._writing = false;
            this._up = true;
            this._backoff = 1;
            const hello = {t: 'hello', role: 'render'};
            if (this._token)
                hello.token = this._token;
            this._queue.push(`${JSON.stringify(hello)}\n`);
            this._pump();
            this._readLoop(gen);
            this._onUp();
        });
    }

    _readLoop(gen) {
        this._in.read_line_async(GLib.PRIORITY_DEFAULT, this._cancel, (stream, res) => {
            if (gen !== this._gen)
                return;
            let line = null;
            try {
                [line] = stream.read_line_finish_utf8(res);
            } catch (e) {
                line = null;
            }
            if (line === null || line === undefined) {
                this._dropped(gen);
                return;
            }
            try {
                this._onMessage(JSON.parse(line));
            } catch (e) {
                console.error(`mnemo-pet: bad message: ${e}`);
            }
            this._readLoop(gen);
        });
    }

    _pump() {
        if (this._writing || !this._out || this._queue.length === 0)
            return;
        const gen = this._gen;
        this._writing = true;
        const bytes = ENC.encode(this._queue.shift());
        this._out.write_all_async(bytes, GLib.PRIORITY_DEFAULT, this._cancel, (s, res) => {
            if (gen !== this._gen)
                return;
            this._writing = false;
            try {
                s.write_all_finish(res);
            } catch (e) {
                this._dropped(gen);
                return;
            }
            this._pump();
        });
    }

    _dropped(gen) {
        if (gen !== this._gen)
            return;
        const wasUp = this._up;
        this._teardown();
        if (wasUp)
            this._onDown();
        if (!this._stopped)
            this._scheduleRetry();
    }

    _teardown() {
        this._gen++;
        this._up = false;
        this._queue = [];
        this._writing = false;
        try {
            this._cancel?.cancel();
        } catch (e) { /* ignore */ }
        try {
            this._conn?.close(null);
        } catch (e) { /* ignore */ }
        this._conn = this._in = this._out = null;
    }

    _scheduleRetry() {
        if (this._stopped || this._timer)
            return;
        const secs = this._backoff;
        this._backoff = Math.min(this._backoff * 2, 10);
        this._timer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, secs, () => {
            this._timer = 0;
            if (!this._stopped)
                this._connect();
            return GLib.SOURCE_REMOVE;
        });
    }
}
