#!/usr/bin/env python3
"""Seed a testing-mode podium server for the load test.

Imports the example packs in problems/, validates them on the real judge,
and creates the student accounts the k6 script signs in as. The contest
itself is created by contest.js at the start of each run, so every run
gets a fresh one.

    python3 -I tools/loadtest/seed.py --base http://127.0.0.1:18080 --users 700

Only works against a server in testing mode (PODIUM_MODE=testing), which
signs anyone in from a form and listens on loopback only. Standard library
only: it runs on the VM image and in WSL2 without installing anything.
"""

import argparse
import http.cookiejar
import io
import json
import pathlib
import sys
import time
import urllib.error
import urllib.request
import zipfile

OWNER = "owner@podium.test"


class Client:
    """One signed-in browser: a cookie jar and JSON helpers."""

    def __init__(self, base, origin):
        self.base = base.rstrip("/")
        # The server's PODIUM_BASE_URL: testing mode accepts the sign-in
        # form only from that origin.
        self.origin = origin
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def call(self, method, path, body=None, content_type="application/json"):
        data = None
        headers = {"Origin": self.origin}
        if body is not None:
            data = body if isinstance(body, bytes) else json.dumps(body).encode()
            headers["Content-Type"] = content_type
        req = urllib.request.Request(self.base + path, data=data, method=method, headers=headers)
        try:
            with self.opener.open(req, timeout=60) as resp:
                raw = resp.read()
                return resp.status, (json.loads(raw) if raw else None)
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                return e.code, json.loads(raw)
            except ValueError:
                return e.code, raw.decode(errors="replace")

    def sign_in(self, email, role, name=""):
        code, out = self.call("POST", "/api/auth/test-signin", {"email": email, "name": name, "role": role, "next": "/"})
        if code != 200:
            sys.exit(f"seed: test sign-in as {email} failed ({code} {out}); is the server in testing mode?")


def zip_pack(folder):
    """The pack folder as a zip, as the problem bank's Import pack expects."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
        for f in sorted(folder.rglob("*")):
            if f.is_file():
                z.write(f, f.relative_to(folder).as_posix())
    return buf.getvalue()


def import_packs(owner, problems_dir):
    code, out = owner.call("GET", "/api/admin/problems")
    if code != 200:
        sys.exit(f"seed: listing problems failed ({code} {out})")
    existing = {p["slug"]: p["id"] for p in out["problems"]}
    ids = {}
    for folder in sorted(p for p in problems_dir.iterdir() if (p / "problem.yaml").is_file()):
        slug = folder.name
        if slug in existing:
            ids[slug] = existing[slug]
            print(f"problem {slug}: already imported (id {ids[slug]})")
            continue
        code, out = owner.call("POST", "/api/admin/problems/import", zip_pack(folder), "application/zip")
        if code != 201:
            sys.exit(f"seed: importing {slug} failed ({code} {out})")
        ids[slug] = out["id"]
        print(f"problem {slug}: imported (id {ids[slug]})")
    return ids


def validate(owner, ids, timeout_s):
    for slug, pid in ids.items():
        code, out = owner.call("GET", f"/api/admin/problems/{pid}/validation")
        if code == 200 and out and out.get("current") and out.get("state") == "passed":
            print(f"problem {slug}: already validated")
            continue
        code, out = owner.call("POST", f"/api/admin/problems/{pid}/validate")
        if code != 202:
            sys.exit(f"seed: validating {slug} failed to start ({code} {out})")
        deadline = time.time() + timeout_s
        while True:
            code, out = owner.call("GET", f"/api/admin/problems/{pid}/validation")
            state = out.get("state") if code == 200 and out else None
            if state == "passed":
                print(f"problem {slug}: validated")
                break
            if state in ("failed", "error"):
                sys.exit(f"seed: validation of {slug} ended {state}: {out.get('error') or out.get('summary')}")
            if time.time() > deadline:
                sys.exit(f"seed: validation of {slug} still {state} after {timeout_s} s; is the judge running?")
            time.sleep(2)


def create_users(base, origin, n):
    for i in range(1, n + 1):
        Client(base, origin).sign_in(student_email(i), "student", f"Load student {i}")
        if i % 100 == 0 or i == n:
            print(f"students: {i} of {n}")


def student_email(i):
    # contest.js derives the same address from the VU number.
    return f"student{i:04d}@load.test"


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--base", default="http://127.0.0.1:8080", help="server address (testing mode)")
    ap.add_argument("--origin", default="http://localhost:5173", help="the server's PODIUM_BASE_URL (the testing-mode default)")
    ap.add_argument("--problems", default=str(pathlib.Path(__file__).resolve().parents[2] / "problems"),
                    help="folder of packs to import (default: the repository's problems/)")
    ap.add_argument("--users", type=int, default=700, help="student accounts to create")
    ap.add_argument("--validate-timeout", type=int, default=600, help="seconds to wait for each validation")
    args = ap.parse_args()

    owner = Client(args.base, args.origin)
    code, out = owner.call("GET", "/api/auth/config")
    if code != 200 or out.get("mode") != "testing":
        sys.exit(f"seed: {args.base} is not a podium server in testing mode ({code} {out})")
    owner.sign_in(OWNER, "owner", "Load owner")
    ids = import_packs(owner, pathlib.Path(args.problems))
    validate(owner, ids, args.validate_timeout)
    create_users(args.base, args.origin, args.users)
    print(json.dumps({"problems": ids, "users": args.users}))


if __name__ == "__main__":
    main()
