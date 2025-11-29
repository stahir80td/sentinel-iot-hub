import { useState, useEffect } from 'react'
import { PlusIcon, TrashIcon, PlayIcon, PauseIcon } from '@heroicons/react/24/outline'
import { automationService } from '../services/api'

interface Automation {
  id: string
  name: string
  description: string
  enabled: boolean
  trigger: {
    type: string
    device_id?: string
    event?: string
  }
  actions: {
    type: string
    command?: string
    params?: Record<string, any>
  }[]
  created_at: string
}

export default function Automations() {
  const [automations, setAutomations] = useState<Automation[]>([])
  const [loading, setLoading] = useState(true)
  const [showAddModal, setShowAddModal] = useState(false)
  const [newAutomation, setNewAutomation] = useState({
    name: '',
    description: '',
    trigger: { type: 'device_event', event: 'motion_detected' },
    actions: [{ type: 'notification', params: { title: '', message: '' } }],
  })

  useEffect(() => {
    fetchAutomations()
  }, [])

  async function fetchAutomations() {
    try {
      const response = await automationService.list()
      setAutomations(response.data.scenarios || [])
    } catch (error) {
      console.error('Failed to fetch automations:', error)
    } finally {
      setLoading(false)
    }
  }

  async function handleAddAutomation(e: React.FormEvent) {
    e.preventDefault()
    try {
      await automationService.create(newAutomation)
      setShowAddModal(false)
      setNewAutomation({
        name: '',
        description: '',
        trigger: { type: 'device_event', event: 'motion_detected' },
        actions: [{ type: 'notification', params: { title: '', message: '' } }],
      })
      fetchAutomations()
    } catch (error) {
      console.error('Failed to create automation:', error)
    }
  }

  async function handleToggle(id: string, enabled: boolean) {
    try {
      if (enabled) {
        await automationService.disable(id)
      } else {
        await automationService.enable(id)
      }
      fetchAutomations()
    } catch (error) {
      console.error('Failed to toggle automation:', error)
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Are you sure you want to delete this automation?')) return
    try {
      await automationService.delete(id)
      fetchAutomations()
    } catch (error) {
      console.error('Failed to delete automation:', error)
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
        <h1 className="text-2xl font-bold text-gray-900">Automations</h1>
        <button
          onClick={() => setShowAddModal(true)}
          className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-primary-600 hover:bg-primary-700"
        >
          <PlusIcon className="h-5 w-5 mr-2" />
          Create Automation
        </button>
      </div>

      {automations.length === 0 ? (
        <div className="text-center py-12 bg-white rounded-lg shadow">
          <AutomationIcon className="mx-auto h-12 w-12 text-gray-400" />
          <h3 className="mt-2 text-sm font-medium text-gray-900">
            No automations
          </h3>
          <p className="mt-1 text-sm text-gray-500">
            Create your first automation to get started.
          </p>
        </div>
      ) : (
        <div className="bg-white shadow overflow-hidden sm:rounded-md">
          <ul className="divide-y divide-gray-200">
            {automations.map((automation) => (
              <li key={automation.id}>
                <div className="px-4 py-4 sm:px-6">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center">
                      <div
                        className={`flex-shrink-0 w-3 h-3 rounded-full mr-3 ${
                          automation.enabled ? 'bg-green-400' : 'bg-gray-300'
                        }`}
                      />
                      <div>
                        <p className="text-sm font-medium text-primary-600 truncate">
                          {automation.name}
                        </p>
                        <p className="text-sm text-gray-500">
                          {automation.description || 'No description'}
                        </p>
                      </div>
                    </div>
                    <div className="flex items-center space-x-2">
                      <button
                        onClick={() =>
                          handleToggle(automation.id, automation.enabled)
                        }
                        className={`inline-flex items-center px-3 py-1 rounded-full text-xs font-medium ${
                          automation.enabled
                            ? 'bg-green-100 text-green-800'
                            : 'bg-gray-100 text-gray-800'
                        }`}
                      >
                        {automation.enabled ? (
                          <>
                            <PauseIcon className="h-3 w-3 mr-1" />
                            Active
                          </>
                        ) : (
                          <>
                            <PlayIcon className="h-3 w-3 mr-1" />
                            Inactive
                          </>
                        )}
                      </button>
                      <button
                        onClick={() => handleDelete(automation.id)}
                        className="text-gray-400 hover:text-red-500"
                      >
                        <TrashIcon className="h-5 w-5" />
                      </button>
                    </div>
                  </div>
                  <div className="mt-2">
                    <div className="text-xs text-gray-500">
                      <span className="font-medium">Trigger:</span>{' '}
                      {automation.trigger.type}
                      {automation.trigger.event && ` (${automation.trigger.event})`}
                    </div>
                    <div className="text-xs text-gray-500">
                      <span className="font-medium">Actions:</span>{' '}
                      {automation.actions.map((a) => a.type).join(', ')}
                    </div>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Add Automation Modal */}
      {showAddModal && (
        <div className="fixed inset-0 z-50 overflow-y-auto">
          <div className="flex min-h-full items-end justify-center p-4 text-center sm:items-center sm:p-0">
            <div
              className="fixed inset-0 bg-gray-500 bg-opacity-75 transition-opacity"
              onClick={() => setShowAddModal(false)}
            />
            <div className="relative transform overflow-hidden rounded-lg bg-white px-4 pb-4 pt-5 text-left shadow-xl transition-all sm:my-8 sm:w-full sm:max-w-lg sm:p-6">
              <h3 className="text-lg font-medium leading-6 text-gray-900 mb-4">
                Create Automation
              </h3>
              <form onSubmit={handleAddAutomation} className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    Name
                  </label>
                  <input
                    type="text"
                    required
                    value={newAutomation.name}
                    onChange={(e) =>
                      setNewAutomation({ ...newAutomation, name: e.target.value })
                    }
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 shadow-sm focus:border-primary-500 focus:outline-none focus:ring-primary-500"
                    placeholder="e.g., Motion Alert"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    Description
                  </label>
                  <textarea
                    value={newAutomation.description}
                    onChange={(e) =>
                      setNewAutomation({
                        ...newAutomation,
                        description: e.target.value,
                      })
                    }
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 shadow-sm focus:border-primary-500 focus:outline-none focus:ring-primary-500"
                    rows={2}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    Trigger Event
                  </label>
                  <select
                    value={newAutomation.trigger.event}
                    onChange={(e) =>
                      setNewAutomation({
                        ...newAutomation,
                        trigger: { ...newAutomation.trigger, event: e.target.value },
                      })
                    }
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 shadow-sm focus:border-primary-500 focus:outline-none focus:ring-primary-500"
                  >
                    <option value="motion_detected">Motion Detected</option>
                    <option value="door_opened">Door Opened</option>
                    <option value="door_closed">Door Closed</option>
                    <option value="temperature_high">Temperature High</option>
                    <option value="temperature_low">Temperature Low</option>
                    <option value="smoke_detected">Smoke Detected</option>
                  </select>
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    Notification Title
                  </label>
                  <input
                    type="text"
                    value={newAutomation.actions[0].params?.title || ''}
                    onChange={(e) =>
                      setNewAutomation({
                        ...newAutomation,
                        actions: [
                          {
                            ...newAutomation.actions[0],
                            params: {
                              ...newAutomation.actions[0].params,
                              title: e.target.value,
                            },
                          },
                        ],
                      })
                    }
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 shadow-sm focus:border-primary-500 focus:outline-none focus:ring-primary-500"
                    placeholder="Alert title"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    Notification Message
                  </label>
                  <input
                    type="text"
                    value={newAutomation.actions[0].params?.message || ''}
                    onChange={(e) =>
                      setNewAutomation({
                        ...newAutomation,
                        actions: [
                          {
                            ...newAutomation.actions[0],
                            params: {
                              ...newAutomation.actions[0].params,
                              message: e.target.value,
                            },
                          },
                        ],
                      })
                    }
                    className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 shadow-sm focus:border-primary-500 focus:outline-none focus:ring-primary-500"
                    placeholder="Alert message"
                  />
                </div>
                <div className="mt-5 sm:mt-6 sm:grid sm:grid-flow-row-dense sm:grid-cols-2 sm:gap-3">
                  <button
                    type="submit"
                    className="inline-flex w-full justify-center rounded-md border border-transparent bg-primary-600 px-4 py-2 text-base font-medium text-white shadow-sm hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 sm:col-start-2 sm:text-sm"
                  >
                    Create
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

function AutomationIcon({ className }: { className?: string }) {
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
        d="M13 10V3L4 14h7v7l9-11h-7z"
      />
    </svg>
  )
}
