import { writable } from 'svelte/store'

export const currentProject = writable(null)
export const currentStep = writable(1)
export const simRequirement = writable('') // scenario / sim requirement shared across steps
