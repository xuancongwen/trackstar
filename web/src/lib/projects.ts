import type { Project } from './types'

/** Starred projects first, each group keeping the order it came in. */
export const starredFirst = (list: Project[]): Project[] => [...list.filter((p) => p.starred), ...list.filter((p) => !p.starred)]
