<p align="center">
  <img src="https://img.shields.io/badge/Platform-Kubernetes-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes"/>
  <img src="https://img.shields.io/badge/Go-1.21-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go"/>
  <img src="https://img.shields.io/badge/Python-3.11-3776AB?style=for-the-badge&logo=python&logoColor=white" alt="Python"/>
  <img src="https://img.shields.io/badge/React-18.2-61DAFB?style=for-the-badge&logo=react&logoColor=black" alt="React"/>
  <img src="https://img.shields.io/badge/TypeScript-5.3-3178C6?style=for-the-badge&logo=typescript&logoColor=white" alt="TypeScript"/>
</p>

# HomeGuard IoT Platform

A production-grade, cloud-native IoT platform demonstrating **polyglot persistence**, **event-driven architecture**, **microservices patterns**, and **Agentic AI** for intelligent smart home security and automation.

---

## Table of Contents

- [Overview](#overview)
- [Key Features](#key-features)
- [Architecture](#architecture)
  - [System Architecture](#system-architecture)
  - [Polyglot Persistence Strategy](#polyglot-persistence-strategy)
  - [Event-Driven Data Flow](#event-driven-data-flow)
  - [AI/ML Pipeline](#aiml-pipeline)
- [Technology Stack](#technology-stack)
- [Microservices](#microservices)
- [Infrastructure Components](#infrastructure-components)
- [Getting Started](#getting-started)
- [Deployment](#deployment)
- [API Reference](#api-reference)
- [Monitoring & Observability](#monitoring--observability)
- [Testing](#testing)
- [Project Structure](#project-structure)
- [License](#license)

---

## Overview

HomeGuard IoT Platform is an enterprise-grade smart home security system that showcases modern software architecture patterns and best practices. The platform processes real-time IoT device events, applies intelligent automation rules, and provides an AI-powered assistant for natural language home control.

### Why This Architecture?

| Challenge | Solution |
|-----------|----------|
| High-volume event ingestion | Apache Kafka for decoupled, scalable event streaming |
| Diverse data access patterns | Polyglot persistence with purpose-built databases |
| Real-time user experience | WebSocket notifications + Redis pub/sub |
| Intelligent automation | Google Gemini-powered AI agent with tool calling |
| Operational visibility | Prometheus + Grafana + structured logging |
| Cloud-native deployment | Kubernetes + Helm charts with multi-environment support |

---

## Key Features

- **Real-Time Device Management** - Monitor and control smart home devices with sub-second latency
- **Event-Driven Processing** - Kafka-based event streaming with guaranteed delivery
- **Intelligent Automation** - Rule-based scenarios with AI-powered decision making
- **Agentic AI Assistant** - Natural language interface powered by Google Gemini
- **Polyglot Persistence** - Right database for each workload (SQL, NoSQL, Time-Series, Cache)
- **Full Observability** - Metrics, logs, and distributed tracing
- **Cloud-Native Deployment** - Kubernetes-native with Helm charts

---

## Architecture

### System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    KUBERNETES CLUSTER                                    │
├─────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                          │
│  ┌────────────────────────────────────────────────────────────────────────────────────┐ │
│  │                              PRESENTATION LAYER                                     │ │
│  │  ┌──────────────────────────────────────────────────────────────────────────────┐  │ │
│  │  │                         Frontend (React + TypeScript)                         │  │ │
│  │  │                                                                               │  │ │
│  │  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │  │ │
│  │  │   │  Dashboard  │  │   Devices   │  │ Automations │  │   AI Assistant      │  │  │ │
│  │  │   │   (Live)    │  │  (Control)  │  │  (Scenarios)│  │  (Gemini Chat)      │  │  │ │
│  │  │   └─────────────┘  └─────────────┘  └─────────────┘  └─────────────────────┘  │  │ │
│  │  └──────────────────────────────────────────────────────────────────────────────┘  │ │
│  └────────────────────────────────────────────────────────────────────────────────────┘ │
│                                           │                                              │
│                                           │ HTTP/REST + WebSocket                        │
│                                           ▼                                              │
│  ┌────────────────────────────────────────────────────────────────────────────────────┐ │
│  │                                 API GATEWAY LAYER                                   │ │
│  │  ┌──────────────────────────────────────────────────────────────────────────────┐  │ │
│  │  │                           API Gateway (Go)                                    │  │ │
│  │  │                                                                               │  │ │
│  │  │   ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐  │  │ │
│  │  │   │   JWT     │  │   Rate    │  │  Circuit  │  │  Request  │  │  Service  │  │  │ │
│  │  │   │   Auth    │  │  Limiting │  │  Breaker  │  │  Logging  │  │  Routing  │  │  │ │
│  │  │   └───────────┘  └───────────┘  └───────────┘  └───────────┘  └───────────┘  │  │ │
│  │  └──────────────────────────────────────────────────────────────────────────────┘  │ │
│  └────────────────────────────────────────────────────────────────────────────────────┘ │
│                                           │                                              │
│              ┌────────────────────────────┼────────────────────────────┐                 │
│              │                            │                            │                 │
│              ▼                            ▼                            ▼                 │
│  ┌────────────────────────────────────────────────────────────────────────────────────┐ │
│  │                              MICROSERVICES LAYER                                    │ │
│  │                                                                                      │ │
│  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐               │ │
│  │   │    User     │  │   Device    │  │   Device    │  │   Event     │               │ │
│  │   │   Service   │  │   Service   │  │   Ingest    │  │  Processor  │               │ │
│  │   │    (Go)     │  │    (Go)     │  │    (Go)     │  │    (Go)     │               │ │
│  │   │             │  │             │  │             │  │             │               │ │
│  │   │  ┌───────┐  │  │  ┌───────┐  │  │  ┌───────┐  │  │  ┌───────┐  │               │ │
│  │   │  │Postgre│  │  │  │MongoDB│  │  │  │ Kafka │  │  │  │Scylla │  │               │ │
│  │   │  │  SQL  │  │  │  │       │  │  │  │Producer│ │  │  │  DB   │  │               │ │
│  │   │  └───────┘  │  │  └───────┘  │  │  └───────┘  │  │  └───────┘  │               │ │
│  │   └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘               │ │
│  │                                                                                      │ │
│  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐               │ │
│  │   │ Notification│  │  Scenario   │  │  Agentic    │  │  Analytics  │               │ │
│  │   │   Service   │  │   Engine    │  │     AI      │  │   Service   │               │ │
│  │   │    (Go)     │  │    (Go)     │  │  (Python)   │  │  (Python)   │               │ │
│  │   │             │  │             │  │             │  │             │               │ │
│  │   │  ┌───────┐  │  │  ┌───────┐  │  │  ┌───────┐  │  │  ┌───────┐  │               │ │
│  │   │  │ Redis │  │  │  │ Redis │  │  │  │Gemini │  │  │  │Timesc-│  │               │ │
│  │   │  │Pub/Sub│  │  │  │ State │  │  │  │  API  │  │  │  │ aleDB │  │               │ │
│  │   │  └───────┘  │  │  └───────┘  │  │  └───────┘  │  │  └───────┘  │               │ │
│  │   └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘               │ │
│  └────────────────────────────────────────────────────────────────────────────────────┘ │
│                                           │                                              │
│  ┌────────────────────────────────────────────────────────────────────────────────────┐ │
│  │                              DATA & MESSAGING LAYER                                 │ │
│  │                                                                                      │ │
│  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐               │ │
│  │   │ PostgreSQL  │  │   MongoDB   │  │    Redis    │  │   Kafka     │               │ │
│  │   │   (Users)   │  │  (Devices)  │  │   (Cache)   │  │  (Events)   │               │ │
│  │   └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘               │ │
│  │                                                                                      │ │
│  │   ┌─────────────┐  ┌─────────────┐                                                 │ │
│  │   │ TimescaleDB │  │  ScyllaDB   │                                                 │ │
│  │   │(Time-Series)│  │  (Events)   │                                                 │ │
│  │   └─────────────┘  └─────────────┘                                                 │ │
│  └────────────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                          │
│  ┌────────────────────────────────────────────────────────────────────────────────────┐ │
│  │                              OBSERVABILITY LAYER                                    │ │
│  │                                                                                      │ │
│  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐               │ │
│  │   │ Prometheus  │  │   Grafana   │  │     N8N     │  │    Loki     │               │ │
│  │   │  (Metrics)  │  │(Dashboards) │  │ (Workflows) │  │   (Logs)    │               │ │
│  │   └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘               │ │
│  └────────────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

### Polyglot Persistence Strategy

The platform employs a **polyglot persistence** architecture, selecting the optimal database technology for each specific data access pattern and workload characteristic.

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                           POLYGLOT PERSISTENCE ARCHITECTURE                              │
├─────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                          │
│  ┌─────────────────────────────────────────────────────────────────────────────────────┐│
│  │                              TRANSACTIONAL DATA                                      ││
│  │  ┌───────────────────────────────────────────────────────────────────────────────┐  ││
│  │  │                         PostgreSQL 15                                          │  ││
│  │  │                                                                                │  ││
│  │  │   Data Type: User accounts, authentication, sessions, subscriptions           │  ││
│  │  │   Access Pattern: Complex queries, JOINs, ACID transactions                   │  ││
│  │  │   Why PostgreSQL: Strong consistency, referential integrity, mature ecosystem │  ││
│  │  │                                                                                │  ││
│  │  │   Tables: users, user_sessions, subscriptions, audit_logs                     │  ││
│  │  │   Indexes: B-tree on email, UUID primary keys                                 │  ││
│  │  └───────────────────────────────────────────────────────────────────────────────┘  ││
│  └─────────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                          │
│  ┌─────────────────────────────────────────────────────────────────────────────────────┐│
│  │                              DOCUMENT DATA                                           ││
│  │  ┌───────────────────────────────────────────────────────────────────────────────┐  ││
│  │  │                           MongoDB 7                                            │  ││
│  │  │                                                                                │  ││
│  │  │   Data Type: Device configurations, automation rules, agent memory            │  ││
│  │  │   Access Pattern: Flexible schema, nested documents, frequent updates         │  ││
│  │  │   Why MongoDB: Schema flexibility, JSON-native, horizontal scaling            │  ││
│  │  │                                                                                │  ││
│  │  │   Collections: devices, automations, agent_conversations, agent_learnings     │  ││
│  │  │   Indexes: device_id, user_id, compound indexes for query optimization        │  ││
│  │  └───────────────────────────────────────────────────────────────────────────────┘  ││
│  └─────────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                          │
│  ┌─────────────────────────────────────────────────────────────────────────────────────┐│
│  │                              TIME-SERIES DATA                                        ││
│  │  ┌───────────────────────────────────────────────────────────────────────────────┐  ││
│  │  │                         TimescaleDB                                            │  ││
│  │  │                                                                                │  ││
│  │  │   Data Type: Sensor readings, temperature, humidity, energy metrics           │  ││
│  │  │   Access Pattern: Time-range queries, aggregations, downsampling              │  ││
│  │  │   Why TimescaleDB: Hypertables, continuous aggregates, compression            │  ││
│  │  │                                                                                │  ││
│  │  │   Hypertables: sensor_readings (auto-partitioned by time)                     │  ││
│  │  │   Features: 90-day retention policy, 7-day compression, hourly aggregates     │  ││
│  │  └───────────────────────────────────────────────────────────────────────────────┘  ││
│  └─────────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                          │
│  ┌─────────────────────────────────────────────────────────────────────────────────────┐│
│  │                              HIGH-VOLUME EVENT DATA                                  ││
│  │  ┌───────────────────────────────────────────────────────────────────────────────┐  ││
│  │  │                           ScyllaDB                                             │  ││
│  │  │                                                                                │  ││
│  │  │   Data Type: IoT events (motion, door, heartbeats), high-write throughput     │  ││
│  │  │   Access Pattern: Write-heavy, partition-key lookups, time-ordered reads      │  ││
│  │  │   Why ScyllaDB: Low-latency writes, linear scalability, CQL compatibility     │  ││
│  │  │                                                                                │  ││
│  │  │   Tables: events_by_device, events_by_user, events_by_type                    │  ││
│  │  │   Partition Strategy: (device_id, event_date) for optimal distribution        │  ││
│  │  └───────────────────────────────────────────────────────────────────────────────┘  ││
│  └─────────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                          │
│  ┌─────────────────────────────────────────────────────────────────────────────────────┐│
│  │                              CACHING & REAL-TIME STATE                               ││
│  │  ┌───────────────────────────────────────────────────────────────────────────────┐  ││
│  │  │                            Redis 7                                             │  ││
│  │  │                                                                                │  ││
│  │  │   Data Type: Device state, session cache, pub/sub channels, rate limits       │  ││
│  │  │   Access Pattern: Sub-millisecond reads, real-time pub/sub, TTL-based expiry  │  ││
│  │  │   Why Redis: In-memory speed, pub/sub, data structures (Hash, Set, Sorted)    │  ││
│  │  │                                                                                │  ││
│  │  │   Keys: device:state:{id}, device:online:{id}, user:devices:{id}              │  ││
│  │  │   Pub/Sub: events:user:{id}, device:state:{id}, agent:response:{id}           │  ││
│  │  └───────────────────────────────────────────────────────────────────────────────┘  ││
│  └─────────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                          │
│  ┌─────────────────────────────────────────────────────────────────────────────────────┐│
│  │                              EVENT STREAMING                                         ││
│  │  ┌───────────────────────────────────────────────────────────────────────────────┐  ││
│  │  │                         Apache Kafka 3.9                                       │  ││
│  │  │                                                                                │  ││
│  │  │   Data Type: Device events, commands, alerts (immutable event log)            │  ││
│  │  │   Access Pattern: Publish/subscribe, replay, exactly-once semantics           │  ││
│  │  │   Why Kafka: Durability, scalability, decoupling, event sourcing              │  ││
│  │  │                                                                                │  ││
│  │  │   Topics: device.events, device.commands, device.heartbeats, device.alerts    │  ││
│  │  │   Mode: KRaft (no ZooKeeper dependency)                                        │  ││
│  │  └───────────────────────────────────────────────────────────────────────────────┘  ││
│  └─────────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

#### Database Selection Matrix

| Workload | Database | Rationale |
|----------|----------|-----------|
| User authentication & profiles | PostgreSQL | ACID compliance, complex queries, referential integrity |
| Device configurations | MongoDB | Flexible schema for varying device types, nested configs |
| Sensor time-series data | TimescaleDB | Hypertables, automatic partitioning, time-based aggregations |
| High-volume IoT events | ScyllaDB | Write-optimized, linear scalability, low-latency reads |
| Real-time device state | Redis | Sub-millisecond access, pub/sub for live updates |
| Event streaming | Kafka | Durable event log, replay capability, decoupled producers/consumers |

---

### Event-Driven Data Flow

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                              EVENT-DRIVEN DATA FLOW                                      │
├─────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                          │
│   ┌──────────────┐                                                                       │
│   │   IoT        │                                                                       │
│   │  Devices     │                                                                       │
│   │  (Sensors,   │                                                                       │
│   │   Locks,     │                                                                       │
│   │   Cameras)   │                                                                       │
│   └──────┬───────┘                                                                       │
│          │                                                                               │
│          │ HTTP/MQTT Events                                                              │
│          ▼                                                                               │
│   ┌──────────────┐         ┌──────────────────────────────────────────────────────┐     │
│   │   Device     │         │                    KAFKA                              │     │
│   │   Ingest     │────────▶│                                                      │     │
│   │   Service    │ Produce │  ┌────────────┐ ┌────────────┐ ┌────────────────┐   │     │
│   │    (Go)      │         │  │  device.   │ │  device.   │ │    device.     │   │     │
│   └──────────────┘         │  │  events    │ │  commands  │ │   heartbeats   │   │     │
│                            │  │            │ │            │ │                │   │     │
│                            │  │ Partition 0│ │ Partition 0│ │  Partition 0   │   │     │
│                            │  │ Partition 1│ │ Partition 1│ │  Partition 1   │   │     │
│                            │  │ Partition 2│ │ Partition 2│ │  Partition 2   │   │     │
│                            │  └────────────┘ └────────────┘ └────────────────┘   │     │
│                            └───────────────────────┬──────────────────────────────┘     │
│                                                    │                                     │
│                              ┌─────────────────────┼─────────────────────┐               │
│                              │                     │                     │               │
│                              ▼                     ▼                     ▼               │
│                    ┌──────────────┐      ┌──────────────┐      ┌──────────────┐         │
│                    │    Event     │      │   Scenario   │      │   Anomaly    │         │
│                    │  Processor   │      │    Engine    │      │     ML       │         │
│                    │    (Go)      │      │    (Go)      │      │   (Python)   │         │
│                    └──────┬───────┘      └──────┬───────┘      └──────────────┘         │
│                           │                     │                                        │
│          ┌────────────────┼────────────────┐    │                                        │
│          │                │                │    │                                        │
│          ▼                ▼                ▼    ▼                                        │
│   ┌────────────┐   ┌────────────┐   ┌────────────┐                                      │
│   │  ScyllaDB  │   │TimescaleDB │   │   Redis    │                                      │
│   │            │   │            │   │            │                                      │
│   │  Raw IoT   │   │ Time-Series│   │  Current   │                                      │
│   │   Events   │   │  Analytics │   │   State    │◄──── Scenario Engine Updates        │
│   │            │   │            │   │  + Pub/Sub │                                      │
│   └────────────┘   └────────────┘   └─────┬──────┘                                      │
│                                           │                                              │
│                                           │ Publish State Changes                        │
│                                           ▼                                              │
│                                    ┌──────────────┐                                      │
│                                    │ Notification │                                      │
│                                    │   Service    │                                      │
│                                    │    (Go)      │                                      │
│                                    └──────┬───────┘                                      │
│                                           │                                              │
│                                           │ WebSocket Push                               │
│                                           ▼                                              │
│                                    ┌──────────────┐                                      │
│                                    │   Frontend   │                                      │
│                                    │   (React)    │                                      │
│                                    │  Real-time   │                                      │
│                                    │   Updates    │                                      │
│                                    └──────────────┘                                      │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

### AI/ML Pipeline

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                              AGENTIC AI ARCHITECTURE                                     │
├─────────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                          │
│   ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│   │                              USER INTERACTION                                     │  │
│   │                                                                                   │  │
│   │    User: "I'm leaving for vacation tomorrow for 2 weeks"                         │  │
│   │                                                                                   │  │
│   └───────────────────────────────────────┬──────────────────────────────────────────┘  │
│                                           │                                              │
│                                           ▼                                              │
│   ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│   │                           AGENTIC AI SERVICE (Python)                             │  │
│   │  ┌────────────────────────────────────────────────────────────────────────────┐  │  │
│   │  │                         Google Gemini Integration                           │  │  │
│   │  │                                                                             │  │  │
│   │  │   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌────────────┐  │  │  │
│   │  │   │   Natural   │    │   Context   │    │    Tool     │    │  Response  │  │  │  │
│   │  │   │  Language   │───▶│  Building   │───▶│   Calling   │───▶│ Generation │  │  │  │
│   │  │   │Understanding│    │  (MCP Data) │    │ (Functions) │    │            │  │  │  │
│   │  │   └─────────────┘    └─────────────┘    └─────────────┘    └────────────┘  │  │  │
│   │  │                                                                             │  │  │
│   │  └────────────────────────────────────────────────────────────────────────────┘  │  │
│   │                                           │                                       │  │
│   │                              Tool Calls   │                                       │  │
│   │                                           ▼                                       │  │
│   │  ┌────────────────────────────────────────────────────────────────────────────┐  │  │
│   │  │                           MCP SERVER (Python)                               │  │  │
│   │  │                    Model Context Protocol - Tool Definitions                │  │  │
│   │  │                                                                             │  │  │
│   │  │   ┌───────────────┐  ┌───────────────┐  ┌───────────────┐  ┌────────────┐  │  │  │
│   │  │   │get_home_      │  │get_device_    │  │control_       │  │set_home_   │  │  │  │
│   │  │   │summary        │  │state          │  │device         │  │mode        │  │  │  │
│   │  │   └───────────────┘  └───────────────┘  └───────────────┘  └────────────┘  │  │  │
│   │  │   ┌───────────────┐  ┌───────────────┐  ┌───────────────┐  ┌────────────┐  │  │  │
│   │  │   │get_recent_    │  │get_anomalies  │  │create_        │  │get_energy_ │  │  │  │
│   │  │   │events         │  │               │  │automation     │  │insights    │  │  │  │
│   │  │   └───────────────┘  └───────────────┘  └───────────────┘  └────────────┘  │  │  │
│   │  └────────────────────────────────────────────────────────────────────────────┘  │  │
│   └──────────────────────────────────────────────────────────────────────────────────┘  │
│                                           │                                              │
│                    ┌──────────────────────┼──────────────────────┐                       │
│                    │                      │                      │                       │
│                    ▼                      ▼                      ▼                       │
│             ┌────────────┐         ┌────────────┐         ┌────────────┐                │
│             │   Redis    │         │  MongoDB   │         │  Device    │                │
│             │  (State)   │         │ (Configs)  │         │  Service   │                │
│             └────────────┘         └────────────┘         └────────────┘                │
│                                                                                          │
│   ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│   │                              AGENT RESPONSE                                       │  │
│   │                                                                                   │  │
│   │    Agent: "I'll prepare your home for vacation mode. Setting up:                 │  │
│   │            ✅ Random light schedule to simulate presence                         │  │
│   │            ✅ HVAC adjusted to 78°F for energy savings                           │  │
│   │            ✅ Enhanced security alerts enabled                                   │  │
│   │            ✅ Daily summary emails configured                                    │  │
│   │            Would you like me to also notify your trusted contacts?"              │  │
│   │                                                                                   │  │
│   └──────────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## Technology Stack

### Languages & Frameworks

| Layer | Technology | Version | Purpose |
|-------|------------|---------|---------|
| **Backend (Primary)** | Go | 1.21 | High-performance microservices |
| **Backend (AI/ML)** | Python | 3.11 | AI integration, analytics |
| **Frontend** | React | 18.2 | Single-page application |
| **Frontend Language** | TypeScript | 5.3 | Type-safe frontend development |

### Backend Technologies

| Component | Technology | Purpose |
|-----------|------------|---------|
| HTTP Router | Gorilla Mux | Request routing, middleware |
| Authentication | golang-jwt/jwt | JWT token generation/validation |
| WebSocket | Gorilla WebSocket | Real-time bidirectional communication |
| Kafka Client | IBM Sarama | Event streaming producer/consumer |
| PostgreSQL Driver | lib/pq | SQL database connectivity |
| MongoDB Driver | mongo-go-driver | Document database operations |
| Redis Client | go-redis | Caching, pub/sub |
| Metrics | prometheus/client_golang | Service metrics exposure |
| Password Hashing | golang.org/x/crypto | bcrypt password security |

### Python Technologies

| Component | Technology | Purpose |
|-----------|------------|---------|
| Web Framework | FastAPI | High-performance async API |
| Server | Uvicorn | ASGI server |
| AI Integration | Google Gemini API | LLM for agentic reasoning |
| HTTP Client | httpx | Async HTTP requests |
| Validation | Pydantic | Data validation |
| Metrics | prometheus-client | Service metrics |

### Frontend Technologies

| Component | Technology | Purpose |
|-----------|------------|---------|
| Build Tool | Vite | Fast development/build |
| UI Framework | React 18 | Component-based UI |
| Routing | React Router v6 | Client-side navigation |
| Styling | Tailwind CSS | Utility-first CSS |
| UI Components | Headless UI | Accessible components |
| Icons | Heroicons | SVG icon library |
| Charts | Recharts | Data visualization |
| HTTP Client | Axios | API communication |

### Infrastructure

| Component | Technology | Version | Purpose |
|-----------|------------|---------|---------|
| Container Orchestration | Kubernetes | 1.24+ | Container management |
| Package Manager | Helm | 3.x | Kubernetes deployments |
| Containerization | Docker | 24.x | Application packaging |
| Local Kubernetes | Rancher Desktop | Latest | Local development cluster |

---

## Microservices

### Service Overview

| Service | Language | Database | Port | Description |
|---------|----------|----------|------|-------------|
| **api-gateway** | Go | - | 8080 | Central entry point, JWT auth, rate limiting, routing |
| **user-service** | Go | PostgreSQL | 8080 | User authentication, registration, profiles |
| **device-service** | Go | MongoDB | 8080 | Device CRUD, configuration management |
| **device-ingest** | Go | Kafka | 8080 | Event ingestion, Kafka producer |
| **event-processor** | Go | ScyllaDB, TimescaleDB | 8080 | Stream processing, event persistence |
| **notification-service** | Go | Redis | 8080 | WebSocket connections, real-time alerts |
| **scenario-engine** | Go | Redis | 8080 | Automation rules, scenario execution |
| **agentic-ai** | Python | MongoDB | 8080 | AI assistant, Gemini integration |
| **frontend** | React/TS | - | 80 | Web UI, dashboard, chat interface |

### Service Communication Matrix

```
┌─────────────────┬─────────────────────────────────────────────────────────────┐
│     Service     │                    Communicates With                         │
├─────────────────┼─────────────────────────────────────────────────────────────┤
│ api-gateway     │ → user-service, device-service, device-ingest,              │
│                 │   event-processor, notification-service, scenario-engine,    │
│                 │   agentic-ai                                                 │
├─────────────────┼─────────────────────────────────────────────────────────────┤
│ user-service    │ → PostgreSQL                                                 │
├─────────────────┼─────────────────────────────────────────────────────────────┤
│ device-service  │ → MongoDB, Redis                                             │
├─────────────────┼─────────────────────────────────────────────────────────────┤
│ device-ingest   │ → Kafka (producer)                                           │
├─────────────────┼─────────────────────────────────────────────────────────────┤
│ event-processor │ → Kafka (consumer), ScyllaDB, TimescaleDB, Redis             │
├─────────────────┼─────────────────────────────────────────────────────────────┤
│ notification    │ → Redis (pub/sub), WebSocket clients                         │
├─────────────────┼─────────────────────────────────────────────────────────────┤
│ scenario-engine │ → Redis, device-service, notification-service                │
├─────────────────┼─────────────────────────────────────────────────────────────┤
│ agentic-ai      │ → Google Gemini API, MongoDB, device-service, Redis          │
└─────────────────┴─────────────────────────────────────────────────────────────┘
```

---

## Infrastructure Components

### Databases

| Database | Version | Purpose | Storage Pattern |
|----------|---------|---------|-----------------|
| PostgreSQL | 15 | User data, sessions | Relational, ACID |
| MongoDB | 7 | Device configs, AI memory | Document store |
| TimescaleDB | Latest | Sensor time-series | Hypertables |
| ScyllaDB | Latest | IoT events | Wide-column |
| Redis | 7 | Cache, pub/sub, state | In-memory KV |

### Message Broker

| Component | Version | Mode | Purpose |
|-----------|---------|------|---------|
| Apache Kafka | 3.9 | KRaft | Event streaming, decoupling |

### Monitoring Stack

| Component | Purpose |
|-----------|---------|
| Prometheus | Metrics collection |
| Grafana | Dashboards, visualization |
| N8N | Workflow automation |

---

## Getting Started

### Prerequisites

- **Docker Desktop** or **Rancher Desktop** with Kubernetes enabled
- **kubectl** CLI configured
- **Helm 3.x** installed
- **PowerShell** (Windows) or **Bash** (Linux/macOS)

### Quick Start

1. **Clone the repository**
   ```bash
   git clone https://github.com/stahir80td/sentinel-iot-hub.git
   cd sentinel-iot-hub
   ```

2. **Configure environment**
   ```bash
   cp .env.example .env
   # Edit .env with your Gemini API key and other configurations
   ```

3. **Deploy infrastructure**
   ```bash
   # Create namespace
   kubectl create namespace sandbox

   # Deploy infrastructure (databases, Kafka, monitoring)
   helm upgrade --install iot-homeguard ./deploy/helm/iot-homeguard \
     -n sandbox -f ./deploy/helm/iot-homeguard/values-local.yaml
   ```

4. **Build application images**
   ```powershell
   # Windows
   .\build-images.ps1

   # Or manually build each service
   docker build -t iot-api-gateway:latest ./services/go/api-gateway
   ```

5. **Deploy applications**
   ```bash
   helm upgrade --install iot-homeguard-apps ./deploy/helm/iot-homeguard-apps \
     -n sandbox -f ./deploy/helm/iot-homeguard-apps/values-local.yaml
   ```

6. **Run smoke tests**
   ```powershell
   # Windows
   .\tests\smoke\smoke-test-local.ps1
   ```

7. **Access the application**
   ```
   Frontend:    http://localhost:30080
   API Gateway: http://localhost:30081
   Grafana:     http://localhost:30082
   ```

---

## Deployment

### Helm Charts

The project includes two Helm charts:

| Chart | Purpose | Components |
|-------|---------|------------|
| `iot-homeguard` | Infrastructure | PostgreSQL, MongoDB, Redis, Kafka, TimescaleDB, ScyllaDB, Prometheus, Grafana, N8N |
| `iot-homeguard-apps` | Applications | All 9 microservices + frontend |

### Environment Configurations

| Environment | Values File | Use Case |
|-------------|-------------|----------|
| Local | `values-local.yaml` | Rancher Desktop / Docker Desktop |
| Development | `values-dv1.yaml` | Remote dev cluster |
| Production | `values-production.yaml` | Production deployment |

### Resource Requirements

| Component | CPU Request | Memory Request |
|-----------|-------------|----------------|
| PostgreSQL | 500m | 1Gi |
| MongoDB | 500m | 1Gi |
| Redis | 250m | 512Mi |
| Kafka | 1000m | 2Gi |
| TimescaleDB | 500m | 1Gi |
| ScyllaDB | 1000m | 2Gi |
| Go Services (x7) | 100m each | 128Mi each |
| Python Services (x1) | 250m | 512Mi |
| Frontend | 50m | 64Mi |
| **Total** | **~6 CPU** | **~12Gi** |

---

## API Reference

### Authentication

```
POST /api/auth/login    → User login, returns JWT
POST /api/auth/register → User registration
GET  /api/users/me      → Current user profile
```

### Devices

```
GET    /api/devices           → List user devices
POST   /api/devices           → Register new device
GET    /api/devices/{id}      → Get device details
PUT    /api/devices/{id}      → Update device config
DELETE /api/devices/{id}      → Remove device
POST   /api/devices/{id}/command → Send device command
```

### Events

```
GET /api/events         → Query events (paginated)
GET /api/events/stream  → WebSocket real-time events
```

### AI Assistant

```
POST /api/ai/chat         → Send message to AI
GET  /api/ai/chat/history → Get conversation history
POST /api/ai/chat/stream  → WebSocket streaming responses
```

### Demo Scenarios

```
GET  /api/demo/scenarios           → List available scenarios
POST /api/demo/scenarios/{name}/start → Start scenario
POST /api/demo/scenarios/{name}/stop  → Stop scenario
```

---

## Monitoring & Observability

### Metrics

All services expose Prometheus metrics at `/metrics`:

- `http_requests_total` - Request count by method, endpoint, status
- `http_request_duration_seconds` - Request latency histogram
- `kafka_messages_produced_total` - Kafka producer metrics
- `kafka_messages_consumed_total` - Kafka consumer metrics

### Dashboards

Pre-configured Grafana dashboards:

- **Service Overview** - Health, latency, error rates
- **Kafka Metrics** - Throughput, lag, partitions
- **Database Metrics** - Connections, queries, performance
- **AI Agent Metrics** - Gemini API usage, response times

### Health Checks

All services implement health endpoints:

```
GET /health  → {"status": "healthy", "version": "1.0.0"}
GET /ready   → Kubernetes readiness probe
GET /live    → Kubernetes liveness probe
```

---

## Testing

### Smoke Tests

```powershell
# Run local environment tests
.\tests\smoke\smoke-test-local.ps1

# Run DV1 environment tests
.\tests\smoke\smoke-test-dv1.ps1
```

### Test Coverage

| Category | Tests |
|----------|-------|
| Infrastructure | Database connectivity, Kafka topics |
| Services | Pod readiness, health endpoints |
| API | Authentication, CRUD operations |
| Integration | Service-to-service communication |

---

## Project Structure

```
iot/
├── services/
│   ├── go/
│   │   ├── api-gateway/        # Central API gateway
│   │   ├── user-service/       # User authentication
│   │   ├── device-service/     # Device management
│   │   ├── device-ingest/      # Event ingestion
│   │   ├── event-processor/    # Stream processing
│   │   ├── notification-service/ # Real-time notifications
│   │   └── scenario-engine/    # Automation rules
│   └── python/
│       └── agentic-ai/         # AI assistant
├── frontend/                   # React TypeScript UI
├── deploy/
│   ├── helm/
│   │   ├── iot-homeguard/      # Infrastructure chart
│   │   └── iot-homeguard-apps/ # Applications chart
│   ├── k8s/                    # Raw Kubernetes manifests
│   └── rancher/                # Rancher-specific configs
├── tests/
│   └── smoke/                  # Smoke test scripts
├── build-images.ps1            # Docker build script
├── deploy-apps.ps1             # Deployment script
└── README.md                   # This file
```

---

## Demo Scenarios

The platform includes pre-built scenarios to demonstrate AI capabilities:

| Scenario | Description | AI Behavior |
|----------|-------------|-------------|
| `vacation_mode` | User announces vacation | Creates comprehensive plan: random lights, HVAC adjustment, enhanced alerts |
| `break_in` | Glass break → motion → door | Full incident response, documentation, contact notification |
| `false_alarm_pet` | 2 AM motion with pet registered | Contextual suppression, learns pattern |
| `guest_arrival` | Expected guest arrives | Auto-unlock, disable alerts for window |
| `low_battery` | Battery drops to 15% | Predictive notification, maintenance scheduling |
| `party_mode` | User announces party | Adjusts sensitivity, monitors perimeter only |
| `fire_emergency` | Smoke + heat detected | Unlock doors, kill HVAC, notify emergency contacts |

---

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

<p align="center">
  <b>HomeGuard IoT Platform</b><br>
  Demonstrating Modern Cloud-Native Architecture
</p>
