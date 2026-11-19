// Skin loading: manifest (pet.json) + frames.
import Clutter from 'gi://Clutter';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import GdkPixbuf from 'gi://GdkPixbuf';
import St from 'gi://St';

// Cogl's typelib name/version has moved between releases; a failed static
// import would take the whole extension down, so load it defensively.
let Cogl = null;
try {
    Cogl = (await import('gi://Cogl')).default;
} catch (e) {
    console.warn(`mnemo-pet: gi://Cogl unavailable (${e}); frames will use CSS backgrounds`);
}

function readText(path) {
    const [ok, bytes] = Gio.File.new_for_path(path).load_contents(null);
    if (!ok)
        throw new Error(`cannot read ${path}`);
    return new TextDecoder().decode(bytes);
}

export function skinDirs(name, extPath) {
    return [
        GLib.build_filenamev([GLib.get_user_data_dir(), 'mnemo-pet', 'skins', name]),
        GLib.build_filenamev([extPath, 'skins', name]),
    ];
}

/** Returns {manifest, dir} or null. */
export function loadManifest(name, extPath) {
    for (const dir of skinDirs(name, extPath)) {
        try {
            const manifest = JSON.parse(readText(GLib.build_filenamev([dir, 'pet.json'])));
            if (manifest.anims && Object.keys(manifest.anims).length > 0)
                return {manifest, dir};
        } catch (e) { /* try next */ }
    }
    return null;
}

/** Decode a PNG into a Clutter content object (throws if unsupported). */
function loadContent(path) {
    if (!Cogl)
        throw new Error('Cogl not available');
    const pb = GdkPixbuf.Pixbuf.new_from_file(path);
    const fmt = pb.get_has_alpha() ? Cogl.PixelFormat.RGBA_8888 : Cogl.PixelFormat.RGB_888;
    const [w, h, stride] = [pb.get_width(), pb.get_height(), pb.get_rowstride()];
    if (St.ImageContent) {
        // GNOME 48+ (Clutter.Image was removed there)
        const image = new St.ImageContent({preferred_width: w, preferred_height: h});
        const bytes = pb.read_pixel_bytes();
        try {
            image.set_bytes(bytes, fmt, w, h, stride);
        } catch (e) {
            // Some versions take a Cogl context as the first argument.
            const ctx = global.stage?.context?.get_backend?.()?.get_cogl_context?.();
            if (!ctx)
                throw e;
            image.set_bytes(ctx, bytes, fmt, w, h, stride);
        }
        return image;
    }
    const image = new Clutter.Image();
    image.set_data(pb.get_pixels(), fmt, w, h, stride);
    return image;
}

/**
 * Preload every frame. mode: 'content' (Clutter.Image), 'style' (CSS
 * background-image) or 'auto' (try content, fall back to style).
 * Returns {anims: {name: {frames: [frame], fps, loop}}, mode}.
 */
export function loadFrames(manifest, dir, mode = 'auto') {
    const first = Object.values(manifest.anims)[0]?.frames?.[0];
    let chosen = mode;
    if (mode === 'auto') {
        try {
            loadContent(GLib.build_filenamev([dir, first]));
            chosen = 'content';
        } catch (e) {
            console.warn(`mnemo-pet: image content unavailable (${e}); using CSS background frames`);
            chosen = 'style';
        }
    }
    const anims = {};
    for (const [name, a] of Object.entries(manifest.anims)) {
        const frames = [];
        for (const f of a.frames ?? []) {
            const path = GLib.build_filenamev([dir, f]);
            try {
                if (chosen === 'content')
                    frames.push({content: loadContent(path)});
                else
                    frames.push({style: `background-image: url("${GLib.filename_to_uri(path, null)}");`});
            } catch (e) {
                console.warn(`mnemo-pet: cannot load frame ${path}: ${e}`);
            }
        }
        if (frames.length > 0)
            anims[name] = {frames, fps: a.fps || 6, loop: a.loop !== false};
    }
    return {anims, mode: chosen};
}
