import { useState } from 'react'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import SystemActivityPanel from './SystemActivityPanel'
import {
  HomeIcon,
  DevicePhoneMobileIcon,
  ChatBubbleLeftRightIcon,
  Cog6ToothIcon,
  Bars3Icon,
  XMarkIcon,
  BellIcon,
  ArrowRightOnRectangleIcon,
} from '@heroicons/react/24/outline'

const navigation = [
  { name: 'Dashboard', href: '/', icon: HomeIcon },
  { name: 'Devices', href: '/devices', icon: DevicePhoneMobileIcon },
  { name: 'AI Assistant', href: '/assistant', icon: ChatBubbleLeftRightIcon },
  { name: 'Settings', href: '/settings', icon: Cog6ToothIcon },
]

export default function Layout() {
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const location = useLocation()
  const { user, logout } = useAuth()

  return (
    <div className="min-h-screen bg-neutral-50">
      {/* Mobile sidebar */}
      <div
        className={`fixed inset-0 z-40 lg:hidden ${sidebarOpen ? '' : 'hidden'}`}
      >
        <div
          className="fixed inset-0 bg-neutral-900/50"
          onClick={() => setSidebarOpen(false)}
        />
        <div className="fixed inset-y-0 left-0 flex w-64 flex-col bg-white shadow-xl">
          <div className="flex h-16 items-center justify-between px-4 border-b border-neutral-200">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 bg-primary-600 rounded-lg flex items-center justify-center">
                <span className="text-white font-bold text-sm">HG</span>
              </div>
              <span className="text-lg font-semibold text-neutral-900">HomeGuard</span>
            </div>
            <button onClick={() => setSidebarOpen(false)} className="text-neutral-500 hover:text-neutral-700">
              <XMarkIcon className="h-6 w-6" />
            </button>
          </div>
          <nav className="flex-1 space-y-1 px-3 py-4">
            {navigation.map((item) => (
              <Link
                key={item.name}
                to={item.href}
                className={`flex items-center px-3 py-2.5 text-sm font-medium rounded-lg transition-colors ${
                  location.pathname === item.href
                    ? 'bg-primary-50 text-primary-700 border-l-4 border-primary-600'
                    : 'text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900'
                }`}
                onClick={() => setSidebarOpen(false)}
              >
                <item.icon className={`mr-3 h-5 w-5 ${location.pathname === item.href ? 'text-primary-600' : ''}`} />
                {item.name}
              </Link>
            ))}
          </nav>
        </div>
      </div>

      {/* Desktop sidebar */}
      <div className="hidden lg:fixed lg:inset-y-0 lg:flex lg:w-64 lg:flex-col">
        <div className="flex flex-1 flex-col border-r border-neutral-200 bg-white">
          <div className="flex h-16 items-center px-4 border-b border-neutral-200">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 bg-primary-600 rounded-lg flex items-center justify-center">
                <span className="text-white font-bold text-sm">HG</span>
              </div>
              <span className="text-lg font-semibold text-neutral-900">HomeGuard</span>
            </div>
          </div>
          <nav className="flex-1 space-y-1 px-3 py-4">
            {navigation.map((item) => (
              <Link
                key={item.name}
                to={item.href}
                className={`flex items-center px-3 py-2.5 text-sm font-medium rounded-lg transition-colors ${
                  location.pathname === item.href
                    ? 'bg-primary-50 text-primary-700'
                    : 'text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900'
                }`}
              >
                <item.icon className={`mr-3 h-5 w-5 ${location.pathname === item.href ? 'text-primary-600' : ''}`} />
                {item.name}
              </Link>
            ))}
          </nav>
          <div className="border-t border-neutral-200 p-4">
            <div className="flex items-center">
              <div className="w-10 h-10 bg-primary-100 rounded-full flex items-center justify-center">
                <span className="text-primary-700 font-semibold text-sm">
                  {user?.name?.charAt(0)?.toUpperCase() || 'U'}
                </span>
              </div>
              <div className="flex-1 min-w-0 ml-3">
                <p className="text-sm font-medium text-neutral-900 truncate">
                  {user?.name}
                </p>
                <p className="text-xs text-neutral-500 truncate">{user?.email}</p>
              </div>
              <button
                onClick={logout}
                className="ml-2 p-2 text-neutral-400 hover:text-neutral-600 hover:bg-neutral-100 rounded-lg transition-colors"
                title="Sign out"
              >
                <ArrowRightOnRectangleIcon className="h-5 w-5" />
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Main content - pb accounts for 50vh data flow panel */}
      <div className="lg:pl-64" style={{ paddingBottom: '52vh' }}>
        {/* Top header bar */}
        <div className="sticky top-0 z-10 flex h-16 bg-white border-b border-neutral-200 shadow-sm">
          <button
            className="px-4 text-neutral-500 hover:text-neutral-700 lg:hidden"
            onClick={() => setSidebarOpen(true)}
          >
            <Bars3Icon className="h-6 w-6" />
          </button>
          <div className="flex flex-1 justify-end px-4 items-center gap-4">
            <button className="relative p-2 text-neutral-500 hover:text-neutral-700 hover:bg-neutral-100 rounded-lg transition-colors">
              <BellIcon className="h-5 w-5" />
              <span className="absolute top-1.5 right-1.5 block h-2 w-2 rounded-full bg-primary-500 ring-2 ring-white" />
            </button>
          </div>
        </div>
        <main className="py-6">
          <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
            <Outlet />
          </div>
        </main>
      </div>

      {/* System Activity Panel */}
      <SystemActivityPanel />
    </div>
  )
}
