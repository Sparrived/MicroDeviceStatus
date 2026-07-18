package com.microdevicestatus.mds;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.provider.Settings;
import android.net.Uri;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.TextView;

public final class MainActivity extends Activity {
    private static final int NOTIFICATION_PERMISSION_REQUEST = 20;
    private EditText endpointInput;
    private EditText tokenInput;
    private EditText intervalInput;
    private CheckBox locationToggle;
    private CheckBox autoResumeToggle;
    private TextView statusText;
    private TextView usageAccessText;
    private String pendingAction;
    private final Handler statusHandler = new Handler(Looper.getMainLooper());
    private final Runnable statusRefresh = new Runnable() {
        @Override
        public void run() {
            refreshStatus();
            statusHandler.postDelayed(this, 1000);
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);
        endpointInput = findViewById(R.id.endpoint_input);
        tokenInput = findViewById(R.id.token_input);
        intervalInput = findViewById(R.id.interval_input);
        locationToggle = findViewById(R.id.location_toggle);
        autoResumeToggle = findViewById(R.id.auto_resume_toggle);
        statusText = findViewById(R.id.status_text);
        usageAccessText = findViewById(R.id.usage_access_text);
        loadPreferences();

        Button startButton = findViewById(R.id.start_button);
        startButton.setOnClickListener(view -> startMonitoring());
        Button stopButton = findViewById(R.id.stop_button);
        stopButton.setOnClickListener(view -> stopMonitoring());
        Button sendNowButton = findViewById(R.id.send_now_button);
        sendNowButton.setOnClickListener(view -> sendNow());
        Button usageAccessButton = findViewById(R.id.usage_access_button);
        usageAccessButton.setOnClickListener(view -> openUsageAccessSettings());

        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, NOTIFICATION_PERMISSION_REQUEST);
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        statusHandler.post(statusRefresh);
    }

    @Override
    protected void onPause() {
        statusHandler.removeCallbacks(statusRefresh);
        super.onPause();
    }

    private void loadPreferences() {
        SharedPreferences preferences = getSharedPreferences(HeartbeatService.PREFERENCES, MODE_PRIVATE);
        endpointInput.setText(preferences.getString(HeartbeatService.KEY_ENDPOINT, "http://10.0.2.2:8080"));
        tokenInput.setText(preferences.getString(HeartbeatService.KEY_TOKEN, ""));
        intervalInput.setText(String.valueOf(preferences.getInt(HeartbeatService.KEY_INTERVAL, 60)));
        locationToggle.setChecked(preferences.getBoolean(HeartbeatService.KEY_LOCATION_ENABLED, false));
        autoResumeToggle.setChecked(preferences.getBoolean(HeartbeatService.KEY_MONITORING_ENABLED, false));
    }

    private void savePreferences() {
        int interval = 60;
        try {
            interval = Integer.parseInt(intervalInput.getText().toString().trim());
        } catch (NumberFormatException ignored) {
        }
        interval = Math.max(15, Math.min(interval, 86400));
        intervalInput.setText(String.valueOf(interval));
        getSharedPreferences(HeartbeatService.PREFERENCES, MODE_PRIVATE).edit()
                .putString(HeartbeatService.KEY_ENDPOINT, endpointInput.getText().toString().trim())
                .putString(HeartbeatService.KEY_TOKEN, tokenInput.getText().toString().trim())
                .putInt(HeartbeatService.KEY_INTERVAL, interval)
                .putBoolean(HeartbeatService.KEY_LOCATION_ENABLED, locationToggle.isChecked())
                .putBoolean(HeartbeatService.KEY_MONITORING_ENABLED, autoResumeToggle.isChecked())
                .apply();
    }

    private void startMonitoring() {
        savePreferences();
        if (requestLocationPermissionIfNeeded(HeartbeatService.ACTION_START)) {
            return;
        }
        dispatchService(HeartbeatService.ACTION_START);
    }

    private void sendNow() {
        savePreferences();
        if (requestLocationPermissionIfNeeded(HeartbeatService.ACTION_SEND_NOW)) {
            return;
        }
        dispatchService(HeartbeatService.ACTION_SEND_NOW);
    }

    private boolean requestLocationPermissionIfNeeded(String action) {
        if (!locationToggle.isChecked() || hasLocationPermission()) {
            return false;
        }
        pendingAction = action;
        if (Build.VERSION.SDK_INT >= 23) {
            requestPermissions(new String[]{Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION}, HeartbeatService.LOCATION_PERMISSION_REQUEST);
        }
        return true;
    }

    private boolean hasLocationPermission() {
        return Build.VERSION.SDK_INT < 23
                || checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
                || checkSelfPermission(Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED;
    }

    private void dispatchService(String action) {
        Intent intent = new Intent(this, HeartbeatService.class).setAction(action);
        if (Build.VERSION.SDK_INT >= 26) {
            startForegroundService(intent);
        } else {
            startService(intent);
        }
    }

    private void stopMonitoring() {
        stopService(new Intent(this, HeartbeatService.class));
        getSharedPreferences(HeartbeatService.PREFERENCES, MODE_PRIVATE).edit()
                .putString(HeartbeatService.KEY_STATUS, "已停止")
                .putBoolean(HeartbeatService.KEY_MONITORING_ENABLED, false)
                .apply();
        refreshStatus();
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        if (requestCode != HeartbeatService.LOCATION_PERMISSION_REQUEST || pendingAction == null) {
            return;
        }
        String action = pendingAction;
        pendingAction = null;
        if (!hasLocationPermission()) {
            getSharedPreferences(HeartbeatService.PREFERENCES, MODE_PRIVATE).edit()
                    .putString(HeartbeatService.KEY_STATUS, "未授予定位权限，已跳过位置")
                    .apply();
        }
        dispatchService(action);
    }

    private void refreshStatus() {
        SharedPreferences preferences = getSharedPreferences(HeartbeatService.PREFERENCES, MODE_PRIVATE);
        usageAccessText.setText(hasUsageAccess() ? "使用情况访问权限：已授予" : "使用情况访问权限：未授予，前台应用将显示为空");
        String status = preferences.getString(HeartbeatService.KEY_STATUS, "尚未启动");
        String sentAt = preferences.getString(HeartbeatService.KEY_LAST_SENT, "");
        if (!sentAt.isEmpty()) {
            status += "\n最后上报：" + sentAt;
        }
        statusText.setText(status);
    }

    private void openUsageAccessSettings() {
        startActivity(new Intent(Settings.ACTION_USAGE_ACCESS_SETTINGS, Uri.parse("package:" + getPackageName())));
    }

    private boolean hasUsageAccess() {
        android.app.AppOpsManager appOps = (android.app.AppOpsManager) getSystemService(APP_OPS_SERVICE);
        return appOps != null && appOps.checkOpNoThrow(android.app.AppOpsManager.OPSTR_GET_USAGE_STATS, android.os.Process.myUid(), getPackageName()) == android.app.AppOpsManager.MODE_ALLOWED;
    }
}
