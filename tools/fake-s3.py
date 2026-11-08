#!/usr/bin/env python3
"""A minimal S3 stand-in for tools/offsite-test.sh: one process, objects in
a folder, no signatures checked. It answers what rclone needs for the
offsite nightly copy (put, list v1 and v2, head, get) and nothing more.
Loopback only.

    python3 tools/fake-s3.py ROOT PORT [LOGFILE]

With LOGFILE, every request is appended as one line: the call class,
the method, the path with its query and the Unix time, separated by tabs. The class is
"C" for what Backblaze B2 bills as class C (a bucket-level GET, which is
ListObjects, ListObjectsV2 or a bucket query, and HEAD on a bucket), "A"
for a write (PUT, POST, DELETE), "B" for an object GET or HEAD. B2's
classes are from its price page, as remembered: check them there.
"""
import hashlib
import http.server
import os
import re
import sys
import time
import urllib.parse
from email.utils import formatdate
from xml.sax.saxutils import escape, unescape

ROOT = sys.argv[1]
PORT = int(sys.argv[2])
LOGFILE = sys.argv[3] if len(sys.argv) > 3 else ""
NS = "http://s3.amazonaws.com/doc/2006-03-01/"


def path_of(bucket, key):
    p = os.path.normpath(os.path.join(ROOT, bucket, key))
    if not p.startswith(os.path.join(ROOT, bucket) + os.sep):
        raise ValueError("key escapes the bucket")
    return p


def iso(t):
    return time.strftime("%Y-%m-%dT%H:%M:%S.000Z", time.gmtime(t))


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def log_call(self):
        if not LOGFILE:
            return
        bucket, key, _ = self.split()
        if self.command in ("GET", "HEAD"):
            cls = "B" if key else "C"
        else:
            cls = "A"
        with open(LOGFILE, "a") as f:
            f.write("%s\t%s\t%s\t%d\n" % (cls, self.command, self.path, time.time()))

    def handle_one_request(self):
        # Wrap the standard dispatch so every method is logged once.
        self.raw_requestline = None
        try:
            self.raw_requestline = self.rfile.readline(65537)
        except (ConnectionError, TimeoutError):
            self.close_connection = True
            return
        if not self.raw_requestline or not self.parse_request():
            self.close_connection = True
            return
        self.log_call()
        fn = getattr(self, "do_" + self.command, None)
        if fn is None:
            return self.reply(501)
        fn()
        self.wfile.flush()

    def split(self):
        u = urllib.parse.urlsplit(self.path)
        parts = urllib.parse.unquote(u.path).lstrip("/").split("/", 1)
        bucket = parts[0]
        key = parts[1] if len(parts) > 1 else ""
        return bucket, key, urllib.parse.parse_qs(u.query, keep_blank_values=True)

    def reply(self, code, body=b"", headers=None):
        self.send_response(code)
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def object_headers(self, p):
        data = open(p, "rb").read()
        h = {
            "ETag": '"%s"' % hashlib.md5(data).hexdigest(),
            "Last-Modified": formatdate(os.path.getmtime(p), usegmt=True),
            "Content-Type": "application/octet-stream",
        }
        meta = p + ".meta"
        if os.path.exists(meta):
            for line in open(meta):
                k, _, v = line.rstrip("\n").partition(":")
                h[k] = v
        return data, h

    def do_PUT(self):
        bucket, key, _ = self.split()
        n = int(self.headers.get("Content-Length", "0"))
        data = self.rfile.read(n)
        if not key:
            os.makedirs(os.path.join(ROOT, bucket), exist_ok=True)
            return self.reply(200)
        p = path_of(bucket, key)
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "wb") as f:
            f.write(data)
        with open(p + ".meta", "w") as f:
            for k, v in self.headers.items():
                if k.lower().startswith("x-amz-meta-"):
                    f.write("%s:%s\n" % (k, v))
        self.reply(200, headers={"ETag": '"%s"' % hashlib.md5(data).hexdigest()})

    def do_DELETE(self):
        bucket, key, _ = self.split()
        if key:
            for p in (path_of(bucket, key), path_of(bucket, key) + ".meta"):
                if os.path.isfile(p):
                    os.remove(p)
        self.reply(204)

    def do_POST(self):
        # Multi-object delete (POST /bucket?delete), which Litestream uses.
        bucket, key, q = self.split()
        n = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(n).decode("utf-8", "replace")
        if "delete" not in q:
            return self.reply(501)
        for k in re.findall(r"<Key>([^<]*)</Key>", body):
            for p in (path_of(bucket, unescape(k)), path_of(bucket, unescape(k)) + ".meta"):
                if os.path.isfile(p):
                    os.remove(p)
        self.reply(200, b'<?xml version="1.0" encoding="UTF-8"?><DeleteResult xmlns="%s"></DeleteResult>' % NS.encode(),
                   {"Content-Type": "application/xml"})

    def do_HEAD(self):
        self.do_GET()

    def do_GET(self):
        bucket, key, q = self.split()
        if key:
            p = path_of(bucket, key)
            if not os.path.isfile(p):
                return self.reply(404, b"<Error><Code>NoSuchKey</Code></Error>")
            data, h = self.object_headers(p)
            return self.reply(200, data, h)
        prefix = q.get("prefix", [""])[0]
        delim = q.get("delimiter", [""])[0]
        base = os.path.join(ROOT, bucket)
        keys = []
        for d, _, files in os.walk(base):
            for name in files:
                if name.endswith(".meta"):
                    continue
                k = os.path.relpath(os.path.join(d, name), base).replace(os.sep, "/")
                if k.startswith(prefix):
                    keys.append(k)
        contents, prefixes = [], set()
        for k in sorted(keys):
            rest = k[len(prefix):]
            if delim and delim in rest:
                prefixes.add(prefix + rest.split(delim, 1)[0] + delim)
                continue
            p = os.path.join(base, k)
            data = open(p, "rb").read()
            contents.append(
                "<Contents><Key>%s</Key><LastModified>%s</LastModified><ETag>&quot;%s&quot;</ETag>"
                "<Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents>"
                % (escape(k), iso(os.path.getmtime(p)), hashlib.md5(data).hexdigest(), len(data))
            )
        common = "".join("<CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes>" % escape(p) for p in sorted(prefixes))
        v2 = q.get("list-type", [""])[0] == "2"
        body = (
            '<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="%s"><Name>%s</Name>'
            "<Prefix>%s</Prefix>%s<MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>%s%s</ListBucketResult>"
            % (NS, escape(bucket), escape(prefix),
               "<KeyCount>%d</KeyCount>" % (len(contents) + len(prefixes)) if v2 else "<Marker></Marker>",
               "".join(contents), common)
        ).encode()
        self.reply(200, body, {"Content-Type": "application/xml"})


http.server.ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
