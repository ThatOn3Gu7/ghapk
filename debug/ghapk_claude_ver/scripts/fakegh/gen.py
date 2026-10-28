#!/usr/bin/env python3
"""Generate a fake GitHub repository (runs, artifacts, APK zips) for developing
and testing ghapk without network access. The APKs carry a real binary
AndroidManifest.xml and a real APK Signing Block v2, so ghapk's own parsers get
exercised end to end.

usage: gen.py OUTDIR
"""
import hashlib, json, os, random, struct, subprocess, sys, tempfile, zipfile, datetime

OUT = sys.argv[1]
REPO = "acme/nazo"
PKG = "quiz.thaton3app.nazo"
NOW = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)


def iso(dt):
    return dt.strftime("%Y-%m-%dT%H:%M:%SZ")


# ---- binary AndroidManifest.xml encoder -------------------------------------
def build_axml(attrs_manifest, min_sdk):
    strings = []

    def sidx(s):
        if s not in strings:
            strings.append(s)
        return strings.index(s)

    ns_uri = "http://schemas.android.com/apk/res/android"
    sidx("versionCode"); sidx("versionName"); sidx("minSdkVersion"); sidx("package")
    sidx("android"); sidx(ns_uri); sidx("manifest"); sidx("uses-sdk")
    for v in attrs_manifest.values():
        if isinstance(v, str):
            sidx(v)

    def pool():
        enc = []
        offs = []
        pos = 0
        for s in strings:
            b = struct.pack("<H", len(s)) + s.encode("utf-16-le") + b"\x00\x00"
            offs.append(pos)
            enc.append(b)
            pos += len(b)
        data = b"".join(enc)
        while len(data) % 4:
            data += b"\x00"
        n = len(strings)
        hdr_size = 28
        strings_start = hdr_size + 4 * n
        size = strings_start + len(data)
        out = struct.pack("<HHIIIIII", 0x0001, hdr_size, size, n, 0, 0, strings_start, 0)
        out += b"".join(struct.pack("<I", o) for o in offs) + data
        return out

    def attr(ns, name, val):
        ns_i = sidx(ns) if ns else 0xFFFFFFFF
        if isinstance(val, str):
            raw = sidx(val)
            return struct.pack("<IIIHBBI", ns_i, sidx(name), raw, 8, 0, 0x03, raw)
        return struct.pack("<IIIHBBI", ns_i, sidx(name), 0xFFFFFFFF, 8, 0, 0x10, val)

    def start(name, attrs):
        body = b"".join(attrs)
        ext = struct.pack("<IIHHHHHH", 0xFFFFFFFF, sidx(name), 20, 20, len(attrs), 0, 0, 0)
        size = 16 + len(ext) + len(body)
        return struct.pack("<HHIII", 0x0102, 16, size, 1, 0xFFFFFFFF) + ext + body

    def end(name):
        ext = struct.pack("<II", 0xFFFFFFFF, sidx(name))
        return struct.pack("<HHIII", 0x0103, 16, 16 + len(ext), 1, 0xFFFFFFFF) + ext

    # the attributes must be interned before the pool is serialised
    a_pkg = attr(None, "package", attrs_manifest["package"])
    a_vc = attr(ns_uri, "versionCode", attrs_manifest["versionCode"])
    a_vn = attr(ns_uri, "versionName", attrs_manifest["versionName"])
    a_sdk = attr(ns_uri, "minSdkVersion", min_sdk)
    body = start("manifest", [a_pkg, a_vc, a_vn]) + start("uses-sdk", [a_sdk]) + end("uses-sdk") + end("manifest")
    p = pool()
    total = 8 + len(p) + len(body)
    return struct.pack("<HHI", 0x0003, 8, total) + p + body


# ---- certificate + APK signing block ---------------------------------------
def make_cert(cn):
    d = tempfile.mkdtemp()
    key, crt = os.path.join(d, "k.pem"), os.path.join(d, "c.der")
    subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key,
                    "-out", crt, "-outform", "DER", "-days", "3650", "-subj", "/CN=" + cn],
                   check=True, capture_output=True)
    return open(crt, "rb").read()


def lp(b):  # length-prefixed
    return struct.pack("<I", len(b)) + b


def signing_block(cert_der):
    digests = lp(b"")
    certs = lp(lp(cert_der))
    attrs = lp(b"")
    signed = lp(digests + certs + attrs)
    signer = lp(signed + lp(b"") + lp(b""))
    v2 = lp(signer)  # sequence of signers
    pair = struct.pack("<Q", 4 + len(v2)) + struct.pack("<I", 0x7109871A) + v2
    size = len(pair) + 8 + 16
    return struct.pack("<Q", size) + pair + struct.pack("<Q", size) + b"APK Sig Block 42"


def inject_block(path, block):
    data = open(path, "rb").read()
    eocd = data.rfind(b"PK\x05\x06")
    cd_size, cd_off = struct.unpack("<II", data[eocd + 12:eocd + 20])
    new = data[:cd_off] + block + data[cd_off:eocd] + data[eocd:]
    new = bytearray(new)
    e2 = len(new) - (len(data) - eocd)
    struct.pack_into("<I", new, e2 + 16, cd_off + len(block))
    open(path, "wb").write(bytes(new))


CERT = make_cert("Nazo Debug")


def make_apk(path, vcode, vname, payload_kb, seed):
    rnd = random.Random(seed)
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("AndroidManifest.xml", build_axml(
            {"package": PKG, "versionCode": vcode, "versionName": vname}, 24))
        z.writestr("classes.dex", rnd.randbytes(payload_kb * 1024))
        z.writestr("resources.arsc", b"\x00" * 2048)
    inject_block(path, signing_block(CERT))


def make_artifact_zip(path, apk_name, vcode, vname, kb, seed, extra=None):
    d = tempfile.mkdtemp()
    apk = os.path.join(d, apk_name)
    make_apk(apk, vcode, vname, kb, seed)
    with zipfile.ZipFile(path, "w", zipfile.ZIP_STORED) as z:
        z.write(apk, apk_name)
        for n, content in (extra or {}).items():
            z.writestr(n, content)


# ---- the repository ---------------------------------------------------------
os.makedirs(os.path.join(OUT, "zips"), exist_ok=True)
wf = [
    {"id": 101, "name": "Android CI", "path": ".github/workflows/ci.yml", "state": "active"},
    {"id": 102, "name": "Release", "path": ".github/workflows/release.yml", "state": "active"},
    {"id": 103, "name": "Lint", "path": ".github/workflows/lint.yml", "state": "active"},
]
runs, artifacts = [], []
art_id = 5000


def add_run(rid, num, wfid, branch, sha, title, status, concl, mins_ago, dur_min, actor="thaton3"):
    created = NOW - datetime.timedelta(minutes=mins_ago)
    updated = created + datetime.timedelta(minutes=dur_min)
    if status != "completed":
        updated = NOW
    runs.append({
        "id": rid, "name": [w for w in wf if w["id"] == wfid][0]["name"], "display_title": title,
        "head_branch": branch, "head_sha": sha, "status": status, "conclusion": concl,
        "event": "push", "run_number": num, "run_attempt": 1, "workflow_id": wfid,
        "created_at": iso(created), "updated_at": iso(updated), "run_started_at": iso(created),
        "html_url": "https://github.com/%s/actions/runs/%d" % (REPO, rid), "actor": {"login": actor},
    })


def add_art(rid, name, size, vcode, vname, sha, kb, apk_name, seed, mins_ago, extra=None, no_apk=False):
    global art_id
    art_id += 1
    zp = os.path.join(OUT, "zips", "%d.zip" % art_id)
    if no_apk:
        with zipfile.ZipFile(zp, "w") as z:
            z.writestr("mapping.txt", "# proguard mapping\n" * 2000)
    else:
        make_artifact_zip(zp, apk_name, vcode, vname, kb, seed, extra)
    sz = os.path.getsize(zp)
    created = NOW - datetime.timedelta(minutes=mins_ago)
    artifacts.append({
        "id": art_id, "name": name, "size_in_bytes": sz, "expired": False,
        "digest": "sha256:" + hashlib.sha256(open(zp, "rb").read()).hexdigest(),
        "created_at": iso(created), "expires_at": iso(created + datetime.timedelta(days=90)),
        "workflow_run": {"id": rid, "head_branch": [r for r in runs if r["id"] == rid][0]["head_branch"],
                         "head_sha": sha},
    })


add_run(9012, 412, 101, "main", "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", "feat: live leaderboard", "in_progress", None, 3, 0)
add_run(9011, 411, 101, "main", "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432", "fix: quiz timer drift on rotate", "completed", "success", 55, 6)
add_run(9010, 410, 101, "main", "1111222233334444555566667777888899990000", "refactor: split QuizViewModel", "completed", "failure", 130, 4)
add_run(9009, 409, 101, "feature/quiz-ui", "abcdef0123456789abcdef0123456789abcdef01", "wip: new answer cards", "completed", "success", 310, 7)
add_run(9008, 408, 103, "main", "2222333344445555666677778888999900001111", "chore: ktlint", "completed", "success", 400, 2)
add_run(9007, 407, 101, "main", "3333444455556666777788889999000011112222", "feat: streak badges", "completed", "success", 1500, 6)
add_run(9006, 406, 101, "dev", "4444555566667777888899990000111122223333", "try: compose 1.7", "completed", "cancelled", 1700, 1)
add_run(9005, 405, 102, "main", "5555666677778888999900001111222233334444", "release 1.4.0", "completed", "success", 3000, 12)
add_run(9004, 404, 101, "main", "6666777788889999000011112222333344445555", "fix: crash on empty deck", "completed", "success", 4300, 6)
add_run(9003, 403, 101, "dev", "7777888899990000111122223333444455556666", "feat: offline cache", "completed", "failure", 5800, 3)
add_run(9002, 402, 101, "main", "8888999900001111222233334444555566667777", "build: bump agp", "completed", "success", 9000, 6)
add_run(9001, 401, 101, "main", "9999000011112222333344445555666677778888", "init: project", "completed", "success", 14000, 5)

KB = 3000  # ~3 MB of incompressible payload per APK, enough to see progress
for abi, seed in (("arm64-v8a", 1), ("armeabi-v7a", 2), ("x86_64", 3), ("universal", 4)):
    add_art(9011, "nazo-debug-" + abi, 0, 142, "1.4.2", runs[1]["head_sha"], KB if abi != "universal" else KB * 2,
            "app-%s-debug.apk" % abi, seed, 50)
add_art(9011, "nazo-release-arm64-v8a", 0, 142, "1.4.2", runs[1]["head_sha"], KB, "app-arm64-v8a-release.apk", 5, 50)
add_art(9011, "nazo-mapping", 0, 0, "", runs[1]["head_sha"], 0, "", 6, 50, no_apk=True)
add_art(9011, "nazo-debug-sha256sums", 0, 0, "", runs[1]["head_sha"], 0, "", 7, 50, no_apk=True)
add_art(9009, "nazo-debug-universal", 0, 141, "1.4.1-quiz", runs[3]["head_sha"], KB, "app-universal-debug.apk", 8, 300)
add_art(9007, "nazo-debug-arm64-v8a", 0, 140, "1.4.0", runs[5]["head_sha"], KB, "app-arm64-v8a-debug.apk", 9, 1490)
add_art(9007, "nazo-debug-armeabi-v7a", 0, 140, "1.4.0", runs[5]["head_sha"], KB, "app-armeabi-v7a-debug.apk", 10, 1490)
add_art(9005, "nazo-release-universal", 0, 140, "1.4.0", runs[7]["head_sha"], KB, "app-universal-release.apk", 11, 2990)
add_art(9004, "nazo-debug-arm64-v8a", 0, 139, "1.3.9", runs[8]["head_sha"], KB, "app-arm64-v8a-debug.apk", 12, 4290)

repo = {"full_name": REPO, "private": False, "default_branch": "main",
        "description": "Nazo: AI-powered anime quiz", "pushed_at": iso(NOW - datetime.timedelta(minutes=3)),
        "html_url": "https://github.com/" + REPO}
branches = [{"name": "main", "protected": True}, {"name": "dev", "protected": False},
            {"name": "feature/quiz-ui", "protected": False}]
commits = [
    {"sha": "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432",
     "commit": {"message": "fix: quiz timer drift on rotate\n\nbody", "author": {"name": "thaton3", "date": iso(NOW - datetime.timedelta(minutes=60))}}},
    {"sha": "1111222233334444555566667777888899990000",
     "commit": {"message": "refactor: split QuizViewModel", "author": {"name": "thaton3", "date": iso(NOW - datetime.timedelta(minutes=140))}}},
]
jobs = {"jobs": [{"name": "build", "status": "completed", "conclusion": "failure", "steps": [
    {"name": "Set up job", "conclusion": "success", "number": 1},
    {"name": "Compile debug", "conclusion": "failure", "number": 2}]}]}
releases = [{"id": 1, "tag_name": "v1.4.0", "name": "1.4.0", "draft": False, "prerelease": False,
             "published_at": iso(NOW - datetime.timedelta(days=2)), "html_url": "https://github.com/%s/releases/v1.4.0" % REPO,
             "assets": [{"id": 77, "name": "nazo-1.4.0.apk", "size": 3000000, "state": "uploaded"}]}]
db = {"repo": repo, "runs": runs, "artifacts": artifacts, "workflows": wf, "branches": branches,
      "commits": commits, "jobs": jobs, "releases": releases, "user": {"login": "thaton3"}}
json.dump(db, open(os.path.join(OUT, "db.json"), "w"), indent=1)
open(os.path.join(OUT, "cert.der"), "wb").write(CERT)
print("fake repo written to", OUT, "-", len(artifacts), "artifacts")
