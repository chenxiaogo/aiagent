#!/usr/bin/env python3
"""MCP stdio 桥接：把 aiagent 的 HTTP JSON-RPC 端点暴露成本地 stdio MCP 服务器。

为什么需要它
------------
aiagent 目前只实现了两种 HTTP 传输：

  * SSE 双通道（2024-11-05）：GET  /api/mcp/agents/<slug>/sse + POST .../messages
  * 自定义 JSON-RPC：        POST /api/mcp/agents/<slug>/stream

它**没有** Streamable HTTP（单端点 POST，2025-03-26 起的主流传输），
而较新的 MCP 客户端（含 opencode）默认按 Streamable HTTP 去连，直接 404。
stdio 则是所有 MCP 客户端都支持的传输，用它绕开传输层差异最稳。

用法（opencode / Claude Desktop / Cursor 均按此配置）
------------------------------------------------
    python3 server/tests/mcp_stdio_bridge.py \\
        --url http://127.0.0.1:8080/api/mcp/agents/agent-6/stream \\
        --api-key ak-xxxxxxxx

也支持环境变量：MCP_URL / MCP_API_KEY（命令行参数优先）。

协议细节
--------
* stdio 帧为换行分隔的 JSON（NDJSON），一进一出。
* 对 notification（无 id 的请求，如 notifications/initialized）**不回写响应**，
  符合 JSON-RPC 规范，也避免客户端解析器错位。
* 所有运行日志写 stderr，stdout 只承载协议报文。
"""

import argparse
import json
import os
import sys

try:
    import requests
except ImportError:
    sys.stderr.write("缺少依赖，请先执行：pip3 install requests\n")
    sys.exit(2)


def log(msg):
    sys.stderr.write(f"[mcp-bridge] {msg}\n")
    sys.stderr.flush()


def main():
    ap = argparse.ArgumentParser(description="aiagent MCP stdio 桥接")
    ap.add_argument("--url", default=os.environ.get("MCP_URL", ""),
                    help="aiagent 的 /stream 端点完整 URL")
    ap.add_argument("--api-key", default=os.environ.get("MCP_API_KEY", ""),
                    help="客户 API Key（平台全局端点可留空）")
    ap.add_argument("--timeout", type=float, default=60.0)
    args = ap.parse_args()

    if not args.url:
        log("缺少 --url（或环境变量 MCP_URL）")
        return 2

    headers = {"Content-Type": "application/json"}
    if args.api_key:
        headers["X-Api-Key"] = args.api_key

    session = requests.Session()
    log(f"桥接到 {args.url}")

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except Exception as e:
            log(f"忽略非法 JSON 帧: {e}")
            continue

        method = req.get("method", "")
        is_notification = "id" not in req
        log(f"<- {method} (id={req.get('id')})")

        try:
            resp = session.post(args.url, json=req, headers=headers, timeout=args.timeout)
        except Exception as e:
            if is_notification:
                continue  # 通知没有响应通道，失败也无需告知客户端
            out = {"jsonrpc": "2.0", "id": req.get("id"),
                   "error": {"code": -32603, "message": f"bridge request failed: {e}"}}
            sys.stdout.write(json.dumps(out) + "\n")
            sys.stdout.flush()
            continue

        # 通知类请求不回写响应（服务端可能仍回了，但客户端不期望）
        if is_notification:
            continue

        try:
            body = resp.json()
        except Exception:
            body = {"jsonrpc": "2.0", "id": req.get("id"),
                    "error": {"code": -32603,
                              "message": f"non-JSON response (HTTP {resp.status_code}): {resp.text[:200]}"}}

        if "id" not in body:
            body["id"] = req.get("id")

        err = body.get("error")
        log(f"-> {method} HTTP {resp.status_code}"
            + (f" error={err.get('code')}" if isinstance(err, dict) else ""))

        sys.stdout.write(json.dumps(body, ensure_ascii=False) + "\n")
        sys.stdout.flush()

    return 0


if __name__ == "__main__":
    sys.exit(main())
