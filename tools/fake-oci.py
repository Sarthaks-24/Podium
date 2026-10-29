#!/usr/bin/env python3
"""Stand-in for the parts of Oracle's API, the site and the heartbeat that
tools/oci-watchdog.sh talks to, for tools/oci-watchdog-test.sh. Every API
request's signature is checked against PUBKEY with openssl, the way
Oracle checks it; a bad one gets 401. Loopback only.

    python3 tools/fake-oci.py DIR PORT PUBKEY

DIR/state holds the VM's lifecycle state (GONE answers 404), DIR/healthz
"ok" or anything else, and DIR/log collects one line per call.
"""
import base64
import http.server
import os
import subprocess
import sys
import tempfile
import urllib.parse

DIR, PORT, PUBKEY = sys.argv[1], int(sys.argv[2]), sys.argv[3]


def read(name, default=""):
    try:
        return open(os.path.join(DIR, name)).read().strip()
    except FileNotFoundError:
        return default


def log(line):
    with open(os.path.join(DIR, "log"), "a") as f:
        f.write(line + "\n")


def signature_ok(handler, method):
    auth = handler.headers.get("authorization", "")
    if not auth.startswith("Signature "):
        return False
    fields = {}
    for part in auth[len("Signature "):].split(","):
        k, _, v = part.partition("=")
        fields[k] = v.strip('"')
    names = fields.get("headers", "").split()
    need = ["date", "(request-target)", "host"]
    if method == "POST":
        need += ["content-length", "content-type", "x-content-sha256"]
    if sorted(names) != sorted(need) or fields.get("algorithm") != "rsa-sha256":
        return False
    lines = []
    for n in names:
        if n == "(request-target)":
            lines.append("(request-target): %s %s" % (method.lower(), handler.path))
        else:
            lines.append("%s: %s" % (n, handler.headers.get(n, "")))
    with tempfile.NamedTemporaryFile(delete=False) as sig:
        sig.write(base64.b64decode(fields.get("signature", "")))
    try:
        r = subprocess.run(
            ["openssl", "dgst", "-sha256", "-verify", PUBKEY, "-signature", sig.name],
            input="\n".join(lines).encode(), capture_output=True)
        return r.returncode == 0
    finally:
        os.unlink(sig.name)


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, code, body=""):
        data = body.encode()
        self.send_response(code)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def body(self):
        n = int(self.headers.get("Content-Length", "0") or 0)
        return self.rfile.read(n).decode() if n else ""

    def api(self, method):
        if not signature_ok(self, method):
            log("api %s bad-signature" % method)
            return self.reply(401, '{"code":"NotAuthenticated"}')
        state = read("state", "RUNNING")
        u = urllib.parse.urlsplit(self.path)
        if state == "GONE":
            log("api %s 404" % method)
            return self.reply(404, '{"code":"NotAuthorizedOrNotFound"}')
        if method == "POST":
            if urllib.parse.parse_qs(u.query).get("action") != ["START"]:
                return self.reply(400)
            open(os.path.join(DIR, "state"), "w").write("STARTING")
            log("api POST START")
            return self.reply(200, '{"lifecycleState":"STARTING"}')
        log("api GET %s" % state)
        return self.reply(200, '{"id":"x","lifecycleState":"%s"}' % state)

    def do_GET(self):
        if self.path.startswith("/20160918/"):
            return self.api("GET")
        if self.path == "/healthz":
            if read("healthz", "ok") == "ok":
                return self.reply(200, '{"status":"ok"}')
            return self.reply(503, '{"status":"down"}')
        self.reply(404)

    def do_POST(self):
        if self.path.startswith("/20160918/"):
            self.body()
            return self.api("POST")
        text = self.body()
        if self.path in ("/ping", "/ping/fail", "/ntfy"):
            log("%s %s" % (self.path, text))
            return self.reply(200)
        self.reply(404)


http.server.ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
