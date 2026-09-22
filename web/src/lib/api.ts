import type {
  Comment,
  Iteration,
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
  const headers: Record<string, string> = { 'X-Tracker-Client': CLIENT_ID }
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
  users: () => request<User[]>('GET', '/api/users'),

  projects: () => request<Project[]>('GET', '/api/projects'),
  project: (ref: string | number) => request<Project>('GET', `/api/projects/${ref}`),
  createProject: (input: Partial<Project>) => request<Project>('POST', '/api/projects', input),
  updateProject: (id: number, input: Partial<Project>) => request<Project>('PATCH', `/api/projects/${id}`, input),
  deleteProject: (id: number) => request<void>('DELETE', `/api/projects/${id}`),

  stories: (projectId: number, opts: { q?: string; done?: boolean } = {}) => {
    const params = new URLSearchParams()
    if (opts.q) params.set('q', opts.q)
    if (opts.done) params.set('section', 'done')
    const qs = params.toString()
    return request<Story[]>('GET', `/api/projects/${projectId}/stories${qs ? `?${qs}` : ''}`)
  },
  createStory: (projectId: number, input: NewStory) =>
    request<Story>('POST', `/api/projects/${projectId}/stories`, input),
  story: (id: number) => request<StoryDetail>('GET', `/api/stories/${id}`),
  updateStory: (id: number, patch: StoryPatch) => request<Story>('PATCH', `/api/stories/${id}`, patch),
  deleteStory: (id: number) => request<void>('DELETE', `/api/stories/${id}`),
  moveStory: (id: number, move: MoveRequest) => request<MoveResult>('POST', `/api/stories/${id}/move`, move),
  addComment: (storyId: number, body: string) =>
    request<Comment>('POST', `/api/stories/${storyId}/comments`, { body }),
  deleteComment: (id: number) => request<void>('DELETE', `/api/comments/${id}`),

  labels: (projectId: number) => request<string[]>('GET', `/api/projects/${projectId}/labels`),
  iterations: (projectId: number) => request<Iteration[]>('GET', `/api/projects/${projectId}/iterations`),
  velocity: (projectId: number) => request<Velocity>('GET', `/api/projects/${projectId}/velocity`),
}
