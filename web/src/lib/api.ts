import type {
  ApiToken,
  Comment,
  CreatedToken,
  Epic,
  Iteration,
  Member,
  Role,
  SavedFilter,
  Task,
  MoveRequest,
  MoveResult,
  NewStory,
  Project,
  Story,
  StoryDetail,
  StoryPatch,
  User,
  Velocity,
} from './types'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

/**
 * Random id for this tab. Sent on every write and echoed in the change
 * events, so the tab can ignore the echo of its own changes.
 */
export const CLIENT_ID = Math.random().toString(36).slice(2, 12)

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'X-Trackstar-Client': CLIENT_ID }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new ApiError(res.status, data?.error ?? `Request failed (${res.status})`)
  return data as T
}

export const api = {
  config: () => request<{ allow_registration: boolean; version: string; timezone: string }>('GET', '/api/config'),
  me: () => request<User>('GET', '/api/me'),
  login: (email: string, password: string) => request<User>('POST', '/api/auth/login', { email, password }),
  register: (email: string, password: string, display_name: string) =>
    request<User>('POST', '/api/auth/register', { email, password, display_name }),
  logout: () => request<void>('POST', '/api/auth/logout'),
  updateMe: (input: { display_name?: string; current_password?: string; new_password?: string }) =>
    request<User>('PATCH', '/api/me', input),
  tokens: () => request<ApiToken[]>('GET', '/api/me/tokens'),
  createToken: (name: string, expires_in_days = 0) =>
    request<CreatedToken>('POST', '/api/me/tokens', { name, expires_in_days }),
  revokeToken: (id: number) => request<void>('DELETE', `/api/me/tokens/${id}`),
  users: () => request<User[]>('GET', '/api/users'),
  updateUser: (id: number, input: { display_name?: string; is_admin?: boolean; is_active?: boolean }) =>
    request<User>('PATCH', `/api/users/${id}`, input),
  setUserPassword: (id: number, password: string) => request<void>('POST', `/api/users/${id}/password`, { password }),

  projects: () => request<Project[]>('GET', '/api/projects'),
  project: (ref: string | number) => request<Project>('GET', `/api/projects/${ref}`),
  createProject: (input: Partial<Project>) => request<Project>('POST', '/api/projects', input),
  updateProject: (id: number, input: Partial<Project>) => request<Project>('PATCH', `/api/projects/${id}`, input),
  deleteProject: (id: number) => request<void>('DELETE', `/api/projects/${id}`),
  archiveProject: (id: number) => request<Project>('POST', `/api/projects/${id}/archive`),
  unarchiveProject: (id: number) => request<Project>('DELETE', `/api/projects/${id}/archive`),

  stories: (projectId: number, opts: { q?: string; done?: boolean; deleted?: boolean } = {}) => {
    const params = new URLSearchParams()
    if (opts.q) params.set('q', opts.q)
    if (opts.done) params.set('section', 'done')
    if (opts.deleted) params.set('section', 'deleted')
    const qs = params.toString()
    return request<Story[]>('GET', `/api/projects/${projectId}/stories${qs ? `?${qs}` : ''}`)
  },
  createStory: (projectId: number, input: NewStory) =>
    request<Story>('POST', `/api/projects/${projectId}/stories`, input),
  story: (id: number) => request<StoryDetail>('GET', `/api/stories/${id}`),
  updateStory: (id: number, patch: StoryPatch) => request<Story>('PATCH', `/api/stories/${id}`, patch),
  deleteStory: (id: number) => request<Story>('DELETE', `/api/stories/${id}`),
  restoreStory: (id: number) => request<Story>('POST', `/api/stories/${id}/restore`),
  moveStory: (id: number, move: MoveRequest) => request<MoveResult>('POST', `/api/stories/${id}/move`, move),
  moveStories: (ids: number[], move: MoveRequest) =>
    request<{ stories: Story[] }>('POST', '/api/stories/move', { ids, ...move }),
  addComment: (storyId: number, body: string) =>
    request<Comment>('POST', `/api/stories/${storyId}/comments`, { body }),
  deleteComment: (id: number) => request<void>('DELETE', `/api/comments/${id}`),

  labels: (projectId: number) => request<string[]>('GET', `/api/projects/${projectId}/labels`),

  epics: (projectId: number) => request<Epic[]>('GET', `/api/projects/${projectId}/epics`),
  createEpic: (projectId: number, input: { name: string; description?: string }) =>
    request<Epic>('POST', `/api/projects/${projectId}/epics`, input),
  updateEpic: (id: number, input: { name?: string; description?: string }) => request<Epic>('PATCH', `/api/epics/${id}`, input),
  demoteEpic: (id: number) => request<void>('DELETE', `/api/epics/${id}`),

  addTask: (storyId: number, description: string) => request<Task>('POST', `/api/stories/${storyId}/tasks`, { description }),
  updateTask: (id: number, input: { description?: string; done?: boolean; position?: number }) =>
    request<Task>('PATCH', `/api/tasks/${id}`, input),
  deleteTask: (id: number) => request<void>('DELETE', `/api/tasks/${id}`),

  filters: (projectId: number) => request<SavedFilter[]>('GET', `/api/projects/${projectId}/filters`),
  saveFilter: (projectId: number, name: string, query: string) =>
    request<SavedFilter>('POST', `/api/projects/${projectId}/filters`, { name, query }),
  deleteFilter: (id: number) => request<void>('DELETE', `/api/filters/${id}`),

  members: (projectId: number) => request<Member[]>('GET', `/api/projects/${projectId}/members`),
  setMember: (projectId: number, userId: number, role: Role) =>
    request<Member[]>('PUT', `/api/projects/${projectId}/members/${userId}`, { role }),
  removeMember: (projectId: number, userId: number) => request<void>('DELETE', `/api/projects/${projectId}/members/${userId}`),
  iterations: (projectId: number) => request<Iteration[]>('GET', `/api/projects/${projectId}/iterations`),
  velocity: (projectId: number) => request<Velocity>('GET', `/api/projects/${projectId}/velocity`),
}
