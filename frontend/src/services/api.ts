import axios from 'axios'

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '',
  headers: {
    'Content-Type': 'application/json',
  },
})

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export const deviceService = {
  list: () => api.get('/api/devices'),
  get: (id: string) => api.get(`/api/devices/${id}`),
  create: (data: any) => api.post('/api/devices', data),
  update: (id: string, data: any) => api.put(`/api/devices/${id}`, data),
  delete: (id: string) => api.delete(`/api/devices/${id}`),
  sendCommand: (id: string, command: string, params?: any) =>
    api.post(`/api/devices/${id}/command`, { command, params }),
  updateStatus: (id: string, status: string) =>
    api.patch(`/api/devices/${id}`, { status }),
}

export const automationService = {
  list: () => api.get('/api/scenarios'),
  get: (id: string) => api.get(`/api/scenarios/${id}`),
  create: (data: any) => api.post('/api/scenarios', data),
  update: (id: string, data: any) => api.put(`/api/scenarios/${id}`, data),
  delete: (id: string) => api.delete(`/api/scenarios/${id}`),
  enable: (id: string) => api.post(`/api/scenarios/${id}/enable`),
  disable: (id: string) => api.post(`/api/scenarios/${id}/disable`),
}

export const aiService = {
  chat: (message: string) => api.post('/api/agent/chat', { message }),
  getHistory: () => api.get('/api/agent/history'),
  clearHistory: () => api.delete('/api/agent/history'),
}

export const notificationService = {
  list: () => api.get('/api/notifications'),
  markRead: (id: string) => api.post(`/api/notifications/${id}/read`),
  markAllRead: () => api.post('/api/notifications/read-all'),
}
