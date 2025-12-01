# Grafana Dashboards for HomeGuard IoT Platform

## Polyglot Data Flow Dashboard

This dashboard visualizes the **polyglot persistence architecture** and shows real-time data flowing through all layers of the system.

### What It Shows

The dashboard displays activity from all polyglot layers:

| Layer | Color | Description |
|-------|-------|-------------|
| **MongoDB** | Green (#4DB33D) | Document storage for device configs |
| **Redis** | Red (#DC382D) | Caching and real-time state |
| **Kafka** | Orange (#FF6B00) | Event streaming and messaging |
| **TimescaleDB** | Cyan (#00B4D8) | Time-series analytics storage |
| **ScyllaDB** | Indigo (#6366F1) | High-volume event storage |
| **N8N** | Pink (#EC4899) | Workflow automation |
| **Event Processor** | Amber (#F59E0B) | Kafka consumer processing |
| **Device Service** | Blue (#3B82F6) | Device command handling |

### Dashboard Panels

1. **Header** - Architecture overview
2. **Polyglot Layer Activity** - Stacked bar chart showing events/min by source
3. **Layer Stats** - Individual stat panels for MongoDB, Redis, Kafka, TimescaleDB, ScyllaDB, N8N
4. **Event Distribution Pie Chart** - Shows proportion of events by layer
5. **Events by Action Type** - What operations are being performed
6. **Activity Events Rate** - Prometheus metrics by source
7. **Live Activity Stream** - Real-time log viewer with parsed fields
8. **Device Commands Rate** - Commands by type (unlock, lock, etc.)
9. **Device Operations Rate** - CRUD operations
10. **Redis Cache Metrics** - Hit ratio, hits, misses

---

## Prerequisites

### Data Sources Required

You need to configure the following data sources in Grafana:

#### 1. Loki (for logs)
- **Name**: `loki`
- **Type**: Loki
- **URL**: `http://iot-loki.sandbox:3100` (or your Loki endpoint)

#### 2. Prometheus (for metrics)
- **Name**: `prometheus`
- **Type**: Prometheus
- **URL**: `http://iot-prometheus.sandbox:9090` (or your Prometheus endpoint)

---

## How to Import the Dashboard

### Option 1: Import via Grafana UI

1. Open Grafana at `http://grafana.homeguard.localhost` (or your Grafana URL)
2. Login with your credentials
3. Click the **+** icon in the left sidebar
4. Select **Import**
5. Click **Upload JSON file**
6. Select `grafana/dashboards/polyglot-data-flow.json`
7. Select your data sources:
   - **loki**: Select your Loki data source
   - **prometheus**: Select your Prometheus data source
8. Click **Import**

### Option 2: Import via API

```bash
# Set your Grafana URL and API key
GRAFANA_URL="http://grafana.homeguard.localhost"
GRAFANA_API_KEY="your-api-key"

# Import the dashboard
curl -X POST "$GRAFANA_URL/api/dashboards/db" \
  -H "Authorization: Bearer $GRAFANA_API_KEY" \
  -H "Content-Type: application/json" \
  -d @grafana/dashboards/polyglot-data-flow.json
```

### Option 3: Provision via Helm (Recommended for Production)

Add dashboard provisioning to your Helm values:

```yaml
grafana:
  dashboardProviders:
    dashboardproviders.yaml:
      apiVersion: 1
      providers:
        - name: 'homeguard'
          orgId: 1
          folder: 'HomeGuard'
          type: file
          disableDeletion: false
          editable: true
          options:
            path: /var/lib/grafana/dashboards/homeguard

  dashboardsConfigMaps:
    homeguard: "iot-grafana-dashboards"
```

Then create a ConfigMap with the dashboard:

```bash
kubectl create configmap iot-grafana-dashboards \
  --from-file=polyglot-data-flow.json=grafana/dashboards/polyglot-data-flow.json \
  -n sandbox
```

---

## Verify Data is Flowing

After importing, verify data is appearing:

1. **Send a test command**:
```bash
TOKEN="your-jwt-token"
curl -X POST "http://homeguard.localhost/api/devices/{device-id}/command" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"command": "unlock"}'
```

2. **Check the dashboard** - You should see:
   - Events appearing in the bar chart
   - Stats incrementing for each layer
   - Live logs in the activity stream

---

## Troubleshooting

### No data in Loki panels

1. Verify Loki is running: `kubectl get pods -n sandbox | grep loki`
2. Check services are logging with `[ACTIVITY]` prefix
3. Verify Loki data source URL is correct

### No data in Prometheus panels

1. Verify Prometheus is scraping services: Check targets at `http://prometheus.homeguard.localhost/targets`
2. Ensure services expose `/metrics` endpoint
3. Check metric names match (e.g., `activity_events_published_total`)

### Dashboard shows "No data"

1. Set time range to "Last 15 minutes"
2. Trigger some device commands to generate activity
3. Check browser console for data source errors

---

## Customization

### Adding More Layers

To add a new polyglot layer to tracking:

1. In your service code, log with the `[ACTIVITY]` format:
```go
log.Printf("[ACTIVITY] source=newlayer action=SomeAction details=Description user=%s device=%s severity=info", userID, deviceID)
```

2. Add color override in the dashboard JSON for consistency

### Modifying Time Ranges

Edit the `time` section in the JSON:
```json
"time": {
  "from": "now-15m",
  "to": "now"
}
```

---

## Architecture Reference

```
User Command → API Gateway → Device Service → Kafka → Event Processor → N8N Webhook
                                   │                        │
                              MongoDB                  TimescaleDB + ScyllaDB
                                   │
                                Redis
```

Each layer emits `[ACTIVITY]` logs that are:
1. Collected by Loki
2. Parsed using LogQL patterns
3. Displayed in real-time on the dashboard
