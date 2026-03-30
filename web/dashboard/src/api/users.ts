import { apiFetch } from './client'
import type { User, CreateUserRequest, UpdateUserRequest } from './types'

export const usersApi = {
  list: () => apiFetch<User[]>('/users'),

  get: (username: string) => apiFetch<User>(`/users/${username}`),

  create: (req: CreateUserRequest) =>
    apiFetch<User>('/users', {
      method: 'POST',
      body: JSON.stringify(req),
    }),

  update: (username: string, req: UpdateUserRequest) =>
    apiFetch<User>(`/users/${username}`, {
      method: 'PUT',
      body: JSON.stringify(req),
    }),

  delete: (username: string) =>
    apiFetch<string>(`/users/${username}`, { method: 'DELETE' }),
}
