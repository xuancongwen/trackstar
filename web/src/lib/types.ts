export type StoryType = 'feature' | 'bug' | 'chore'

export type StoryState =
  | 'icebox'
  | 'backlog'
  | 'unstarted'
  | 'started'
  | 'finished'
  | 'delivered'
  | 'accepted'
  | 'rejected'

/** Panels on the board. `done` is read-only history. */
export type Section = 'icebox' | 'backlog' | 'current' | 'done'
export type DropSection = Exclude<Section, 'done'>

export interface User {
  id: number
  email: string
  display_name: string
  is_admin: boolean
}

export interface Project {
  id: number
  name: string
  description: string
  slug: string
  iteration_length_days: number
  iteration_start_weekday: number
  velocity_window: number
}

export interface Story {
  id: number
  project_id: number
  title: string
  description: string
  type: StoryType
  state: StoryState
  section: Section
  estimate: number | null
  position: number
  requester_id: number
  owner_id: number | null
  labels: string[]
  comment_count: number
  created_at: string
  updated_at: string
  accepted_at: string | null
}

export interface Comment {
  id: number
  story_id: number
  user_id: number
  body: string
  created_at: string
}

export interface StoryDetail extends Story {
  comments: Comment[]
}

export interface Iteration {
  number: number
  start_at: string
  end_at: string
  current: boolean
  points: number
  accepted_stories: number
}

export interface Velocity {
  velocity: number
  average: number
  window: number
  estimated: boolean
  iterations: { number: number; points: number }[]
}

export interface MoveRequest {
  section: DropSection
  prev_id: number | null
  next_id: number | null
}

export interface MoveResult {
  story: Story
  renormalized: boolean
}

export type StoryPatch = Partial<
  Pick<Story, 'title' | 'description' | 'type' | 'state' | 'estimate' | 'owner_id' | 'requester_id' | 'labels'>
>

export interface NewStory {
  title: string
  description?: string
  type: StoryType
  estimate: number | null
  section: DropSection
}
