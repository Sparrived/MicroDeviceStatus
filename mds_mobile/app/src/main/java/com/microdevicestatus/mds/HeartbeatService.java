package com.microdevicestatus.mds;

import android.app.ActivityManager;
import android.app.AlarmManager;
import android.app.AppOpsManager;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.app.usage.UsageEvents;
import android.app.usage.UsageStats;
import android.app.usage.UsageStatsManager;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.SharedPreferences;
import android.content.pm.ApplicationInfo;
import android.content.pm.LauncherActivityInfo;
import android.content.pm.LauncherApps;
import android.content.pm.PackageManager;
import android.content.pm.ServiceInfo;
import android.hardware.display.DisplayManager;
import android.net.ConnectivityManager;
import android.net.NetworkCapabilities;
import android.os.Build;
import android.os.Debug;
import android.os.Environment;
import android.os.IBinder;
import android.os.PowerManager;
import android.os.Process;
import android.os.SystemClock;
import android.os.health.HealthStats;
import android.os.health.SystemHealthManager;
import android.os.health.UidHealthStats;
import android.location.Address;
import android.location.Geocoder;
import android.location.Location;
import android.location.LocationListener;
import android.location.LocationManager;
import android.os.StatFs;
import android.util.Log;
import android.view.Display;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStreamReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.text.SimpleDateFormat;
import java.util.ArrayList;
import java.util.Date;
import java.util.List;
import java.util.Locale;
import java.util.TimeZone;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.CountDownLatch;

public final class HeartbeatService extends Service {
    public static final String ACTION_START = "com.microdevicestatus.mds.START";
    public static final String ACTION_SEND_NOW = "com.microdevicestatus.mds.SEND_NOW";
    private static final String ACTION_RESTART = "com.microdevicestatus.mds.RESTART";
    public static final String PREFERENCES = "mds_mobile";
    public static final String KEY_ENDPOINT = "endpoint";
    public static final String KEY_TOKEN = "token";
    public static final String KEY_INTERVAL = "interval";
    public static final String KEY_LOCATION_ENABLED = "location_enabled";
    public static final String KEY_MONITORING_ENABLED = "monitoring_enabled";
    public static final String KEY_STATUS = "status";
    public static final String KEY_LAST_SENT = "last_sent";
    public static final int LOCATION_PERMISSION_REQUEST = 21;
    private static final String CHANNEL_ID = "mds_status";
    private static final int NOTIFICATION_ID = 7001;
    private static final int RESTART_REQUEST_CODE = 7002;
    private static final long RESTART_DELAY_MILLIS = 5000L;
    private static final String TAG = "MDS";

    private final AtomicBoolean cycleRunning = new AtomicBoolean();
    private ScheduledExecutorService executor;
    private long previousCpuTotal;
    private long previousCpuIdle;
    private long previousAppCpuTime;
    private long previousAppCpuWallTime;
    private String cpuScope = "unavailable";

    @Override
    public void onCreate() {
        super.onCreate();
        createNotificationChannel();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? ACTION_START : intent.getAction();
        if (ACTION_RESTART.equals(action)) {
            cancelRestart();
        }
        if (!ACTION_SEND_NOW.equals(action) && !getPreferences().getBoolean(KEY_MONITORING_ENABLED, false)) {
            stopSelf(startId);
            return START_NOT_STICKY;
        }
        if (ACTION_SEND_NOW.equals(action)) {
            startWorker();
            executor.execute(this::runCycle);
        } else {
            restartWorker();
        }
        return START_STICKY;
    }

    @Override
    public void onDestroy() {
        if (executor != null) {
            executor.shutdownNow();
        }
        cancelRestart();
        if (getPreferences().getBoolean(KEY_MONITORING_ENABLED, false)) {
            scheduleRestart();
        }
        stopForeground(true);
        super.onDestroy();
    }

    @Override
    public void onTaskRemoved(Intent rootIntent) {
        if (getPreferences().getBoolean(KEY_MONITORING_ENABLED, false)) {
            scheduleRestart();
        }
        super.onTaskRemoved(rootIntent);
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    private void startWorker() {
        if (executor != null && !executor.isShutdown()) {
            return;
        }
        promoteToForeground();
        executor = Executors.newSingleThreadScheduledExecutor();
        int interval = getPreferences().getInt(KEY_INTERVAL, 60);
        executor.scheduleAtFixedRate(this::runCycle, 0, Math.max(15, interval), TimeUnit.SECONDS);
        setStatus("监控中，等待上报");
    }

    private void restartWorker() {
        if (executor != null) {
            executor.shutdownNow();
            executor = null;
        }
        startWorker();
    }

    private void promoteToForeground() {
        Notification foregroundNotification = notification("正在监控设备状态");
        if (Build.VERSION.SDK_INT >= 29) {
            int type = ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC;
            if (getPreferences().getBoolean(KEY_LOCATION_ENABLED, false) && hasLocationPermission()) {
                type |= ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION;
            }
            startForeground(NOTIFICATION_ID, foregroundNotification, type);
        } else {
            startForeground(NOTIFICATION_ID, foregroundNotification);
        }
    }

    private void runCycle() {
        if (!cycleRunning.compareAndSet(false, true)) {
            return;
        }
        try {
            String payload = buildPayload();
            sendWithQueue(payload);
            String sentAt = now();
            getPreferences().edit().putString(KEY_STATUS, "运行正常").putString(KEY_LAST_SENT, sentAt).apply();
            updateNotification("最后上报 " + sentAt);
        } catch (Exception error) {
            Log.w(TAG, "heartbeat deferred", error);
            setStatus("等待网络恢复：" + error.getMessage());
            updateNotification("等待网络恢复");
        } finally {
            cycleRunning.set(false);
        }
    }

    private String buildPayload() throws Exception {
        JSONObject root = new JSONObject();
        root.put("reported_at", now());
        root.put("client_version", BuildConfig.VERSION_NAME);
        root.put("platform", "android");
        root.put("hostname", Build.MANUFACTURER + " " + Build.MODEL);

        JSONObject metrics = new JSONObject();
        ActivityManager.MemoryInfo memory = new ActivityManager.MemoryInfo();
        ((ActivityManager) getSystemService(ACTIVITY_SERVICE)).getMemoryInfo(memory);
        metrics.put("memory_total_bytes", memory.totalMem);
        metrics.put("memory_used_bytes", Math.max(0L, memory.totalMem - memory.availMem));
        metrics.put("memory_percent", memory.totalMem == 0 ? 0 : (memory.totalMem - memory.availMem) * 100.0 / memory.totalMem);

        StatFs storage = new StatFs(Environment.getDataDirectory().getPath());
        long totalBytes = storage.getTotalBytes();
        long freeBytes = storage.getAvailableBytes();
        metrics.put("disk_total_bytes", totalBytes);
        metrics.put("disk_free_bytes", freeBytes);
        metrics.put("disk_used_percent", totalBytes == 0 ? 0 : (totalBytes - freeBytes) * 100.0 / totalBytes);

        Float battery = batteryPercent();
        if (battery != null) {
            metrics.put("battery_percent", battery);
        }
        metrics.put("network_connected", isNetworkConnected());
        double cpu = cpuPercent();
        if (cpu >= 0) {
            metrics.put("cpu_percent", cpu);
            metrics.put("cpu_scope", cpuScope);
        }
        root.put("metrics", metrics);

        if (getPreferences().getBoolean(KEY_LOCATION_ENABLED, false)) {
            JSONObject location = currentLocation();
            if (location != null) {
                root.put("location", location);
            }
        }

        JSONObject foregroundApp = currentForegroundApp();
        root.put("foreground_app", foregroundApp == null ? JSONObject.NULL : foregroundApp);

        JSONArray processes = new JSONArray();
        Debug.MemoryInfo appMemory = new Debug.MemoryInfo();
        Debug.getMemoryInfo(appMemory);
        processes.put(new JSONObject()
                .put("name", getPackageName())
                .put("pid", android.os.Process.myPid())
                .put("memory_bytes", appMemory.getTotalPss() * 1024L));
        root.put("processes", processes);
        return root.toString();
    }

    private JSONObject currentLocation() {
        if (!hasLocationPermission()) {
            return null;
        }
        LocationManager manager = (LocationManager) getSystemService(LOCATION_SERVICE);
        if (manager == null) {
            return null;
        }
        Location best = null;
        for (String provider : new String[]{LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER}) {
            try {
                if (manager.isProviderEnabled(provider)) {
                    Location candidate = manager.getLastKnownLocation(provider);
                    best = betterLocation(best, candidate);
                }
            } catch (SecurityException ignored) {
                return null;
            }
        }

        CountDownLatch latch = new CountDownLatch(1);
        final Location[] latest = new Location[]{best};
        LocationListener listener = new LocationListener() {
            @Override
            public void onLocationChanged(Location location) {
                latest[0] = betterLocation(latest[0], location);
                latch.countDown();
            }

            @Override
            public void onProviderEnabled(String provider) {
            }

            @Override
            public void onProviderDisabled(String provider) {
            }
        };
        try {
            for (String provider : new String[]{LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER}) {
                if (manager.isProviderEnabled(provider)) {
                    manager.requestLocationUpdates(provider, 0, 0, listener, android.os.Looper.getMainLooper());
                }
            }
            latch.await(3, TimeUnit.SECONDS);
        } catch (SecurityException ignored) {
        } catch (InterruptedException ignored) {
            Thread.currentThread().interrupt();
        } finally {
            try {
                manager.removeUpdates(listener);
            } catch (SecurityException ignored) {
            }
        }
        return locationJson(latest[0]);
    }

    private boolean hasLocationPermission() {
        return Build.VERSION.SDK_INT < 23
                || checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION) == android.content.pm.PackageManager.PERMISSION_GRANTED
                || checkSelfPermission(android.Manifest.permission.ACCESS_COARSE_LOCATION) == android.content.pm.PackageManager.PERMISSION_GRANTED;
    }

    private Location betterLocation(Location current, Location candidate) {
        if (candidate == null) {
            return current;
        }
        if (current == null || candidate.getTime() > current.getTime() || candidate.getAccuracy() < current.getAccuracy()) {
            return candidate;
        }
        return current;
    }

    private JSONObject locationJson(Location location) {
        if (location == null) {
            return null;
        }
        try {
            Address address = reverseGeocodeAddress(location);
            if (address == null) {
                return null;
            }
            JSONObject result = new JSONObject();
            putNonEmpty(result, "country", address.getCountryName());
            putNonEmpty(result, "province", address.getAdminArea());
            putNonEmpty(result, "city", address.getLocality());
            String district = address.getSubLocality();
            if (district == null || district.trim().isEmpty()) {
                district = address.getSubAdminArea();
            }
            putNonEmpty(result, "district", district);
            return result.length() == 0 ? null : result;
        } catch (Exception error) {
            return null;
        }
    }

    private JSONObject currentForegroundApp() {
        if (!isScreenInteractive()) {
            return screenOffForegroundApp();
        }
        if (!hasUsageAccess()) {
            return null;
        }
        UsageStatsManager manager = (UsageStatsManager) getSystemService(USAGE_STATS_SERVICE);
        if (manager == null) {
            return null;
        }
        long end = System.currentTimeMillis();
        UsageEvents events = manager.queryEvents(end - TimeUnit.HOURS.toMillis(24), end);
        UsageEvents.Event event = new UsageEvents.Event();
        String packageName = null;
        long capturedAt = 0;
        while (events.hasNextEvent()) {
            events.getNextEvent(event);
            int eventType = event.getEventType();
            if ((eventType == UsageEvents.Event.ACTIVITY_RESUMED
                    || eventType == UsageEvents.Event.MOVE_TO_FOREGROUND)
                    && event.getTimeStamp() >= capturedAt) {
                packageName = event.getPackageName();
                capturedAt = event.getTimeStamp();
            }
        }
        if (packageName == null || isExcludedForegroundPackage(packageName)) {
            UsageStats fallback = latestUserApp(manager, end);
            if (fallback == null) {
                return null;
            }
            packageName = fallback.getPackageName();
            capturedAt = fallback.getLastTimeUsed();
        }
        try {
            String appName = applicationName(packageName);
            JSONObject result = new JSONObject();
            result.put("name", appName);
            result.put("package_name", packageName);
            result.put("captured_at", formatTime(capturedAt));
            return result;
        } catch (org.json.JSONException ignored) {
            return null;
        }
    }

    private String applicationName(String packageName) {
        try {
            ApplicationInfo info = getPackageManager().getApplicationInfo(packageName, 0);
            CharSequence label = getPackageManager().getApplicationLabel(info);
            if (label != null && label.length() > 0) {
                return label.toString();
            }
        } catch (PackageManager.NameNotFoundException | SecurityException ignored) {
        }

        LauncherApps launcherApps = (LauncherApps) getSystemService(LAUNCHER_APPS_SERVICE);
        if (launcherApps != null) {
            try {
                List<LauncherActivityInfo> activities = launcherApps.getActivityList(
                        packageName, Process.myUserHandle());
                if (!activities.isEmpty()) {
                    CharSequence label = activities.get(0).getLabel();
                    if (label != null && label.length() > 0) {
                        return label.toString();
                    }
                }
            } catch (SecurityException ignored) {
            }
        }
        return packageName;
    }

    private UsageStats latestUserApp(UsageStatsManager manager, long end) {
        List<UsageStats> stats = manager.queryUsageStats(
                UsageStatsManager.INTERVAL_DAILY,
                end - TimeUnit.HOURS.toMillis(24),
                end);
        UsageStats latest = null;
        for (UsageStats candidate : stats) {
            if (candidate == null || isExcludedForegroundPackage(candidate.getPackageName())) {
                continue;
            }
            if (latest == null || candidate.getLastTimeUsed() > latest.getLastTimeUsed()) {
                latest = candidate;
            }
        }
        return latest;
    }

    private boolean isScreenInteractive() {
        PowerManager powerManager = (PowerManager) getSystemService(POWER_SERVICE);
        if (powerManager != null && !powerManager.isInteractive()) {
            return false;
        }
        DisplayManager displayManager = (DisplayManager) getSystemService(DISPLAY_SERVICE);
        Display display = displayManager == null ? null : displayManager.getDisplay(Display.DEFAULT_DISPLAY);
        return display == null || display.getState() == Display.STATE_ON;
    }

    private JSONObject screenOffForegroundApp() {
        try {
            return new JSONObject()
                    .put("name", "息屏")
                    .put("package_name", "screen_off")
                    .put("captured_at", now());
        } catch (org.json.JSONException ignored) {
            return null;
        }
    }

    private boolean hasUsageAccess() {
        AppOpsManager appOps = (AppOpsManager) getSystemService(APP_OPS_SERVICE);
        return appOps != null && appOps.checkOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), getPackageName()) == AppOpsManager.MODE_ALLOWED;
    }

    private boolean isExcludedForegroundPackage(String packageName) {
        if (packageName == null || packageName.equals(getPackageName())) {
            return true;
        }
        if (packageName.equals("com.android.systemui") || packageName.startsWith("com.android.systemui:")) {
            return true;
        }
        return false;
    }

    @SuppressWarnings("deprecation")
    private Address reverseGeocodeAddress(Location location) {
        if (!Geocoder.isPresent()) {
            return null;
        }
        try {
            List<Address> addresses = new Geocoder(this, Locale.getDefault()).getFromLocation(location.getLatitude(), location.getLongitude(), 1);
            if (addresses == null || addresses.isEmpty()) {
                return null;
            }
            return addresses.get(0);
        } catch (IOException | IllegalArgumentException ignored) {
            return null;
        }
    }

    private void putNonEmpty(JSONObject target, String key, String value) throws org.json.JSONException {
        if (value != null && !value.trim().isEmpty()) {
            target.put(key, value.trim());
        }
    }

    private void sendWithQueue(String payload) throws Exception {
        List<String> pending = readQueue();
        pending.add(payload);
        if (pending.size() > 200) {
            pending = new ArrayList<>(pending.subList(pending.size() - 200, pending.size()));
        }
        for (int index = 0; index < pending.size(); index++) {
            try {
                post(pending.get(index));
            } catch (Exception error) {
                writeQueue(pending.subList(index, pending.size()));
                throw error;
            }
        }
        writeQueue(new ArrayList<>());
    }

    private void post(String payload) throws Exception {
        String endpoint = getPreferences().getString(KEY_ENDPOINT, "").trim();
        String token = getPreferences().getString(KEY_TOKEN, "").trim();
        if (endpoint.isEmpty() || token.isEmpty()) {
            throw new IOException("服务地址和设备令牌不能为空");
        }
        URL url = new URL(endpoint.replaceAll("/+$", "") + "/api/v1/heartbeats");
        HttpURLConnection connection = (HttpURLConnection) url.openConnection();
        connection.setRequestMethod("POST");
        connection.setConnectTimeout(10000);
        connection.setReadTimeout(15000);
        connection.setDoOutput(true);
        connection.setRequestProperty("Authorization", "Bearer " + token);
        connection.setRequestProperty("Content-Type", "application/json");
        byte[] body = payload.getBytes(StandardCharsets.UTF_8);
        connection.setFixedLengthStreamingMode(body.length);
        try (OutputStream output = connection.getOutputStream()) {
            output.write(body);
        }
        int status = connection.getResponseCode();
        if (status < 200 || status >= 300) {
            InputStream errorStream = connection.getErrorStream();
            String detail = errorStream == null ? "" : readBody(errorStream);
            throw new IOException("服务器返回 " + status + " " + detail.trim());
        }
        connection.disconnect();
    }

    private List<String> readQueue() throws IOException {
        File file = queueFile();
        List<String> items = new ArrayList<>();
        if (!file.exists()) {
            return items;
        }
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(new FileInputStream(file), StandardCharsets.UTF_8))) {
            String line;
            while ((line = reader.readLine()) != null) {
                if (!line.trim().isEmpty()) {
                    items.add(line);
                }
            }
        }
        return items;
    }

    private void writeQueue(List<String> items) throws IOException {
        File file = queueFile();
        if (items.isEmpty()) {
            if (file.exists() && !file.delete()) {
                throw new IOException("无法清理离线队列");
            }
            return;
        }
        try (FileOutputStream output = new FileOutputStream(file, false)) {
            for (String item : items) {
                output.write(item.getBytes(StandardCharsets.UTF_8));
                output.write('\n');
            }
        }
    }

    private File queueFile() {
        return new File(getFilesDir(), "heartbeats.jsonl");
    }

    private String readBody(InputStream input) throws IOException {
        StringBuilder body = new StringBuilder();
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(input, StandardCharsets.UTF_8))) {
            String line;
            while ((line = reader.readLine()) != null) {
                body.append(line);
            }
        }
        return body.toString();
    }

    private double cpuPercent() {
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(new FileInputStream("/proc/stat"), StandardCharsets.UTF_8))) {
            String[] fields = reader.readLine().trim().split("\\s+");
            if (fields.length < 5 || !"cpu".equals(fields[0])) {
                return -1;
            }
            cpuScope = "device";
            long total = 0;
            long idle = 0;
            for (int index = 1; index < fields.length; index++) {
                long value = Long.parseLong(fields[index]);
                total += value;
                if (index == 4 || index == 5) {
                    idle += value;
                }
            }
            if (previousCpuTotal == 0) {
                previousCpuTotal = total;
                previousCpuIdle = idle;
                return 0;
            }
            long totalDelta = total - previousCpuTotal;
            long idleDelta = idle - previousCpuIdle;
            previousCpuTotal = total;
            previousCpuIdle = idle;
            return totalDelta <= 0 ? 0 : Math.max(0, Math.min(100, (totalDelta - idleDelta) * 100.0 / totalDelta));
        } catch (Exception ignored) {
            double load = cpuLoadPercent();
            if (load >= 0) {
                cpuScope = "device_load";
                return load;
            }
            double app = appCpuPercent();
            if (app >= 0) {
                cpuScope = "app_uid";
                return app;
            }
            cpuScope = "unavailable";
            return -1;
        }
    }

    private double cpuLoadPercent() {
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(
                new FileInputStream("/proc/loadavg"), StandardCharsets.UTF_8))) {
            String[] fields = reader.readLine().trim().split("\\s+");
            if (fields.length == 0) {
                return -1;
            }
            double load = Double.parseDouble(fields[0]);
            int processors = Math.max(1, Runtime.getRuntime().availableProcessors());
            return Math.max(0, Math.min(100, load * 100.0 / processors));
        } catch (Exception ignored) {
            return -1;
        }
    }

    private double appCpuPercent() {
        try {
            SystemHealthManager manager = getSystemService(SystemHealthManager.class);
            if (manager == null) {
                return -1;
            }
            HealthStats stats = manager.takeMyUidSnapshot();
            if (stats == null
                    || !stats.hasMeasurement(UidHealthStats.MEASUREMENT_USER_CPU_TIME_MS)
                    || !stats.hasMeasurement(UidHealthStats.MEASUREMENT_SYSTEM_CPU_TIME_MS)) {
                return -1;
            }
            long cpuTime = stats.getMeasurement(UidHealthStats.MEASUREMENT_USER_CPU_TIME_MS)
                    + stats.getMeasurement(UidHealthStats.MEASUREMENT_SYSTEM_CPU_TIME_MS);
            long wallTime = SystemClock.elapsedRealtime();
            if (previousAppCpuTime == 0 || previousAppCpuWallTime == 0) {
                previousAppCpuTime = cpuTime;
                previousAppCpuWallTime = wallTime;
                return 0;
            }
            long cpuDelta = cpuTime - previousAppCpuTime;
            long wallDelta = wallTime - previousAppCpuWallTime;
            previousAppCpuTime = cpuTime;
            previousAppCpuWallTime = wallTime;
            int processors = Math.max(1, Runtime.getRuntime().availableProcessors());
            return wallDelta <= 0 ? 0 : Math.max(0, Math.min(100,
                    cpuDelta * 100.0 / wallDelta / processors));
        } catch (Exception ignored) {
            return -1;
        }
    }

    private Float batteryPercent() {
        Intent battery = registerReceiver(null, new IntentFilter(Intent.ACTION_BATTERY_CHANGED));
        if (battery == null) {
            return null;
        }
        int level = battery.getIntExtra("level", -1);
        int scale = battery.getIntExtra("scale", -1);
        return level < 0 || scale <= 0 ? null : level * 100f / scale;
    }

    private boolean isNetworkConnected() {
        ConnectivityManager manager = (ConnectivityManager) getSystemService(CONNECTIVITY_SERVICE);
        if (manager == null || manager.getActiveNetwork() == null) {
            return false;
        }
        NetworkCapabilities capabilities = manager.getNetworkCapabilities(manager.getActiveNetwork());
        return capabilities != null && capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET);
    }

    private SharedPreferences getPreferences() {
        return getSharedPreferences(PREFERENCES, MODE_PRIVATE);
    }

    private void scheduleRestart() {
        AlarmManager alarmManager = (AlarmManager) getSystemService(ALARM_SERVICE);
        if (alarmManager == null || !getPreferences().getBoolean(KEY_MONITORING_ENABLED, false)) {
            return;
        }
        PendingIntent restart = restartPendingIntent();
        long triggerAt = SystemClock.elapsedRealtime() + RESTART_DELAY_MILLIS;
        if (Build.VERSION.SDK_INT >= 23) {
            alarmManager.setAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, restart);
        } else {
            alarmManager.set(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, restart);
        }
    }

    private void cancelRestart() {
        AlarmManager alarmManager = (AlarmManager) getSystemService(ALARM_SERVICE);
        if (alarmManager != null) {
            alarmManager.cancel(restartPendingIntent());
        }
    }

    private PendingIntent restartPendingIntent() {
        Intent intent = new Intent(this, HeartbeatService.class).setAction(ACTION_RESTART);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE;
        if (Build.VERSION.SDK_INT >= 26) {
            return PendingIntent.getForegroundService(this, RESTART_REQUEST_CODE, intent, flags);
        }
        return PendingIntent.getService(this, RESTART_REQUEST_CODE, intent, flags);
    }

    private void setStatus(String status) {
        getPreferences().edit().putString(KEY_STATUS, status).apply();
    }

    private String now() {
        return formatTime(System.currentTimeMillis());
    }

    private String formatTime(long timestamp) {
        SimpleDateFormat format = new SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US);
        format.setTimeZone(TimeZone.getTimeZone("UTC"));
        return format.format(new Date(timestamp));
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT < 26) {
            return;
        }
        NotificationChannel channel = new NotificationChannel(CHANNEL_ID, "MDS 状态上报", NotificationManager.IMPORTANCE_LOW);
        getSystemService(NotificationManager.class).createNotificationChannel(channel);
    }

    private Notification notification(String content) {
        Intent intent = new Intent(this, MainActivity.class);
        PendingIntent pendingIntent = PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        Notification.Builder builder = Build.VERSION.SDK_INT >= 26
                ? new Notification.Builder(this, CHANNEL_ID)
                : new Notification.Builder(this);
        return builder.setContentTitle("MDS Mobile")
                .setContentText(content)
                .setSmallIcon(android.R.drawable.stat_notify_sync_noanim)
                .setContentIntent(pendingIntent)
                .setOngoing(true)
                .build();
    }

    private void updateNotification(String content) {
        NotificationManager manager = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        manager.notify(NOTIFICATION_ID, notification(content));
    }
}
