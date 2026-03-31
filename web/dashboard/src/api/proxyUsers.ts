import { apiFetch } from './client'
import type { ProxyUser, CreateProxyUserRequest, UpdateProxyUserRequest } from './types'

export const proxyUsersApi = {
  list: () => apiFetch<ProxyUser[]>('/proxy-users'),

  get: (username: string) => apiFetch<ProxyUser>(`/proxy-users/${username}`),

  create: (req: CreateProxyUserRequest) =>
    apiFetch<ProxyUser>('/proxy-users', {
      method: 'POST',
      body: JSON.stringify(req),
    }),

  update: (username: string, req: UpdateProxyUserRequest) =>
    apiFetch<ProxyUser>(`/proxy-users/${username}`, {
      method: 'PUT',
      body: JSON.stringify(req),
    }),

  delete: (username: string) =>
    apiFetch<string>(`/proxy-users/${username}`, { method: 'DELETE' }),
}
