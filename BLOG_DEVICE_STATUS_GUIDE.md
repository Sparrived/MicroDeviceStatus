# 博客设备状态调用指南

本文面向 Next.js 博客开发者，说明如何安全调用 MicroDeviceStatus（MDS）公开设备状态接口。

博客只调用 MDS 的公开快照接口，不直接访问 SQLite、不调用管理接口，也不在浏览器中保存任何 MDS 令牌。

## 1. 调用接口

```text
GET {MDS_API_URL}/api/v1/public/snapshot
Authorization: Bearer {MDS_PUBLIC_STATUS_TOKEN}
```

例如：

```bash
curl -sS https://status.example.com/api/v1/public/snapshot \
  -H "Authorization: Bearer $MDS_PUBLIC_STATUS_TOKEN"
```

接口返回的设备由 MDS 服务端的 `MDS_PUBLIC_DEVICE_IDS` 白名单决定。博客不能通过请求参数读取其他设备。

## 2. 博客环境变量

在博客部署平台配置以下服务端变量：

```env
MDS_API_URL=https://status.example.com
MDS_PUBLIC_STATUS_TOKEN=与 MDS 服务端相同的只读令牌
```

注意：

- 不要使用 `NEXT_PUBLIC_MDS_PUBLIC_STATUS_TOKEN`。
- 不要在客户端组件中读取 `process.env.MDS_PUBLIC_STATUS_TOKEN`。
- 不要把 `MDS_ADMIN_TOKEN` 配置到博客中。
- `MDS_API_URL` 不要带结尾 `/`，代码也应处理结尾斜杠。

## 3. Next.js 服务端代理

推荐在博客中增加服务端路由，例如：

```text
app/api/device-status/route.ts
```

示例实现：

```ts
import { NextResponse } from "next/server";

export const runtime = "nodejs";

export async function GET() {
  const apiURL = process.env.MDS_API_URL?.replace(/\/+$/, "");
  const token = process.env.MDS_PUBLIC_STATUS_TOKEN;

  if (!apiURL || !token) {
    return NextResponse.json(
      { status: "unavailable", error: "device status is not configured" },
      { status: 503 },
    );
  }

  try {
    const response = await fetch(`${apiURL}/api/v1/public/snapshot`, {
      headers: { Authorization: `Bearer ${token}` },
      next: { revalidate: 60 },
    });

    if (!response.ok) {
      return NextResponse.json(
        { status: "unavailable", error: `MDS returned ${response.status}` },
        { status: 503 },
      );
    }

    const snapshot = await response.json();
    return NextResponse.json(snapshot, {
      headers: {
        "Cache-Control": "s-maxage=60, stale-while-revalidate=300",
      },
    });
  } catch {
    return NextResponse.json(
      { status: "unavailable", error: "MDS is temporarily unavailable" },
      { status: 503 },
    );
  }
}
```

浏览器只访问博客自己的路由：

```ts
const response = await fetch("/api/device-status");
const data = await response.json();
```

这样浏览器开发者工具中不会出现 MDS 地址中的只读令牌。

## 4. 响应结构

成功响应：

```json
{
  "generated_at": "2026-07-18T09:00:00Z",
  "status_policy": {
    "online_after_seconds": 300,
    "stale_after_seconds": 1800
  },
  "devices": [
    {
      "id": "computer-device-id",
      "name": "我的电脑",
      "platform": "windows",
      "status": "online",
      "heartbeat_age_seconds": 18,
      "last_seen_at": "2026-07-18T08:59:42Z",
      "reported_at": "2026-07-18T08:59:40Z",
      "metrics": {
        "cpu_percent": 12.5,
        "memory_percent": 43.2,
        "disk_used_percent": 68.1,
        "battery_percent": null,
        "network_connected": null
      },
      "foreground_app": {
        "name": "Visual Studio Code",
        "process_name": "Code.exe",
        "package_name": null,
        "captured_at": "2026-07-18T08:59:40Z"
      },
      "location": null
    }
  ]
}
```

所有公开字段都是固定投影。没有采集到的字段返回 `null`，博客不应根据字段是否存在判断设备状态。

## 5. 状态显示

`status` 已由 MDS 根据服务端接收时间计算：

| 值 | 含义 | 推荐文案 |
|---|---|---|
| `never_seen` | 设备从未收到过心跳 | 尚未上报 |
| `online` | 距离最近一次服务端接收心跳小于在线阈值 | 在线 |
| `stale` | 已超过在线阈值，但未超过延迟阈值 | 延迟 |
| `offline` | 超过延迟阈值没有收到心跳 | 离线 |

默认阈值是在线 `300` 秒、延迟 `1800` 秒，以响应中的 `status_policy` 为准。

`offline` 只表示 MDS 在规定时间内没有收到心跳，不能显示为“已确认关机”。断网、休眠、客户端崩溃、后台服务被杀和关机都会表现为离线。

博客应显示：

- 当前状态。
- `last_seen_at`，即 MDS 服务端最后收到心跳的时间。
- `generated_at`，即本次快照生成时间。
- `heartbeat_age_seconds`，有值时可显示“约 N 秒前”。

## 6. 公开字段使用方式

### 基础指标

`metrics` 中的字段可能为 `null`：

- `cpu_percent`：CPU 使用率。
- `memory_percent`：内存使用率。
- `disk_used_percent`：磁盘使用率。
- `battery_percent`：手机电量；电脑通常为 `null`。
- `network_connected`：手机网络连接状态。

数字字段直接按百分比显示即可，不要把 `null` 当成 `0`。

### 前台应用

`foreground_app` 可能为 `null`：

- Windows 通常使用 `name` 和 `process_name`。
- Android 通常使用 `name` 和 `package_name`。
- `captured_at` 是前台应用采集时间，不是博客页面加载时间。

博客不要显示 Windows 原始窗口标题，也不要自行从管理接口读取完整进程列表。

### 位置

`location` 可能为 `null`，或仅包含区级信息：

```json
{
  "district": "滨湖区",
  "city": null,
  "captured_at": "2026-07-18T08:58:50Z",
  "accuracy_meters": 80
}
```

公开接口不会返回 `latitude` 和 `longitude`。博客只显示城市/区名、采集时间和可选精度，不要尝试从其他接口补回精确坐标。

## 7. 异常与缓存

博客页面应区分两种情况：

1. MDS 正常返回设备状态：按设备的 `status` 显示。
2. 博客代理返回 `503`：显示“设备状态暂时不可用”，不要把所有设备改成“离线”。

服务端 `fetch` 使用 `revalidate: 60`，博客最多每 60 秒向 MDS 请求一次新快照。响应头中的 `stale-while-revalidate=300` 允许部署平台在短时间内继续提供旧快照。生产部署仍应观察平台是否支持 Next.js Data Cache；不支持时，应使用平台缓存或 Redis 保存最近一次成功快照。

建议页面提供一个“数据生成于”时间，使用 `generated_at`，不要使用浏览器当前时间伪造数据新鲜度。

## 8. 不应调用的接口

博客不应调用以下接口：

```text
POST /api/v1/devices
GET  /api/v1/devices
GET  /api/v1/devices/{id}
GET  /api/v1/devices/{id}/latest
GET  /api/v1/devices/{id}/reports
```

这些接口属于管理面板，需要管理员权限，可能返回设备令牌相关管理信息、完整原始心跳、窗口标题、进程列表和原始位置。

博客也不应：

- 直接连接 MDS SQLite 文件。
- 让浏览器直接携带 `MDS_PUBLIC_STATUS_TOKEN` 请求 MDS。
- 把 `MDS_ADMIN_TOKEN` 当作公开接口令牌。
- 把没有心跳解释成已确认关机。
- 把原始经纬度写入博客页面或博客自己的公开 API。

## 9. 验收清单

部署博客后，依次确认：

```powershell
$headers = @{ Authorization = "Bearer $env:MDS_PUBLIC_STATUS_TOKEN" }
Invoke-RestMethod `
  -Uri "$env:MDS_API_URL/api/v1/public/snapshot" `
  -Headers $headers
```

- MDS 的公开接口使用 HTTPS。
- 正确只读令牌能够获得快照。
- 错误令牌返回 `401`。
- 白名单外设备不会出现在 `devices` 中。
- 浏览器 Network 面板中看不到 MDS 令牌。
- MDS 暂时不可用时，博客显示“暂时不可用”或最近一次缓存。
- 页面不显示原始经纬度、完整窗口标题和完整进程列表。
- 设备停止上报后，状态按 `status_policy` 依次变为 `stale`、`offline`。
- 页面同时显示 `generated_at` 和各设备的 `last_seen_at`。

## 10. MDS 侧配置示例

```env
MDS_ADDR=127.0.0.1:8080
MDS_COOKIE_SECURE=1
MDS_ADMIN_TOKEN=仅供管理使用的管理员令牌
MDS_ADMIN_USERNAME=admin
MDS_ADMIN_PASSWORD=管理员密码
MDS_PUBLIC_STATUS_TOKEN=仅供博客服务端使用的只读令牌
MDS_PUBLIC_DEVICE_IDS=电脑设备ID,手机设备ID
MDS_STATUS_ONLINE_SECONDS=300
MDS_STATUS_STALE_SECONDS=1800
MDS_REPORT_RETENTION_DAYS=30
```

公网访问链路应为：

```text
博客服务端 -> HTTPS -> Caddy/nginx/IIS -> 127.0.0.1:8080 -> MDS
```

设备客户端和博客都使用公网 HTTPS 地址，MDS 的 `8080` 端口只监听本机或私有网络。
