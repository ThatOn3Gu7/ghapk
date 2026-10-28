"""Shared state for the fake phone: an installed-app database in a JSON file."""
import json, os, struct, zipfile

DIR = os.environ.get("FAKEPHONE_DIR", "/tmp/fakephone")
STATE = os.path.join(DIR, "state.json")
FAKE = os.environ.get("FAKEGH_DIR", os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "fake"))


def load():
    os.makedirs(DIR, exist_ok=True)
    if not os.path.exists(STATE):
        cert = open(os.path.join(FAKE, "cert.der"), "rb").read().hex()
        json.dump({"quiz.thaton3app.nazo": {"versionCode": 141, "versionName": "1.4.1-quiz",
                                            "abi": "arm64-v8a", "cert": os.environ.get("FAKEPHONE_CERT", cert)}},
                  open(STATE, "w"))
    return json.load(open(STATE))


def save(s):
    json.dump(s, open(STATE, "w"))


def manifest_attrs(apk):
    """Tiny AXML reader: returns {attrname: value} of the manifest element."""
    b = zipfile.ZipFile(apk).read("AndroidManifest.xml")
    u16 = lambda o: struct.unpack_from("<H", b, o)[0]
    u32 = lambda o: struct.unpack_from("<I", b, o)[0]
    pool, out, off = [], {}, 8
    while off + 8 <= len(b):
        t, h, sz = u16(off), u16(off + 2), u32(off + 4)
        if t == 1:
            n, ss = u32(off + 8), u32(off + 20)
            for i in range(n):
                o = off + ss + u32(off + h + 4 * i)
                l = u16(o)
                pool.append(b[o + 2:o + 2 + 2 * l].decode("utf-16-le"))
        elif t == 0x102 and not out:
            ext = off + h
            cnt, st = u16(ext + 12), u16(ext + 8)
            for i in range(cnt):
                a = ext + st + 20 * i
                name, typ, data = pool[u32(a + 4)], b[a + 15], u32(a + 16)
                out[name] = pool[data] if typ == 3 else data
        off += sz
    return out
