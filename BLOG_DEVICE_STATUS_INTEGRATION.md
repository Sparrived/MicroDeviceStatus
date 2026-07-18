# MDS 与博客设备状态对接方案

版本：2026-07-18

本文说明 MicroDeviceStatus（以下简称 MDS）为了向个人博客提供设备状态，需要修改哪些内容、哪些内容可以直接复用，以及最终的部署和验收方式。

本文的目标设备是：

- 一台 Windows 电脑
- 一部 Android 手机
- 一个 Next.js 博客

如果手机是 iPhone，当前 MDS 没有 iOS 客户端，本文中的 Android 改造不能直接复用。

## 1. 结论

MDS 现有的心跳接收、设备令牌、SQLite 存储和历史查询已经足够作为基础。博客对接不需要重写 MDS，也不需要引入消息队列、WebSocket 或独立数据库。

为了达到目标，MDS 侧建议分成两类改造：

### 必须改造

1. 增加面向博客的只读状态快照接口。
2. 为快照接口增加单独的只读令牌和设备白名单。
3. Android 客户端增加“当前前台应用”采集。
4. Android 客户端增加开机后的自动恢复上报。
5. Windows 桌面客户端配置为用户登录后自动启动。
6. 位置数据增加区级展示策略，博客不能直接使用原始经纬度。
7. MDS 服务通过公网 HTTPS 暴露，使博客服务端和设备客户端都能访问。

### 推荐改造

1. 将在线、延迟、离线的判断统一放到 MDS，而不是由博客重复实现。
2. 增加报告保留时间和定期清理，避免 SQLite 无限增长。
3. Windows 前台应用同时保存进程名和显示名，减少把文档标题误当作软件名的情况。
4. 为公开快照建立固定字段投影，不直接返回完整原始心跳。

## 2. 当前代码已经具备的能力

### 2.1 服务端

当前服务端已经提供：

```text
GET  /healthz
POST /api/v1/devices
GET  /api/v1/devices
GET  /api/v1/devices/{id}
GET  /api/v1/devices/{id}/latest
GET  /api/v1/devices/{id}/reports
POST /api/v1/heartbeats
```

当前管理查询接口需要 `MDS_ADMIN_TOKEN` 或已登录的管理会话。它们适合内置管理面板，不适合直接让博客浏览器调用。

当前服务端会：

- 用设备令牌接收心跳。
- 只在数据库中保存设备令牌的 SHA-256 哈希。
- 保存完整的原始心跳 JSON。
- 自动记录服务端 `received_at`。
- 使用 `last_seen_at` 判断设备最近是否上报。
- 使用 SQLite WAL 模式保存数据。

### 2.2 Windows 桌面端

当前 `mds_desktop` 已经采集：

- CPU 使用率
- 内存总量、已用量和使用率
- 系统盘容量、剩余空间和使用率
- Windows 前台窗口
- 进程列表中的前 8 个进程

当前 Windows 前台字段主要来自活动窗口标题，因此可能得到“某个文档 - 编辑器”而不是纯软件名。后续可以增加进程名字段。

### 2.3 Android 客户端

当前 `mds_mobile` 已经采集：

- CPU 使用率
- 内存使用率
- 存储空间
- 电量百分比
- 网络连接状态
- 可选的 GPS 或网络位置
- MDS 自身进程的内存占用

当前 Android 客户端尚未采集其他软件的前台应用。它上报的 `processes` 目前只有 MDS 自己的进程，不能作为“当前正在使用的软件”。

当前 Android 客户端也没有 `BOOT_COMPLETED` 接收器，因此手机重启后不会自动恢复心跳服务。

## 3. 推荐整体架构

```text
Windows 桌面客户端 ─┐
                    ├─ HTTPS POST /api/v1/heartbeats ─┐
Android 客户端 ─────┘                                │
                                                     v
                                           MicroDeviceStatus
                                           SQLite + 管理 API
                                                     │
                          HTTPS + 只读令牌             │
                                                     v
                                             Next.js 服务端 API
                                                     │
                                                     v
                                               博客设备页面
```

博客浏览器不应该直接访问 MDS 管理 API，也不应该持有 `MDS_ADMIN_TOKEN`。推荐由博客的 Next.js 服务端访问 MDS，再把过滤后的结果返回给浏览器。

## 4. 服务端必须增加的内容

### 4.1 增加独立的只读令牌

当前只有管理员令牌和设备令牌：

- `MDS_ADMIN_TOKEN`：创建设备、读取所有设备和历史报告。
- 设备令牌：设备自身只能提交心跳。

建议新增：

```text
MDS_PUBLIC_STATUS_TOKEN
MDS_PUBLIC_DEVICE_IDS
MDS_STATUS_ONLINE_SECONDS
MDS_STATUS_STALE_SECONDS
MDS_REPORT_RETENTION_DAYS
```

示例：

```env
MDS_PUBLIC_STATUS_TOKEN=generate-a-separate-long-random-token
MDS_PUBLIC_DEVICE_IDS=computer-device-id,phone-device-id
MDS_STATUS_ONLINE_SECONDS=300
MDS_STATUS_STALE_SECONDS=1800
MDS_REPORT_RETENTION_DAYS=30
```

说明：

- `MDS_PUBLIC_STATUS_TOKEN` 只允许读取公开快照，不能创建设备或读取任意历史报告。
- `MDS_PUBLIC_DEVICE_IDS` 是允许出现在博客中的设备 ID 白名单。
- 不建议使用设备名称作为白名单，因为设备名称可能被修改或重复。
- `MDS_STATUS_ONLINE_SECONDS` 默认可以设置为 300 秒。
- `MDS_STATUS_STALE_SECONDS` 默认可以设置为 1800 秒。
- `MDS_REPORT_RETENTION_DAYS` 可先设置为 30 天，后续根据磁盘空间调整。
- 所有令牌都只能放在服务端环境变量或受限权限的配置文件中。

如果第一阶段希望少改 MDS，可以暂时让博客服务端使用 `MDS_ADMIN_TOKEN` 调用现有查询 API。但这只是过渡方案，正式部署应改为独立只读令牌。

### 4.2 增加公开快照接口

建议增加：

```text
GET /api/v1/public/snapshot
Authorization: Bearer <MDS_PUBLIC_STATUS_TOKEN>
```

该接口只返回 `MDS_PUBLIC_DEVICE_IDS` 中的设备。

成功响应建议固定为：

```json
{
  "generated_at": "2026-07-18T09:00:00Z",
  "status_policy": {
    "online_after_seconds": 300,
    "stale_after_seconds": 1800
  },
  "devices": [
    {
      "id": "device-id",
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
        "battery_percent": null
      },
      "foreground_app": {
        "name": "Visual Studio Code",
        "process_name": "Code.exe",
        "captured_at": "2026-07-18T08:59:40Z"
      },
      "location": null
    },
    {
      "id": "phone-device-id",
      "name": "我的手机",
      "platform": "android",
      "status": "online",
      "heartbeat_age_seconds": 31,
      "last_seen_at": "2026-07-18T08:59:29Z",
      "reported_at": "2026-07-18T08:59:28Z",
      "metrics": {
        "battery_percent": 76,
        "network_connected": true
      },
      "foreground_app": {
        "name": "微信",
        "package_name": "com.tencent.mm",
        "captured_at": "2026-07-18T08:59:20Z"
      },
      "location": {
        "country": "中国",
        "province": "江苏省",
        "city": "无锡市",
        "district": "滨湖区",
        "captured_at": "2026-07-18T08:58:50Z",
        "accuracy_meters": 80
      }
    }
  ]
}
```

### 4.2.1 状态定义

状态应由 MDS 根据服务端接收时间计算：

```text
没有任何心跳             never_seen
距离 last_seen_at < 300s  online
距离 last_seen_at < 1800s stale
其他                     offline
```

`last_seen_at` 必须使用服务端接收时间，而不是设备自己提交的 `reported_at`。这样设备时间错误不会影响在线判断。

这里的 `offline` 只能表示“在规定时间内没有收到心跳”，不能证明设备一定关机。断网、客户端崩溃、系统休眠、应用被系统终止和真正关机都会表现为离线。

### 4.2.2 公开接口不得返回的字段

公开快照默认不返回：

- 管理员令牌或设备令牌。
- 完整历史报告。
- 完整进程列表。
- Windows 窗口完整标题中的文档名。
- 手机原始纬度和经度。
- 主机名、用户目录、PID 等不必要的内部信息。

管理面板仍然可以继续使用现有管理接口查看完整原始心跳。

### 4.3 服务端代码改动位置

建议在 `main.go` 中增加以下逻辑：

1. 在 `server` 结构中增加公开令牌、公开设备 ID 集合和状态阈值。
2. 在 `main()` 中读取新增环境变量。
3. 在 `routes()` 中注册 `GET /api/v1/public/snapshot`。
4. 增加单独的 `requirePublicStatusToken()`，不要复用管理员权限判断。
5. 增加 `statusForDevice()`，统一计算 `never_seen`、`online`、`stale` 和 `offline`。
6. 增加 `publicSnapshot()`，读取白名单设备的最新报告。
7. 增加 `projectPublicPayload()`，只复制允许公开的字段。
8. 对没有某个字段的设备返回 `null`，不要让博客根据字段是否存在猜测设备状态。
9. 对快照接口设置 `Cache-Control: no-store`，缓存策略交给博客服务端控制。

不建议博客直接查询 MDS 的 SQLite 文件。数据库应该只由 MDS 进程访问。

### 4.4 服务端测试要求

在 `main_test.go` 中增加至少这些测试：

- 没有令牌时返回 `401`。
- 使用管理员令牌访问公开接口时，行为符合明确的权限策略。
- 使用错误的公开令牌时返回 `401`。
- 白名单外的设备不会出现在响应中。
- `last_seen_at` 在阈值边界前后产生正确状态。
- 从完整报告中只投影允许公开的字段。
- 原始经纬度不会出现在公开响应中。
- 未上报过的设备返回 `never_seen`。
- 缺少电量、位置或前台应用时仍返回完整设备对象。
- 公开接口不会返回任何令牌或完整历史记录。

## 5. Windows 桌面端改造

### 5.1 基本指标不需要重写

当前桌面端已经满足电脑的基础运行指标：

- CPU 使用率
- 内存使用率
- 磁盘使用率
- 前台窗口

如果“基本硬件信息”只指运行指标，桌面采集器可以直接使用。

如果还需要 CPU 型号、显卡型号、温度或风扇转速，则需要额外增加 Windows WMI 或厂商接口采集。这些字段不应作为博客第一版的硬依赖，因为不同硬件的温度接口并不统一。

### 5.2 前台应用字段建议调整

当前 Windows 端使用活动窗口标题作为 `foreground_app.name` 和 `foreground_app.title`。建议改为：

```json
{
  "name": "Visual Studio Code",
  "process_name": "Code.exe",
  "title": "可选，默认不公开",
  "captured_at": "2026-07-18T08:59:40Z"
}
```

采集逻辑建议：

1. 用 `GetForegroundWindow()` 获取活动窗口。
2. 用 `GetWindowThreadProcessId()` 获取窗口所属 PID。
3. 用 PID 读取进程名和应用显示名。
4. 窗口标题只保存在原始报告中，公开快照默认不返回。

这样博客显示的是软件名，而不是正在编辑的文件名或网页标题。

### 5.3 让桌面端登录后自动运行

电脑在线状态依赖 `mds_desktop` 持续运行。当前客户端是命令行程序，需要配置为当前用户登录后自动启动。

建议使用用户上下文的计划任务，而不是 SYSTEM 服务。原因是 Windows 的前台窗口属于交互用户会话，SYSTEM 服务通常无法正确看到用户当前窗口。

示例：

```powershell
$action = New-ScheduledTaskAction `
  -Execute "C:\mds\mds_desktop.exe" `
  -Argument "-config C:\mds\mds-desktop.json" `
  -WorkingDirectory "C:\mds"

$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$principal = New-ScheduledTaskPrincipal `
  -UserId $env:USERNAME `
  -LogonType Interactive `
  -RunLevel LeastPrivilege

Register-ScheduledTask `
  -TaskName "MicroDeviceStatus Desktop Agent" `
  -Action $action `
  -Trigger $trigger `
  -Principal $principal
```

`C:\mds\mds-desktop.json` 需要限制 NTFS 权限，只允许当前用户和管理员读取，因为其中包含设备令牌。

建议桌面客户端的心跳间隔设置为 60 秒。这样 5 分钟在线阈值能够容忍几次网络抖动，同时页面仍然足够及时。

## 6. Android 客户端改造

### 6.1 增加当前前台应用采集

Android 普通应用不能直接读取所有正在运行的应用。推荐使用 `UsageStatsManager`，需要用户在系统设置中手动授予“使用情况访问权限”。

需要增加的权限和设置入口：

```xml
<uses-permission android:name="android.permission.PACKAGE_USAGE_STATS" />
```

应用设置页应增加：

- 当前是否拥有使用情况访问权限。
- 跳转到系统“使用情况访问权限”页面的按钮。
- 没有权限时明确上报 `foreground_app: null`，不能误报 MDS 自身为当前应用。

采集算法建议：

1. 在每次心跳前读取最近 10 分钟的 Usage Events。
2. 查找最后一个 `ACTIVITY_RESUMED` 或兼容版本的前台事件。
3. 排除 MDS 自己的包名和明显的系统启动器，除非没有其他应用可用。
4. 用 `PackageManager` 将包名转换为应用名称。
5. 记录事件时间作为 `captured_at`。

建议心跳字段：

```json
{
  "foreground_app": {
    "name": "微信",
    "package_name": "com.tencent.mm",
    "captured_at": "2026-07-18T08:59:20Z"
  }
}
```

`UsageStatsManager` 的实时性受 Android 版本、厂商系统和权限影响。如果要求更接近实时，可以使用无障碍服务，但它的权限更敏感、用户提示更强，也不建议作为第一版默认方案。

### 6.2 增加手机重启后的自动恢复

需要增加：

```xml
<uses-permission android:name="android.permission.RECEIVE_BOOT_COMPLETED" />
```

并增加一个只在用户开启“开机自动上报”时工作的 `BroadcastReceiver`：

```xml
<receiver
    android:name=".BootReceiver"
    android:enabled="true"
    android:exported="false">
    <intent-filter>
        <action android:name="android.intent.action.BOOT_COMPLETED" />
    </intent-filter>
</receiver>
```

`BootReceiver` 的行为：

1. 读取本地的 `monitoring_enabled` 设置。
2. 如果用户没有开启自动恢复，直接结束。
3. 如果已开启，在允许的 Android 版本上启动 `HeartbeatService`。
4. 服务启动后立即发送一次心跳。

Android 12 及以上、以及部分国产系统可能限制后台启动和自启动。应用内应提供以下说明：

- 允许通知权限。
- 允许应用自启动。
- 将 MDS Mobile 加入电池优化白名单或“不限制电量使用”。
- 允许后台运行。

这些设置不能由普通 APK 在所有设备上自动完成，因此“开机自动恢复”必须在真实手机上验收，不能只通过编译判断。

### 6.3 手机位置与区级展示

当前客户端已经可以上报：

```json
{
  "location": {
    "latitude": 31.4900,
    "longitude": 120.3100,
    "accuracy_meters": 80,
    "provider": "network",
    "captured_at": "2026-07-18T08:58:50Z"
  }
}
```

但博客需求是“精确到区”，因此不能直接把上述原始字段放进公开快照。推荐分两层保存：

- 管理接口和原始报告：可以保存原始坐标，供本人查看。
- 公开快照：只返回国家、省、市、区和采集时间，不返回经纬度。

区名获取有两种方案：

### 方案 A：Android 端反向地理编码

Android 客户端根据坐标生成国家、省、市、区字段后再上报：

```json
{
  "location": {
    "country": "中国",
    "province": "江苏省",
    "city": "无锡市",
    "district": "滨湖区",
    "accuracy_meters": 80,
    "captured_at": "2026-07-18T08:58:50Z"
  }
}
```

优点是 MDS 服务端不需要保存地图 API 密钥。缺点是 Android 系统的 `Geocoder` 受系统服务和网络影响，部分设备可能返回空值或不同语言的行政区名称。

### 方案 B：MDS 服务端反向地理编码

MDS 服务端收到新位置后调用指定的地理编码服务，并缓存结果。可以为此增加：

```env
MDS_GEOCODER_URL=https://example-geocoder.invalid/reverse
MDS_GEOCODER_TOKEN=replace-with-geocoder-token
```

服务端应将坐标先按网格取整后再查询和缓存，避免每次心跳调用一次外部服务。原始坐标不应出现在公开接口中。

对于个人博客第一版，推荐先采用方案 A 或手动维护区名，先完成状态链路，再决定是否引入外部地图服务。

### 6.4 电量不需要额外采集改造

Android 当前已经通过系统电池广播获取电量百分比。博客只需要读取：

```text
metrics.battery_percent
```

可以后续增加充电状态，但不是当前需求的必要条件。

## 7. 在线状态的边界

MDS 不可能在设备断电瞬间收到一个可靠的“停止”请求。以下情况都应该被统一解释为“未收到最近心跳”：

- 设备关机。
- 设备断网。
- 手机系统杀死后台服务。
- 电脑进入休眠。
- 客户端进程异常退出。
- MDS 服务或反向代理不可用。

因此博客文案建议使用：

```text
在线
延迟
离线
最后上报时间
```

不要使用“已确认关机”这类无法由心跳系统证明的文案。

## 8. 数据保留和数据库改造

当前 MDS 保存完整原始心跳，而且还没有自动清理机制。如果两个设备每 60 秒上报一次：

- 每天约 2880 条报告。
- 每月约 86400 条报告。
- 进程列表、窗口标题和位置字段会使单条 JSON 继续变大。

建议增加报告保留策略：

```env
MDS_REPORT_RETENTION_DAYS=30
```

实现方式可以先保持简单：

1. 服务启动时执行一次清理。
2. 服务运行期间每天执行一次清理。
3. 删除 `reported_at` 早于保留期限的报告。
4. 清理后执行 SQLite checkpoint 或按运维需要执行 `VACUUM`。
5. 清理前保留数据库备份。

第一版不需要把所有指标拆成新的 SQLite 列。原始 JSON 结构已经足够支撑当前快照接口，只有在需要复杂历史统计时再增加专门的聚合表。

## 9. 部署要求

### 9.1 MDS 服务

推荐部署为：

```text
公网 HTTPS
    -> Caddy / nginx / IIS
    -> 127.0.0.1:8080 的 MDS
    -> 本地 SQLite 文件
```

MDS 当前不直接终止 TLS，因此必须通过反向代理或云负载均衡提供 HTTPS。

生产环境建议：

```env
MDS_ADDR=127.0.0.1:8080
MDS_COOKIE_SECURE=1
MDS_ADMIN_TOKEN=strong-admin-token
MDS_PUBLIC_STATUS_TOKEN=strong-read-only-token
```

不要直接把 `8080` 暴露到公网。公网只开放反向代理的 `443` 端口。

### 9.2 设备客户端

Windows 和 Android 客户端的 endpoint 都应使用：

```text
https://status.example.com
```

不要在真实设备上使用明文 HTTP。客户端设备令牌只用于提交心跳，不能用管理员令牌替代。

### 9.3 博客服务端

博客服务端保存：

```env
MDS_API_URL=https://status.example.com
MDS_PUBLIC_STATUS_TOKEN=the-same-read-only-token
```

该变量必须是服务端变量，不能以 `NEXT_PUBLIC_` 开头。博客浏览器只能访问博客自己的 `/api/device-status`，不能直接拿到 MDS 令牌。

如果博客部署在 Vercel，而 MDS 位于家庭网络，MDS 必须有公网可访问的 HTTPS 地址。仅在家中电脑上监听 `127.0.0.1` 无法让 Vercel 访问。

## 10. CORS 和实时推送

第一版不需要配置 CORS，也不需要 WebSocket。

推荐使用：

- 设备客户端每 60 秒主动上报。
- 博客服务端每 30-60 秒重新读取 MDS 快照。
- 博客页面显示最后更新时间。

这样实现简单、断线可恢复，且符合 MDS 当前“客户端主动发起 HTTP 请求”的设计。

只有在确实需要秒级实时刷新时，才考虑 SSE 或 WebSocket。当前需求没有这个必要。

## 11. 验收清单

### 11.1 服务端接口

```powershell
$headers = @{ Authorization = "Bearer $env:MDS_PUBLIC_STATUS_TOKEN" }
Invoke-RestMethod `
  -Uri https://status.example.com/api/v1/public/snapshot `
  -Headers $headers
```

必须验证：

- HTTPS 访问正常。
- 错误令牌返回 `401`。
- 白名单外设备不出现。
- 电脑在线时显示 CPU、内存、磁盘和前台应用。
- 手机在线时显示电量、位置区名和前台应用。
- 原始经纬度不出现在公开响应。
- 设备停止客户端后，状态在阈值后变为离线。
- MDS 重启后设备令牌和历史报告仍然存在。

### 11.2 Windows

- 用户登录后桌面客户端自动启动。
- 客户端配置文件权限正确。
- 前台切换到不同软件后，下一次心跳能更新软件名。
- 电脑休眠、断网和关机后，博客显示离线或延迟，而不是错误显示为在线。
- 客户端网络恢复后，离线队列能够正常补发。

### 11.3 Android

- 首次启动可以保存 MDS 地址和设备令牌。
- 未授予定位权限时，心跳仍能正常发送。
- 授予定位权限后，原始报告包含位置。
- 未授予使用情况访问权限时，前台应用为空且不误报 MDS。
- 授予使用情况访问权限后，能识别最近使用的应用。
- 开启自动恢复后，手机重启能恢复前台服务。
- 关闭监控后不会继续发送心跳。
- 系统限制后台运行时，界面能提示用户处理电池优化和自启动设置。

### 11.4 博客

- 博客服务端能读取 MDS 快照。
- 浏览器开发者工具中看不到 MDS 管理令牌和只读令牌。
- MDS 暂时不可用时，博客显示上一次缓存或明确的暂时不可用状态。
- 博客不显示原始经纬度、完整窗口标题和完整进程列表。
- 页面显示数据生成时间和各设备最后心跳时间。

## 12. 推荐实施顺序

### 第一阶段：先打通安全链路

1. 部署 MDS 到公网 HTTPS。
2. 创建电脑和手机设备令牌。
3. 增加公开设备白名单和只读快照接口。
4. 让博客服务端读取快照。
5. 先展示在线状态、最后上报时间和基础指标。

### 第二阶段：补齐客户端采集

1. 配置 Windows 用户登录自动启动。
2. Android 增加前台应用采集和权限入口。
3. Android 增加开机自动恢复。
4. 增加位置区名和公开位置脱敏。

### 第三阶段：完善长期运行能力

1. 增加报告保留和清理。
2. 增加数据库备份和恢复演练。
3. 增加设备禁用或令牌撤销。
4. 增加公开快照接口的审计日志和访问限流。
5. 根据真实使用情况决定是否增加硬件型号、温度和历史图表。

## 13. 不建议在这一阶段做的事情

- 不要让博客直接连接 SQLite。
- 不要把 `MDS_ADMIN_TOKEN` 放进浏览器或 `NEXT_PUBLIC_*` 环境变量。
- 不要把原始经纬度作为博客公开接口字段。
- 不要为了状态展示引入消息队列或 WebSocket。
- 不要一开始就为每一种指标创建数据库列。
- 不要把 Windows MDS 桌面采集器作为 SYSTEM 服务运行后再期待它能读取用户前台窗口。
- 不要把“没有心跳”解释成已经确认关机。

## 14. 最终需要修改的文件范围

按推荐方案，MDS 仓库最终大致会涉及：

```text
main.go
main_test.go
PROJECT_CONTEXT.md
README.md
DEPLOY.md

mds_desktop/main.go
mds_desktop/metrics.go
mds_desktop/metrics_windows.go
mds_desktop/README.md

mds_mobile/app/src/main/AndroidManifest.xml
mds_mobile/app/src/main/java/.../HeartbeatService.java
mds_mobile/app/src/main/java/.../MainActivity.java
mds_mobile/app/src/main/java/.../BootReceiver.java
mds_mobile/app/src/main/res/layout/activity_main.xml
mds_mobile/README.md
```

不需要修改博客才能先完成 MDS 侧接口和客户端采集，但博客最终仍需要增加自己的服务端代理路由和设备状态页面。
