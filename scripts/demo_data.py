"""Build demo data folders (fake accounts and projects) for the user-guide screenshots.

Usage: python3 make_demo.py <out-dir>
Creates <out-dir>/{welcome,ready,syncing,done} data roots for CLAUDE_SYNC_DATA.
"""
import hashlib, json, os, shutil, sys, uuid
from datetime import datetime, timedelta, timezone

out = sys.argv[1]
now = datetime.now(timezone.utc)
iso = lambda d: d.strftime("%Y-%m-%dT%H:%M:%SZ")
uid = lambda s: str(uuid.uuid5(uuid.NAMESPACE_URL, "claude-sync-demo/" + s))

SRC, TGT = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
PROJECTS = [  # name, docs, files, artifacts in its chats
    ("Acme: Website Revamp", 4, 2, 3), ("Acme: Q3 Planning", 2, 0, 2), ("Acme: Hiring Pipeline", 1, 0, 1),
    ("Acme: Customer Research", 3, 5, 4), ("Acme: Brand Guidelines", 0, 2, 1), ("Acme: Pricing Model", 1, 0, 2),
    ("Acme: Onboarding Kit", 2, 0, 1), ("Acme: Security Review", 1, 1, 1), ("Acme: Partner Program", 2, 0, 0),
    ("Personal: Travel Notes", 1, 0, 1), ("Personal: Reading List", 1, 0, 0), ("Family: Holiday Plans", 0, 1, 1),
]
SKILLS = ["meeting-notes", "proposal-writer", "sql-helper", "brand-voice", "weekly-report"]
MEMORY = """**Work context**

Alex leads product at Acme Corp, a small software company. Prefers short answers with a clear recommendation first.

**Top of mind**

Preparing the Q3 roadmap and a website refresh.
"""


def write(path, data):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(data if isinstance(data, str) else json.dumps(data, indent=2))


def pulled(root, when):
    m = os.path.join(root, "migration")
    docs = files = arts = 0
    for i, (name, nd, nf, na) in enumerate(PROJECTS):
        p = uid(name)
        write(f"{m}/projects/{p}/project.json", {"uuid": p, "name": name, "description": "", "is_private": True,
              "is_starred": False, "prompt_template": "", "updated_at": iso(when), "order": i,
              "synced": f"{iso(when)}|{nd}|{nf}"})
        for d in range(nd):
            write(f"{m}/projects/{p}/docs/{uid(name+'d'+str(d))}.json",
                  {"uuid": uid(name + 'd' + str(d)), "file_name": f"notes-{d+1}.md", "content": "# Notes\n"})
        for f in range(nf):
            fid = uid(name + 'f' + str(f))
            write(f"{m}/projects/{p}/files/{fid}.bin", "%PDF-1.4 demo")
            write(f"{m}/projects/{p}/files/{fid}.json", {"uuid": fid, "file_name": f"brief-{f+1}.pdf",
                  "file_kind": "blob", "size_bytes": 1200000, "mime": "application/pdf"})
        if na:
            c = uid(name + "chat")
            write(f"{m}/chats/{c}.json", {"uuid": c, "name": name + " chat", "project_uuid": p, "updated_at": iso(when),
                  "artifacts": [{"id": f"artifact:{k}", "kind": "artifact", "title": f"Draft {k+1}", "type": "text/markdown",
                                 "file_name": f"Artifact - Draft {k+1}.md", "content": "# Draft\n"} for k in range(na)]})
        docs, files, arts = docs + nd, files + nf, arts + na
    loose = uid("loose-chat")
    write(f"{m}/chats/{loose}.json", {"uuid": loose, "name": "Quick question", "project_uuid": "", "updated_at": iso(when),
          "artifacts": [{"id": "artifact:x", "kind": "artifact", "title": "Idea", "type": "text/markdown",
                         "file_name": "Artifact - Idea.md", "content": "idea"}]})
    for k, name in enumerate(SKILLS):
        sid = "skill_demo" + str(k)
        write(f"{m}/skills/{sid}.skill", "PK demo")
        write(f"{m}/skills/{sid}.json", {"id": sid, "name": name, "description": "", "source": "custom",
              "updated_at": iso(when), "size_bytes": 2048})
    write(f"{m}/memory.md", MEMORY)
    write(f"{m}/manifest.json", {"source_account": "alex@example.com", "source_org": SRC, "pulled_at": iso(when),
          "total": len(PROJECTS), "complete": True, "phase": "", "phase_started_at": "0001-01-01T00:00:00Z",
          "phase_done": 0, "phase_total": 0, "chats_total": 48, "projects": len(PROJECTS), "docs": docs,
          "files": files, "chats": 48, "artifacts": arts + 1, "artifacts_no_project": 1, "skills": len(SKILLS)})
    sel = {uid(n): not n.lower().startswith(("personal:", "family:", "travel")) for n, *_ in PROJECTS}
    write(f"{m}/selection.json", sel)
    for acc in ("source", "target"):
        write(os.path.join(root, "profiles", acc, "Local State"), "{}")
    return sel


def settings(root, extra=None):
    s = {"terms_version": "1", "copy_reviewed_at": "2026-09-01T00:00:00Z", "source_org": SRC, "source_org_name": "Alex (personal)", "target_org": TGT, "target_org_name": "Acme Team"}
    s.update(extra or {})
    write(os.path.join(root, "migration", "settings.json"), s)


def pushed_state(root, sel):
    st = {"target_org": TGT, "projects": {}}
    for name, nd, nf, na in PROJECTS:
        p = uid(name)
        if not sel[p]:
            continue
        done = {"status": "done", "target": uid(name + "t")}
        st["projects"][p] = {"target": uid(name + "t"),
                             "docs": {uid(name + 'd' + str(d)): done for d in range(nd)},
                             "files": {uid(name + 'f' + str(f)): done for f in range(nf)},
                             "artifacts": {f"{uid(name + 'chat')}/artifact:{k}": done for k in range(na)}}
    st["skills"] = {"skill_demo" + str(k): {"status": "done"} for k in range(len(SKILLS))}
    write(os.path.join(root, "migration", "state.json"), st)


for name in ("welcome", "ready", "syncing", "done"):
    shutil.rmtree(os.path.join(out, name), ignore_errors=True)
    os.makedirs(os.path.join(out, name), exist_ok=True)

# ready: pulled, orgs chosen, nothing sent yet
sel = pulled(os.path.join(out, "ready"), now - timedelta(minutes=12))
settings(os.path.join(out, "ready"), {"copy_reviewed_at": None})

# done: everything sent and verified, memory sent, last sync found a little
root = os.path.join(out, "done")
sel = pulled(root, now - timedelta(hours=2))
pushed_state(root, sel)
settings(root, {"memory_sent_at": iso(now - timedelta(hours=2)),
                "memory_sha": hashlib.sha256(MEMORY.encode()).hexdigest(),
                "last_sync_at": iso(now - timedelta(minutes=6)), "last_sync_new_projects": 1,
                "last_sync_changed_projects": 1, "last_sync_chats": 3, "last_sync_artifacts": 4, "last_sync_sent": 7})
write(os.path.join(root, "migration", "verify.json"),
      {p: {"ok": True, "checked_at": iso(now - timedelta(minutes=5))} for p, on in sel.items() if on})

# syncing: a pull reading chats, 40% done, started 10 minutes ago
root = os.path.join(out, "syncing")
sel = pulled(root, now - timedelta(hours=2))
pushed_state(root, sel)
settings(root)
man = json.load(open(os.path.join(root, "migration", "manifest.json")))
man.update({"complete": False, "phase": "chats", "phase_started_at": iso(now - timedelta(minutes=10)),
            "phase_done": 400, "phase_total": 1000, "chats_total": 1400, "chats": 800, "artifacts": 427})
write(os.path.join(root, "migration", "manifest.json"), man)
write(os.path.join(root, "migration", "job.lock"), "pull")
print("demo data in", out)
