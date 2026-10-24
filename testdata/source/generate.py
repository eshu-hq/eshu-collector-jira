import json, os, copy
OUT = "testdata/source"
BASE = "http://jira.fixture.invalid"
NOW = "2026-10-01T12:00:00Z"
ENV = {"JIRA_EMAIL": "bot@fixture.invalid", "JIRA_TOKEN": "tok-SECRET-123"}
CFG = {"site_id": "jira.fixture.invalid", "base_url": BASE, "email_env": "JIRA_EMAIL", "token_env": "JIRA_TOKEN",
       "jql": "project = ESHU", "updated_lookback": "87600h"}

def ok(body, headers=None):
    r = {"status": 200, "body": body}
    if headers: r["headers"] = headers
    return r
def err(status, msgs=None, headers=None):
    r = {"status": status, "body": {"errorMessages": msgs or ["fixture error %d" % status]}}
    if headers: r["headers"] = headers
    return r

def user(n):
    return {"accountId": "acct-%d" % n, "displayName": "Person %d" % n}

def issue(i, key, status, summary, project=("10000", "ESHU")):
    return {"id": str(i), "key": key, "self": "%s/rest/api/3/issue/%d?token=leak#frag" % (BASE, i),
            "fields": {"summary": summary, "created": "2026-09-01T10:00:00.000+0000", "updated": "2026-09-30T11:30:00.000+0000",
                       "resolutiondate": "2026-09-30T12:00:00.000+0000" if status == "Done" else None,
                       "issuetype": {"id": "10002", "name": "Task"}, "status": {"id": "3" if status != "Done" else "10001", "name": status},
                       "project": {"id": project[0], "key": project[1], "name": "Eshu Platform"},
                       "assignee": user(1), "reporter": user(2)}}

def search_resp(issues, token="", last=True):
    r = {"issues": issues, "isLast": last}
    if token: r["nextPageToken"] = token
    return r

def meta_ok(prefix_entries):
    return prefix_entries

def metadata(overrides=None):
    resp = {}
    resp["GET /rest/api/3/project/search?maxResults=100&startAt=0"] = ok({"startAt": 0, "maxResults": 100, "total": 2, "isLast": True, "values": [
        {"id": "10000", "key": "ESHU", "name": "Eshu Platform", "self": BASE + "/rest/api/3/project/10000?token=x", "projectTypeKey": "software",
         "style": "next-gen", "archived": False, "deleted": False, "projectCategory": {"id": "10100", "name": "Platform"},
         "insight": {"lastIssueUpdateTime": "2026-09-30T10:00:00.000+0000", "totalIssueCount": 42}},
        {"id": "10001", "key": "OLD", "name": "Retired", "self": BASE + "/rest/api/3/project/10001", "projectTypeKey": "business",
         "style": "classic", "archived": True, "deleted": False}]})
    resp["GET /rest/api/3/issuetype/project?projectId=10000"] = ok([
        {"id": "10002", "name": "Task", "description": "A task", "entityId": "ent-1", "hierarchyLevel": 0, "subtask": False, "self": BASE + "/rest/api/3/issuetype/10002",
         "scope": {"type": "PROJECT", "project": {"id": "10000"}}},
        {"id": "10003", "name": "Sub-task", "description": "", "hierarchyLevel": -1, "subtask": True, "self": BASE + "/rest/api/3/issuetype/10003"}])
    resp["GET /rest/api/3/issuetype/project?projectId=10001"] = ok([])
    resp["GET /rest/api/3/statuses/search?maxResults=100&startAt=0"] = ok({"startAt": 0, "maxResults": 100, "total": 2, "isLast": True, "values": [
        {"id": "3", "name": "In Progress", "description": "Working", "statusCategory": "IN_PROGRESS", "self": BASE + "/rest/api/3/status/3", "scope": {"type": "PROJECT", "project": {"id": "10000"}}},
        {"id": "10001", "name": "Done", "description": "", "statusCategory": "DONE", "self": BASE + "/rest/api/3/status/10001"}]})
    resp["GET /rest/api/3/field/search?maxResults=100&startAt=0"] = ok({"startAt": 0, "maxResults": 100, "total": 2, "isLast": True, "values": [
        {"id": "summary", "name": "Summary", "description": "", "self": BASE + "/rest/api/3/field/summary", "schema": {"type": "string", "system": "summary"}},
        {"id": "customfield_10010", "name": "Story Points", "description": "estimate", "schema": {"type": "number", "custom": "com.atlassian.jira.plugin.system.customfieldtypes:float", "customId": 10010}}]})
    resp["GET /rest/api/3/workflows/search?maxResults=100&startAt=0"] = ok({"startAt": 0, "maxResults": 100, "total": 1, "isLast": True, "values": [
        {"id": "wf-1", "name": "Eshu Workflow", "description": "flow", "scope": {"type": "PROJECT", "project": {"id": "10000"}},
         "version": {"id": "v-1", "versionNumber": 3},
         "statuses": [{"statusReference": "ref-1", "statusId": "3", "deprecated": False}, {"statusReference": "ref-2", "statusId": "10001", "deprecated": True}],
         "transitions": [{"id": "11", "name": "Start", "type": "DIRECTED", "toStatusReference": "ref-1", "links": [{"fromStatusReference": "ref-2"}, {"fromStatusReference": "ref-0"}, {"fromStatusReference": " "}],
                          "validators": [{}], "triggers": [], "actions": []}]}]})
    if overrides: resp.update(overrides)
    return resp

def basic():
    resp = metadata()
    resp["POST /rest/api/3/search/jql#"] = ok(search_resp([issue(10001, "ESHU-1", "In Progress", "Fix deploy <b>&amp; secret summary"), issue(10002, "ESHU-2", "Done", "Second")], token="tok2", last=False))
    resp["POST /rest/api/3/search/jql#tok2"] = ok(search_resp([issue(10003, "ESHU-3", "To Do", "Third")]))
    resp["GET /rest/api/3/issue/ESHU-1/changelog?maxResults=50&startAt=0"] = ok({"startAt": 0, "maxResults": 50, "total": 4, "isLast": True, "values": [
        {"id": "5001", "author": user(3), "created": "2026-09-20T09:00:00.000+0000", "items": [
            {"field": "status", "fromString": "To Do", "toString": "In Progress", "from": "1", "to": "3", "fieldId": "status"},
            {"field": "assignee", "fromString": "Person 1", "toString": "Person 2", "fieldId": "assignee"},
            {"field": "Story Points", "fromString": "3", "toString": "5", "fieldId": "customfield_10010"}]},
        {"id": "5002", "author": user(1), "created": "2026-09-21T09:00:00.000+0000", "items": [
            {"field": "labels", "fromString": "a", "toString": "b"},
            {"field": "labels", "fromString": "b", "toString": "c"}]},
        {"id": "5003", "created": "2026-09-22T09:00:00.000+0000", "items": [{"field": "resolution", "from": "9", "to": "10"}]}]})
    resp["GET /rest/api/3/issue/ESHU-2/changelog?maxResults=50&startAt=0"] = ok({"values": []})
    resp["GET /rest/api/3/issue/ESHU-3/changelog?maxResults=50&startAt=0"] = ok({"startAt": 0, "maxResults": 50, "total": 1, "isLast": True, "values": [
        {"id": "5101", "author": user(3), "created": "2026-09-25T09:00:00.000+0000", "items": [{"field": "summary", "fromString": "Old title", "toString": "Third"}]}]})
    resp["GET /rest/api/3/issue/ESHU-1/remotelink"] = ok([
        {"id": 7001, "globalId": "gh-1", "application": {"name": "GitHub", "type": "com.github"}, "relationship": "mentioned in",
         "object": {"url": "https://github.com/Eshu-HQ/Eshu/pull/42?token=abc&ref=x#frag", "title": "Fix deploy", "summary": "PR summary"}},
        {"id": 7002, "application": {"name": "Confluence", "type": "com.atlassian.confluence"}, "object": {"url": "https://wiki.fixture.invalid/pages/1", "title": "Design"}},
        {"id": 7001, "globalId": "gh-1-dup", "application": {"name": "GitHub", "type": "com.github"}, "object": {"url": "https://github.com/Eshu-HQ/Eshu/pull/42"}},
        {"id": 7003, "application": {"name": "GitLab", "type": "gitlab"}, "relationship": "implements", "object": {"url": "https://gitlab.fixture.invalid/Group/Sub/Repo/-/merge_requests/7", "title": "MR"}},
        {"application": {"name": "Weird"}, "object": {}},
        {"id": "str-id", "object": {"url": "https://user:pw@example.invalid/x?api_key=1"}}])
    resp["GET /rest/api/3/issue/ESHU-2/remotelink"] = ok([])
    resp["GET /rest/api/3/issue/ESHU-3/remotelink"] = ok([])
    return {"name": "basic", "now": NOW, "env": ENV, "config": dict(CFG), "responses": resp}

def degraded():
    s = basic()
    s["name"] = "degraded"
    r = s["responses"]
    r["GET /rest/api/3/issuetype/project?projectId=10001"] = err(403)
    r["GET /rest/api/3/workflows/search?maxResults=100&startAt=0"] = err(404)
    r["GET /rest/api/3/statuses/search?maxResults=100&startAt=0"] = err(410)
    return s

def paged():
    resp = metadata()
    cfg = dict(CFG)
    cfg.update({"issue_limit": 2, "changelog_limit": 2, "remote_link_limit": 1, "metadata_limit": 1})
    # metadata_limit=1 changes the queries
    resp = {}
    resp["GET /rest/api/3/project/search?maxResults=1&startAt=0"] = ok({"startAt": 0, "maxResults": 1, "total": 2, "isLast": False, "values": [
        {"id": "10000", "key": "ESHU", "name": "Eshu Platform", "projectTypeKey": "software"}]})
    resp["GET /rest/api/3/issuetype/project?projectId=10000"] = ok([{"id": "10002", "name": "Task"}, {"id": "10003", "name": "Bug"}])
    resp["GET /rest/api/3/statuses/search?maxResults=1&startAt=0"] = ok({"startAt": 0, "maxResults": 1, "total": 5, "isLast": False, "values": [{"id": "3", "name": "In Progress", "statusCategory": "IN_PROGRESS"}]})
    resp["GET /rest/api/3/field/search?maxResults=1&startAt=0"] = ok({"startAt": 0, "maxResults": 1, "total": 9, "isLast": False, "values": [{"id": "summary", "name": "Summary", "schema": {"type": "string", "system": "summary"}}]})
    resp["GET /rest/api/3/workflows/search?maxResults=1&startAt=0"] = ok({"startAt": 0, "maxResults": 1, "total": 3, "isLast": False, "values": [{"id": "wf-1", "name": "W", "scope": {"type": "GLOBAL"}, "version": {"id": "v", "versionNumber": 1}}]})
    resp["POST /rest/api/3/search/jql#"] = ok(search_resp([issue(10001, "ESHU-1", "To Do", "One")], token="p2", last=False))
    resp["POST /rest/api/3/search/jql#p2"] = ok(search_resp([issue(10002, "ESHU-2", "To Do", "Two")], token="p3", last=False))
    # issue_limit=2 reached: truncated because provider still offers p3.
    resp["GET /rest/api/3/issue/ESHU-1/changelog?maxResults=2&startAt=0"] = ok({"startAt": 0, "maxResults": 1, "total": 3, "isLast": False, "values": [
        {"id": "6001", "created": "2026-09-20T09:00:00.000+0000", "items": [{"field": "status", "fromString": "A", "toString": "B"}]}]})
    resp["GET /rest/api/3/issue/ESHU-1/changelog?maxResults=1&startAt=1"] = ok({"startAt": 1, "maxResults": 1, "total": 3, "isLast": False, "values": [
        {"id": "6002", "created": "2026-09-21T09:00:00.000+0000", "items": [{"field": "status", "fromString": "B", "toString": "C"}, {"field": "priority", "fromString": "Low", "toString": "High"}]}]})
    resp["GET /rest/api/3/issue/ESHU-2/changelog?maxResults=2&startAt=0"] = ok({"values": []})
    resp["GET /rest/api/3/issue/ESHU-1/remotelink"] = ok([
        {"id": 8001, "application": {"name": "GitHub", "type": "com.github"}, "object": {"url": "https://github.com/a/b/pull/1"}},
        {"id": 8002, "application": {"name": "GitHub", "type": "com.github"}, "object": {"url": "https://github.com/a/b/pull/2"}}])
    resp["GET /rest/api/3/issue/ESHU-2/remotelink"] = ok([])
    return {"name": "paged", "now": NOW, "env": ENV, "config": cfg, "responses": resp}

def failure(name, search_resp_entry=None, overrides=None):
    s = basic()
    s["name"] = name
    if search_resp_entry is not None:
        s["responses"]["POST /rest/api/3/search/jql#"] = search_resp_entry
    if overrides: s["responses"].update(overrides)
    return s

scenarios = [basic(), degraded(), paged(),
    failure("failure_unauthorized", err(401)),
    failure("failure_forbidden", err(403)),
    failure("failure_not_found", err(404)),
    failure("failure_gone", err(410)),
    failure("failure_rate_limited", err(429, headers={"Retry-After": "120", "RateLimit-Reason": "jira-quota"})),
    failure("failure_bad_request", err(400)),
    failure("failure_server_error", err(503)),
    failure("failure_changelog_error", overrides={"GET /rest/api/3/issue/ESHU-3/changelog?maxResults=50&startAt=0": err(500)}),
    failure("failure_remote_link_gone", overrides={"GET /rest/api/3/issue/ESHU-2/remotelink": err(410)}),
    failure("failure_metadata_server", overrides={"GET /rest/api/3/project/search?maxResults=100&startAt=0": err(502)}),
    failure("failure_metadata_forbidden_projects", overrides={"GET /rest/api/3/project/search?maxResults=100&startAt=0": err(403)}),
    failure("failure_decode", ok({"issues": "not-a-list"})),
]
os.makedirs(OUT, exist_ok=True)
for s in scenarios:
    with open(os.path.join(OUT, s["name"] + ".json"), "w") as f:
        json.dump(s, f, indent=2, sort_keys=True)
        f.write("\n")
print(len(scenarios), "scenarios")
