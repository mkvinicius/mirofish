const BASE = '/api/v1'

async function request(method, path, body) {
  const opts = {
    method,
    headers: { 'Content-Type': 'application/json' }
  }
  if (body !== undefined) opts.body = JSON.stringify(body)
  const res = await fetch(BASE + path, opts)
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error || res.statusText)
  }
  return res.json()
}

export const api = {
  // Projects
  createProject: (name, description) => request('POST', '/projects', { name, description }),
  listProjects: () => request('GET', '/projects'),
  deleteProject: (id) => request('DELETE', `/projects/${id}`),

  // Graph (Step 1)
  buildGraph: (id, document) => request('POST', `/projects/${id}/graph`, { document }),
  getNodes: (id, types) => request('GET', `/projects/${id}/graph/nodes${types ? '?types=' + types : ''}`),
  searchGraph: (id, q) => request('GET', `/projects/${id}/graph/search?q=${encodeURIComponent(q)}`),

  // Agents (Step 2)
  generateProfiles: (id, entity_types, sim_requirement) =>
    request('POST', `/projects/${id}/agents/generate`, { entity_types, sim_requirement }),
  listAgents: (id) => request('GET', `/projects/${id}/agents`),

  // Simulation (Step 3)
  startSimulation: (id, total_hours, platform, topic) =>
    request('POST', `/projects/${id}/simulation/start`, { total_hours, platform, topic }),
  stopSimulation: (id) => request('POST', `/projects/${id}/simulation/stop`),
  getSimulationStatus: (id) => request('GET', `/projects/${id}/simulation/status`),
  getSimulationActions: (id) => request('GET', `/projects/${id}/simulation/actions`),
  getSimHistory: (id) => request('GET', `/projects/${id}/simulation/history`),

  // Report (Step 4)
  generateReport: (id, sim_requirement) =>
    request('POST', `/projects/${id}/report/generate`, { sim_requirement }),
  getReport: (id) => request('GET', `/projects/${id}/report`),
  exportReportUrl: (id) => `${BASE}/projects/${id}/report/export`,
  reportStreamUrl: (id) => `${BASE}/projects/${id}/report/stream`,

  // Chat (Step 5)
  chat: (id, sim_requirement, message, history) =>
    request('POST', `/projects/${id}/chat`, { sim_requirement, message, history })
}
