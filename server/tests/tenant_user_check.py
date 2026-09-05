#!/usr/bin/env python3
"""客户 ↔ 系统用户 对齐检查。

客户主体应该是平台用户：授权谁调用智能体，就是从用户列表里挑人。
这个脚本把两边的现状摊开，看「客户有没有对应到具体用户」。

用法：
    python3 server/tests/tenant_user_check.py
    python3 server/tests/tenant_user_check.py --base-url http://10.0.0.5:8080

退出码：0 = 全部对齐；1 = 存在未关联客户；2 = 无法检查（服务/登录问题）
"""

import argparse
import sys

try:
    import requests
except ImportError:
    print("缺少依赖，请先执行：pip3 install requests")
    sys.exit(2)


def main():
    ap = argparse.ArgumentParser(description="aiagent 客户与系统用户对齐检查")
    ap.add_argument("--base-url", default="http://127.0.0.1:8080")
    ap.add_argument("--username", default="admin")
    ap.add_argument("--password", default="admin123")
    args = ap.parse_args()

    base = args.base_url.rstrip("/")

    try:
        lr = requests.post(f"{base}/api/auth/login",
                           json={"username": args.username, "password": args.password}, timeout=15)
    except Exception as e:
        print(f"服务不可达：{e}")
        return 2
    if lr.status_code != 200 or not lr.json().get("token"):
        print(f"登录失败：HTTP {lr.status_code} {lr.text[:200]}")
        return 2

    token = lr.json()["token"]
    h = {"Authorization": f"Bearer {token}"}

    tr = requests.get(f"{base}/api/tenants", headers=h, timeout=15)
    if tr.status_code == 404:
        print("接口 /api/tenants 不存在 —— 后端尚未重启到包含客户管理的新版本")
        return 2
    if tr.status_code != 200:
        print(f"拉取客户列表失败：HTTP {tr.status_code} {tr.text[:200]}")
        return 2
    tenants = tr.json().get("data") or []

    cr = requests.get(f"{base}/api/tenants/candidates", headers=h, timeout=15)
    if cr.status_code != 200:
        print(f"拉取用户列表失败：HTTP {cr.status_code} {cr.text[:200]}")
        return 2
    users = cr.json().get("data") or []

    print(f"目标服务：{base}\n")

    print("=" * 68)
    print(f"{'客户':<24}{'用户':<20}{'凭据':>6}{'订阅':>6}  状态")
    print("=" * 68)
    linked, orphan = [], []
    for t in tenants:
        name = (t.get("name") or "")[:22]
        uname = t.get("username") or "-"
        row = f"{name:<24}{uname:<20}{t.get('clientCount', 0):>6}{t.get('subCount', 0):>6}"
        if t.get("userId"):
            if t.get("userStatus") == 0:
                row += "  已关联（但该用户已停用/删除）"
            else:
                row += "  已关联"
            linked.append(t)
        else:
            row += "  ⚠ 未关联用户"
            orphan.append(t)
        print(row)

    if not tenants:
        print("（暂无客户）")

    print("\n" + "=" * 68)
    print("可作为客户的系统用户")
    print("=" * 68)
    for u in users:
        label = u.get("nickname") or u.get("username")
        role = u.get("roleName") or ("管理员" if u.get("isAdmin") else "-")
        state = f"已是客户（{u.get('tenantName')}）" if u.get("tenantId") else "尚未成为客户"
        stopped = "" if u.get("status") == 1 else "  [已停用]"
        print(f"  #{u.get('id'):<4}{label:<20}{role:<12}{state}{stopped}")
    if not users:
        print("（暂无用户）")

    print("\n" + "=" * 68)
    print(f"客户 {len(tenants)} 个：已关联 {len(linked)}，未关联 {len(orphan)}")
    print(f"系统用户 {len(users)} 个：其中已是客户 {sum(1 for u in users if u.get('tenantId'))} 个")
    if orphan:
        print("\n未关联客户的租户（需人工绑定或清理）：")
        for t in orphan:
            print(f"  #{t.get('id')} {t.get('name')} —— 可用 PUT /api/tenants/{t.get('id')}/bind "
                  f"绑定，或 DELETE /api/tenants/{t.get('id')} 删除")
    print("=" * 68)
    return 0 if not orphan else 1


if __name__ == "__main__":
    sys.exit(main())
