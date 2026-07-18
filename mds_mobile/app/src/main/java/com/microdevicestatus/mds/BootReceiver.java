package com.microdevicestatus.mds;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.os.Build;

public final class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        String action = intent == null ? null : intent.getAction();
        if (!Intent.ACTION_BOOT_COMPLETED.equals(action)
                && !Intent.ACTION_MY_PACKAGE_REPLACED.equals(action)) {
            return;
        }
        if (!context.getSharedPreferences(HeartbeatService.PREFERENCES, Context.MODE_PRIVATE)
                .getBoolean(HeartbeatService.KEY_MONITORING_ENABLED, false)) {
            return;
        }
        Intent serviceIntent = new Intent(context, HeartbeatService.class).setAction(HeartbeatService.ACTION_START);
        if (Build.VERSION.SDK_INT >= 26) {
            context.startForegroundService(serviceIntent);
        } else {
            context.startService(serviceIntent);
        }
    }
}
