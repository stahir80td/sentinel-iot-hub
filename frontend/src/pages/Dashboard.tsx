import { useState, useEffect } from 'react'
import {
  DevicePhoneMobileIcon,
  BoltIcon,
  ExclamationTriangleIcon,
  CheckCircleIcon,
} from '@heroicons/react/24/outline'
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts'
import { deviceService } from '../services/api'

interface DeviceConfig {
  power_on?: boolean
  locked?: boolean
  target_temp?: number
  brightness?: number
}

interface Device {
  id: string
  name: string
  type: string
  status: string
  location: string
  last_seen: string
  config: DeviceConfig
}

interface Stats {
  totalDevices: number
  activeDevices: number
  alerts: number
  automations: number
}

const mockChartData = [
  { name: '00:00', events: 12 },
  { name: '04:00', events: 8 },
  { name: '08:00', events: 24 },
  { name: '12:00', events: 45 },
  { name: '16:00', events: 38 },
  { name: '20:00', events: 28 },
  { name: '24:00', events: 15 },
]

// Device type icons and colors
const deviceTypeInfo: Record<string, { icon: string; color: string }> = {
  motion_sensor: { icon: '👁️', color: 'bg-blue-100' },
  door_sensor: { icon: '🚪', color: 'bg-orange-100' },
  camera: { icon: '📷', color: 'bg-red-100' },
  thermostat: { icon: '🌡️', color: 'bg-cyan-100' },
  smart_lock: { icon: '🔐', color: 'bg-green-100' },
  light: { icon: '💡', color: 'bg-yellow-100' },
  smoke_detector: { icon: '🔥', color: 'bg-gray-100' },
}

function getDeviceIcon(type: string): string {
  return deviceTypeInfo[type]?.icon || '📱'
}

function getDeviceIconBg(type: string): string {
  return deviceTypeInfo[type]?.color || 'bg-gray-100'
}

function getDeviceStateIndicator(device: Device): { icon: string; text: string; color: string } | null {
  switch (device.type) {
    case 'smart_lock':
      return device.config?.locked
        ? { icon: '🔒', text: 'Locked', color: 'text-green-600' }
        : { icon: '🔓', text: 'Unlocked', color: 'text-yellow-600' }
    case 'light':
      return device.config?.power_on
        ? { icon: '💡', text: 'On', color: 'text-yellow-500' }
        : { icon: '⚫', text: 'Off', color: 'text-gray-400' }
    case 'thermostat':
      return device.config?.target_temp
        ? { icon: '🌡️', text: `${device.config.target_temp}°F`, color: 'text-cyan-600' }
        : null
    case 'camera':
      return device.status === 'active'
        ? { icon: '🔴', text: 'Recording', color: 'text-red-500' }
        : { icon: '⚫', text: 'Offline', color: 'text-gray-400' }
    default:
      return null
  }
}

function formatLastSeen(lastSeen: string): string {
  const date = new Date(lastSeen)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)

  if (diffMins < 1) return 'Just now'
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  return date.toLocaleDateString()
}

export default function Dashboard() {
  const [devices, setDevices] = useState<Device[]>([])
  const [stats, setStats] = useState<Stats>({
    totalDevices: 0,
    activeDevices: 0,
    alerts: 0,
    automations: 0,
  })
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchData()
  }, [])

  async function fetchData() {
    try {
      const response = await deviceService.list()
      const deviceList = response.data.devices || []
      setDevices(deviceList)
      setStats({
        totalDevices: deviceList.length,
        activeDevices: deviceList.filter((d: Device) => d.status === 'online').length,
        alerts: 2,
        automations: 5,
      })
    } catch (error) {
      console.error('Failed to fetch devices:', error)
    } finally {
      setLoading(false)
    }
  }

  const statCards = [
    {
      name: 'Total Devices',
      value: stats.totalDevices,
      icon: DevicePhoneMobileIcon,
      color: 'bg-blue-500',
    },
    {
      name: 'Active Devices',
      value: stats.activeDevices,
      icon: CheckCircleIcon,
      color: 'bg-green-500',
    },
    {
      name: 'Active Alerts',
      value: stats.alerts,
      icon: ExclamationTriangleIcon,
      color: 'bg-yellow-500',
    },
    {
      name: 'Automations',
      value: stats.automations,
      icon: BoltIcon,
      color: 'bg-purple-500',
    },
  ]

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary-600"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>

      {/* Stats */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {statCards.map((stat) => (
          <div
            key={stat.name}
            className="bg-white overflow-hidden shadow rounded-lg"
          >
            <div className="p-5">
              <div className="flex items-center">
                <div className={`flex-shrink-0 ${stat.color} rounded-md p-3`}>
                  <stat.icon className="h-6 w-6 text-white" />
                </div>
                <div className="ml-5 w-0 flex-1">
                  <dl>
                    <dt className="text-sm font-medium text-gray-500 truncate">
                      {stat.name}
                    </dt>
                    <dd className="text-2xl font-semibold text-gray-900">
                      {stat.value}
                    </dd>
                  </dl>
                </div>
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* Charts and Device List */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* Event Activity Chart */}
        <div className="bg-white shadow rounded-lg p-6">
          <h2 className="text-lg font-medium text-gray-900 mb-4">
            Event Activity (24h)
          </h2>
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={mockChartData}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="name" />
                <YAxis />
                <Tooltip />
                <Line
                  type="monotone"
                  dataKey="events"
                  stroke="#3b82f6"
                  strokeWidth={2}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Recent Devices */}
        <div className="bg-white shadow rounded-lg p-6">
          <h2 className="text-lg font-medium text-gray-900 mb-4">
            Recent Devices
          </h2>
          <div className="flow-root">
            {devices.length === 0 ? (
              <p className="text-gray-500 text-center py-4">
                No devices registered yet
              </p>
            ) : (
              <ul className="divide-y divide-gray-200">
                {devices.slice(0, 5).map((device) => {
                  const stateIndicator = getDeviceStateIndicator(device)
                  return (
                    <li key={device.id} className="py-3">
                      <div className="flex items-center space-x-3">
                        {/* Device Type Icon */}
                        <div className={`flex-shrink-0 w-10 h-10 rounded-lg ${getDeviceIconBg(device.type)} flex items-center justify-center text-xl`}>
                          {getDeviceIcon(device.type)}
                        </div>

                        {/* Device Info */}
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center space-x-2">
                            <p className="text-sm font-medium text-gray-900 truncate">
                              {device.name}
                            </p>
                            <div
                              className={`w-2 h-2 rounded-full ${
                                device.status === 'active'
                                  ? 'bg-green-400'
                                  : 'bg-gray-300'
                              }`}
                              title={device.status === 'active' ? 'Active' : 'Inactive'}
                            />
                          </div>
                          <p className="text-xs text-gray-500">
                            {device.type.replace('_', ' ')} {device.location && `- ${device.location}`}
                          </p>
                          <p className="text-xs text-gray-400">
                            {formatLastSeen(device.last_seen)}
                          </p>
                        </div>

                        {/* State Indicator */}
                        {stateIndicator && (
                          <div className={`flex items-center space-x-1 ${stateIndicator.color}`}>
                            <span className="text-lg">{stateIndicator.icon}</span>
                            <span className="text-xs font-medium">{stateIndicator.text}</span>
                          </div>
                        )}
                      </div>
                    </li>
                  )
                })}
              </ul>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
