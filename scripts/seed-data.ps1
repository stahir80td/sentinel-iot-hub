#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Seeds all HomeGuard IoT Platform databases with initial demo data.

.DESCRIPTION
    This script populates PostgreSQL (users), MongoDB (devices), Redis (scenarios, notifications),
    and optionally TimescaleDB and ScyllaDB with sample data for local development and demos.

.PARAMETER Namespace
    Kubernetes namespace where services are deployed. Default: sandbox

.EXAMPLE
    .\seed-data.ps1
    .\seed-data.ps1 -Namespace production
#>

param(
    [string]$Namespace = "sandbox"
)

$ErrorActionPreference = "Stop"

Write-Host "=============================================" -ForegroundColor Cyan
Write-Host "  HomeGuard IoT Platform - Data Seeding" -ForegroundColor Cyan
Write-Host "=============================================" -ForegroundColor Cyan
Write-Host ""

# =============================================================================
# HELPER FUNCTIONS
# =============================================================================

function Get-PodName {
    param([string]$AppLabel)
    $pod = kubectl get pods -n $Namespace -l "app=$AppLabel" -o jsonpath='{.items[0].metadata.name}' 2>$null
    if (-not $pod) {
        throw "Pod with label app=$AppLabel not found in namespace $Namespace"
    }
    return $pod
}

function Test-PodReady {
    param([string]$AppLabel)
    $status = kubectl get pods -n $Namespace -l "app=$AppLabel" -o jsonpath='{.items[0].status.phase}' 2>$null
    return $status -eq "Running"
}

# =============================================================================
# WAIT FOR INFRASTRUCTURE
# =============================================================================

Write-Host "[1/6] Checking infrastructure pods..." -ForegroundColor Yellow

$requiredPods = @("iot-postgresql", "iot-mongodb", "iot-redis")
foreach ($pod in $requiredPods) {
    if (-not (Test-PodReady -AppLabel $pod)) {
        Write-Host "  ERROR: $pod is not running!" -ForegroundColor Red
        Write-Host "  Run 'kubectl get pods -n $Namespace' to check status" -ForegroundColor Red
        exit 1
    }
    Write-Host "  [OK] $pod is running" -ForegroundColor Green
}

# =============================================================================
# SEED POSTGRESQL - USERS
# =============================================================================

Write-Host ""
Write-Host "[2/6] Seeding PostgreSQL (users)..." -ForegroundColor Yellow

# Password requirements: minimum 8 characters
# We register users via the API to generate proper bcrypt hashes
# Passwords: demo1234 (for john, sarah, admin), guest123 (for guest)

$pgPod = Get-PodName -AppLabel "iot-postgresql"

# Clear existing users first
$clearSQL = @"
DELETE FROM refresh_tokens;
DELETE FROM users WHERE email IN ('john@demo.com', 'sarah@demo.com', 'admin@demo.com', 'guest@demo.com');
"@
$clearSQL | kubectl exec -i -n $Namespace $pgPod -- psql -U homeguard -d homeguard | Out-Null

# Register users via the API using a shell script in a curl pod
Write-Host "  Registering demo users via API..." -ForegroundColor Gray

$registerScript = @'
#!/bin/sh
register() {
    result=$(curl -s -X POST http://iot-user-service:8080/auth/register -H "Content-Type: application/json" -d "$1")
    if echo "$result" | grep -q '"token"'; then
        echo "OK: $2"
    else
        echo "SKIP: $2 (may already exist)"
    fi
}
register '{"email":"john@demo.com","password":"demo1234","name":"John Smith"}' "john@demo.com"
register '{"email":"sarah@demo.com","password":"demo1234","name":"Sarah Johnson"}' "sarah@demo.com"
register '{"email":"admin@demo.com","password":"demo1234","name":"Admin User"}' "admin@demo.com"
register '{"email":"guest@demo.com","password":"guest123","name":"Guest User"}' "guest@demo.com"
'@

$ErrorActionPreference = "SilentlyContinue"
$registerScript | kubectl run curl-register --image=curlimages/curl --rm -i --restart=Never -n $Namespace -- sh 2>&1 | ForEach-Object {
    $line = $_.ToString()
    if ($line -match "^OK:") {
        Write-Host "    Registered: $($line -replace '^OK:\s*', '')" -ForegroundColor Gray
    } elseif ($line -match "^SKIP:") {
        Write-Host "    $($line -replace '^SKIP:\s*', '')" -ForegroundColor Yellow
    }
}
$ErrorActionPreference = "Stop"

# Update user IDs to predictable values and set admin role
# First delete refresh tokens to avoid FK constraint issues
$updateSQL = @"
DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email IN ('john@demo.com', 'sarah@demo.com', 'admin@demo.com', 'guest@demo.com'));
UPDATE users SET id = '11111111-1111-1111-1111-111111111111' WHERE email = 'john@demo.com';
UPDATE users SET id = '22222222-2222-2222-2222-222222222222' WHERE email = 'sarah@demo.com';
UPDATE users SET id = '33333333-3333-3333-3333-333333333333', role = 'admin' WHERE email = 'admin@demo.com';
UPDATE users SET id = '44444444-4444-4444-4444-444444444444' WHERE email = 'guest@demo.com';
SELECT id, email, name, role FROM users ORDER BY email;
"@
$updateSQL | kubectl exec -i -n $Namespace $pgPod -- psql -U homeguard -d homeguard

if ($LASTEXITCODE -eq 0) {
    Write-Host "  [OK] Users seeded successfully" -ForegroundColor Green
} else {
    Write-Host "  [WARN] PostgreSQL seeding may have issues" -ForegroundColor Yellow
}

# =============================================================================
# SEED MONGODB - DEVICES
# =============================================================================

Write-Host ""
Write-Host "[3/6] Seeding MongoDB (devices)..." -ForegroundColor Yellow

$devicesJS = @'
// Switch to homeguard database
db = db.getSiblingDB('homeguard');

// Clear existing data
db.devices.deleteMany({});
db.device_commands.deleteMany({});

// Current timestamp
var now = new Date();

// John's devices (Smart Home Focus)
db.devices.insertMany([
    {
        _id: "dev-john-thermostat-001",
        user_id: "11111111-1111-1111-1111-111111111111",
        name: "Living Room Thermostat",
        type: "thermostat",
        manufacturer: "Nest",
        model: "Learning Thermostat 3rd Gen",
        location: "Living Room",
        status: "active",
        online: true,
        token: "tok_john_therm_" + ObjectId().toString(),
        config: {
            target_temp: 72,
            mode: "auto",
            fan: "auto",
            schedule_enabled: true
        },
        metadata: {
            firmware_version: "5.9.3",
            wifi_strength: -45,
            last_maintenance: new Date("2024-06-15")
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-john-doorbell-001",
        user_id: "11111111-1111-1111-1111-111111111111",
        name: "Front Door Camera",
        type: "camera",
        manufacturer: "Ring",
        model: "Video Doorbell Pro 2",
        location: "Front Door",
        status: "active",
        online: true,
        token: "tok_john_door_" + ObjectId().toString(),
        config: {
            motion_detection: true,
            motion_sensitivity: "medium",
            night_vision: true,
            two_way_audio: true
        },
        metadata: {
            firmware_version: "3.14.1",
            resolution: "1536p",
            field_of_view: "150deg"
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-john-lock-001",
        user_id: "11111111-1111-1111-1111-111111111111",
        name: "Front Door Lock",
        type: "smart_lock",
        manufacturer: "August",
        model: "Wi-Fi Smart Lock 4th Gen",
        location: "Front Door",
        status: "active",
        online: true,
        token: "tok_john_lock_" + ObjectId().toString(),
        config: {
            auto_lock: true,
            auto_lock_delay: 30,
            door_sense: true
        },
        metadata: {
            battery_level: 85,
            firmware_version: "1.12.2"
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-john-light-001",
        user_id: "11111111-1111-1111-1111-111111111111",
        name: "Living Room Lights",
        type: "light",
        manufacturer: "Philips Hue",
        model: "White and Color Ambiance",
        location: "Living Room",
        status: "active",
        online: true,
        token: "tok_john_light_" + ObjectId().toString(),
        config: {
            brightness: 80,
            color: "#FFFFFF",
            color_temp: 4000
        },
        metadata: {
            bulb_count: 4,
            bridge_id: "HUE-BRIDGE-001"
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    }
]);

// Sarah's devices (Security Focus)
db.devices.insertMany([
    {
        _id: "dev-sarah-camera-001",
        user_id: "22222222-2222-2222-2222-222222222222",
        name: "Backyard Camera",
        type: "camera",
        manufacturer: "Arlo",
        model: "Ultra 2 Spotlight",
        location: "Backyard",
        status: "active",
        online: true,
        token: "tok_sarah_cam1_" + ObjectId().toString(),
        config: {
            motion_detection: true,
            motion_zones: ["zone1", "zone2"],
            recording_mode: "continuous",
            spotlight_enabled: true
        },
        metadata: {
            firmware_version: "2.1.5.2",
            resolution: "4K",
            battery_level: 92
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-sarah-camera-002",
        user_id: "22222222-2222-2222-2222-222222222222",
        name: "Garage Camera",
        type: "camera",
        manufacturer: "Arlo",
        model: "Ultra 2",
        location: "Garage",
        status: "active",
        online: true,
        token: "tok_sarah_cam2_" + ObjectId().toString(),
        config: {
            motion_detection: true,
            recording_mode: "motion",
            night_vision: true
        },
        metadata: {
            firmware_version: "2.1.5.2",
            resolution: "4K",
            battery_level: 78
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-sarah-sensor-001",
        user_id: "22222222-2222-2222-2222-222222222222",
        name: "Front Door Sensor",
        type: "contact_sensor",
        manufacturer: "Samsung SmartThings",
        model: "Multipurpose Sensor",
        location: "Front Door",
        status: "active",
        online: true,
        token: "tok_sarah_sens1_" + ObjectId().toString(),
        config: {
            sensitivity: "high",
            tamper_detection: true
        },
        metadata: {
            battery_level: 95,
            firmware_version: "1.0.4"
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-sarah-motion-001",
        user_id: "22222222-2222-2222-2222-222222222222",
        name: "Hallway Motion Sensor",
        type: "motion_sensor",
        manufacturer: "Samsung SmartThings",
        model: "Motion Sensor",
        location: "Hallway",
        status: "active",
        online: true,
        token: "tok_sarah_mot1_" + ObjectId().toString(),
        config: {
            sensitivity: "medium",
            pet_immune: true
        },
        metadata: {
            battery_level: 88,
            firmware_version: "1.0.3"
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-sarah-alarm-001",
        user_id: "22222222-2222-2222-2222-222222222222",
        name: "Home Alarm System",
        type: "alarm",
        manufacturer: "SimpliSafe",
        model: "Base Station",
        location: "Utility Closet",
        status: "active",
        online: true,
        token: "tok_sarah_alarm_" + ObjectId().toString(),
        config: {
            mode: "home",
            entry_delay: 30,
            exit_delay: 60,
            siren_volume: "high"
        },
        metadata: {
            firmware_version: "3.2.1",
            cellular_backup: true,
            battery_backup: true
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    }
]);

// Admin's devices (Enterprise/Testing)
db.devices.insertMany([
    {
        _id: "dev-admin-hub-001",
        user_id: "33333333-3333-3333-3333-333333333333",
        name: "Central Hub",
        type: "hub",
        manufacturer: "HomeGuard",
        model: "Enterprise Hub Pro",
        location: "Server Room",
        status: "active",
        online: true,
        token: "tok_admin_hub_" + ObjectId().toString(),
        config: {
            protocols: ["zigbee", "zwave", "wifi", "bluetooth"],
            max_devices: 500
        },
        metadata: {
            firmware_version: "2.0.0",
            uptime_days: 45,
            connected_devices: 3
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-admin-env-001",
        user_id: "33333333-3333-3333-3333-333333333333",
        name: "Environment Monitor",
        type: "environment_sensor",
        manufacturer: "Airthings",
        model: "View Plus",
        location: "Office",
        status: "active",
        online: true,
        token: "tok_admin_env_" + ObjectId().toString(),
        config: {
            alert_thresholds: {
                co2: 1000,
                humidity_low: 30,
                humidity_high: 60,
                temperature_low: 65,
                temperature_high: 80
            }
        },
        metadata: {
            firmware_version: "1.5.2",
            sensors: ["temperature", "humidity", "co2", "voc", "pm25", "radon"]
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    }
]);

// Guest's devices (Simple setup for testing)
db.devices.insertMany([
    {
        _id: "dev-guest-plug-001",
        user_id: "44444444-4444-4444-4444-444444444444",
        name: "Smart Plug",
        type: "smart_plug",
        manufacturer: "TP-Link",
        model: "Kasa Smart Plug",
        location: "Bedroom",
        status: "active",
        online: true,
        token: "tok_guest_plug_" + ObjectId().toString(),
        config: {
            power_on: true,
            schedule: null
        },
        metadata: {
            firmware_version: "1.0.8",
            energy_monitoring: true,
            current_power_watts: 45
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    },
    {
        _id: "dev-guest-bulb-001",
        user_id: "44444444-4444-4444-4444-444444444444",
        name: "Desk Lamp",
        type: "light",
        manufacturer: "LIFX",
        model: "Mini Color",
        location: "Bedroom",
        status: "active",
        online: true,
        token: "tok_guest_bulb_" + ObjectId().toString(),
        config: {
            brightness: 100,
            color: "#FFA500",
            power_on: true
        },
        metadata: {
            firmware_version: "3.70",
            wifi_strength: -38
        },
        last_seen: now,
        created_at: now,
        updated_at: now
    }
]);

// Create indexes
db.devices.createIndex({ user_id: 1 });
db.devices.createIndex({ type: 1 });
db.devices.createIndex({ status: 1 });
db.devices.createIndex({ token: 1 }, { unique: true, sparse: true });

// Show counts
print("Devices seeded:");
print("  John:  " + db.devices.countDocuments({ user_id: "11111111-1111-1111-1111-111111111111" }));
print("  Sarah: " + db.devices.countDocuments({ user_id: "22222222-2222-2222-2222-222222222222" }));
print("  Admin: " + db.devices.countDocuments({ user_id: "33333333-3333-3333-3333-333333333333" }));
print("  Guest: " + db.devices.countDocuments({ user_id: "44444444-4444-4444-4444-444444444444" }));
print("  Total: " + db.devices.countDocuments());
'@

$mongoPod = Get-PodName -AppLabel "iot-mongodb"
$devicesJS | kubectl exec -i -n $Namespace $mongoPod -- mongosh --username root --password homeguard-mongo-2024 --authenticationDatabase admin --quiet

if ($LASTEXITCODE -eq 0) {
    Write-Host "  [OK] Devices seeded successfully" -ForegroundColor Green
} else {
    Write-Host "  [WARN] MongoDB seeding may have issues" -ForegroundColor Yellow
}

# =============================================================================
# SEED REDIS - SCENARIOS & NOTIFICATIONS
# =============================================================================

Write-Host ""
Write-Host "[4/6] Seeding Redis (scenarios & notifications)..." -ForegroundColor Yellow

$redisPod = Get-PodName -AppLabel "iot-redis"

# Helper function to execute Redis commands
function Invoke-Redis {
    param([string]$Command)
    kubectl exec -n $Namespace $redisPod -- redis-cli $Command.Split(" ") 2>$null | Out-Null
}

# Clear existing scenarios and notifications
kubectl exec -n $Namespace $redisPod -- redis-cli KEYS "scenarios:*" | ForEach-Object {
    if ($_) { kubectl exec -n $Namespace $redisPod -- redis-cli DEL $_ | Out-Null }
}
kubectl exec -n $Namespace $redisPod -- redis-cli KEYS "notifications:*" | ForEach-Object {
    if ($_) { kubectl exec -n $Namespace $redisPod -- redis-cli DEL $_ | Out-Null }
}

# John's automation scenario - Turn on lights when motion detected
$johnScenario1 = @{
    id = "scen-john-001"
    user_id = "11111111-1111-1111-1111-111111111111"
    name = "Motion Light Automation"
    description = "Turn on living room lights when motion is detected"
    enabled = $true
    trigger = @{
        type = "device_event"
        device_id = "dev-john-doorbell-001"
        event = "motion_detected"
    }
    conditions = @()
    actions = @(
        @{
            type = "device_command"
            device_id = "dev-john-light-001"
            command = "turn_on"
            params = @{ brightness = 100 }
        }
    )
    created_at = (Get-Date -Format "o")
    updated_at = (Get-Date -Format "o")
} | ConvertTo-Json -Depth 10 -Compress

# Sarah's security scenario
$sarahScenario1 = @{
    id = "scen-sarah-001"
    user_id = "22222222-2222-2222-2222-222222222222"
    name = "Security Alert"
    description = "Send notification when door sensor is triggered while away"
    enabled = $true
    trigger = @{
        type = "device_event"
        device_id = "dev-sarah-sensor-001"
        event = "door_opened"
    }
    conditions = @(
        @{
            type = "device_state"
            device_id = "dev-sarah-alarm-001"
            property = "mode"
            operator = "eq"
            value = "away"
        }
    )
    actions = @(
        @{
            type = "notification"
            params = @{
                title = "Security Alert"
                message = "Front door opened while alarm is in away mode!"
                priority = "high"
            }
        }
    )
    created_at = (Get-Date -Format "o")
    updated_at = (Get-Date -Format "o")
} | ConvertTo-Json -Depth 10 -Compress

# Push scenarios to Redis
$johnScenario1Escaped = $johnScenario1 -replace '"', '\"'
$sarahScenario1Escaped = $sarahScenario1 -replace '"', '\"'

kubectl exec -n $Namespace $redisPod -- redis-cli RPUSH "scenarios:11111111-1111-1111-1111-111111111111" $johnScenario1 | Out-Null
kubectl exec -n $Namespace $redisPod -- redis-cli RPUSH "scenarios:22222222-2222-2222-2222-222222222222" $sarahScenario1 | Out-Null

# Add sample notifications
$notification1 = @{
    id = "notif-001"
    user_id = "11111111-1111-1111-1111-111111111111"
    device_id = "dev-john-thermostat-001"
    type = "device_alert"
    title = "Temperature Alert"
    message = "Living room temperature reached target of 72F"
    priority = "normal"
    timestamp = (Get-Date -Format "o")
    read = $false
} | ConvertTo-Json -Depth 5 -Compress

$notification2 = @{
    id = "notif-002"
    user_id = "22222222-2222-2222-2222-222222222222"
    device_id = "dev-sarah-camera-001"
    type = "motion_alert"
    title = "Motion Detected"
    message = "Motion detected in backyard"
    priority = "high"
    timestamp = (Get-Date -Format "o")
    read = $false
} | ConvertTo-Json -Depth 5 -Compress

kubectl exec -n $Namespace $redisPod -- redis-cli RPUSH "notifications:11111111-1111-1111-1111-111111111111" $notification1 | Out-Null
kubectl exec -n $Namespace $redisPod -- redis-cli RPUSH "notifications:22222222-2222-2222-2222-222222222222" $notification2 | Out-Null

# Seed device online status cache
kubectl exec -n $Namespace $redisPod -- redis-cli SET "device:status:dev-john-thermostat-001" "online" EX 300 | Out-Null
kubectl exec -n $Namespace $redisPod -- redis-cli SET "device:status:dev-john-doorbell-001" "online" EX 300 | Out-Null
kubectl exec -n $Namespace $redisPod -- redis-cli SET "device:status:dev-sarah-camera-001" "online" EX 300 | Out-Null
kubectl exec -n $Namespace $redisPod -- redis-cli SET "device:status:dev-sarah-alarm-001" "online" EX 300 | Out-Null

Write-Host "  [OK] Redis data seeded successfully" -ForegroundColor Green

# =============================================================================
# SEED TIMESCALEDB - TIME SERIES DATA (Optional)
# =============================================================================

Write-Host ""
Write-Host "[5/6] Seeding TimescaleDB (analytics)..." -ForegroundColor Yellow

# Check if TimescaleDB pod exists
$tsPodExists = kubectl get pods -n $Namespace -l "app=iot-timescaledb" -o name 2>$null

if ($tsPodExists) {
    $timeseriesSQL = @"
-- Create hypertable for device metrics if not exists
CREATE TABLE IF NOT EXISTS device_metrics (
    time TIMESTAMPTZ NOT NULL,
    device_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    metric_value DOUBLE PRECISION,
    metadata JSONB
);

-- Convert to hypertable (ignore if already converted)
SELECT create_hypertable('device_metrics', 'time', if_not_exists => TRUE);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_device_metrics_device ON device_metrics (device_id, time DESC);
CREATE INDEX IF NOT EXISTS idx_device_metrics_user ON device_metrics (user_id, time DESC);

-- Insert sample data for the last 24 hours
INSERT INTO device_metrics (time, device_id, user_id, metric_name, metric_value, metadata)
SELECT
    NOW() - (interval '1 hour' * generate_series(0, 23)),
    'dev-john-thermostat-001',
    '11111111-1111-1111-1111-111111111111',
    'temperature',
    70 + (random() * 4),
    '{"unit": "fahrenheit"}'::jsonb
FROM generate_series(0, 23);

INSERT INTO device_metrics (time, device_id, user_id, metric_name, metric_value, metadata)
SELECT
    NOW() - (interval '1 hour' * generate_series(0, 23)),
    'dev-john-thermostat-001',
    '11111111-1111-1111-1111-111111111111',
    'humidity',
    40 + (random() * 20),
    '{"unit": "percent"}'::jsonb
FROM generate_series(0, 23);

INSERT INTO device_metrics (time, device_id, user_id, metric_name, metric_value, metadata)
SELECT
    NOW() - (interval '1 hour' * generate_series(0, 23)),
    'dev-admin-env-001',
    '33333333-3333-3333-3333-333333333333',
    'co2',
    400 + (random() * 200),
    '{"unit": "ppm"}'::jsonb
FROM generate_series(0, 23);

SELECT COUNT(*) as sample_metrics_count FROM device_metrics;
"@

    $tsPod = Get-PodName -AppLabel "iot-timescaledb"
    $ErrorActionPreference = "SilentlyContinue"
    $timeseriesSQL | kubectl exec -i -n $Namespace $tsPod -- psql -U homeguard -d homeguard_analytics 2>&1 | Where-Object { $_ -notmatch "^NOTICE:" }
    $ErrorActionPreference = "Stop"
    Write-Host "  [OK] TimescaleDB seeded successfully" -ForegroundColor Green
} else {
    Write-Host "  [SKIP] TimescaleDB pod not found" -ForegroundColor Yellow
}

# =============================================================================
# SEED SCYLLADB - EVENT STORAGE (Optional)
# =============================================================================

Write-Host ""
Write-Host "[6/6] Seeding ScyllaDB (events)..." -ForegroundColor Yellow

$scyllaPodExists = kubectl get pods -n $Namespace -l "app=iot-scylladb" -o name 2>$null

if ($scyllaPodExists) {
    $scyllaCQL = @"
-- Create keyspace
CREATE KEYSPACE IF NOT EXISTS homeguard
WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

USE homeguard;

-- Device events table
CREATE TABLE IF NOT EXISTS device_events (
    device_id text,
    event_time timestamp,
    event_id uuid,
    user_id text,
    event_type text,
    payload text,
    PRIMARY KEY ((device_id), event_time, event_id)
) WITH CLUSTERING ORDER BY (event_time DESC, event_id ASC);

-- Events by user (for querying user's events)
CREATE TABLE IF NOT EXISTS events_by_user (
    user_id text,
    event_time timestamp,
    event_id uuid,
    device_id text,
    event_type text,
    payload text,
    PRIMARY KEY ((user_id), event_time, event_id)
) WITH CLUSTERING ORDER BY (event_time DESC, event_id ASC);

-- Insert sample events
INSERT INTO device_events (device_id, event_time, event_id, user_id, event_type, payload)
VALUES ('dev-john-thermostat-001', toTimestamp(now()), uuid(), '11111111-1111-1111-1111-111111111111', 'temperature_change', '{"old": 71, "new": 72}');

INSERT INTO device_events (device_id, event_time, event_id, user_id, event_type, payload)
VALUES ('dev-john-doorbell-001', toTimestamp(now()), uuid(), '11111111-1111-1111-1111-111111111111', 'motion_detected', '{"confidence": 0.95, "zone": "front"}');

INSERT INTO device_events (device_id, event_time, event_id, user_id, event_type, payload)
VALUES ('dev-sarah-camera-001', toTimestamp(now()), uuid(), '22222222-2222-2222-2222-222222222222', 'motion_detected', '{"confidence": 0.88, "zone": "backyard"}');

INSERT INTO events_by_user (user_id, event_time, event_id, device_id, event_type, payload)
VALUES ('11111111-1111-1111-1111-111111111111', toTimestamp(now()), uuid(), 'dev-john-thermostat-001', 'temperature_change', '{"old": 71, "new": 72}');

SELECT COUNT(*) FROM device_events;
"@

    $scyllaPod = Get-PodName -AppLabel "iot-scylladb"
    $scyllaCQL | kubectl exec -i -n $Namespace $scyllaPod -- cqlsh 2>$null

    if ($LASTEXITCODE -eq 0) {
        Write-Host "  [OK] ScyllaDB seeded successfully" -ForegroundColor Green
    } else {
        Write-Host "  [WARN] ScyllaDB seeding may have issues" -ForegroundColor Yellow
    }
} else {
    Write-Host "  [SKIP] ScyllaDB pod not found" -ForegroundColor Yellow
}

# =============================================================================
# SUMMARY
# =============================================================================

Write-Host ""
Write-Host "=============================================" -ForegroundColor Cyan
Write-Host "           Seeding Complete!" -ForegroundColor Green
Write-Host "=============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Demo Users Created:" -ForegroundColor White
Write-Host "  - john@demo.com   / demo1234  (Smart Home User)" -ForegroundColor Gray
Write-Host "  - sarah@demo.com  / demo1234  (Security User)" -ForegroundColor Gray
Write-Host "  - admin@demo.com  / demo1234  (Admin)" -ForegroundColor Gray
Write-Host "  - guest@demo.com  / guest123  (Guest)" -ForegroundColor Gray
Write-Host ""
Write-Host "Devices Created: 13 total" -ForegroundColor White
Write-Host "  - John:  4 devices (thermostat, camera, lock, lights)" -ForegroundColor Gray
Write-Host "  - Sarah: 5 devices (cameras, sensors, alarm)" -ForegroundColor Gray
Write-Host "  - Admin: 2 devices (hub, environment sensor)" -ForegroundColor Gray
Write-Host "  - Guest: 2 devices (smart plug, light bulb)" -ForegroundColor Gray
Write-Host ""
Write-Host "Access the application at:" -ForegroundColor White
Write-Host "  http://homeguard.localhost" -ForegroundColor Cyan
Write-Host ""
