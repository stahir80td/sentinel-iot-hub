import { useState, useEffect, useRef, useCallback } from 'react'
import { useAuth } from '../context/AuthContext'
import {
  ChevronUpIcon,
  ChevronDownIcon,
  XMarkIcon,
  SignalIcon,
  SignalSlashIcon,
} from '@heroicons/react/24/outline'

interface ActivityEvent {
  id: string
  timestamp: string
  source: string
  icon: string
  action: string
  details: string
  user_id: string
  device_id?: string
  severity: 'info' | 'warning' | 'alert'
}

// Polyglot architecture source configuration with colors and icons
const sourceConfig: Record<string, { label: string; color: string; bgColor: string; icon: string }> = {
  gateway: {
    label: 'API Gateway',
    color: 'text-slate-700',
    bgColor: 'bg-slate-100 border-slate-300',
    icon: '🌐'
  },
  ai: {
    label: 'AI Agent',
    color: 'text-purple-700',
    bgColor: 'bg-purple-100 border-purple-300',
    icon: '🤖'
  },
  device: {
    label: 'Device',
    color: 'text-blue-700',
    bgColor: 'bg-blue-100 border-blue-300',
    icon: '📱'
  },
  kafka: {
    label: 'Kafka',
    color: 'text-orange-700',
    bgColor: 'bg-orange-100 border-orange-300',
    icon: '📨'
  },
  redis: {
    label: 'Redis Cache',
    color: 'text-red-700',
    bgColor: 'bg-red-100 border-red-300',
    icon: '⚡'
  },
  mongodb: {
    label: 'MongoDB',
    color: 'text-green-700',
    bgColor: 'bg-green-100 border-green-300',
    icon: '🍃'
  },
  timescaledb: {
    label: 'TimescaleDB',
    color: 'text-cyan-700',
    bgColor: 'bg-cyan-100 border-cyan-300',
    icon: '📊'
  },
  scylladb: {
    label: 'ScyllaDB',
    color: 'text-indigo-700',
    bgColor: 'bg-indigo-100 border-indigo-300',
    icon: '🔷'
  },
  n8n: {
    label: 'N8N Workflow',
    color: 'text-pink-700',
    bgColor: 'bg-pink-100 border-pink-300',
    icon: '⚙️'
  },
  processor: {
    label: 'Event Processor',
    color: 'text-amber-700',
    bgColor: 'bg-amber-100 border-amber-300',
    icon: '🔄'
  },
  notification: {
    label: 'Notification',
    color: 'text-teal-700',
    bgColor: 'bg-teal-100 border-teal-300',
    icon: '🔔'
  },
}

export default function SystemActivityPanel() {
  const [isExpanded, setIsExpanded] = useState(true)
  const [isMinimized, setIsMinimized] = useState(false)
  const [activities, setActivities] = useState<ActivityEvent[]>([])
  const [isConnected, setIsConnected] = useState(false)
  const [connectionError, setConnectionError] = useState<string | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const activityContainerRef = useRef<HTMLDivElement>(null)
  const { token } = useAuth()

  const connectWebSocket = useCallback(() => {
    if (!token) return

    // Build WebSocket URL
    const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsHost = window.location.host
    const wsUrl = `${wsProtocol}//${wsHost}/api/activity/stream`

    console.log('[ActivityPanel] Connecting to:', wsUrl)

    try {
      const ws = new WebSocket(wsUrl, ['Bearer', token])
      wsRef.current = ws

      ws.onopen = () => {
        console.log('[ActivityPanel] WebSocket connected')
        setIsConnected(true)
        setConnectionError(null)
      }

      ws.onmessage = (event) => {
        try {
          const activity: ActivityEvent = JSON.parse(event.data)
          console.log('[ActivityPanel] Received activity:', activity)
          setActivities((prev) => {
            const newActivities = [activity, ...prev].slice(0, 100) // Keep last 100
            return newActivities
          })
        } catch (err) {
          console.error('[ActivityPanel] Failed to parse activity:', err)
        }
      }

      ws.onclose = (event) => {
        console.log('[ActivityPanel] WebSocket closed:', event.code, event.reason)
        setIsConnected(false)
        wsRef.current = null

        // Reconnect after 5 seconds
        if (reconnectTimeoutRef.current) {
          clearTimeout(reconnectTimeoutRef.current)
        }
        reconnectTimeoutRef.current = setTimeout(() => {
          console.log('[ActivityPanel] Attempting to reconnect...')
          connectWebSocket()
        }, 5000)
      }

      ws.onerror = (error) => {
        console.error('[ActivityPanel] WebSocket error:', error)
        setConnectionError('Connection failed')
      }
    } catch (err) {
      console.error('[ActivityPanel] Failed to create WebSocket:', err)
      setConnectionError('Failed to connect')
    }
  }, [token])

  useEffect(() => {
    connectWebSocket()

    return () => {
      if (wsRef.current) {
        wsRef.current.close()
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
    }
  }, [connectWebSocket])

  // Auto-scroll to show newest activities
  useEffect(() => {
    if (activityContainerRef.current && isExpanded) {
      activityContainerRef.current.scrollTop = 0
    }
  }, [activities, isExpanded])

  const formatTime = (timestamp: string) => {
    try {
      const date = new Date(timestamp)
      if (isNaN(date.getTime())) {
        return '--:--:--'
      }
      return date.toLocaleTimeString('en-US', {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      })
    } catch {
      return '--:--:--'
    }
  }

  const getSourceConfig = (source: string) => {
    return sourceConfig[source.toLowerCase()] || {
      label: source,
      color: 'text-neutral-700',
      bgColor: 'bg-neutral-100 border-neutral-300',
      icon: '📌'
    }
  }

  if (isMinimized) {
    return (
      <div className="fixed bottom-0 right-4 z-50">
        <button
          onClick={() => setIsMinimized(false)}
          className="flex items-center gap-2 bg-neutral-800 text-white px-4 py-2 rounded-t-lg shadow-lg hover:bg-neutral-700 transition-colors"
        >
          <SignalIcon className={`h-4 w-4 ${isConnected ? 'text-green-400' : 'text-red-400'}`} />
          <span className="text-sm font-medium">Data Flow Monitor</span>
          {activities.length > 0 && (
            <span className="bg-primary-500 text-white text-xs px-2 py-0.5 rounded-full">
              {activities.length}
            </span>
          )}
          <ChevronUpIcon className="h-4 w-4" />
        </button>
      </div>
    )
  }

  return (
    <div className="fixed bottom-0 left-64 right-0 z-50 bg-neutral-900 text-white shadow-2xl transition-all duration-300 border-t border-neutral-700">
      {/* Header */}
      <div
        className="flex items-center justify-between px-4 py-2.5 bg-neutral-800 cursor-pointer"
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            {isConnected ? (
              <SignalIcon className="h-4 w-4 text-green-400" />
            ) : (
              <SignalSlashIcon className="h-4 w-4 text-red-400" />
            )}
            <span className="font-semibold text-sm">Polyglot Data Flow</span>
          </div>
          <span className={`text-xs px-2 py-0.5 rounded-full ${isConnected ? 'bg-green-500/20 text-green-400' : 'bg-red-500/20 text-red-400'}`}>
            {isConnected ? 'Live' : connectionError || 'Disconnected'}
          </span>
          {activities.length > 0 && (
            <span className="bg-primary-500/20 text-primary-400 text-xs px-2 py-0.5 rounded-full">
              {activities.length} events
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={(e) => {
              e.stopPropagation()
              setActivities([])
            }}
            className="text-xs text-neutral-400 hover:text-white px-3 py-1 rounded-md hover:bg-neutral-700 transition-colors"
          >
            Clear
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation()
              setIsMinimized(true)
            }}
            className="p-1.5 hover:bg-neutral-700 rounded-md transition-colors"
          >
            <XMarkIcon className="h-4 w-4" />
          </button>
          {isExpanded ? (
            <ChevronDownIcon className="h-5 w-5" />
          ) : (
            <ChevronUpIcon className="h-5 w-5" />
          )}
        </div>
      </div>

      {/* Activity List */}
      {isExpanded && (
        <div
          ref={activityContainerRef}
          className="max-h-72 overflow-y-auto px-4 py-3 space-y-2"
        >
          {activities.length === 0 ? (
            <div className="text-center py-8 text-neutral-500">
              <div className="flex justify-center gap-3 mb-3">
                {['🌐', '📨', '🍃', '⚡', '📊'].map((icon, i) => (
                  <span key={i} className="text-2xl opacity-50">{icon}</span>
                ))}
              </div>
              <p className="text-sm font-medium">Waiting for data flow events...</p>
              <p className="text-xs mt-1 text-neutral-600">
                Events from Gateway → Kafka → MongoDB → Redis → TimescaleDB will appear here
              </p>
            </div>
          ) : (
            activities.map((activity, index) => {
              const config = getSourceConfig(activity.source)
              return (
                <div
                  key={activity.id || index}
                  className={`flex items-start gap-3 p-3 rounded-lg bg-neutral-800/70 border-l-4 hover:bg-neutral-800 transition-colors ${
                    activity.severity === 'alert'
                      ? 'border-red-500'
                      : activity.severity === 'warning'
                      ? 'border-yellow-500'
                      : 'border-primary-500'
                  }`}
                >
                  <span className="text-xl flex-shrink-0">{activity.icon || config.icon}</span>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span
                        className={`text-xs px-2 py-0.5 rounded-md border font-medium ${config.bgColor} ${config.color}`}
                      >
                        {config.label}
                      </span>
                      <span className="text-sm font-medium text-white">{activity.action}</span>
                      <span className="text-xs text-neutral-500">{formatTime(activity.timestamp)}</span>
                    </div>
                    <p className="text-xs text-neutral-400 mt-1 truncate">{activity.details}</p>
                  </div>
                </div>
              )
            })
          )}
        </div>
      )}

      {/* Legend bar */}
      {isExpanded && activities.length > 0 && (
        <div className="px-4 py-2 bg-neutral-800/50 border-t border-neutral-700 flex flex-wrap gap-2 text-xs">
          <span className="text-neutral-500 mr-2">Polyglot Stack:</span>
          {Object.entries(sourceConfig).slice(0, 6).map(([key, config]) => (
            <span key={key} className={`px-2 py-0.5 rounded border ${config.bgColor} ${config.color}`}>
              {config.icon} {config.label}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
