import { useState, useEffect } from 'react'
import { PlusIcon, TrashIcon } from '@heroicons/react/24/outline'
import { deviceService } from '../services/api'

interface Device {
  id: string
  name: string
  type: string
  status: string
  location: string
  last_seen: string
  firmware_version: string
}

const deviceTypes = [
  'motion_sensor',
  'door_sensor',
  'camera',
  'thermostat',
  'smart_lock',
  'light',
  'smoke_detector',
]

export default function Devices() {
  const [devices, setDevices] = useState<Device[]>([])
  const [loading, setLoading] = useState(true)
  const [showAddModal, setShowAddModal] = useState(false)
  const [newDevice, setNewDevice] = useState({
    name: '',
    type: 'motion_sensor',
    location: '',
  })

  useEffect(() => {
    fetchDevices()
  }, [])

  async function fetchDevices() {
    try {
      const response = await deviceService.list()
      setDevices(response.data.devices || [])
    } catch (error) {
      console.error('Failed to fetch devices:', error)
    } finally {
      setLoading(false)
    }
  }

  async function handleAddDevice(e: React.FormEvent) {
    e.preventDefault()
    try {
      await deviceService.create(newDevice)
      setShowAddModal(false)
      setNewDevice({ name: '', type: 'motion_sensor', location: '' })
      fetchDevices()
    } catch (error) {
      console.error('Failed to add device:', error)
    }
  }

  async function handleDeleteDevice(id: string) {
    if (!confirm('Are you sure you want to delete this device?')) return
    try {
      await deviceService.delete(id)
      fetchDevices()
    } catch (error) {
      console.error('Failed to delete device:', error)
    }
  }

  async function handleSendCommand(id: string, command: string) {
    try {
      await deviceService.sendCommand(id, command)
      fetchDevices()
    } catch (error) {
      console.error('Failed to send command:', error)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary-600"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-900">Devices</h1>
        <button
          onClick={() => setShowAddModal(true)}
          className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-primary-600 hover:bg-primary-700"
        >
          <PlusIcon className="h-5 w-5 mr-2" />
          Add Device
        </button>
      </div>

      {devices.length === 0 ? (
        <div className="text-center py-12 bg-white rounded-lg shadow">
          <DeviceIcon className="mx-auto h-12 w-12 text-gray-400" />
          <h3 className="mt-2 text-sm font-medium text-gray-900">No devices</h3>
          <p className="mt-1 text-sm text-gray-500">
            Get started by adding a new device.
          </p>
        </div>
      ) : (
        <div className="bg-white shadow overflow-hidden sm:rounded-md">
          <ul className="divide-y divide-gray-200">
            {devices.map((device) => (
              <li key={device.id}>
                <div className="px-4 py-4 sm:px-6">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center">
                      <div
                        className={`flex-shrink-0 w-3 h-3 rounded-full mr-3 ${
                          device.status === 'online'
                            ? 'bg-green-400'
                            : 'bg-gray-300'
                        }`}
                      />
                      <div>
                        <p className="text-sm font-medium text-primary-600 truncate">
                          {device.name}
                        </p>
                        <p className="text-sm text-gray-500">
                          {device.type} - {device.location}
                        </p>
                      </div>
                    </div>
                    <div className="flex items-center space-x-2">
                      <span
                        className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                          device.status === 'online'
                            ? 'bg-green-100 text-green-800'
                            : 'bg-gray-100 text-gray-800'
                        }`}
                      >
                        {device.status}
                      </span>
                      {device.type === 'smart_lock' && (
                        <>
                          <button
                            onClick={() => handleSendCommand(device.id, 'lock')}
                            className="text-sm text-primary-600 hover:text-primary-700"
                          >
                            Lock
                          </button>
                          <button
                            onClick={() => handleSendCommand(device.id, 'unlock')}
                            className="text-sm text-primary-600 hover:text-primary-700"
                          >
                            Unlock
                          </button>
                        </>
                      )}
                      {device.type === 'light' && (
                        <>
                          <button
                            onClick={() => handleSendCommand(device.id, 'on')}
                            className="text-sm text-primary-600 hover:text-primary-700"
                          >
                            On
                          </button>
                          <button
                            onClick={() => handleSendCommand(device.id, 'off')}
                            className="text-sm text-primary-600 hover:text-primary-700"
                          >
                            Off
                          </button>
                        </>
                      )}
                      <button
                        onClick={() => handleDeleteDevice(device.id)}
                        className="text-gray-400 hover:text-red-500"
                      >
                        <TrashIcon className="h-5 w-5" />
                      </button>
                    </div>
                  </div>
                  <div className="mt-2 text-xs text-gray-400">
                    Last seen: {new Date(device.last_seen).toLocaleString()}
                    {device.firmware_version && ` | FW: ${device.firmware_version}`}
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Add Device Modal */}
      {showAddModal && (
        <div className="fixed inset-0 z-50 overflow-y-auto">
          <div className="flex min-h-full items-end justify-center p-4 text-center sm:items-center sm:p-0">
            <div
              className="fixed inset-0 bg-gray-500 bg-opacity-75 transition-opacity"
              onClick={() => setShowAddModal(false)}
            />
            <div className="relative transform overflow-hidden rounded-lg bg-white px-4 pb-4 pt-5 text-left shadow-xl transition-all sm:my-8 sm:w-full sm:max-w-lg sm:p-6">
              <h3 className="text-lg font-medium leading-6 text-gray-900 mb-4">
                Add New Device
              </h3>
              <form onSubmit={handleAddDevice} className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    Device Name
                  </label>
                  <input
                    type="text"
                    required
                    value={newDevice.name}
                    onChange={(e) =>
                      setNewDevice({ ...newDevice, name: e.target.value })
                    }
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 shadow-sm focus:border-primary-500 focus:outline-none focus:ring-primary-500"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    Device Type
                  </label>
                  <select
                    value={newDevice.type}
                    onChange={(e) =>
                      setNewDevice({ ...newDevice, type: e.target.value })
                    }
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 shadow-sm focus:border-primary-500 focus:outline-none focus:ring-primary-500"
                  >
                    {deviceTypes.map((type) => (
                      <option key={type} value={type}>
                        {type.replace('_', ' ')}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    Location
                  </label>
                  <input
                    type="text"
                    value={newDevice.location}
                    onChange={(e) =>
                      setNewDevice({ ...newDevice, location: e.target.value })
                    }
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 shadow-sm focus:border-primary-500 focus:outline-none focus:ring-primary-500"
                    placeholder="e.g., Living Room"
                  />
                </div>
                <div className="mt-5 sm:mt-6 sm:grid sm:grid-flow-row-dense sm:grid-cols-2 sm:gap-3">
                  <button
                    type="submit"
                    className="inline-flex w-full justify-center rounded-md border border-transparent bg-primary-600 px-4 py-2 text-base font-medium text-white shadow-sm hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 sm:col-start-2 sm:text-sm"
                  >
                    Add Device
                  </button>
                  <button
                    type="button"
                    onClick={() => setShowAddModal(false)}
                    className="mt-3 inline-flex w-full justify-center rounded-md border border-gray-300 bg-white px-4 py-2 text-base font-medium text-gray-700 shadow-sm hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 sm:col-start-1 sm:mt-0 sm:text-sm"
                  >
                    Cancel
                  </button>
                </div>
              </form>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function DeviceIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2}
        d="M12 18h.01M8 21h8a2 2 0 002-2V5a2 2 0 00-2-2H8a2 2 0 00-2 2v14a2 2 0 002 2z"
      />
    </svg>
  )
}
