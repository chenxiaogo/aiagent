#!/usr/bin/env python3
"""aiagent MCP 端点冒烟测试。

验证两类 MCP 端点能否被标准 MCP 客户端（Cursor / opencode / Claude Desktop 等）调用：

  1. 平台全局 MCP   POST /api/mcp/stream               （当前无鉴权）
  2. 智能体对外 MCP POST /api/mcp/agents/<slug>/stream （X-Api-Key 鉴权）
  3. SSE 传输       GET  /api/mcp/agents/<slug>/sse
                    POST /api/mcp/agents/<slug>/messages

脚本会自动：登录 → 挑一个已发布且暴露了工具的智能体 → 建临时凭据 → 跑用例 → 删凭据。

用法：
    python3 server/tests/mcp_smoke_test.py
    python3 server/tests/mcp_smoke_test.py --base-url http://10.0.0.5:8080
    python3 server/tests/mcp_smoke_test.py --agent-id 2 --keep-key

依赖：python3 + requests
"""

import argparse
import json
import queue
import re
import sys
import threading
import time

try:
    import requests
except ImportError:
    print("缺少依赖，请先执行：pip3 install requests")
    sys.exit(2)

# ---------- 结果收集 ----------

RESULTS = []


def record(name, ok, detail=""):
    RESULTS.append((name, ok, detail))
    flag = "PASS" if ok else "FAIL"
    line = f"  [{flag}] {name}"
    if detail:
        line += f"  -> {detail}"
    print(line)
    return ok


def section(title):
    print(f"\n{title}")


# ---------- JSON-RPC ----------

_rpc_id = [0]


def rpc(method, params=None, rid=None):
    if rid is None:
        _rpc_id[0] += 1
        rid = _rpc_id[0]
    body = {"jsonrpc": "2.0", "id": rid, "method": method}
    if params is not None:
        body["params"] = params
    return body


def post_json(url, payload, headers=None, timeout=15):
    return requests.post(url, json=payload, headers=headers or {}, timeout=timeout)


def short(text, n=90):
    text = " ".join(str(text).split())
    return text if len(text) <= n else text[: n - 3] + "..."


# ---------- 测试用例 ----------


def test_global_stream(base):
    """平台全局 MCP：无需鉴权的 JSON-RPC 端点。"""
    section("A. 平台全局 MCP  POST /api/mcp/stream（无鉴权）")
    url = f"{base}/api/mcp/stream"

    # A1 initialize
    r = post_json(url, rpc("initialize", {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {"name": "smoke-test", "version": "1.0.0"},
    }))
    ok = r.status_code == 200 and r.json().get("result", {}).get("serverInfo")
    record("A1 initialize 返回 serverInfo", ok,
           short(r.json().get("result", {}).get("serverInfo")) if ok else f"HTTP {r.status_code}")

    # A2 tools/list
    r = post_json(url, rpc("tools/list", {}))
    tools = r.json().get("result", {}).get("tools", []) if r.status_code == 200 else []
    names = [t.get("name") for t in tools]
    record("A2 tools/list 返回工具", len(tools) > 0, f"{len(tools)} 个: {', '.join(names)}")

    # A3 tools/call
    r = post_json(url, rpc("tools/call", {"name": "list_agents", "arguments": {}}))
    ok = r.status_code == 200 and "result" in r.json() and "error" not in r.json()
    txt = ""
    if ok:
        content = r.json()["result"].get("content", [{}])
        txt = content[0].get("text", "") if content else ""
    record("A3 tools/call list_agents", ok, short(txt) if ok else short(r.text))

    # A4 未知方法应返回 -32601
    r = post_json(url, rpc("no/such/method", {}))
    code = r.json().get("error", {}).get("code")
    record("A4 未知方法返回 -32601", code == -32601, f"code={code}")


def test_agent_stream(base, slug, key, expected_tools):
    """智能体对外 MCP：X-Api-Key 鉴权的 JSON-RPC 端点。"""
    section(f"B. 智能体对外 MCP  POST /api/mcp/agents/{slug}/stream（X-Api-Key）")
    url = f"{base}/api/mcp/agents/{slug}/stream"

    # B1 无 Key
    r = post_json(url, rpc("initialize", {"protocolVersion": "2024-11-05"}))
    record("B1 无 Key 被拒绝", r.status_code == 401, f"HTTP {r.status_code}")

    # B2 错误 Key
    r = post_json(url, rpc("initialize", {"protocolVersion": "2024-11-05"}),
                  headers={"X-Api-Key": "ak-definitely-wrong"})
    record("B2 错误 Key 被拒绝", r.status_code in (401, 403), f"HTTP {r.status_code}")

    headers = {"X-Api-Key": key}

    # B3 initialize
    r = post_json(url, rpc("initialize", {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {"name": "smoke-test", "version": "1.0.0"},
    }), headers=headers)
    body = r.json() if r.status_code == 200 else {}
    result = body.get("result", {})
    ok = bool(result.get("serverInfo"))
    record("B3 initialize 返回 serverInfo", ok, short(result.get("serverInfo")))

    # B3b 协议版本协商：客户端要 2025-06-18 时服务端回什么
    r2 = post_json(url, rpc("initialize", {
        "protocolVersion": "2025-06-18",
        "capabilities": {},
        "clientInfo": {"name": "smoke-test", "version": "1.0.0"},
    }), headers=headers)
    negotiated = r2.json().get("result", {}).get("protocolVersion") if r2.status_code == 200 else None
    print(f"  [INFO] 请求 2025-06-18 → 服务端回 {negotiated}（协商失败会使新版客户端直接断开）")

    # B4 notification（客户端初始化后会发，不应被当成错误）
    note = {"jsonrpc": "2.0", "method": "notifications/initialized"}
    r = post_json(url, note, headers=headers)
    has_err = "error" in (r.json() if r.status_code == 200 else {})
    record("B4 notifications/initialized 不报错", not has_err, f"HTTP {r.status_code}")

    # B5 tools/list
    r = post_json(url, rpc("tools/list", {}), headers=headers)
    tools = r.json().get("result", {}).get("tools", []) if r.status_code == 200 else []
    names = sorted(t.get("name") for t in tools)
    ok = len(tools) > 0
    record("B5 tools/list 返回工具", ok, f"{len(tools)} 个: {', '.join(names)}")
    if expected_tools:
        missing = sorted(set(expected_tools) - set(names))
        record("B5b 工具集与发布快照一致", not missing,
               "一致" if not missing else f"快照有但接口未返回: {missing}")

    # B6 tools/call：挑一个无必填参数的工具
    call_name, call_args = pick_callable_tool(names)
    r = post_json(url, rpc("tools/call", {"name": call_name, "arguments": call_args}),
                  headers=headers)
    body = r.json() if r.status_code == 200 else {}
    call_ok = "result" in body and "error" not in body
    content = body.get("result", {}).get("content", [{}])
    txt = content[0].get("text", "") if content else ""
    is_error = body.get("result", {}).get("isError", False)
    record(f"B6 tools/call {call_name}", call_ok and not is_error, short(txt))

    # B7 未暴露的工具应被拒绝
    r = post_json(url, rpc("tools/call", {"name": "__not_exposed__", "arguments": {}}),
                  headers=headers)
    body = r.json() if r.status_code == 200 else {}
    is_error = body.get("result", {}).get("isError", False)
    record("B7 未暴露工具返回 isError", is_error, short(body.get("result", {}).get("content", [{}])[0].get("text", "")))


def pick_callable_tool(names):
    """挑一个无需必填参数、只读的工具来测调用链路。"""
    for cand, args in (("agent_info", {}), ("list_videos", {}),
                       ("list_knowledge_bases", {}), ("list_reports", {}),
                       ("list_agents", {})):
        if cand in names:
            return cand, args
    # 都带必填参数时，退而求其次传一个样例值
    if "video_search" in names:
        return "video_search", {"query": "测试"}
    if "doc_search" in names:
        return "doc_search", {"query": "测试"}
    if "camera_search" in names:
        return "camera_search", {"query": "测试"}
    return names[0] if names else "agent_info", {}


def test_agent_sse(base, slug, key):
    """SSE 传输：GET /sse 建流，POST /messages 发消息，响应经 SSE 回传。"""
    section(f"C. SSE 传输  GET /api/mcp/agents/{slug}/sse")
    url = f"{base}/api/mcp/agents/{slug}/sse"
    headers = {"X-Api-Key": key, "Accept": "text/event-stream"}

    try:
        resp = requests.get(url, headers=headers, stream=True, timeout=(5, 8))
    except Exception as e:
        record("C1 建立 SSE 连接", False, str(e))
        return

    if resp.status_code != 200:
        record("C1 建立 SSE 连接", False, f"HTTP {resp.status_code}")
        return

    # SSE 规定 UTF-8；requests 对 text/event-stream 无 charset 时会退回 latin-1，
    # 中文工具描述会被解出 C1 控制字符，进而让 json.loads 失败。
    resp.encoding = "utf-8"

    events = queue.Queue()

    def reader():
        try:
            buf = []
            for raw in resp.iter_lines(decode_unicode=True):
                if raw is None:
                    continue
                if raw == "":
                    if buf:
                        events.put("\n".join(buf))
                        buf = []
                    continue
                buf.append(raw)
        except Exception:
            pass

    threading.Thread(target=reader, daemon=True).start()

    # 等 endpoint 事件，拿到 sessionId 与回传地址
    endpoint = None
    deadline = time.time() + 6
    while time.time() < deadline:
        try:
            evt = events.get(timeout=1)
        except queue.Empty:
            continue
        m = re.search(r"data:\s*(\S+)", evt)
        if m:
            endpoint = m.group(1)
            break

    if not endpoint:
        record("C1 建立 SSE 连接", False, "10s 内未收到 endpoint 事件")
        resp.close()
        return
    session_m = re.search(r"sessionId=([^&\s]+)", endpoint)
    session = session_m.group(1) if session_m else ""
    record("C1 建立 SSE 连接并拿到 endpoint", True, endpoint)

    msg_url = f"{base}/api/mcp/agents/{slug}/messages?sessionId={session}"

    def roundtrip(label, payload, matcher, timeout=6):
        """发一条 JSON-RPC 到 /messages，等响应经 SSE 回传。"""
        pr = post_json(msg_url, payload, headers={"X-Api-Key": key})
        if pr.status_code not in (200, 202):
            record(f"{label} POST /messages 被接受", False, f"HTTP {pr.status_code}")
            return None
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                evt = events.get(timeout=1)
            except queue.Empty:
                continue
            m = re.search(r"data:\s*(.+)", evt)
            if m and matcher(m.group(1)):
                record(f"{label} SSE 回传响应", True, short(m.group(1)))
                try:
                    return json.loads(m.group(1))
                except Exception as e:
                    record(f"{label} 响应解析失败", False, str(e))
                    return None
        record(f"{label} SSE 回传响应", False, f"{timeout}s 内未收到匹配的事件")
        return None

    # initialize：握手
    roundtrip("C2 initialize", rpc("initialize", {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {"name": "smoke-test", "version": "1.0.0"},
    }), lambda s: "serverInfo" in s)

    # tools/list：客户端连上后第一件事就是拉工具清单
    listed = roundtrip("C3 tools/list", rpc("tools/list", {}), lambda s: '"tools"' in s)
    if listed:
        names = sorted(t.get("name") for t in listed.get("result", {}).get("tools", []))
        record("C3b SSE 通道拿到工具清单", bool(names), ", ".join(names))

        # tools/call：真正跑一次工具，验证结果能经 SSE 回传
        call_name, call_args = pick_callable_tool(names)
        result = roundtrip(f"C4 tools/call {call_name}",
                           rpc("tools/call", {"name": call_name, "arguments": call_args}),
                           lambda s: '"content"' in s)
        if result:
            content = result.get("result", {}).get("content", [{}])
            is_error = result.get("result", {}).get("isError", False)
            record(f"C4b {call_name} 执行成功", not is_error,
                   short(content[0].get("text", "") if content else ""))

    resp.close()


# ---------- 环境准备 ----------


def login(base, username, password):
    r = post_json(f"{base}/api/auth/login", {"username": username, "password": password})
    if r.status_code != 200 or not r.json().get("token"):
        print(f"登录失败：HTTP {r.status_code} {short(r.text)}")
        return None
    return r.json()["token"]


def pick_agent(base, token, agent_id=None):
    """挑一个已发布、且发布快照里暴露了工具的智能体。"""
    h = {"Authorization": f"Bearer {token}"}
    r = requests.get(f"{base}/api/agents?pageSize=100", headers=h, timeout=15)
    agents = (r.json().get("data") or {}).get("list") or []
    published = [a for a in agents if a.get("status") == "published"]
    if agent_id:
        published = [a for a in published if a.get("id") == agent_id] or published
    if not published:
        print("没有已发布的智能体，无法测试智能体 MCP 端点")
        return None, []

    for a in published:
        vr = requests.get(f"{base}/api/agents/{a['id']}/versions", headers=h, timeout=15)
        releases = (vr.json().get("data") or {}).get("releases") or []
        if not releases:
            continue
        cur_id = (vr.json().get("data") or {}).get("currentReleaseId")
        cur = next((x for x in releases if x.get("id") == cur_id), releases[0])
        try:
            snap = json.loads(cur.get("snapshot") or "{}")
        except Exception:
            snap = {}
        exposed = snap.get("exposedTools") or []
        if exposed:
            return a, exposed
    return published[0], []


def create_client(base, token, agent_id, name):
    r = post_json(f"{base}/api/agents/{agent_id}/clients",
                  {"name": name, "tenantName": "冒烟测试", "scopes": ["mcp"],
                   "quotaRpm": 120, "quotaTpd": 10000},
                  headers={"Authorization": f"Bearer {token}"})
    data = (r.json() or {}).get("data") or {}
    return data.get("plainKey"), (data.get("client") or {}).get("id")


def delete_client(base, token, agent_id, client_id):
    requests.delete(f"{base}/api/agents/{agent_id}/clients/{client_id}",
                    headers={"Authorization": f"Bearer {token}"}, timeout=15)


# ---------- 主流程 ----------


def main():
    ap = argparse.ArgumentParser(description="aiagent MCP 端点冒烟测试")
    ap.add_argument("--base-url", default="http://127.0.0.1:8080")
    ap.add_argument("--username", default="admin")
    ap.add_argument("--password", default="admin123")
    ap.add_argument("--agent-id", type=int, default=None, help="指定智能体 ID，默认自动挑")
    ap.add_argument("--keep-key", action="store_true", help="保留测试凭据不删除")
    args = ap.parse_args()

    base = args.base_url.rstrip("/")
    print(f"目标服务：{base}")

    try:
        requests.get(f"{base}/api/health", timeout=8)
    except Exception as e:
        print(f"服务不可达：{e}")
        return 2

    # 1. 平台全局端点（不需要登录）
    test_global_stream(base)

    # 2. 智能体端点（需要准备凭据）
    token = login(base, args.username, args.password)
    if not token:
        print("\n登录失败，跳过 B/C 组用例")
    else:
        agent, exposed = pick_agent(base, token, args.agent_id)
        if not agent:
            print("\n无可用智能体，跳过 B/C 组用例")
        else:
            slug = agent.get("slug")
            print(f"\n选用智能体：#{agent.get('id')} {agent.get('name')} (slug={slug})，"
                  f"快照暴露工具: {', '.join(exposed) or '无'}")
            name = f"mcp-smoke-{int(time.time())}"
            key, cid = create_client(base, token, agent["id"], name)
            if not key:
                print("创建测试凭据失败，跳过 B/C 组用例")
            else:
                try:
                    test_agent_stream(base, slug, key, exposed)
                    test_agent_sse(base, slug, key)
                    if not args.keep_key:
                        print(f"\n  生成的测试凭据（保留用 --keep-key）：{key}")
                finally:
                    if not args.keep_key and cid:
                        delete_client(base, token, agent["id"], cid)
                        print("  已清理临时凭据")
                    elif args.keep_key:
                        print(f"\n  保留凭据：{key}")

    # 汇总
    passed = sum(1 for _, ok, _ in RESULTS if ok)
    total = len(RESULTS)
    print(f"\n{'=' * 60}")
    print(f"结果：{passed}/{total} 通过")
    failed = [n for n, ok, _ in RESULTS if not ok]
    if failed:
        print("失败项：" + ", ".join(failed))
    print("=" * 60)
    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())
